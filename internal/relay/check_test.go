package relay

import (
	"strings"
	"testing"
)

// TestCheckPasses `squint --check` 自己必须是绿的。
//
// 曾经这里恒红：判定只数「滚进历史后被折叠的行」，而样本很短、从不滚出屏幕，
// 计数永远是 0——屏幕上明明已经折叠好了，工具却报「规则跟这个 agent 对不上」。
// 自检工具误报比不报更糟，会把人送去改根本没坏的规则。
func TestCheckPasses(t *testing.T) {
	report, ok := Check()
	if !ok {
		t.Fatalf("内置样本自检不该失败:\n%s", report)
	}
	if !strings.Contains(report, "running") {
		t.Errorf("报告里应该能看到折叠后的样子:\n%s", report)
	}
}

// TestResizeKeepsHistory 改窗口大小之后，下一帧不能再按旧的退行量往回擦。
//
// 终端自己已经重排过了，旧的行号完全对不上；照着退就是拿 ESC[J 去擦已经落地的历史。
func TestResizeKeepsHistory(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	user := newTestTerminal(t, 60, 10)
	for i := 0; i < 30; i++ {
		sc.Write([]byte("keep-me-" + itoa(i) + "\r\r\n"))
	}
	sc.Write([]byte("> prompt\r\r\n\x1b[1A\x1b[3G")) // 光标停在输入框，不在最后一行
	user.Write(sc.Frame())

	sc.Resize(60, 10) // 尺寸没变也要走同一条路径：清掉退行量才对
	user.Write(sc.Frame())
	user.Write(sc.Frame())

	joined := strings.Join(screenLines(user), "\n")
	for _, must := range []string{"keep-me-25", "keep-me-29"} {
		if !strings.Contains(joined, must) {
			t.Errorf("resize 后历史被擦掉了，%q 不见了\n%s", must, joined)
		}
	}
}
