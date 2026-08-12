package relay

import (
	"os"
	"strings"
	"testing"
)

// diffRepaint 复刻 Claude Code 2.1.227 真实的画法（PTY 实抓，不是照屏幕抄的）：
// 先 ESC[nA 往上跳，再用 CR ESC[1B 一行行往下**逐格补画**，一次写入里塞进好几个
// 屏幕行；词与词之间没有空格字节，靠 ESC[<n>G 摆列位置；行尾是 \r\r\n。
//
// 之所以要把这份形态钉进测试：按行切字节流那套曾经三次「单测全绿、线上全废」，
// 每次都因为素材照着屏幕抄，比现实干净。现在只要还能折叠，说明模拟器这条路是通的。
func diffRepaint() string {
	var b strings.Builder
	for i := 0; i < 30; i++ { // 先垫满一屏，逼着工具块滚进历史
		b.WriteString("filler line\r\r\n")
	}
	b.WriteString("\x1b[?2026h")                             // 同步输出开始
	b.WriteString("\x1b[3A\r\x1b[2K")                        // 往上跳 3 行，先擦掉这一行
	b.WriteString("\x1b[2G\x1b[92m⏺\x1b[39m")                // 只画标记
	b.WriteString("\x1b[4G\x1b[1mBash\x1b[22m(ls -la /tmp)") // 工具名和参数是另一次写入
	b.WriteString("\r\x1b[1B\x1b[2K\x1b[2G\x1b[2m⎿\x1b[22m\x1b[6Gtotal 8")
	b.WriteString("\r\x1b[1B\x1b[2K\x1b[4G\x1b[2m… +78 lines (ctrl+o to expand)\x1b[22m")
	b.WriteString("\r\x1b[1B\x1b[2K")
	b.WriteString("\x1b[?2026l")
	b.WriteString("\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G答案在这里，必须活下来\r\r\n")
	return b.String()
}

func TestDiffRepaintCollapses(t *testing.T) {
	kept, sc := replay(mustRules(t), []byte(diffRepaint()))
	got := strings.Join(kept, "\n")

	if sc.stats.Hits == 0 && sc.liveHits == 0 {
		t.Fatalf("差量重绘的工具块一次都没折叠——模拟器这条路没走通\n%s", got)
	}
	// 命令原文留在折叠行里是有意的（🔍 running  ls -la /tmp），要没的是块里的输出预览
	for _, gone := range []string{"total 8", "+78 lines", "Bash("} {
		if strings.Contains(got, gone) {
			t.Errorf("没折叠掉 %q\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "running") {
		t.Errorf("折叠行没出现\n%s", got)
	}
	if !strings.Contains(got, "答案在这里，必须活下来") {
		t.Errorf("正文被吃了\n%s", got)
	}
}

// realCapture 是一整场真实 Claude Code 2.1.227 会话的 PTY 抓包，默认就跑。
//
// 里面的标识符和中文做过等长替换（脱敏脚本只动内容、一个结构字节都不碰），
// 所以转义序列、行尾、列定位、字符宽度跟原件完全一致——换句话说，渲染路径上
// 该踩的坑一个不少，只是不含任何私有代码。脱敏前后重放结果一致：217 行、90 处折叠。
const realCapture = "testdata/cc-2.1.227.raw"

// TestRealCapture 是这个项目真正的验收：合成素材只能证明我们能解析自己造的字节。
// 之前三次「单测全绿、线上全废」，每次都是因为素材比现实干净。
func TestRealCapture(t *testing.T) {
	path := realCapture
	if p := os.Getenv("SQUINT_TEST_CAPTURE"); p != "" {
		path = p // 想拿自己的抓包验就设这个
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	visible, sc := replay(mustRules(t), data)
	t.Logf("agent 写了 %d 行，折叠 %d，最终看得见 %d 行", sc.stats.Lines, sc.stats.Hits, len(visible))

	if sc.stats.Hits == 0 {
		t.Fatal("真实抓包一次都没折叠——规则跟这个版本的 UI 对不上了")
	}
	joined := strings.Join(visible, "\n")
	if !strings.Contains(joined, "running") {
		t.Error("折叠行没出现在用户屏幕上")
	}
	// 折叠对了不代表没吃东西。锚点要选**本来就该活下来**的东西：状态栏、agent 画的
	// 表格、正文（脱敏后中文统一成「文」）。
	// 别拿会被当噪声去掉的字样当锚点——那是在给自己立一个矛盾的验收标准。
	for _, must := range []string{"accept edits", "┌", "文"} {
		if !strings.Contains(joined, must) {
			t.Errorf("屏幕上少了 %q，像是被擦掉了", must)
		}
	}
	// 折叠得再准，把屏幕清空也是坏的。
	shown := 0
	for _, l := range visible {
		if strings.TrimSpace(l) != "" {
			shown++
		}
	}
	if shown < sc.stats.Lines/4 {
		t.Errorf("屏幕上只剩 %d 行（agent 写了 %d 行），像是在吃内容而不是折叠", shown, sc.stats.Lines)
	}
}

// TestBlankLineDoesNotEndBlock 命令输出里的空行不能被当成块结束。
//
// 真实抓包里的形状（脱敏后）：
//
//	⏺ Bash(...)
//	  ⎿  package foo
//	                    ← 代码片段里本来就有的空行
//	     import (
//	     … +88 lines (ctrl+o to expand)
//
// 拿空行当结束标志，块就在这里提前收尾，`import (` 原样漏出来——用户屏幕上每个
// 折叠行下面都孤零零挂着一句代码。
func TestBlankLineDoesNotEndBlock(t *testing.T) {
	f := newFilter(mustRules(t), 100)
	block := []string{
		"⏺ Bash(sed -n 1,120p a.go)",
		"  ⎿  package foo",
		"",
		"     import (",
		"     … +88 lines (ctrl+o to expand)",
		"",
		"⏺ 这句是 agent 说的话，必须留下",
	}
	var kept []string
	for _, l := range block {
		if d := f.Judge(l); d.Verdict != Drop {
			kept = append(kept, d.Text)
		}
	}
	got := strings.Join(kept, "\n")
	for _, gone := range []string{"import (", "package foo", "+88 lines"} {
		if strings.Contains(got, gone) {
			t.Errorf("块里的 %q 漏出来了:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "这句是 agent 说的话") {
		t.Errorf("正文被吃了:\n%s", got)
	}
}
