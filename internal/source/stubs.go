package source

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"

	"github.com/mayangzz/squint/internal/event"
)

func init() {
	register(codexSource{})
	register(geminiSource{})
}

// codexSource / geminiSource 是骨架：命令行拼装按各家文档写好了，Parse 还没对齐它们
// 真实的事件字段。等有人拿真实输出跑一遍再补——凭文档猜字段名跟凭肉眼抄标记字符
// 是同一类错误（见 internal/relay/rules.json 的注释）。
//
// 补的方法：先 `<cli> --output-format ... > sample.jsonl` 抓一份真实输出，
// 再照着 claude.go 的写法把字段映射到 Event。

type codexSource struct{}

func (codexSource) Name() string { return "codex" }

func (codexSource) Cmd(prompt, model string, extra []string) *exec.Cmd {
	args := []string{"exec", "--json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, extra...)
	c := exec.Command("codex", args...)
	c.Stdin = stringReader(prompt)
	return c
}

func (codexSource) Parse(r io.Reader, out chan<- event.Event) { passthroughJSONL(r, out) }

type geminiSource struct{}

func (geminiSource) Name() string { return "gemini" }

func (geminiSource) Cmd(prompt, model string, extra []string) *exec.Cmd {
	args := []string{"--output-format", "stream-json", "--prompt", prompt}
	if model != "" {
		args = append(args, "--model", model)
	}
	c := exec.Command("gemini", append(args, extra...)...)
	return c
}

func (geminiSource) Parse(r io.Reader, out chan<- event.Event) { passthroughJSONL(r, out) }

// passthroughJSONL 是骨架的临时行为：认不出结构就把每行当一句话透出来，
// 至少能看到东西、也能拿来观察真实字段长什么样，而不是静默吐空。
func passthroughJSONL(r io.Reader, out chan<- event.Event) {
	defer close(out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var probe map[string]any
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		out <- event.Event{Kind: event.KindText, Label: string(line), Raw: append([]byte(nil), line...)}
	}
	out <- event.Event{Kind: event.KindDone}
}
