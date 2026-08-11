package relay

import (
	"bufio"
	"fmt"
	"os"
)

// debugMaxLines 记这么多行就停，只留计数。一场会话几万行，全写下来没人看得动。
const debugMaxLines = 1200

// DebugLog 记录每一行最后去了哪儿。
//
// 存在的理由：折叠没生效时，「规则没匹配上」和「内容压根没送进过滤器」在屏幕上
// 长得一模一样，光看截图分不出来，而这两者的修法完全相反。这里把每行的判定
// 连同原始字节一起落盘，一次会话就能定位。
//
// 只有 pump 那个 goroutine 会写，所以不加锁。nil receiver 安全，调用点不用判空。
type DebugLog struct {
	w      *bufio.Writer
	f      *os.File
	n      int
	capped bool
}

// OpenDebug 打开落盘目标；打不开就返回 nil（诊断不该把主流程搞挂）。
func OpenDebug(path string) *DebugLog {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "squint: cannot open debug log %s: %v\r\n", path, err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "squint: writing line-by-line debug log to %s\r\n", path)
	return &DebugLog{w: bufio.NewWriter(f), f: f}
}

// note 记一行：kind 是判定结果，text 是剥掉转义后用于匹配的文本，raw 是原始字节。
func (d *DebugLog) note(kind, text string, raw []byte) {
	if d == nil {
		return
	}
	d.n++
	if d.n > debugMaxLines {
		d.capped = true
		return
	}
	fmt.Fprintf(d.w, "%-6s text=%q raw=%q\n", kind, text, raw)
}

// Close 收尾，把汇总写在文件末尾——排查时先看这几行就知道往哪儿查。
func (d *DebugLog) Close(f *Filter) {
	if d == nil {
		return
	}
	if d.capped {
		fmt.Fprintf(d.w, "... truncated at %d entries (%d total)\n", debugMaxLines, d.n)
	}
	fmt.Fprintf(d.w, "\nsummary: rules=%q whole_lines=%d collapsed=%d partial_lines=%d\n",
		f.Rules.Name, f.Lines, f.Hits, f.Partials)
	_ = d.w.Flush()
	_ = d.f.Close()
}
