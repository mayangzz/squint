package lens

import (
	"fmt"
	"strings"

	"github.com/mayangzz/squint/internal/event"
)

const (
	dim   = "\033[2m"
	green = "\033[32m"
	red   = "\033[31m"
	reset = "\033[0m"
)

func init() {
	Register("minimal", func() Lens { return &minimal{} })
	Register("trace", func() Lens { return &minimal{detail: true} })
	Register("quiet", func() Lens { return &minimal{onlyAnswer: true} })
	Register("errors", func() Lens { return &minimal{onlyErrors: true} })
}

// minimal 一个实现覆盖四种内置镜片——它们只差「显不显示某类东西」几个开关，
// 拆四个结构体纯属重复。
type minimal struct {
	detail     bool // trace：每步带命令原文
	onlyAnswer bool // quiet：只要最终答案
	onlyErrors bool // errors：只要失败的步骤

	steps  int
	answer strings.Builder
}

func (m *minimal) View(e event.Event) (string, bool) {
	switch e.Kind {
	case event.KindStart:
		if m.onlyAnswer || m.onlyErrors {
			return "", false
		}
		return fmt.Sprintf("%s  %s · %s%s\n", dim, e.Label, shortPath(e.Detail), reset), true

	case event.KindStep:
		m.steps++
		if m.onlyAnswer || m.onlyErrors {
			return "", false
		}
		line := fmt.Sprintf("  %s✓%s %s", green, reset, e.Label)
		if m.detail && e.Detail != "" {
			line += fmt.Sprintf("\n    %s%s%s", dim, truncate(e.Detail, 100), reset)
		}
		return line, true

	case event.KindError:
		if m.onlyAnswer {
			return "", false
		}
		return fmt.Sprintf("  %s✗%s %s %s%s%s", red, reset, e.Label, dim, truncate(e.Detail, 100), reset), true

	case event.KindText:
		m.answer.WriteString(e.Label)
		return "", false

	case event.KindDone:
		answer := strings.TrimSpace(m.answer.String())
		if m.onlyAnswer {
			return answer, true
		}
		foot := fmt.Sprintf("  %s%d steps · %.1fs · $%.4f%s", dim, m.steps, float64(e.MS)/1000, e.Cost, reset)
		if m.onlyErrors {
			return foot, true
		}
		return "\n" + answer + "\n\n" + foot, true
	}
	return "", false
}

// shortPath 只留最后两段目录，全路径在终端里又长又没信息量。
func shortPath(p string) string {
	parts := strings.Split(strings.TrimSuffix(p, "/"), "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
