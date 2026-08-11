// Package lens 决定「看到什么」。squint 的名字就是这个意思：眯着眼看，细节糊掉只剩形状。
//
// 两种镜片来源，一个配置开关(--lens / config.lens)选用哪个：
//   - 内置：minimal / trace / quiet / errors
//   - 外部进程：~/.squint/lenses/ 下任何语言写的可执行文件，收 JSON 事件、吐要显示的行
package lens

import (
	"fmt"

	"github.com/mayangzz/squint/internal/event"
)

// Lens 每收到一个事件，返回要打印的内容；返回 ok=false 表示这条不显示。
// 有状态（攒最终答案、数步数）由实现自己持有，所以每次运行 New 一个新实例。
type Lens interface {
	View(event.Event) (string, bool)
}

// Factory 造一个镜片实例。
type Factory func() Lens

var registry = map[string]Factory{}

// Register 登记镜片。加一种看法 = 加一个实现 + 这里一行，其余代码不动。
func Register(name string, f Factory) { registry[name] = f }

// New 按名字造镜片；内置找不到时回退到外部进程镜片（名字当命令名）。
func New(name string) (Lens, error) {
	if f, ok := registry[name]; ok {
		return f(), nil
	}
	l, err := newExternal(name)
	if err != nil {
		return nil, fmt.Errorf("unknown lens %q: %w", name, err)
	}
	return l, nil
}

// Names 列出内置镜片。
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	return out
}
