//go:build windows

package relay

import "os"

// Windows 没有 SIGWINCH，也没有可用的 PTY 后端（creack/pty 在这里返回 ErrUnsupported）。
// 留个空实现让整个模块能在 Windows 上编译——这样 `go install` 不会因为一个平台专属
// 依赖而整包失败，headless 模式（squint "prompt" / --replay / 全部镜片）照常可用，
// 只有 --relay 会在运行时报明确的错。
func watchResize(*os.File, *Filter) (stop func()) { return func() {} }
