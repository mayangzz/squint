package relay

import (
	"fmt"
	"io"
	"os"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// sampleTUI 是一段**真实形态**的 Claude Code 输出，不是照屏幕抄的纯文本。
// 三个地方肉眼看不出来，抄错任何一个都会让规则静默失效：
//
//	标记字符  ⏺ 是 U+23FA、⎿ 是 U+23BF，屏幕上看着像 ● 和 └
//	行尾      \r\r\n——子进程写 "\r\n"，PTY 的 ONLCR 又把 \n 展成 "\r\n"
//	词间距    没有空格字节，靠 \x1b[<n>G 摆列位置
//
// 这个项目最严重的几个 bug 全出在这三点上，而单测每次都是绿的，因为素材和代码
// 犯了同一个错。改这里之前先抓一份真字节（SQUINT_CAPTURE=/tmp/x.raw）。
const sampleTUI = "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G\x1b[1mBash\x1b[22m(sed -n 280,300p service/bill/calc.go; echo === ; sed -n 55,75p report.go)\r\r\n" +
	"\x1b[2G\x1b[2m⎿\x1b[22m\x1b[6GError: Exit code 1\r\r\n" +
	"\x1b[6Gfor _, record := range result {\r\r\n" +
	"\x1b[4G\x1b[2m… +13 lines (ctrl+o to expand)\x1b[22m\r\r\n" +
	"\r\r\n" +
	"\x1b[4GSearched for 1 pattern (ctrl+o to expand)\r\r\n" +
	"\x1b[4GShell cwd was reset to /Users/x/repo\r\r\n" +
	"\r\r\n" +
	"\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4Gthis is the agent talking, must survive\r\r\n"

// replayChunk 是重放时每次喂多少字节。切碎是刻意的：真实会话里输出本来就是
// 一批批到的，每批画一帧。整段一次喂进去只会走出一条现实中不存在的路径。
const replayChunk = 4096

// replay 把一段原始 PTY 字节完整跑一遍，返回**用户终端最终看得见的内容**。
//
// 关键在于它接了第二个模拟器当「用户那台终端」：squint 吐出的字节再画一遍，
// 断言的就是屏幕最终长什么样，而不是我们打算画什么。退行擦多了会吃掉历史、
// 光标算错会串行——这些只有真画一遍才看得见，这个项目已经因此栽过三次。
func replay(rules *Rules, data []byte) (visible []string, sc *screen) {
	sc = newScreen(rules, 120, 24)
	user := vt.NewEmulator(120, 24)
	// 模拟器会回应终端能力查询，没人读这条管子就会在第一个查询上死锁。
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	go func() { _, _ = io.Copy(io.Discard, user) }()

	for off := 0; off < len(data); off += replayChunk {
		end := min(off+replayChunk, len(data))
		sc.Write(data[off:end])
		_, _ = user.Write(sc.Frame())
	}
	return screenLines(user), sc
}

// screenLines 取一台模拟器上看得见的一切：滚出去的历史 + 当前屏幕。
func screenLines(em *vt.Emulator) []string {
	var out []string
	for _, l := range em.Scrollback().Lines() {
		out = append(out, lineText(l))
	}
	w := em.Width()
	for y := 0; y < em.Height(); y++ {
		line := make(uv.Line, w)
		for x := 0; x < w; x++ {
			if c := em.CellAt(x, y); c != nil {
				line[x] = *c
			}
		}
		out = append(out, lineText(line))
	}
	return out
}

// CheckCapture 重放一份 SQUINT_CAPTURE 抓下来的原始字节，报告折叠了多少。
// 别人反馈「没生效」时，让对方发这个文件比发截图有用得多——渲染结果看不出
// 行尾长什么样、词之间有没有空格，而规则恰恰栽在这些地方。
func CheckCapture(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("cannot read capture: %v", err), false
	}
	rules, err := LoadRules()
	if err != nil {
		return fmt.Sprintf("failed to load rules: %v", err), false
	}
	visible, sc := replay(rules, data)
	shown := 0
	for _, l := range visible {
		if strings.TrimSpace(l) != "" {
			shown++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "replayed %s (%d bytes) through rule set %q\n", path, len(data), rules.Name)
	fmt.Fprintf(&b, "  collapsed %d lines, you would see %d\n", sc.Collapses(), shown)
	switch {
	case shown == 0:
		// 折叠得再准，把屏幕清空也是坏的。这条专挡「越擦越多」那类 bug。
		b.WriteString("  x nothing left on screen - squint is eating content, not collapsing it\n")
		return b.String(), false
	case sc.Collapses() == 0:
		b.WriteString("  x screen rendered but no rule matched - rules do not fit this agent\n")
		return b.String(), false
	}
	b.WriteString("  ok rules are working\n")
	return b.String(), true
}

// Check 校验当前生效的规则集：能不能编译、拿真实样本还能不能折叠。
func Check() (string, bool) {
	rules, err := LoadRules()
	if err != nil {
		return fmt.Sprintf("failed to load rules: %v", err), false
	}
	kept, sc := replay(rules, []byte(sampleTUI))
	joined := strings.Join(kept, "\n")

	var b strings.Builder
	fmt.Fprintf(&b, "rule set %q\n", rules.Name)
	// 判定一律看用户屏幕。样本很短、从不滚进历史，只数历史那份计数会恒为 0——
	// 屏幕上明明已经折叠好了，自检却报「规则没命中」。
	fmt.Fprintf(&b, "  %d collapses\n", sc.Collapses())

	ok := true
	if sc.Collapses() == 0 {
		b.WriteString("  x no rule ever matched - rules do not fit the sample\n")
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
		if strings.TrimSpace(l) != "" {
			fmt.Fprintf(&b, "  | %s\n", l)
		}
	}
	return b.String(), ok
}
