package relay

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed rules.json
var defaultRules []byte

// Rules 描述「一个 agent TUI 长什么样」。抽成数据而不是写死在代码里，是因为它跟着
// 上游 UI 走：换个 CLI、上游改个符号，改这个文件就行，不用重编译 squint。
type Rules struct {
	Name string `json:"name"` // 规则集名字，自检信息里会打出来
	// Head 工具块头行，如 `⏺ Bash(...)`。必须有两个捕获组：工具名、参数。
	Head string `json:"head"`
	// Branch 块内续行（`⎿ …`）。
	Branch string `json:"branch"`
	// Noise 任何时候都不显示的整行。
	Noise []string `json:"noise"`
	// HeadMaxWidth 头行参数最多留几个字。
	HeadMaxWidth int `json:"head_max_width"`
	// HeadFormat 折叠后那一行长什么样。占位符：{tool} {arg}。
	HeadFormat string `json:"head_format"`
	// DedupeRepeats 连着重复的同一行只显示一次。agent 反复读同一个文件、
	// 反复跑同一条命令时，重复提醒没有信息量，只是把屏幕撑满。
	DedupeRepeats *bool `json:"dedupe_repeats"`
	// MergeToolRuns 连着调用同一个工具的若干次并成一行，缀上次数。
	// 一口气跑七条 shell 命令时，七行提示不如一行「在跑 shell，第 7 条」有用。
	MergeToolRuns *bool `json:"merge_tool_runs"`
	// HideInputHint 抹掉输入框里光标之后的暗色文字（CC 的输入提示与历史补全）。
	HideInputHint *bool `json:"hide_input_hint"`
	// Tools 按工具名覆盖 HeadFormat，想写成「🔍 正在查找 {arg}」就写在这里。
	// 键大小写不敏感，值同样支持 {tool} {arg}。
	Tools map[string]string `json:"tools"`

	tools map[string]string // 键统一小写后的 Tools，查表用

	head   *regexp.Regexp
	branch *regexp.Regexp
	noise  []*regexp.Regexp
}

// LoadRules 优先读 $SQUINT_RULES（"builtin" 强制用内置），其次 ~/.squint/rules.json，都没有就用内置的那份。
func LoadRules() (*Rules, error) {
	data := defaultRules
	path := os.Getenv("SQUINT_RULES")
	explicit := path != ""
	if !explicit {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".squint", "rules.json")
		}
	}
	if path != "" && path != "builtin" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			data = b
		case explicit || !os.IsNotExist(err):
			// 文件在但读不了（权限/是目录）——静默回退内置规则的话，用户会以为
			// 自己的改动生效了，其实一直没读到。
			fmt.Fprintf(os.Stderr, "squint: ignoring %s: %v\n", path, err)
		}
	}
	var r Rules
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	if err := r.compile(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *Rules) compile() error {
	var err error
	if r.head, err = regexp.Compile(r.Head); err != nil {
		return fmt.Errorf("rules.head: %w", err)
	}
	if r.head.NumSubexp() < 2 {
		return fmt.Errorf("rules.head needs two capture groups (tool name, args), got %d", r.head.NumSubexp())
	}
	if r.branch, err = regexp.Compile(r.Branch); err != nil {
		return fmt.Errorf("rules.branch: %w", err)
	}
	if r.Head == "" || r.Branch == "" {
		return fmt.Errorf("rules.head / rules.branch must not be empty: an empty regexp matches every line and would swallow the whole terminal")
	}
	r.noise = r.noise[:0]
	for i, p := range r.Noise {
		if p == "" {
			return fmt.Errorf("rules.noise[%d] is empty: an empty regexp matches every line and would swallow the whole terminal", i)
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("rules.noise[%d]: %w", i, err)
		}
		r.noise = append(r.noise, re)
	}
	if r.HeadMaxWidth <= 0 {
		r.HeadMaxWidth = 60
	}
	if r.HeadFormat == "" {
		r.HeadFormat = "● {tool}({arg})"
	}
	if r.DedupeRepeats == nil {
		on := true // 默认开：重复行没信息量
		r.DedupeRepeats = &on
	}
	if r.HideInputHint == nil {
		on := true // 提示文字跟自己敲的字挤在一行，看着像画坏了
		r.HideInputHint = &on
	}
	if r.MergeToolRuns == nil {
		on := true // 默认开：连着同一个工具，知道它在跑什么就够了
		r.MergeToolRuns = &on
	}
	r.tools = make(map[string]string, len(r.Tools))
	for k, v := range r.Tools {
		r.tools[strings.ToLower(k)] = v
	}
	return nil
}

func (r *Rules) isNoise(s string) bool {
	for _, re := range r.noise {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// formatHead 按规则把工具名和参数渲染成折叠后的那一行。
// 优先用 tools 里该工具的专属写法，没有就用 HeadFormat。
func (r *Rules) formatHead(tool, arg string) string {
	f, ok := r.tools[strings.ToLower(tool)]
	if !ok {
		f = r.HeadFormat
	}
	return strings.NewReplacer("{tool}", tool, "{arg}", arg).Replace(f)
}
