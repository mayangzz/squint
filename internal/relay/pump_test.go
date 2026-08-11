package relay

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestPumpCRLF 锁住最要命的那个 bug：PTY 默认 ONLCR，行尾是 \r\n 不是 \n。
// 早期版本在 '\r' 分支直接透传，导致过滤器对真实会话一行都没生效过
// （单测却是绿的——因为测试素材是手写的 \n）。这里两种行尾都必须折叠。
func TestPumpCRLF(t *testing.T) {
	body := "⏺ Bash(ls -la)\n  ⎿ total 8\n    … +78 lines (ctrl+o to expand)\n\n⏺ 正文必须留下\n"

	for _, tc := range []struct{ name, in string }{
		{"LF", body},
		{"CRLF", strings.ReplaceAll(body, "\n", "\r\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFilter(mustRules(t), 100)
			var out bytes.Buffer
			pump(strings.NewReader(tc.in), &out, f)
			got := ansi.ReplaceAllString(out.String(), "")

			if f.Hits == 0 {
				t.Fatalf("一次都没折叠，过滤器空转了。输出:\n%q", got)
			}
			for _, gone := range []string{"+78 lines", "total 8"} {
				if strings.Contains(got, gone) {
					t.Errorf("没折叠掉 %q，输出:\n%q", gone, got)
				}
			}
			if !strings.Contains(got, "正文必须留下") {
				t.Errorf("正文被吃了，输出:\n%q", got)
			}
		})
	}
}

// TestPumpBareCRPassthrough spinner 用裸 \r 原地重画，不能被当成行尾吃掉。
func TestPumpBareCRPassthrough(t *testing.T) {
	f := &Filter{Rules: mustRules(t)}
	var out bytes.Buffer
	pump(strings.NewReader("Thinking |\rThinking /\rThinking -\r"), &out, f)
	if n := strings.Count(out.String(), "Thinking"); n != 3 {
		t.Errorf("spinner 的 3 次重画应原样透传，实际 %d 次: %q", n, out.String())
	}
}

// TestPumpBlankLines 空行不能被 nil/空切片的哨兵歧义吃掉。
func TestPumpBlankLines(t *testing.T) {
	f := &Filter{Rules: mustRules(t)}
	var out bytes.Buffer
	pump(strings.NewReader("\r\n\r\nabc\r\n"), &out, f)
	if n := strings.Count(out.String(), "\n"); n != 3 {
		t.Errorf("开头两个空行应保留，期望 3 个换行实际 %d: %q", n, out.String())
	}
}

// slowReader 模拟涓流：每次只给一小段，中间断流——正是打字回显的样子。
type slowReader struct {
	chunks [][]byte
	i      int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.i >= len(s.chunks) {
		return 0, io.EOF
	}
	n := copy(p, s.chunks[s.i])
	s.i++
	return n, nil
}

// TestPumpPartialLineEchoes 半行必须立刻可见。
// 打字回显、光标定位、输入框重画都不带 \n，攒着不发 = 界面完全冻住
// （用户实测：打字看不见、回车没反应、但命令其实在跑）。
func TestPumpPartialLineEchoes(t *testing.T) {
	var out bytes.Buffer
	src := &slowReader{chunks: [][]byte{[]byte("h"), []byte("e"), []byte("llo")}}
	pump(src, &out, newFilter(mustRules(t), 100))
	if got := out.String(); got != "hello" {
		t.Errorf("涓流输入应原样立刻透出，得到 %q", got)
	}
}

// TestPumpPartialThenNewline 前缀已经吐过之后，行尾只补剩下的，不能重复也不能再过滤。
func TestPumpPartialThenNewline(t *testing.T) {
	var out bytes.Buffer
	src := &slowReader{chunks: [][]byte{[]byte("⏺ Bash(ls)"), []byte("\r\n")}}
	pump(src, &out, newFilter(mustRules(t), 100))
	got := ansi.ReplaceAllString(out.String(), "")
	if strings.Count(got, "Bash") != 1 {
		t.Errorf("前缀已输出，不该重复或吞掉，得到 %q", got)
	}
}

// TestPumpBatchStillFilters 成批到达（滚动历史、工具块）仍然要正常折叠。
func TestPumpBatchStillFilters(t *testing.T) {
	in := "⏺ Bash(ls -la)\r\n  ⎿ total 8\r\n    … +78 lines (ctrl+o to expand)\r\n⏺ 正文\r\n"
	var out bytes.Buffer
	f := newFilter(mustRules(t), 100)
	pump(strings.NewReader(in), &out, f)
	got := ansi.ReplaceAllString(out.String(), "")
	if f.Hits == 0 || strings.Contains(got, "+78 lines") {
		t.Errorf("整批到达时该照常折叠，Hits=%d 输出 %q", f.Hits, got)
	}
}
