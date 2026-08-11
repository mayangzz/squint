package relay

import (
	"os"
	"testing"
)

// TestMain 把 HOME 指到一个空目录再跑测试。
//
// LoadRules 会优先读 ~/.squint/rules.json，也就是说**开发者自己的本机覆盖会渗进测试**：
// 本机把折叠文案改成中文，断言 "running" 的用例就红了，而 CI 上又是绿的。
// 测试要验的是内置规则，不是谁的口味。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "squint-test-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir) // Windows
	os.Exit(m.Run())
}
