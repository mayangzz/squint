package source

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/mayangzz/squint/internal/event"
)

func init() { register(claudeSource{}) }

// claudeSource 对接 Claude Code 的 `-p --output-format stream-json`。
type claudeSource struct{}

func (claudeSource) Name() string { return "claude" }

func (claudeSource) Cmd(prompt, model string, extra []string) *exec.Cmd {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, extra...)
	c := exec.Command("claude", args...)
	c.Stdin = stringReader(prompt)
	return c
}

// claudeLine 只声明我们要用的字段，其余交给 json 忽略。
type claudeLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Cwd     string `json:"cwd"`
	Message struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			ID      string          `json:"id"`
			Name    string          `json:"name"`
			Input   json.RawMessage `json:"input"`
			IsError bool            `json:"is_error"`
			ToolUse string          `json:"tool_use_id"`
			Content json.RawMessage `json:"content"`
		} `json:"content"`
	} `json:"message"`
	TotalCost  float64 `json:"total_cost_usd"`
	DurationMS int     `json:"duration_ms"`
}

func (claudeSource) Parse(r io.Reader, out chan<- event.Event) {
	defer close(out)
	lines, emitted := 0, 0
	// 两种静默失效，跟 relay 的 HealthWarning 是同一类问题：
	//   1. 单行超过 buffer 上限 → Scan 直接返回 false，看着像正常 EOF，后半段全丢
	//   2. 上游改字段名 → json.Unmarshal 不报错（未知字段被忽略），只是全成零值
	// 两种都会安静地少输出，不吭一声。
	defer func() {
		if lines > 0 && emitted == 0 {
			fmt.Fprintf(os.Stderr, "squint: read %d lines but recognized nothing — the %s stream format may have changed\n", lines, "claude")
		}
	}()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // 单条事件可能很大(整份文件内容)
	tools := map[string]string{}                     // tool_use_id → 工具名，给失败事件补标签

	for sc.Scan() {
		lines++
		raw := sc.Bytes()
		var l claudeLine
		if json.Unmarshal(raw, &l) != nil {
			continue // 非 JSON 行直接跳过，别让一条脏数据打断整个流
		}
		switch l.Type {
		case "system":
			if l.Subtype == "init" {
				out <- event.Event{Kind: event.KindStart, Label: l.Model, Detail: l.Cwd}
			}
		case "assistant":
			for _, b := range l.Message.Content {
				switch b.Type {
				case "tool_use":
					tools[b.ID] = b.Name
					label, detail := describe(b.Name, b.Input)
					out <- event.Event{Kind: event.KindStep, Label: label, Detail: detail, Tool: b.Name, Raw: clone(raw)}
				case "text":
					if b.Text != "" {
						out <- event.Event{Kind: event.KindText, Label: b.Text}
					}
				}
			}
		case "user":
			for _, b := range l.Message.Content {
				if b.IsError {
					out <- event.Event{Kind: event.KindError, Label: tools[b.ToolUse], Detail: string(b.Content), Tool: tools[b.ToolUse]}
				}
			}
		case "result":
			out <- event.Event{Kind: event.KindDone, Cost: l.TotalCost, MS: l.DurationMS}
		default:
			continue
		}
		emitted++
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "squint: stream truncated: %v\n", err)
	}
}

// describe 把工具调用压成「一句人话 + 细节」。
// description 是 Claude Code 自带字段，本来就是给人看的，优先用它。
func describe(tool string, input json.RawMessage) (label, detail string) {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	str := func(k string) string {
		s, _ := m[k].(string)
		return s
	}
	label = str("description")
	for _, k := range []string{"command", "file_path", "pattern", "path", "url", "query"} {
		if v := str(k); v != "" {
			detail = v
			break
		}
	}
	if label == "" {
		label = tool
		if detail != "" {
			label = tool + " " + detail
		}
	}
	return label, detail
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }
