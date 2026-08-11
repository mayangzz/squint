// Package relay 在你和 agent 的真实 TUI 之间插一层，只改显示、不改行为。
//
// 可行的前提（已实测）：Claude Code 用的是经典主屏渲染器——不进 alt-screen、
// 不发 [2J 清屏、不用 [H 定位光标，输出是一行一行追加的。所以按行过滤就够，
// 不需要实现终端模拟器。
package relay

import (
	"regexp"
	"strings"
	"sync/atomic"
)

const (
	dim   = "\x1b[2m"
	reset = "\x1b[0m"
)

// ansi 匹配所有 CSI 序列，用来剥出纯文本做判断——输出仍然发原始字节，保留颜色。
var ansi = regexp.MustCompile(`\x1b\[[0-9;<>?]*[a-zA-Z]`)

// Filter 是一个按行工作的状态机：一次喂一行原始字节，返回要输出的字节。
// 返回 nil 表示这行不显示。规则来自 Rules，不写死在这里。
type Filter struct {
	Rules *Rules

	width    atomic.Int64 // resize goroutine 写、pump goroutine 读，必须原子
	inBlock  bool
	lastHead string // 上一条折叠行，用于压掉连续重复
	Lines    int    // 总共看过多少行
	Hits     int    // 命中折叠的行数——为 0 说明规则跟当前 UI 对不上（见 HealthWarning）
}

// Line 处理一行（不含换行符）。
func (f *Filter) Line(raw []byte) []byte {
	f.Lines++
	r := f.Rules
	text := strings.TrimRight(ansi.ReplaceAllString(string(raw), ""), " \t\r")

	switch {
	case r.head.MatchString(text):
		f.inBlock = true
		f.Hits++
		line := f.collapseHead(text)
		// 连着重复的同一行压掉：agent 反复读同一个文件、反复跑同一条命令是常态，
		// 第二次开始就没有新信息了，只是占屏幕。
		if *r.DedupeRepeats && string(line) == f.lastHead {
			return nil
		}
		f.lastHead = string(line)
		return line

	case r.isNoise(text):
		f.Hits++
		return nil

	case f.inBlock:
		if strings.TrimSpace(text) == "" {
			f.inBlock = false // 空行 = 块结束
			return raw
		}
		if r.branch.MatchString(text) || strings.HasPrefix(text, "    ") {
			f.Hits++
			return nil
		}
		f.inBlock = false // 顶格普通文本说明块已结束（agent 开始说话）
		return raw

	default:
		if strings.TrimSpace(text) != "" {
			f.lastHead = "" // 中间出现过正文，之后同样的调用是新一轮，不该压掉
		}
		return raw
	}
}

// collapseHead 把 `⏺ Bash(一长串命令)` 压成一行，超宽截断。
// 保留工具名让人知道在干嘛，参数只留开头。
func (f *Filter) collapseHead(text string) []byte {
	m := f.Rules.head.FindStringSubmatch(text)
	tool, arg := m[1], strings.TrimSuffix(m[2], ")")
	limit := f.Rules.HeadMaxWidth
	if w := int(f.width.Load()); w > 20 && w-len(tool)-8 < limit {
		limit = w - len(tool) - 8
	}
	if rs := []rune(arg); len(rs) > limit {
		arg = string(rs[:limit]) + "…"
	}
	return []byte(dim + f.Rules.formatHead(tool, arg) + reset)
}

// minLinesForHealthCheck 少于这么多行就不下结论——可能只是开了一下就退出了。
const minLinesForHealthCheck = 80

// HealthWarning 在「跑了一整场却一次都没折叠过」时返回一句提示，否则空串。
//
// 存在的理由：折叠规则是贴着上游 TUI 的文本长相写的，上游改个符号就会**静默失效**——
// 界面看着正常，只是什么都没被折叠。作者本人就踩过（把 ⏺ 看成 ●，正则永远匹配不上，
// 而单测因为素材也照肉眼抄所以照样通过）。所以宁可啰嗦一句，也不让它默默不干活。
func (f *Filter) HealthWarning() string {
	if f.Lines < minLinesForHealthCheck || f.Hits > 0 {
		return ""
	}
	return "squint: saw " + itoa(f.Lines) + " lines and collapsed nothing — rule set \"" + f.Rules.Name +
		"\" may not match this agent version. Run `squint --check`, or edit ~/.squint/rules.json."
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

// SetWidth 由 resize goroutine 调用。
func (f *Filter) SetWidth(w int) { f.width.Store(int64(w)) }

// newFilter 造一个带宽度的 Filter。
func newFilter(r *Rules, width int) *Filter {
	f := &Filter{Rules: r}
	f.SetWidth(width)
	return f
}
