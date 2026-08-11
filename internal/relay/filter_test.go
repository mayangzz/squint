package relay

import (
	"strings"
	"testing"
)

// 素材抄自真实刷屏那一段。标记字符用 PTY 实抓到的 ⏺/⎿，不是终端里看着像的 ●/└。
const sample = `⏺ Bash(sed -n 280,300p service/risk/chat.go; echo === ; sed -n 240,260p service/risk/chat.go; echo ===; sed -n 55,75p app/api/route/report/report.go)
  ⎿ Error: Exit code 1
            }
        for _, logMap := range result {
                if isSameMsgInfo(msg, logMap) {
                        msg.OriginalMsg = logMap["content"]
    … +13 lines (ctrl+o to expand)

  Searched for 1 pattern (ctrl+o to expand)

⏺ Bash(sed -n 220,300p service/risk/chat.go)
  ⎿   }
    … +78 lines (ctrl+o to expand)
  Shell cwd was reset to /Users/wepie/local/golangProject/wespy-http-go

⏺ 策略 114 = CensorStrategyUserCheck「个人资料先审后发」（昵称 / 头像 / 签名改动）
一、什么时候建 114`

func TestFilterCollapsesToolBlocks(t *testing.T) {
	f := newFilter(mustRules(t), 100)
	var out []string
	for _, line := range strings.Split(sample, "\n") {
		if d := f.Judge(line); d.Verdict != Drop {
			out = append(out, d.Text)
		}
	}
	got := strings.Join(out, "\n")

	// 该消失的
	for _, gone := range []string{
		"+13 lines", "+78 lines", "Searched for 1 pattern",
		"Shell cwd was reset", "isSameMsgInfo", "Error: Exit code 1",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("这行本该被折叠掉，却还在输出里: %q\n---\n%s", gone, got)
		}
	}
	// 该留下的
	// 默认规则把 Bash 渲染成「🔍 running …」，断言渲染后的样子而不是原始工具名
	for _, keep := range []string{"running", "策略 114", "一、什么时候建 114"} {
		if !strings.Contains(got, keep) {
			t.Errorf("这行不该被吃掉: %q\n---\n%s", keep, got)
		}
	}
	// 长命令必须被截断，不能整条铺出来
	if strings.Contains(got, "app/api/route/report/report.go") {
		t.Errorf("头行没截断:\n%s", got)
	}
	t.Logf("过滤后（%d 行 → %d 行）:\n%s", len(strings.Split(sample, "\n")), len(out), got)
}

func mustRules(t *testing.T) *Rules {
	t.Helper()
	r, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	return r
}

// TestHealthWarning 锁住「规则静默失效」的自检：改了 UI 标记之后必须能被发现。
func TestHealthWarning(t *testing.T) {
	f := newFilter(mustRules(t), 100)
	for i := 0; i < minLinesForHealthCheck+1; i++ {
		f.Judge("普通输出，什么规则都不命中")
	}
	if f.HealthWarning() == "" {
		t.Error("一次都没折叠过，应该报警")
	}
	f2 := newFilter(mustRules(t), 100)
	f2.Judge("⏺ Bash(ls)")
	for i := 0; i < minLinesForHealthCheck+1; i++ {
		f2.Judge("正文")
	}
	if w := f2.HealthWarning(); w != "" {
		t.Errorf("命中过就不该报警，却报了: %s", w)
	}
}

// TestCustomToolFormat 折叠后那一行必须可自定义——把命令原文换成一句人话。
func TestCustomToolFormat(t *testing.T) {
	r := mustRules(t)
	r.Tools = map[string]string{"bash": "🔍 正在查找 {arg}", "Read": "📖 {tool} {arg}"}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"⏺ Bash(grep -rn foo .)": "🔍 正在查找 grep -rn foo .",
		"⏺ Read(a.go)":           "📖 Read a.go",
		"⏺ Glob(*.go)":           "● Glob(*.go)", // 没配的走默认 head_format
	}
	for in, want := range cases {
		f := newFilter(r, 100)
		got := strings.TrimSuffix(strings.TrimPrefix(f.Judge(in).Text, dim), reset)
		if got != want {
			t.Errorf("%q\n got: %q\nwant: %q", in, got, want)
		}
	}
}

// TestDedupeRepeats 连着重复的调用只显示一次；中间隔了正文就重新算一轮。
func TestDedupeRepeats(t *testing.T) {
	f := newFilter(mustRules(t), 100)
	seq := []string{"⏺ Bash(ls)", "⏺ Bash(ls)", "⏺ Bash(ls)", "⏺ Bash(pwd)", "⏺ Bash(pwd)"}
	shown := 0
	for _, l := range seq {
		if f.Judge(l).Verdict != Drop {
			shown++
		}
	}
	if shown != 2 {
		t.Errorf("5 条里只有 2 种命令，应只显示 2 行，实际 %d", shown)
	}

	f2 := newFilter(mustRules(t), 100)
	for _, l := range []string{"⏺ Bash(ls)", "中间有正文", "⏺ Bash(ls)"} {
		f2.Judge(l)
	}
	if f2.Judge("⏺ Bash(pwd)").Verdict == Drop {
		t.Error("换了命令不该被压掉")
	}
}

// TestHeadDoesNotEatProse 工具名允许带一个空格（Web Search），但不能因此把正文当成工具调用。
// 放宽匹配最容易出的事故就是这个：agent 说句带括号的话，整段就被折叠没了。
func TestHeadDoesNotEatProse(t *testing.T) {
	f := newFilter(mustRules(t), 100)
	prose := []string{
		"⏺ Now I will check (the config) before moving on",
		"⏺ 策略 114 = CensorStrategyUserCheck（个人资料先审后发）",
		"⏺ done — see service/censor/payload.go:44 (line 44)",
		"⏺ 这里有个坑(注意)",
	}
	for _, p := range prose {
		if d := f.Judge(p); d.Verdict == Collapse {
			t.Errorf("正文被当成工具调用折叠了: %q -> %q", p, d.Text)
		}
	}
	// 真的工具调用还得认得出来，包括带空格的那个
	for _, tool := range []string{"⏺ Bash(ls)", "⏺ Web Search(golang pty)", "⏺ Update(a.go)"} {
		if d := f.Judge(tool); d.Verdict != Collapse {
			t.Errorf("没认出工具调用: %q", tool)
		}
	}
}
