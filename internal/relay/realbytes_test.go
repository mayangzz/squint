package relay

import (
	"bytes"
	"strings"
	"testing"
)

// 下面这些常量是从真实 PTY 抓出来的，不是照屏幕抄的。抄屏幕已经害过这个项目三次：
// 把 ⏺(U+23FA) 看成 ●、把行尾当成 \r\n、以为词之间有空格。三次单测都是绿的，
// 因为素材和代码犯了同一个错。改这里之前先抓字节。
const (
	// 行尾是 \r\r\n：子进程写 "\r\n"，PTY 的 ONLCR 又把 \n 展成 "\r\n"。
	crlf = "\r\r\n"
	// 词之间没有空格字节，靠 \x1b[<n>G 摆列位置。
	head   = "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G\x1b[1mBash\x1b[22m(ls -la)"
	branch = "\x1b[2G\x1b[2m⎿\x1b[22m\x1b[6Gtotal 8"
	noise  = "\x1b[4G\x1b[2m… +78 lines (ctrl+o to expand)\x1b[22m"
	talk   = "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G目录里有 8 个文件"
)

// TestVisibleRestoresColumns 列定位必须还原成空格。
// Claude Code 逐词摆位，删光转义序列会得到 "Accessingworkspace:" 这种连体字，
// 任何带 \s 的规则都匹配不上——而且失败完全静默。
func TestVisibleRestoresColumns(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\x1b[2G\x1b[93m\x1b[1mAccessing\x1b[12Gworkspace:\x1b[22m\x1b[39m", " Accessing workspace:"},
		{head, " ⏺ Bash(ls -la)"},
		{branch, " ⎿   total 8"},
		{"abc\x1b[3Cdef", "abc   def"}, // CUF 相对右移
	} {
		if got := visible([]byte(tc.in)); got != tc.want {
			t.Errorf("visible(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

// TestRealBytesCollapse 端到端：拿真实形态的字节喂 pump，必须真的折叠。
func TestRealBytesCollapse(t *testing.T) {
	in := head + crlf + branch + crlf + noise + crlf + crlf + talk + crlf
	var out bytes.Buffer
	f := newFilter(mustRules(t), 100)
	pump(strings.NewReader(in), &out, f)
	got := ansi.ReplaceAllString(out.String(), "")

	if f.Hits == 0 {
		t.Fatalf("真实字节一次都没折叠——过滤器空转。输出 %q", got)
	}
	if f.Partials != 0 {
		t.Errorf("字节是连着来的，不该有半行，实际 %d", f.Partials)
	}
	for _, gone := range []string{"total 8", "+78 lines"} {
		if strings.Contains(got, gone) {
			t.Errorf("没折叠掉 %q，输出 %q", gone, got)
		}
	}
	if !strings.Contains(got, "running") {
		t.Errorf("折叠行没出现，输出 %q", got)
	}
	if !strings.Contains(got, "目录里有 8 个文件") {
		t.Errorf("正文被吃了，输出 %q", got)
	}
}

// TestRealBytesCRRunNotMistakenForRedraw \r\r\n 是行尾，不是两次原地重画。
// 判错的话每一行都被原样放行，过滤器只收到空行——界面完全正常，只是什么都没折叠。
func TestRealBytesCRRunIsLineEnd(t *testing.T) {
	var out bytes.Buffer
	f := newFilter(mustRules(t), 100)
	pump(strings.NewReader(head+crlf), &out, f)
	if f.Lines != 1 {
		t.Fatalf("\\r\\r\\n 应该正好成 1 行，实际 %d 行（0 表示又被当成重画了）", f.Lines)
	}
	if f.Hits != 1 {
		t.Errorf("这一行该被折叠，Hits=%d 输出 %q", f.Hits, out.String())
	}
}
