package relay

import (
	"strings"
	"testing"
)

// 2.1.284 起启动横幅前带一个 ST 结尾的窗口标题（ESC ] 0 ; … ESC \），标题文字不能画到屏幕上。
func TestWindowTitleNotPainted(t *testing.T) {
	sc := newScreen(mustRules(t), 100, 10)
	sc.Write([]byte("\x1b]0;\xe2\x9c\xb3 Claude Code\x1b\\\x1b[91m \xe2\x96\x90\x1b[40m\xe2\x96\x9b\xe2\x96\x88\xe2\x96\x88\x1b[12G\x1b[39m\x1b[49m\x1b[1mClaude\x1b[19GCode\x1b[24G\x1b[22mv2.1.284\r\r\n"))
	rows, _, _ := sc.liveRegion()
	got := strings.Join(rows, "\n")
	if strings.Count(got, "Claude") != 1 {
		t.Errorf("window title leaked onto the screen:\n%q", got)
	}
}

// 标题被切在两次 read 之间也不能漏到屏幕上。
func TestWindowTitleSplitAcrossReads(t *testing.T) {
	sc := newScreen(mustRules(t), 40, 4)
	sc.Write([]byte("\x1b]0;\xe2\x9c\xb3 Wor"))
	sc.Write([]byte("king\x1b\\abc\r\n"))
	rows, plain, _ := sc.liveRegion()
	if plain[0] != "abc" {
		t.Errorf("got %q", rows[0])
	}
	if !strings.Contains(string(sc.forward), "\x1b]0;\xe2\x9c\xb3 Working\x1b\\") {
		t.Errorf("title not forwarded: %q", sc.forward)
	}
}
