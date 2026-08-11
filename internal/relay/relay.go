package relay

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

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

// pump 按行读 PTY 输出，过滤后写出去。
//
// 注意：TUI 的输入框/spinner 是靠 \r 原地重画的，那部分不能按行缓冲——否则光标
// 和输入回显会卡住。所以只有遇到 \n 才成行送进过滤器，裸 \r 片段直接透传。
func pump(src io.Reader, dst io.Writer, f *Filter) {
	r := bufio.NewReaderSize(src, 64*1024)
	// 输出也要缓冲。TUI 一次 write 出来的重绘帧里有几十个 \r 片段，逐片直写终端
	// 就是几十次 syscall——原生 claude 是一次写完的，拆开之后手感明显发卡。
	// 攒着，等这一批字节读干净了再一次性 flush（见下面的 Buffered()==0）。
	w := bufio.NewWriterSize(dst, 64*1024)
	defer w.Flush()
	line := make([]byte, 0, 4096) // 非 nil：Filter 用 nil 表示「这行不显示」，空行不能跟它混淆
	partial := false              // 当前这半行的前缀已经原样吐出去了，行尾到了不能再过滤
	for {
		if r.Buffered() == 0 {
			// 数据断流了。手上这半行**可能永远等不到行尾**——输入回显、光标定位、
			// 输入框重画都不带 \n。攒着不发就是「打字看不见、回车没反应」。
			// 所以先原样吐出去保证交互，代价是这行放弃过滤（partial 记下来）。
			//
			// 好在成批到达的内容（滚动历史、工具块）几乎总是整行落在同一次缓冲填充里，
			// 该折叠的照样折叠；只有涓流式的交互回显走这条不过滤的快路。
			if len(line) > 0 {
				_, _ = w.Write(line)
				line = line[:0]
				partial = true
			}
			// 再把攒的输出推给终端才去阻塞读下一批：
			// 延迟只来自等数据，不来自等缓冲。
			_ = w.Flush()
		}
		b, err := r.ReadByte()
		if err != nil {
			if len(line) > 0 {
				_, _ = w.Write(line)
			}
			return
		}
		switch b {
		case '\n':
			if partial {
				_, _ = w.Write(append(line, '\r', '\n')) // 前缀已出，补完剩下的，不过滤
				partial = false
			} else if out := f.Line(line); out != nil {
				_, _ = w.Write(append(out, '\r', '\n'))
			}
			line = line[:0]
		case '\r':
			// PTY 默认开 ONLCR：子进程写 "\n"，master 端读到 "\r\n"。实测真实会话
			// 2067 个 CRLF、0 个裸 LF —— 也就是说**每一行**都会先撞到这里。
			// 早期版本在这里直接透传，等于过滤器全程空转（HealthWarning 也不响，
			// 因为 Lines 只在 '\n' 分支自增）。所以必须先分清是行尾还是原地重画。
			//
			// 只在缓冲区已有字节时 Peek：Peek 会阻塞等下一个字节，spinner 打完
			// "...\r" 停顿时会把重画片段扣住不输出，交互回显直接卡死。
			// ONLCR 的 \r\n 是同一次 write 产生的，绝大多数落在同一次缓冲填充里。
			if r.Buffered() > 0 {
				if nb, _ := r.Peek(1); len(nb) > 0 && nb[0] == '\n' {
					continue // 行尾，交给 '\n' 分支按行过滤
				}
			}
			_, _ = w.Write(append(line, '\r'))
			line = line[:0]
			partial = false
		default:
			line = append(line, b)
		}
	}
}
