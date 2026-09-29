package relay

import (
	"fmt"
	"strings"
	"testing"
)

// 打字时推迟的那帧不能吃掉滚出屏幕的历史，也不能吞掉要透传的输入模式序列。
func TestDeferredFrameKeepsHistoryAndForward(t *testing.T) {
	sc := newScreen(mustRules(t), 40, 5)
	sc.Write([]byte("first\r\n"))
	_ = sc.Frame()

	for i := 0; i < 8; i++ {
		sc.Write([]byte(fmt.Sprintf("line-%d\r\n", i)))
	}
	sc.Write([]byte("\x1b[?2004h"))
	out := sc.FrameDeferRepaint()
	if !sc.Pending() {
		t.Fatal("new scrollback while typing should defer the repaint")
	}
	if string(out) != "\x1b[?2004h" {
		t.Errorf("deferred frame should only carry the passthrough, got %q", out)
	}

	full := string(sc.Frame())
	for i := 0; i < 8; i++ {
		if want := fmt.Sprintf("line-%d", i); !strings.Contains(full, want) {
			t.Errorf("%s lost after the deferred frame", want)
		}
	}
}
