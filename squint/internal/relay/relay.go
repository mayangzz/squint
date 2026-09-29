package relay

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// frameInterval 是一批输出到齐后等多久才重绘。
//
// 攒一下是必要的：上游一次重绘会分成好几次 write 到达，来一批画一次就是撕裂和抖动。
// 上限在这里也顺带定住了（约 60fps），刷屏输出不会把 CPU 拉满。
// 十几毫秒人眼无感，打字回显不会因此变糊。
const frameInterval = 16 * time.Millisecond

// Run 在 PTY 里跑 agent，把它的屏幕过滤后画到真终端，键盘输入原样透传。
// agent 完全不知道中间隔了一层：权限弹窗、斜杠命令、Esc 打断全都照常。
//
// 注意：Run 会把 stdin 切成 raw 模式，并起一个读 stdin 的 goroutine——那个 goroutine
// 阻塞在 tty 上没法打断，只随进程退出消失。所以 Run 必须是进程做的最后一件事，
// 不要在库里反复调用。
func Run(name string, args []string) (int, error) {
	startedAt := time.Now()
	rules, err := LoadRules()
	if err != nil {
		return 1, err
	}
	cmd := exec.Command(name, args...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return 1, err
	}
	defer ptmx.Close()

	w, h := termSize()
	sc := newScreen(rules, w, h)

	resized := make(chan struct{}, 1)
	stopResize := watchResize(ptmx, resized)
	defer stopResize()

	// 原始模式：按键必须逐个透传，不能等回车，也别让本地回显重复打印。
	// restore 提成变量是因为信号路径也要用——被 SIGTERM 杀掉时 defer 不会跑，
	// 终端会留在 raw 模式，用户得手敲 stty sane 才能恢复。
	restore := func() {}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "squint: cannot set raw mode (%v); keys may echo twice\r\n", err)
		} else {
			restore = func() { _ = term.Restore(int(os.Stdin.Fd()), old) }
		}
	}
	defer restore()
	stopDeath := onDeath(restore)
	defer stopDeath()

	// 记下最后一次按键：打字期间整片重画会擦掉输入法的预编辑（它不经过 PTY，
	// 模拟器看不见），所以那几十毫秒里把重画推迟掉。
	var lastKey atomic.Int64
	go func() { _, _ = io.Copy(&keyWatcher{w: ptmx, at: &lastKey}, os.Stdin) }()
	// 终端能力查询（DA1、光标位置等）由模拟器代答，回复要发给子进程而不是用户终端。
	// 不排空这条管子的话，模拟器会在第一个查询上直接卡死。
	go func() { _, _ = io.Copy(ptmx, sc.em) }()

	// SQUINT_CAPTURE=/path 时把 PTY 原始字节另存一份。
	// 「感觉卡」「好像吞了东西」这类问题光靠描述查不动——有了原始字节就能离线复现，
	// `squint --check <文件>` 直接重放。
	var out io.Reader = ptmx
	if path := os.Getenv("SQUINT_CAPTURE"); path != "" {
		if cf, err := os.Create(path); err == nil {
			defer cf.Close()
			out = io.TeeReader(ptmx, cf)
			fmt.Fprintf(os.Stderr, "squint: capturing raw PTY bytes to %s\r\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "squint: cannot capture to %s: %v\r\n", path, err)
		}
	}
	drive(out, os.Stdout, sc, resized, &lastKey)

	// 规则静默失效是这类工具最阴的失败模式：界面看着正常，只是什么都没折叠。
	// 这个 defer 注册在 restore 之后，LIFO 下先于 restore 执行，也就是仍在 raw 模式里
	// 打印——所以下面手写了 \r\n。改动这段时注意顺序耦合。
	if warn := sc.HealthWarning(); warn != "" {
		defer func() { _, _ = os.Stderr.WriteString("\r\n" + warn + "\r\n") }()
	}
	// 升级过 squint 的话，退出时说一句——花屏多半就是这个窗口一直跑着旧代码。
	if note := SelfStaleNote(startedAt); note != "" {
		defer func() { _, _ = os.Stderr.WriteString("\r\n" + note + "\r\n") }()
	}
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// drive 读子进程输出喂给模拟器，攒够一批就画一帧。
//
// 读单独起 goroutine、主循环 select，是为了把「还有没有后续字节」和「该画了」分开：
// 一次 read 的切分点由内核缓冲决定，跟一帧的边界毫无关系。
// typingQuiet 距最后一次按键这么久之内算「用户还在打字」，期间不整片重画。
// 300ms 足够覆盖输入法一次选词，又不至于让 spinner 明显卡顿。
const typingQuiet = 300 * time.Millisecond

// keyWatcher 透传按键的同时记下时间，纯计时用，一个字节都不改。
type keyWatcher struct {
	w  io.Writer
	at *atomic.Int64
}

func (k *keyWatcher) Write(p []byte) (int, error) {
	k.at.Store(time.Now().UnixNano())
	return k.w.Write(p)
}

func drive(src io.Reader, dst io.Writer, sc *screen, resized <-chan struct{}, lastKey *atomic.Int64) {
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				chunks <- c
			}
			if err != nil {
				return
			}
		}
	}()

	w := bufio.NewWriterSize(dst, 256*1024)
	defer w.Flush()
	timer := time.NewTimer(frameInterval)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var due <-chan time.Time
	// 打字期间欠下的整片重画要补上。子进程可能正好没有新输出（spinner 停了），
	// 光等 chunks 是等不到的，所以自己起一个慢表来收尾。
	settle := time.NewTicker(typingQuiet / 2)
	defer settle.Stop()

	typing := func() bool {
		if lastKey == nil {
			return false
		}
		last := lastKey.Load()
		return last > 0 && time.Since(time.Unix(0, last)) < typingQuiet
	}
	paint := func() {
		var b []byte
		if typing() {
			b = sc.FrameDeferRepaint() // 打字期间欠着整片重画，等停手再补
		} else {
			b = sc.Frame()
		}
		if len(b) == 0 {
			return
		}
		_, _ = w.Write(b)
		_ = w.Flush()
	}
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				if b := sc.Frame(); len(b) > 0 { // 收尾这帧必须画，不受打字推迟影响
					_, _ = w.Write(b)
					_ = w.Flush()
				}
				_, _ = w.Write([]byte("\r\n"))
				return
			}
			sc.Write(c)
			if sc.PendingHistory() >= historyFlushAt {
				// 滚得太快，再攒就要丢行了（模拟器的暂存区会淘汰最老的），先画一帧腾空。
				due = nil
				paint()
				continue
			}
			if due == nil { // 一批的第一块就起表，攒够 frameInterval 一次画完
				timer.Reset(frameInterval)
				due = timer.C
			}
		case <-due:
			due = nil
			paint()

		case <-settle.C:
			if sc.Pending() && !typing() {
				paint()
			}

		case <-resized:
			// 终端自己已经重排过了，退回去擦上一帧只会擦错地方，直接从当前位置重画。
			cols, rows := termSize()
			sc.Resize(cols, rows)
			paint()
		}
	}
}
