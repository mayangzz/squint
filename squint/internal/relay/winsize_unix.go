//go:build !windows

package relay

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// watchResize 让 PTY 跟随宿主终端尺寸，否则 agent 一直按 80 列排版。
// 返回 stop 函数；SIGWINCH 是 Unix 专属，Windows 版是空实现。
// watchResize 让 PTY 跟随宿主终端尺寸，并把「尺寸变了」告诉渲染循环。
//
// 只发通知、不直接改 screen：模拟器和帧状态全归渲染循环那一个 goroutine 管，
// 信号处理器伸手进去改就是数据竞争。少一个锁，也少一类只在改窗口大小时才复现的 bug。
func watchResize(ptmx *os.File, notify chan<- struct{}) (stop func()) {
	inherit(ptmx) // 先同步取一次初始尺寸：异步的话第一屏可能已经按 80 列画完了
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
			inherit(ptmx)
			select { // 不阻塞：连续拖动窗口时丢掉中间几次没关系，最后一次到了就行
			case notify <- struct{}{}:
			default:
			}
		}
	}()
	return func() {
		signal.Stop(ch) // 顺序不能反：先 Stop 再 close，否则 signal 包往已关闭 channel 发送会 panic
		close(ch)
		<-done
	}
}

func inherit(ptmx *os.File) { _ = pty.InheritSize(os.Stdin, ptmx) }

// termSize 取宿主终端尺寸，拿不到就按 80x24。
func termSize() (w, h int) {
	if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil && w > 0 && h > 0 {
		return w, h
	}
	return 80, 24
}
