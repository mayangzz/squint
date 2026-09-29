//go:build !windows

package relay

import (
	"os"
	"os/signal"
	"syscall"
)

// onDeath 在收到 SIGTERM/SIGHUP 时先还原终端再按原语义退出，
// 避免用户的 shell 被留在 raw 模式。
func onDeath(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s, ok := <-ch
		if !ok {
			return
		}
		restore()
		signal.Stop(ch)
		if sig, ok := s.(syscall.Signal); ok {
			signal.Reset(sig)
			_ = syscall.Kill(syscall.Getpid(), sig)
		}
		os.Exit(1)
	}()
	return func() { signal.Stop(ch); close(ch) }
}
