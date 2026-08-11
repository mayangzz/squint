package relay

import (
	"fmt"
	"strings"
)

// sampleTUI 是一段真实的 Claude Code 输出（PTY 实抓，标记字符是 ⏺/⎿ 不是 ●/└）。
// 规则改动后拿它当靶子，确认还能折叠——纯数据的规则集编译通过 ≠ 还能命中。
const sampleTUI = "⏺ Bash(sed -n 280,300p service/risk/chat.go; echo === ; sed -n 55,75p report.go)\n" +
	"  ⎿ Error: Exit code 1\n" +
	"        for _, logMap := range result {\n" +
	"    … +13 lines (ctrl+o to expand)\n" +
	"\n" +
	"  Searched for 1 pattern (ctrl+o to expand)\n" +
	"  Shell cwd was reset to /Users/x/repo\n" +
	"\n" +
	"⏺ this is the agent talking, must survive\n"

// Check 校验当前生效的规则集：能不能编译、拿真实样本还能不能折叠。
// 返回给人看的报告和是否通过。
func Check() (string, bool) {
	rules, err := LoadRules()
	if err != nil {
		return fmt.Sprintf("failed to load rules: %v", err), false
	}
	f := newFilter(rules, 100)
	var kept []string
	lines := strings.Split(strings.TrimRight(sampleTUI, "\n"), "\n")
	for _, l := range lines {
		if out := f.Line([]byte(l)); out != nil {
			kept = append(kept, ansi.ReplaceAllString(string(out), ""))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "rule set %q\n", rules.Name)
	fmt.Fprintf(&b, "  sample %d lines -> %d kept, %d collapses\n", len(lines), len(kept), f.Hits)

	ok := true
	if f.Hits == 0 {
		b.WriteString("  x no rule ever matched - rules do not fit the sample\n")
		ok = false
	}
	for _, must := range []string{"this is the agent talking"} {
		if !strings.Contains(strings.Join(kept, "\n"), must) {
			fmt.Fprintf(&b, "  x swallowed real content: %q\n", must)
			ok = false
		}
	}
	for _, gone := range []string{"+13 lines", "Searched for 1 pattern", "Shell cwd was reset", "Error: Exit code 1"} {
		if strings.Contains(strings.Join(kept, "\n"), gone) {
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
