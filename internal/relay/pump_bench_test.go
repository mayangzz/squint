package relay

import (
	"strings"
	"testing"
)

// countWriter 记录写了多少次——终端每次 write 都是一次 syscall，
// 次数比字节数更能反映手感。
type countWriter struct{ n, bytes int }

func (c *countWriter) Write(p []byte) (int, error) { c.n++; c.bytes += len(p); return len(p), nil }

// redrawFrame 模拟 TUI 的一帧：一堆用裸 \r 原地重画的片段 + 几行落地内容。
// 原生 claude 是一次 write 吐出来的，pump 如果逐片直写就会放大成几十次 syscall。
func redrawFrame() string {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("✻ Thinking… (")
		b.WriteString(strings.Repeat("·", i%8))
		b.WriteString(")\r")
	}
	b.WriteString("⏺ Bash(go test ./...)\r\n")
	b.WriteString("  ⎿ ok  pkg  0.1s\r\n")
	b.WriteString("    … +12 lines (ctrl+o to expand)\r\n")
	return b.String()
}

func TestPumpWriteBatching(t *testing.T) {
	in := strings.Repeat(redrawFrame(), 20)
	var w countWriter
	pump(strings.NewReader(in), &w, newFilter(mustRules(t), 100))

	// 输入里有 20×43 ≈ 860 个片段。缓冲生效的话写次数应该低一个量级。
	t.Logf("输入 %d 字节 / 约 860 个片段 → 实际 write %d 次，输出 %d 字节", len(in), w.n, w.bytes)
	if w.n > 100 {
		t.Errorf("write 次数 %d 太多，输出缓冲没生效（逐片直写会让终端明显发卡）", w.n)
	}
}

func BenchmarkPump(b *testing.B) {
	in := strings.Repeat(redrawFrame(), 50)
	r, _ := LoadRules()
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pump(strings.NewReader(in), &countWriter{}, newFilter(r, 100))
	}
}
