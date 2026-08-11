package relay

import (
	"bytes"
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// 同步输出：一帧的字节全到齐了再让终端刷新，否则重绘中途会被看到，表现为撕裂/闪烁。
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// 模拟器只替我们暂存「刚滚出屏幕、还没被取走」的行，每帧取空；真正的历史交给
// 用户终端自己的滚动缓冲。
//
// 但暂存区是会淘汰的：一帧之内滚出的行超过容量，最老的会被**悄悄丢掉**。
// agent 输出一个大文件时一帧滚上千行是常事，所以攒到 historyFlushAt 就提前画一帧，
// 让上限由「多久画一次」而不是「一帧能滚多少」决定。
const (
	scrollbackWindow = 4096
	historyFlushAt   = 1024
)

// screen 把子进程的输出喂进终端模拟器，再按规则重新渲染一份「过滤后的屏幕」。
//
// 为什么非要模拟器：Claude Code 2.1.227 是差量重绘——ESC[nA 上跳、CR ESC[1B 逐格补画，
// 一次写入里塞进好几个屏幕行。字节流里的「行」和屏幕上的行没有对应关系，无状态的
// 按行过滤既切不出行，删掉行还会打乱上游的光标算术（表现为工具块和折叠行一起消失）。
// 跑完模拟器才谈得上「一行」。
//
// 而且模拟器带来一个额外好处：活动区也能过滤。子进程的光标只作用在模拟器的屏幕上，
// 跟我们最终显示什么完全无关，所以整片区域可以由我们自己重画。
//
// 显示模型是「上面追加历史 + 下面重绘活动区」：
//
//	[已滚出屏幕的行] 过滤后逐行追加，永不重绘 —— 用户终端原生的滚动缓冲照常可用
//	[模拟器当前屏幕] 每帧擦掉重画，过滤后行数只会更少，所以永远塞得进一屏
type screen struct {
	em       *vt.Emulator
	rules    *Rules
	stats    *Filter // 跨帧存活：负责已落地的历史，也用来累计自检计数
	curVisib bool

	cursorRow int    // 上一帧结束时光标停在活动区第几行，下一帧靠它退回区首
	liveHits  int    // 最近一帧在活动区折叠掉几行
	histRun   string // 历史里最后写出去的那串折叠行是哪个工具，用来接着并
}

func newScreen(rules *Rules, w, h int) *screen {
	s := &screen{
		em:       vt.NewEmulator(w, h),
		rules:    rules,
		stats:    newFilter(rules, w),
		curVisib: true,
	}
	s.em.Scrollback().SetMaxLines(scrollbackWindow)
	s.em.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { s.curVisib = visible },
	})
	return s
}

func (s *screen) Write(p []byte) { _, _ = s.em.Write(p) }

// PendingHistory 有多少行滚出了屏幕、还没被取走。超过 historyFlushAt 就该马上画一帧，
// 否则暂存区淘汰起来是无声的。
func (s *screen) PendingHistory() int { return s.em.Scrollback().Len() }

func (s *screen) Resize(w, h int) {
	s.em.Resize(w, h)
	s.stats.width = w
	// 终端自己会重排，退回去擦只会擦错地方；清掉退行量，下一帧从当前位置重新开始。
	s.cursorRow = 0
}

// Frame 产出这一帧要写给真终端的字节。
func (s *screen) Frame() []byte {
	var b bytes.Buffer
	b.WriteString(syncBegin)
	b.WriteString("\x1b[?25l") // 重绘期间藏光标，否则它会在半成品上乱跳

	// 退回上一帧活动区的开头，把它整片擦掉。
	//
	// 退多少由**上一帧光标停在第几行**决定，不是活动区有几行——收尾时光标被放回
	// 子进程要的位置（通常是输入框，不在最后一行）。按行数退会退过头，ESC[J 就
	// 擦掉了上面已经落地的历史：折叠得再准，屏幕也会被自己拆掉。
	if s.cursorRow > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", s.cursorRow)
	}
	b.WriteString("\r\x1b[J")

	s.writeHistory(&b)
	s.writeLive(&b)

	if s.curVisib {
		b.WriteString("\x1b[?25h")
	}
	b.WriteString(syncEnd)
	return b.Bytes()
}

// writeHistory 把刚滚出屏幕的行过滤后追加出去。这部分一旦写出就不再动，
// 于是用户终端原生的滚动缓冲、鼠标选择、Cmd+F 全都照常可用。
func (s *screen) writeHistory(b *bytes.Buffer) {
	sb := s.em.Scrollback()
	if sb.Len() == 0 {
		return
	}
	alt := s.em.IsAltScreen()
	for _, line := range sb.Lines() {
		text := lineText(line)
		d := Decision{Verdict: Keep, Text: text}
		if !alt {
			d = s.stats.Judge(text)
		}
		switch d.Verdict {
		case Drop:
			continue
		case Collapse:
			// 历史是流式写出去的，写过就改不了了，所以同一串里后面几次直接不显示。
			// 活动区那边能原地改，会顺带把次数标出来——「还在动」这个信号本来也只在那看。
			if *s.rules.MergeToolRuns && s.histRun == d.Tool {
				continue
			}
			s.histRun = d.Tool
			b.WriteString(d.Text)
		default:
			if strings.TrimSpace(text) != "" {
				s.histRun = "" // 中间出现正文，之后的调用算新一串
			}
			b.WriteString(renderLine(line))
		}
		b.WriteString("\r\n")
	}
	// 取空，避免下一帧重复吐同一批行；容量到顶时的淘汰也不会让我们漏行。
	sb.Clear()
}

// writeLive 重画模拟器当前屏幕，并把光标放回子进程要的位置。
func (s *screen) writeLive(b *bytes.Buffer) {
	rows, plain, cursorRow := s.liveRegion()

	// 去掉尾部空行，但不能越过光标所在行——输入框下面通常还有几行空白。
	last := len(rows) - 1
	for last > cursorRow && strings.TrimSpace(plain[last]) == "" {
		last--
	}
	rows, plain = rows[:last+1], plain[:last+1]

	for i, row := range rows {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(row)
	}
	s.cursorRow = cursorRow

	cur := s.em.CursorPosition()
	if up := len(rows) - 1 - cursorRow; up > 0 {
		fmt.Fprintf(b, "\x1b[%dA", up)
	}
	b.WriteString("\r")
	if cur.X > 0 {
		fmt.Fprintf(b, "\x1b[%dC", cur.X)
	}
}

// liveRegion 过滤模拟器当前屏幕，返回渲染好的行、对应纯文本，以及光标落在第几行。
func (s *screen) liveRegion() (rows, plain []string, cursorRow int) {
	// 活动区每帧重判，所以计数不能记进 stats（同一行会被算几十次），用一份临时状态。
	// 但 inBlock 和 lastHead 必须从历史那边接上：工具块常常头行已经滚进历史、
	// 续行还留在屏幕上，断开的话续行不会被折叠、重复命令也会重新冒出来。
	f := newFilter(s.rules, s.em.Width())
	f.inBlock, f.lastHead = s.stats.inBlock, s.stats.lastHead
	alt := s.em.IsAltScreen()
	cur := s.em.CursorPosition()

	// 连着调用同一个工具的若干次并成一行：agent 一口气跑七条 sed，七行「正在查找」
	// 没有信息量——知道它在跑 shell 就够了。并出来的那行带次数和**最新**在做什么，
	// 每帧重画，所以它是活的：数字往上走就说明还在干活。
	runTool, runIdx, runCount := "", -1, 0

	for y := 0; y < s.em.Height(); y++ {
		line := s.lineAt(y)
		text := lineText(line)
		d := Decision{Verdict: Keep, Text: text}
		if !alt {
			d = f.Judge(text)
		}
		if y == cur.Y {
			cursorRow = len(rows) // 光标行在过滤后是第几行
		}
		switch d.Verdict {
		case Drop:
			continue
		case Collapse:
			if runIdx >= 0 && d.Tool == runTool && *s.rules.MergeToolRuns {
				runCount++
				merged := f.runLine(d.Tool, d.Arg, runCount)
				rows[runIdx], plain[runIdx] = merged, merged
				continue
			}
			runTool, runIdx, runCount = d.Tool, len(rows), 1
			rows, plain = append(rows, d.Text), append(plain, d.Text)
		default:
			if strings.TrimSpace(text) != "" {
				runTool, runIdx, runCount = "", -1, 0 // 中间出现正文，之后的调用算新一串
			}
			rows, plain = append(rows, renderLine(line)), append(plain, text)
		}
	}
	if len(rows) == 0 { // 整屏被过滤空：仍要留一行给光标待着
		rows, plain = []string{""}, []string{""}
	}
	if cursorRow >= len(rows) {
		cursorRow = len(rows) - 1
	}
	s.liveHits = f.Hits // 只留最近一帧：活动区每帧重判，累加会把计数撑成天文数字
	return rows, plain, cursorRow
}

// Collapses 返回一共折叠掉多少行。
//
// 历史那份是累计的；活动区每帧整片重判，只能取最近一帧——但短会话里工具块很可能
// 自始至终没滚出屏幕，那时全靠活动区这一份，漏了就会误报「一次都没折叠」。
func (s *screen) Collapses() int { return s.stats.Hits + s.liveHits }

// HealthWarning 见 Filter.HealthWarning，但把活动区的折叠也算进去。
func (s *screen) HealthWarning() string {
	if s.Collapses() > 0 {
		return ""
	}
	return s.stats.HealthWarning()
}

func (s *screen) lineAt(y int) uv.Line {
	w := s.em.Width()
	line := make(uv.Line, w)
	for x := 0; x < w; x++ {
		if c := s.em.CellAt(x, y); c != nil {
			line[x] = *c
		}
	}
	return line
}

// lineText 取一行的纯文本，供规则匹配。
// 顺手把 NBSP 换成普通空格：上游用它做对齐，但 `\s` 匹配不上，规则会莫名其妙不命中。
func lineText(line uv.Line) string {
	return strings.TrimRight(strings.ReplaceAll(line.String(), " ", " "), " \t")
}

// renderLine 渲染一行，保留颜色。尾部空格切掉，省字节也省得选中时拖出一片空白。
func renderLine(line uv.Line) string {
	last := len(line) - 1
	for last >= 0 && isBlank(line[last]) {
		last--
	}
	if last < 0 {
		return ""
	}
	return line[:last+1].Render() + "\x1b[0m"
}

func isBlank(c uv.Cell) bool {
	return c.Content == "" || c.Content == " " || c.Content == " "
}
