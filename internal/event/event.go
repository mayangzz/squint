// Package event 定义 squint 的统一事件模型。
//
// 这是整个架构的腰：左边各家 agent CLI 的流式输出翻译成 Event，右边镜片只认 Event。
// 加一个后端不用动任何镜片，加一个镜片不用动任何后端。
package event

// Kind 是事件类型。显式取值，别用 iota——它要跟外部镜片进程的 JSON 协议对齐。
type Kind string

const (
	KindStart Kind = "start" // 会话开始：模型、目录
	KindStep  Kind = "step"  // agent 做了一件事（工具调用）
	KindText  Kind = "text"  // agent 说的话
	KindError Kind = "error" // 某一步失败了
	KindDone  Kind = "done"  // 收尾：耗时、成本
)

// Event 是归一化后的一条事件。字段刻意少——镜片要的信息就这些，
// 原始载荷留在 Raw 里给需要的镜片自己挖。
type Event struct {
	Kind   Kind    `json:"kind"`
	Label  string  `json:"label"`            // 一句人话：「读风控聊天逻辑」
	Detail string  `json:"detail,omitempty"` // 命令原文 / 文件路径，默认不展示
	Tool   string  `json:"tool,omitempty"`   // 工具名：Bash / Read / Grep
	Cost   float64 `json:"cost,omitempty"`   // 累计花费(USD)，仅 done
	MS     int     `json:"ms,omitempty"`     // 耗时毫秒，仅 done
	Raw    []byte  `json:"-"`                // 后端原始行，外部镜片按需取
}
