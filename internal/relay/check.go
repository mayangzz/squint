package relay

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// sampleTUI 是一段**真实形态**的 Claude Code 输出，不是照屏幕抄的纯文本。
// 三个地方肉眼看不出来，抄错任何一个规则都会静默失效：
//
//	标记字符  ⏺ 是 U+23FA、⎿ 是 U+23BF，屏幕上看着像 ● 和 └
//	行尾      \r\r\n——子进程写 "\r\n"，PTY 的 ONLCR 又把 \n 展成 "\r\n"
//	词间距    没有空格字节，靠 \x1b[<n>G 摆列位置
//
// 这个项目最严重的三个 bug 全出在这三点上，而单测每次都是绿的，因为素材
// 和代码犯了同一个错。改这里之前先抓一份真字节（SQUINT_CAPTURE=/tmp/x）。
const sampleTUI = "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G\x1b[1mBash\x1b[22m(sed -n 280,300p service/risk/chat.go; echo === ; sed -n 55,75p report.go)\r\r\n" +
	"\x1b[2G\x1b[2m⎿\x1b[22m\x1b[6GError: Exit code 1\r\r\n" +
	"\x1b[6Gfor _, logMap := range result {\r\r\n" +
	"\x1b[4G\x1b[2m… +13 lines (ctrl+o to expand)\x1b[22m\r\r\n" +
	"\r\r\n" +
	"\x1b[4GSearched for 1 pattern (ctrl+o to expand)\r\r\n" +
	"\x1b[4GShell cwd was reset to /Users/x/repo\r\r\n" +
	"\r\r\n" +
	"\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4Gthis is the agent talking, must survive\r\r\n"

// CheckCapture 把一份 SQUINT_CAPTURE 抓下来的原始字节重放一遍，报告折叠了多少。
// 别人反馈「没生效」时，让对方发这个文件比发截图有用得多——渲染结果看不出
// 行尾长什么样、词之间有没有空格，而规则恰恰是栽在这些地方。
func CheckCapture(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("cannot read capture: %v", err), false
	}
	rules, err := LoadRules()
	if err != nil {
		return fmt.Sprintf("failed to load rules: %v", err), false
	}
	f := newFilter(rules, 100)
	pump(bytes.NewReader(data), io.Discard, f)

	var b strings.Builder
	fmt.Fprintf(&b, "replayed %s (%d bytes) through rule set %q\n", path, len(data), rules.Name)
	fmt.Fprintf(&b, "  whole lines %d, collapsed %d, partial %d\n", f.Lines, f.Hits, f.Partials)
	switch {
	case f.Lines == 0:
		b.WriteString("  x no line ever formed - line-ending detection is wrong for this agent\n")
		return b.String(), false
	case f.Partials > f.Lines:
		b.WriteString("  x most output never reached the filter - squint bug, not your rules\n")
		return b.String(), false
	case f.Hits == 0:
		b.WriteString("  x lines formed but no rule matched - rules do not fit this agent\n")
		return b.String(), false
	}
	fmt.Fprintf(&b, "  ok collapsed %d%% of lines\n", 100*f.Hits/f.Lines)
	return b.String(), true
}

// Check 校验当前生效的规则集：能不能编译、拿真实样本还能不能折叠。
// 走的是完整的 pump 通路而不是直接调 Filter——行尾判定和列还原都在那条路上，
// 只测 Filter 的话，前两次把过滤器整个架空的 bug 依然是绿的。
func Check() (string, bool) {
	rules, err := LoadRules()
	if err != nil {
		return fmt.Sprintf("failed to load rules: %v", err), false
	}
	f := newFilter(rules, 100)
	var raw bytes.Buffer
	pump(strings.NewReader(sampleTUI), &raw, f)

	var kept []string
	for _, l := range strings.Split(strings.TrimRight(raw.String(), "\r\n"), "\r\n") {
		kept = append(kept, visible([]byte(l)))
	}
	joined := strings.Join(kept, "\n")

	var b strings.Builder
	fmt.Fprintf(&b, "rule set %q\n", rules.Name)
	fmt.Fprintf(&b, "  sample %d lines -> %d kept, %d collapses, %d partial\n",
		f.Lines, len(kept), f.Hits, f.Partials)

	ok := true
	if f.Hits == 0 {
		b.WriteString("  x no rule ever matched - rules do not fit the sample\n")
		ok = false
	}
	if f.Partials > 0 {
		fmt.Fprintf(&b, "  x %d lines never reached the filter - squint bug, not your rules\n", f.Partials)
		ok = false
	}
	if !strings.Contains(joined, "this is the agent talking") {
		b.WriteString("  x swallowed real content\n")
		ok = false
	}
	for _, gone := range []string{"+13 lines", "Searched for 1 pattern", "Shell cwd was reset", "Error: Exit code 1"} {
		if strings.Contains(joined, gone) {
			fmt.Fprintf(&b, "  x failed to collapse: %q\n", gone)
			ok = false
		}
	}
	if ok {
		b.WriteString("  ok rules are working\n")
	}
	b.WriteString("\nafter collapsing:\n")
	for _, l := range kept {
		fmt.Fprintf(&b, "  | %s\n", l)
	}
	return b.String(), ok
}
