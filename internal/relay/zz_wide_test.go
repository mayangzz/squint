package relay

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// 超长单行（SLS 全量日志那种几千字的 JSON）不能让渲染出来的行超过屏宽：
// squint 关了自动折行，多出来的字符会堆在最后一列，光标列随即算错，满屏错位。
func TestLongLineNeverExceedsWidth(t *testing.T) {
	const w = 100
	sc := newScreen(mustRules(t), w, 10)
	long := `{"__tag__:__pack_id__":"330730F05986E71E-26C5","host":"app-shanghai.api.10-0.example.internal","msg":"` +
		strings.Repeat("日志追踪 abcdefg ", 300) + `"}`
	sc.Write([]byte(long + "\r\n"))
	rows, _, _ := sc.liveRegion()
	for i, r := range rows {
		if got := ansi.StringWidth(r); got > w {
			t.Errorf("第 %d 行渲染宽度 %d > 屏宽 %d", i, got, w)
		}
	}
}

// 中文 + spinner 反复原地重画时，行数不该失控增长（行数错了退行量就错，屏幕开始串）。
func TestSpinnerRepaintStable(t *testing.T) {
	sc := newScreen(mustRules(t), 80, 12)
	sc.Write([]byte("务日志追踪：起后台任务补齐 23 天\r\n"))
	_ = sc.Frame()
	for i := 0; i < 50; i++ {
		sc.Write([]byte("\rShimmying… 852s · ↓ 1.1k tokens"))
		_ = sc.Frame()
	}
	rows, _, cur := sc.liveRegion()
	if cur >= len(rows) {
		t.Fatalf("光标行 %d 越界（共 %d 行）", cur, len(rows))
	}
	if len(rows) > 12 {
		t.Errorf("活动区涨到 %d 行，超过屏高 12", len(rows))
	}
}
