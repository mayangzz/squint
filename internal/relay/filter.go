// Package relay 在你和 agent 的真实 TUI 之间插一层，只改显示、不改行为。
//
// 做法是跑一个真终端模拟器：子进程的输出先喂进去还原成屏幕，再按规则重新渲染。
// 曾经试过更省事的「按行过滤字节流」，那条路是死的——Claude Code 2.1.227 用差量
// 重绘拼屏幕（ESC[nA 上跳、CR ESC[1B 逐格补画，一次写入里塞好几个屏幕行），
// 字节流里的「行」跟屏幕上的行没有对应关系。细节见 screen.go 的注释。
package relay

import (
	"strings"
)

const (
	dim   = "\x1b[2m"
	reset = "\x1b[0m"
)

// Verdict 一行屏幕文本的去向。
type Verdict int

const (
	Keep     Verdict = 0 // 原样显示
	Drop     Verdict = 1 // 不显示
	Collapse Verdict = 2 // 换成折叠后的那一行
)

// Decision 是对一行的判定。带上工具名和参数，是为了让调用方能把**连续调用同一个
// 工具**的若干行并成一行——那件事只有看得见上下文的调用方做得了，Filter 是逐行的。
type Decision struct {
	Verdict Verdict
	Text    string // Collapse 时是折叠后的整行；Keep 时是原文
	Tool    string // Collapse 时的工具名
	Arg     string // Collapse 时的参数（已截断）
}

// Filter 按规则判定每一行怎么显示，规则来自 Rules，不写死在这里。
type Filter struct {
	Rules *Rules
	width int

	inBlock  bool
	lastHead string // 上一条折叠行，用于压掉连续重复
	Lines    int    // 判过多少行
	Hits     int    // 折叠掉多少行——为 0 说明规则跟当前 UI 对不上（见 HealthWarning）
}

func newFilter(r *Rules, width int) *Filter { return &Filter{Rules: r, width: width} }

// Judge 判定一行屏幕文本该怎么显示。
//
// 入参是**已经由模拟器还原好的纯文本**，不含转义序列——这是整个设计的前提。
// 直接拿字节流按行切是行不通的，见包注释。
func (f *Filter) Judge(text string) Decision {
	f.Lines++
	r := f.Rules
	text = strings.TrimRight(text, " \t")

	switch {
	case r.head.MatchString(text):
		f.inBlock = true
		f.Hits++
		tool, arg := f.parseHead(text)
		line := dim + r.formatHead(tool, arg) + reset
		// 连着重复的同一行压掉：agent 反复读同一个文件、反复跑同一条命令是常态，
		// 第二次开始就没有新信息了，只是占屏幕。
		if *r.DedupeRepeats && line == f.lastHead {
			return Decision{Verdict: Drop}
		}
		f.lastHead = line
		return Decision{Verdict: Collapse, Text: line, Tool: tool, Arg: arg}

	case r.isNoise(text):
		f.Hits++
		return Decision{Verdict: Drop}

	case f.inBlock:
		if strings.TrimSpace(text) == "" {
			f.inBlock = false // 空行 = 块结束
			return Decision{Verdict: Keep, Text: text}
		}
		if r.branch.MatchString(text) || strings.HasPrefix(text, "    ") {
			f.Hits++
			return Decision{Verdict: Drop}
		}
		f.inBlock = false // 顶格普通文本说明块已结束（agent 开始说话）
		return Decision{Verdict: Keep, Text: text}

	default:
		if strings.TrimSpace(text) != "" {
			f.lastHead = "" // 中间出现过正文，之后同样的调用是新一轮，不该压掉
		}
		return Decision{Verdict: Keep, Text: text}
	}
}

// parseHead 从 `⏺ Bash(一长串命令)` 里取出工具名和参数，参数超宽截断。
// 保留工具名让人知道在干嘛，参数只留开头。
func (f *Filter) parseHead(text string) (tool, arg string) {
	m := f.Rules.head.FindStringSubmatch(text)
	tool, arg = m[1], strings.TrimSuffix(m[2], ")")
	limit := f.Rules.HeadMaxWidth
	if w := f.width; w > 20 && w-len(tool)-8 < limit {
		limit = w - len(tool) - 8
	}
	if rs := []rune(arg); len(rs) > limit {
		arg = string(rs[:limit]) + "…"
	}
	return tool, arg
}

// runLine 是「连着调用同一个工具 n 次」并出来的那一行：只保留最新在做什么，
// 后面缀上次数。次数会随着新调用往上走，所以这行在活动区里是活的——
// 一眼就知道它还在干活，而不是七行一模一样的提示挤满屏幕。
func (f *Filter) runLine(tool, arg string, n int) string {
	line := f.Rules.formatHead(tool, arg)
	if n > 1 {
		line += " ×" + itoa(n)
	}
	return dim + line + reset
}

// minLinesForHealthCheck 少于这么多行就不下结论——可能只是开了一下就退出了。
const minLinesForHealthCheck = 80

// HealthWarning 在「跑了一整场却一次都没折叠过」时返回一句提示，否则空串。
//
// 存在的理由：折叠规则是贴着上游 TUI 的文本长相写的，上游改个符号就会**静默失效**——
// 界面看着正常，只是什么都没被折叠。这个项目在这上面栽过三次（把 ⏺ 看成 ●、
// 把行尾当成 \r\n、以为词之间有空格），每次单测都是绿的。宁可啰嗦一句。
func (f *Filter) HealthWarning() string {
	if f.Lines < minLinesForHealthCheck || f.Hits > 0 {
		return ""
	}
	return "squint: judged " + itoa(f.Lines) + " lines and collapsed nothing — rule set \"" +
		f.Rules.Name + "\" may not match this agent version. Run `squint --check`, " +
		"or capture a session with SQUINT_CAPTURE=/tmp/x.raw and run `squint --check /tmp/x.raw`."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
