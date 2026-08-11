package relay

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestForwardsTerminalModes 配置终端**输入行为**的序列必须原样透到真终端。
//
// 模拟器会把子进程的输出整个吃掉——对「画什么」是对的，但有一类序列压根不是在画东西：
// 它们在开关括号粘贴、鼠标上报、键盘协议。截在中间就是把功能关掉：
// 用户实测粘贴图片直接失效，因为终端从没进入过括号粘贴模式。
//
// 这条是 squint 的底线：只改显示，不碰行为。
func TestForwardsTerminalModes(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()

	// 真实抓包里 Claude Code 启动时发的那一串
	sc.Write([]byte("\x1b[?25l\x1b[?2004h\x1b[?1004h\x1b[?2031h\x1b[<u\x1b[>1u\x1b[>4;2m"))
	sc.Write([]byte("\x1b]0;✳ Claude Code\x1b\\hello\r\r\n"))
	frame := string(sc.Frame())

	for _, must := range []string{
		"\x1b[?2004h", // 括号粘贴——粘贴图片/多行全靠它
		"\x1b[?1004h", // 焦点上报
		"\x1b[?2031h", // 配色变更通知
		"\x1b[<u",     // kitty 键盘协议
		"\x1b[>1u",
		"\x1b[>4;2m",           // modifyOtherKeys
		"\x1b]0;✳ Claude Code", // 窗口标题
	} {
		if !strings.Contains(frame, must) {
			t.Errorf("没转发 %q，这个特性在真终端上就是坏的", must)
		}
	}
	// 转发只发一次，别每帧重复灌
	if again := string(sc.Frame()); strings.Contains(again, "\x1b[?2004h") {
		t.Error("同一条模式序列被重复转发了")
	}
}

// TestForwardsAcrossChunkBoundary 序列被切在两次 read 之间也不能漏。
// 一次 read 读多少字节由内核缓冲决定，跟序列边界毫无关系。
func TestForwardsAcrossChunkBoundary(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	sc.Write([]byte("text\x1b[?20"))
	sc.Write([]byte("04h more"))
	if frame := string(sc.Frame()); !strings.Contains(frame, "\x1b[?2004h") {
		t.Errorf("跨读边界的序列漏转了: %q", frame)
	}
}

// TestDoesNotForwardLayoutSequences 画面类的序列绝不能转——转出去会把我们自己的排版打乱。
func TestDoesNotForwardLayoutSequences(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	sc.Write([]byte("\x1b[10A\x1b[5G\x1b[2J\x1b[?1049h\x1b[?2026htext"))
	frame := string(sc.Frame())
	for _, banned := range []string{"\x1b[10A", "\x1b[2J", "\x1b[?1049h"} {
		// 注意 ESC[5G / ESC[?2026h 我们自己也会发，所以只查这几个不会自产的
		if bytes.Count([]byte(frame), []byte(banned)) > 0 {
			t.Errorf("不该转发 %q，会打乱我们自己的排版", banned)
		}
	}
}

// TestDiffOnlyRewritesChangedRows 只有一行变了就只重写那一行。
//
// 整片擦掉重画（ESC[J）在不支持同步输出的终端上是肉眼可见的闪——用户实测就是「一闪一闪」。
func TestDiffOnlyRewritesChangedRows(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	sc.Write([]byte("alpha\r\r\nbravo\r\r\ncharlie\r\r\n> "))
	sc.Frame() // 第一帧建立基线，整片画

	sc.Write([]byte("x")) // 只改输入框那一行
	second := string(sc.Frame())

	if strings.Contains(second, "\x1b[J") {
		t.Errorf("只变了一行却整片擦重画，这就是闪的来源: %q", second)
	}
	for _, unchanged := range []string{"alpha", "bravo", "charlie"} {
		if strings.Contains(second, unchanged) {
			t.Errorf("没变的行 %q 被重写了", unchanged)
		}
	}
}

// TestDiffFallsBackWhenLayoutChanges 行数变了就得整片重来，逐行比对会串位。
func TestDiffFallsBackWhenLayoutChanges(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	sc.Write([]byte("one\r\r\ntwo\r\r\n"))
	sc.Frame()
	sc.Write([]byte("three\r\r\nfour\r\r\n")) // 多出两行
	if second := string(sc.Frame()); !strings.Contains(second, "\x1b[J") {
		t.Errorf("行数变了应该整片重来: %q", second)
	}
}
