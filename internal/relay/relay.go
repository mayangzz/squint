package relay

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// Run 在 PTY 里跑 agent，把它的输出按行过滤后写到真终端，键盘输入原样透传。
// agent 完全不知道中间隔了一层：权限弹窗、斜杠命令、Esc 打断全都照常。
//
// 注意：Run 会把 stdin 切成 raw 模式，并起一个读 stdin 的 goroutine——那个 goroutine
// 阻塞在 tty 上没法打断，只随进程退出消失。所以 Run 必须是进程做的最后一件事，
// 不要在库里反复调用。
func Run(name string, args []string) (int, error) {
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

	f := &Filter{Rules: rules}
	if path := os.Getenv("SQUINT_DEBUG"); path != "" {
		f.Debug = OpenDebug(path)
		defer f.Debug.Close(f)
	}
	stopResize := watchResize(ptmx, f)
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

	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// SQUINT_CAPTURE=/path 时把 PTY 原始字节另存一份。
	// 「感觉卡」「好像吞了东西」这类问题光靠描述查不动——有了原始字节就能离线复现，
	// 直接喂给 pump 的测试跑，不用反复占用真实会话。
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
	pump(out, os.Stdout, f)

	// 规则静默失效是这类工具最阴的失败模式：界面看着正常，只是什么都没折叠。
	// 这个 defer 注册在 restore 之后，LIFO 下先于 restore 执行，也就是仍在 raw 模式里
	// 打印——所以下面手写了 \r\n。改动这段时注意顺序耦合。
	if w := f.HealthWarning(); w != "" {
		defer func() { _, _ = os.Stderr.WriteString("\r\n" + w + "\r\n") }()
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

// settleDelay 是「这半行还会不会补齐」的等待窗口。
//
// 拿捏两头：等太短，一次 read 切在行中间就把这行判成半行、放弃过滤（旧版本就是
// 这么把过滤器架空的——PTY 一次读多少字节由内核缓冲决定，跟行边界毫无关系）；
// 等太长，输入回显和光标定位会滞后，手感发飘。几毫秒人眼无感，而真正的「断流」
// （agent 在思考、在等你打字）远不止几毫秒，两者分得干净。
var settleDelay = 4 * time.Millisecond

// pump 按行读 PTY 输出，过滤后写出去。
//
// 难点在于「一行读完了」和「这一批字节读完了」是两回事。TUI 的输入框和 spinner
// 靠裸 \r 原地重画，永远等不到 \n，攒着不发就是打字看不见、回车没反应；而工具块
// 是正常的整行，必须成行才能过滤。read 的切分点跟这两者都无关。
//
// 解法：只在**真的没有后续字节**时才放弃过滤。读放在单独 goroutine 里，主循环
// 拿到一批就处理，剩下半行时起一个 settleDelay 的表——表响之前又来字节就接着攒，
// 表响了才原样吐出去。
func pump(src io.Reader, dst io.Writer, f *Filter) {
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

	// 输出也要缓冲。TUI 一次 write 出来的重绘帧里有几十个 \r 片段，逐片直写终端
	// 就是几十次 syscall——原生 claude 是一次写完的，拆开之后手感明显发卡。
	w := bufio.NewWriterSize(dst, 64*1024)
	defer w.Flush()

	line := make([]byte, 0, 4096) // 非 nil：Filter 用 nil 表示「这行不显示」，空行不能跟它混淆
	partial := false              // 这半行的前缀已经原样吐出去了，行尾到了不能再过滤
	crs := 0                      // 攒着的连续 \r，还不知道是行尾还是原地重画

	endLine := func() {
		crs = 0
		if partial {
			_, _ = w.Write(append(line, '\r', '\n')) // 前缀已出，补完剩下的，不过滤
			partial = false
		} else if out := f.Line(line); out != nil {
			_, _ = w.Write(append(out, '\r', '\n'))
		}
		line = line[:0]
	}
	// flushCR 把确定是「原地重画」的 \r 片段原样发出去。
	flushCR := func() {
		for ; crs > 0; crs-- {
			line = append(line, '\r')
		}
		_, _ = w.Write(line)
		line = line[:0]
		partial = false
	}

	process := func(chunk []byte) {
		for _, b := range chunk {
			switch {
			case b == '\r':
				// 关键：行尾要按**一串** \r 判，不能只看一个。
				// 子进程自己写 "\r\n"，PTY 的 ONLCR 又把那个 \n 展成 "\r\n"，
				// 落到 master 端就是 "\r\r\n"（实测 19/20 行如此）。按单个 \r 判的话，
				// 每一行都会因为「下一个字节不是 \n」被当成原地重画原样放行，
				// 过滤器全程只收到空行——界面完全正常，只是什么都没折叠。
				crs++
			case b == '\n':
				endLine()
			default:
				if crs > 0 {
					flushCR() // \r 后面接的是正文，那就真是原地重画
				}
				line = append(line, b)
			}
		}
	}

	timer := time.NewTimer(settleDelay)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var settle <-chan time.Time

	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				if crs > 0 {
					flushCR()
				} else if len(line) > 0 {
					_, _ = w.Write(line)
				}
				return
			}
			process(c)
			if len(line) > 0 || crs > 0 {
				timer.Reset(settleDelay)
				settle = timer.C
			} else {
				settle = nil
			}
			_ = w.Flush() // 先把攒的推给终端再回去等，延迟只来自等数据

		case <-settle:
			// 等窗口到了还没有后续字节：这是真的断流（输入回显、光标定位、
			// 输入框重画都不带行尾）。原样吐出去保交互，代价是这行放弃过滤。
			settle = nil
			switch {
			case crs > 0:
				flushCR()
			case len(line) > 0:
				f.notePartial(line)
				_, _ = w.Write(line)
				line = line[:0]
				partial = true
			}
			_ = w.Flush()
		}
	}
}
