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
func watchResize(ptmx *os.File, f *Filter) (stop func()) {
	syncSize(ptmx, f) // 先同步取一次初始尺寸：异步的话第一屏可能已经按 80 列画完了
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
			syncSize(ptmx, f)
		}
	}()
	return func() {
		signal.Stop(ch) // 顺序不能反：先 Stop 再 close，否则 signal 包往已关闭 channel 发送会 panic
		close(ch)
		<-done
	}
}

func syncSize(ptmx *os.File, f *Filter) {
	_ = pty.InheritSize(os.Stdin, ptmx)
	if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		f.SetWidth(w)
	}
}
