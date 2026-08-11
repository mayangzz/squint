// Package source 把各家 agent CLI 的流式输出翻成统一 Event。
//
// 加新后端 = 实现一个 Source + 在 registry 里登记一行，镜片侧完全不用改。
package source

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/mayangzz/squint/internal/event"
)

// Source 是一个 agent 后端。Cmd 给出怎么起进程，Parse 把它的 stdout 翻成 Event。
type Source interface {
	Name() string
	// Cmd 组装子进程。prompt 走 stdin，避免超长参数和引号地狱。
	Cmd(prompt, model string, extra []string) *exec.Cmd
	// Parse 边读边吐事件，读到 EOF 关闭 channel。
	Parse(r io.Reader, out chan<- event.Event)
}

var registry = map[string]Source{}

func register(s Source) { registry[s.Name()] = s }

// Get 按名字取后端。
func Get(name string) (Source, error) {
	s, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown source %q (have: %s)", name, Names())
	}
	return s, nil
}

// Names 列出已注册的后端，给报错和 --help 用。
func Names() string {
	var out string
	for n := range registry {
		if out != "" {
			out += ", "
		}
		out += n
	}
	return out
}
