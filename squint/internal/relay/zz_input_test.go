package relay

import (
	"strings"
	"testing"
)

// 中文输入行逐字增长时，屏幕上只该有一份输入内容。
// 截图里出现过「❯ 方案方案」——同一段输入显示了两遍。
func TestInputRowNoDuplicate(t *testing.T) {
	sc := newScreen(mustRules(t), 80, 8)
	sc.Write([]byte("上一条正文\r\n"))
	_ = sc.Frame()

	// CC 每敲一个字重画一次输入行：回到行首、清行、重写
	for _, typed := range []string{"方", "方案", "方案设", "方案设计"} {
		sc.Write([]byte("\r\x1b[2K❯ " + typed))
		out := string(sc.Frame())
		if n := strings.Count(out, typed); n > 1 {
			t.Errorf("输入 %q 在同一帧里出现 %d 次:\n%q", typed, n, out)
		}
	}
	rows, _, _ := sc.liveRegion()
	joined := strings.Join(rows, "\n")
	if strings.Count(joined, "方案设计") != 1 {
		t.Errorf("屏幕上「方案设计」应只有一份:\n%s", joined)
	}
}

// CC 不清行、只在原位追加字符（另一种常见重画方式）时也不能出现两份。
func TestInputRowAppendNoDuplicate(t *testing.T) {
	sc := newScreen(mustRules(t), 80, 8)
	sc.Write([]byte("❯ "))
	_ = sc.Frame()
	for _, ch := range []string{"方", "案"} {
		sc.Write([]byte(ch))
		_ = sc.Frame()
	}
	rows, _, _ := sc.liveRegion()
	joined := strings.Join(rows, "\n")
	if strings.Count(joined, "方案") != 1 {
		t.Errorf("追加式输入不该重复:\n%s", joined)
	}
}

// 打字期间不能整片重画：中文输入法的预编辑是终端画的、模拟器看不见，
// \x1b[J 会把它一起擦掉，屏幕上就出现两份输入（截图里的「方案方案」）。
func TestDeferRepaintWhileTyping(t *testing.T) {
	sc := newScreen(mustRules(t), 80, 8)
	sc.Write([]byte("第一行\r\n"))
	_ = sc.Frame()

	// 新历史 → 本该整片重画；打字期间要求跳过并记账
	sc.Write([]byte("工具输出一大段\r\n又一段\r\n"))
	if b := sc.FrameDeferRepaint(); len(b) != 0 {
		t.Errorf("打字期间不该输出整片重画，却写了 %d 字节", len(b))
	}
	if !sc.Pending() {
		t.Error("跳过的那一帧要记账，停手后才补得上")
	}

	// 停手后补画，必须真的画出来且带整片擦除
	out := string(sc.Frame())
	if !strings.Contains(out, "\x1b[J") {
		t.Errorf("停手后应补一帧整片重画:\n%q", out)
	}
	if sc.Pending() {
		t.Error("补画之后不该还欠着")
	}
	// 攒着的内容一个都不能少
	if !strings.Contains(out, "又一段") {
		t.Errorf("推迟期间的输出丢了:\n%q", out)
	}
}

// 差量重绘（只改一两行、不擦整片）在打字期间照常进行，否则 spinner 会僵住。
func TestDiffStillPaintsWhileTyping(t *testing.T) {
	sc := newScreen(mustRules(t), 80, 8)
	sc.Write([]byte("Working… 1s\r\n第二行\r\n"))
	_ = sc.Frame()
	// 原地改第一行、光标回原处：行数不变，属于差量重绘
	sc.Write([]byte("\x1b[1;1HWorking… 2s\x1b[3;1H"))
	if b := sc.FrameDeferRepaint(); len(b) == 0 {
		t.Error("行数没变的差量重绘不该被推迟")
	}
}

// CC 的输入提示 / 历史补全一个都不想看到：不管它用 SGR 2、深灰还是偏亮的灰。
func TestHidesAllGrayHintShades(t *testing.T) {
	for _, sgr := range []string{
		"\x1b[2m",                // faint
		"\x1b[38;5;242m",         // 深灰 #6c6c6c
		"\x1b[38;5;250m",         // 偏亮的灰 #bcbcbc
		"\x1b[38;2;200;200;200m", // truecolor 亮灰
	} {
		got, _ := pipe(t, 60, 6, "上一条正文\r\r\n", "❯ 我敲的字"+sgr+"补全提示"+"\x1b[0m\x1b[2;13H")
		joined := strings.Join(got, "\n")
		if strings.Contains(joined, "补全提示") {
			t.Errorf("%q 这种灰没被抹掉:\n%s", sgr, joined)
		}
		if !strings.Contains(joined, "我敲的字") {
			t.Errorf("%q 场景误伤了自己敲的字:\n%s", sgr, joined)
		}
	}
}
