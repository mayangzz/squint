// Package relay 在你和 agent 的真实 TUI 之间插一层，只改显示、不改行为。
//
// 可行的前提（PTY 实抓验证）：Claude Code 用的是经典主屏渲染器，不进 alt-screen、
// 不发 [2J 清屏，落地内容一行一行追加。所以按行过滤就够，不需要终端模拟器。
//
// 但「按行」比看上去难，三个坑都不看字节就发现不了：
//
//	行尾是 \r\r\n  子进程写 "\r\n"，PTY 的 ONLCR 再把 \n 展成 "\r\n"
//	词间没有空格   靠 \x1b[<n>G 摆列位置，删掉转义序列会得到连体字
//	光标会往上跑   \x1b[<n>A 重画未落地的区域（输入框、spinner、进行中的工具块）
//
// 前两条已经修掉（见 pump 的 \r 计数和 visible）。第三条是这层的固有边界：
// 折叠会改变行数，被 cursor-up 覆盖的活动区域因此对不齐，所以只折叠已落地的
// 滚动历史，活动区域原样透传。
package relay

import (
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

const (
	dim   = "\x1b[2m"
	reset = "\x1b[0m"
)

// ansi 匹配转义序列，用来剥出纯文本做判断——输出仍然发原始字节，保留颜色。
// OSC 放最前：它的载荷里可能出现 '['，先按 CSI 切会把它劈开留下残渣。
var ansi = regexp.MustCompile(
	"\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC ... BEL / ST
		"|\x1b\\[[0-9;:<>?]*[ -/]*[@-~]" + // CSI
		"|\x1b[()][0-9A-Za-z]" + // 字符集选择，如 tput sgr0 的 \x1b(B
		"|\x1b[78=>MNOPVWXZ\\\\^_]") // 其余双字节转义

// colMove 从一个转义序列里解出「光标移到第几列」，0 基；不是列定位就返回 false。
// CHA(`\x1b[nG`) 是绝对列，CUF(`\x1b[nC`) 是相对右移。
func colMove(seq string, col int) (int, bool) {
	if len(seq) < 3 || seq[1] != '[' {
		return 0, false
	}
	kind := seq[len(seq)-1]
	if kind != 'G' && kind != 'C' {
		return 0, false
	}
	n := 0
	for _, c := range seq[2 : len(seq)-1] {
		if c < '0' || c > '9' {
			return 0, false // 带参数分隔符的不是简单列定位，别猜
		}
		n = n*10 + int(c-'0')
	}
	if kind == 'C' {
		if n == 0 {
			n = 1
		}
		return col + n, true
	}
	if n > 0 {
		n-- // CHA 是 1 基
	}
	return n, true
}

// visible 把一行原始字节还原成「屏幕上看到的文本」，用来做规则判断；
// 输出仍然发原始字节，颜色不受影响。
//
// 不能简单地把转义序列删光：Claude Code 是用 `\x1b[<n>G` 逐个词摆位置的，
// 词与词之间**根本没有空格字节**。删光了得到的是 `Accessingworkspace:` 这种连体字，
// 任何带 `\s` 的规则都匹配不上——而且失败完全静默，界面照常显示，只是什么都没折叠。
// 所以这里按列把空格补回来。
func visible(raw []byte) string {
	s := string(raw)
	var b strings.Builder
	col, last := 0, 0
	for _, loc := range ansi.FindAllStringIndex(s, -1) {
		text := s[last:loc[0]]
		b.WriteString(text)
		col += utf8.RuneCountInString(text)
		last = loc[1]
		if n, ok := colMove(s[loc[0]:loc[1]], col); ok && n > col {
			b.WriteString(strings.Repeat(" ", n-col))
			col = n
		}
	}
	b.WriteString(s[last:])

	// 残留的控制字符照样会让锚定正则失效，一并抹掉。
	out := strings.Map(func(r rune) rune {
		if r != '\t' && (r < 0x20 || r == 0x7f) {
			return -1
		}
		return r
	}, b.String())
	return strings.TrimRight(out, " \t")
}

// Filter 是一个按行工作的状态机：一次喂一行原始字节，返回要输出的字节。
// 返回 nil 表示这行不显示。规则来自 Rules，不写死在这里。
type Filter struct {
	Rules *Rules
	Debug *DebugLog // SQUINT_DEBUG 打开时记录每行的去向，否则为 nil

	width    atomic.Int64 // resize goroutine 写、pump goroutine 读，必须原子
	inBlock  bool
	lastHead string // 上一条折叠行，用于压掉连续重复
	Lines    int    // 总共看过多少行
	Hits     int    // 命中折叠的行数——为 0 说明规则跟当前 UI 对不上（见 HealthWarning）
	Partials int    // 没等到行尾、原样放行的半行数——高说明过滤器根本没机会看到内容
}

// notePartial 由 pump 在放弃过滤一行时调用。
func (f *Filter) notePartial(raw []byte) {
	f.Partials++
	f.Debug.note("PART", "", raw)
}

// Line 处理一行（不含换行符）。
func (f *Filter) Line(raw []byte) []byte {
	f.Lines++
	r := f.Rules
	text := visible(raw)

	switch {
	case r.head.MatchString(text):
		f.inBlock = true
		f.Hits++
		f.Debug.note("HEAD", text, raw)
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
		f.Debug.note("NOISE", text, raw)
		return nil

	case f.inBlock:
		if strings.TrimSpace(text) == "" {
			f.inBlock = false // 空行 = 块结束
			return raw
		}
		if r.branch.MatchString(text) || strings.HasPrefix(text, "    ") {
			f.Hits++
			f.Debug.note("BRANCH", text, raw)
			return nil
		}
		f.inBlock = false // 顶格普通文本说明块已结束（agent 开始说话）
		f.Debug.note("PASS", text, raw)
		return raw

	default:
		if strings.TrimSpace(text) != "" {
			f.lastHead = "" // 中间出现过正文，之后同样的调用是新一轮，不该压掉
		}
		f.Debug.note("PASS", text, raw)
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
	if f.Lines+f.Partials < minLinesForHealthCheck || f.Hits > 0 {
		return ""
	}
	// 两种失败长得一模一样：规则对不上 UI，或者内容压根没成行送进过滤器。
	// 报出各自的计数，才知道该改规则还是改 pump。
	why := "rule set \"" + f.Rules.Name + "\" may not match this agent version"
	if f.Partials > f.Lines {
		why = "most output never reached the filter (" + itoa(f.Partials) + " partial lines vs " +
			itoa(f.Lines) + " whole ones) — this is a squint bug, not your rules"
	}
	return "squint: collapsed nothing — " + why +
		". Run `squint --check`, or re-run with SQUINT_DEBUG=/tmp/squint.log and open an issue."
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
