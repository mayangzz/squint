package relay

import (
	"io"
	"os"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// BenchmarkFrame 一帧重绘的开销。终端里每 16ms 一帧，这里要远低于那个量级，
// 否则输出刷屏时会把 CPU 吃满、手感变糊。
func BenchmarkFrame(b *testing.B) {
	data, err := os.ReadFile(realCapture)
	if err != nil {
		b.Skip(err)
	}
	r, _ := LoadRules()
	sc := newScreen(r, 120, 40)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	sc.Write(data[:min(60000, len(data))]) // 喂到有真实内容的状态
	user := vt.NewEmulator(120, 40)
	go func() { _, _ = io.Copy(io.Discard, user) }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = user.Write(sc.Frame())
	}
}
