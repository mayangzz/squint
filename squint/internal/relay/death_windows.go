//go:build windows

package relay

// Windows 没有 SIGTERM/SIGHUP 语义，也没有 raw 模式残留的问题（--relay 本来就跑不了）。
func onDeath(func()) (stop func()) { return func() {} }
