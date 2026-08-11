package relay

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
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

// blockingReader 在给下一段之前先卡住，模拟真实的断流：人在打字、agent 在思考。
type blockingReader struct {
	chunks [][]byte
	i      int
	delay  time.Duration
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if b.i >= len(b.chunks) {
		return 0, io.EOF
	}
	if b.i > 0 {
		time.Sleep(b.delay)
	}
	n := copy(p, b.chunks[b.i])
	b.i++
	return n, nil
}

// chanWriter 把每次写送进 channel，用来断言「什么时候」看到的，而不只是「有没有」。
type chanWriter struct{ ch chan string }

func (c *chanWriter) Write(p []byte) (int, error) { c.ch <- string(p); return len(p), nil }

// TestPumpSplitLineStillFilters 锁住本次修的 bug：一次 read 切在行中间是常态
// （PTY 读多少字节由内核缓冲决定，跟行边界毫无关系）。旧实现把这种切分当成断流，
// 于是几乎每一行都走了「放弃过滤」的快路——界面看着完全正常，只是什么都没折叠。
func TestPumpSplitLineStillFilters(t *testing.T) {
	src := &slowReader{chunks: [][]byte{
		[]byte("⏺ Bash("), []byte("ls -la)\r\n  ⎿ tot"), []byte("al 8\r\n"),
	}}
	var out bytes.Buffer
	f := newFilter(mustRules(t), 100)
	pump(src, &out, f)
	got := ansi.ReplaceAllString(out.String(), "")

	if f.Hits == 0 {
		t.Fatalf("切在行中间也该照常折叠，输出 %q", got)
	}
	if strings.Contains(got, "total 8") {
		t.Errorf("续行没折叠掉: %q", got)
	}
	if f.Partials != 0 {
		t.Errorf("字节是连着来的，不算断流，不该产生半行，实际 %d", f.Partials)
	}
}

// TestPumpStalledPartialStaysInstant 另一头：真断流时半行必须立刻可见。
// 打字回显、光标定位、输入框重画都不带行尾，攒着不发 = 界面完全冻住
// （用户实测：打字看不见、回车没反应、但命令其实在跑）。
func TestPumpStalledPartialStaysInstant(t *testing.T) {
	src := &blockingReader{
		chunks: [][]byte{[]byte("⏺ Bash(ls)"), []byte("\r\n")},
		delay:  500 * time.Millisecond,
	}
	w := &chanWriter{ch: make(chan string, 8)}
	f := newFilter(mustRules(t), 100)
	go pump(src, w, f)

	select {
	case got := <-w.ch:
		if !strings.Contains(got, "Bash(ls)") {
			t.Fatalf("断流时半行应原样立刻透出，得到 %q", got)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("断流后迟迟没有输出——界面会冻住")
	}
	if got := <-w.ch; got != "\r\n" {
		t.Errorf("前缀已输出，行尾只该补剩下的，得到 %q", got)
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
