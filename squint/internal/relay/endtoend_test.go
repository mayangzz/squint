package relay

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// pipe 把一段子进程字节按 chunks 切开逐帧跑完，返回**用户终端**看到的内容。
//
// 第二个模拟器扮演「用户那台终端」：断言的是屏幕最终长什么样，而不是我们打算画什么。
// 这是这个项目唯一靠谱的验收方式——退行擦多了会吃掉历史、光标算错会串行，这些只有
// 真画一遍才看得见，之前三次「单测全绿、线上全废」都栽在断言中间产物上。
func pipe(t *testing.T, w, h int, chunks ...string) ([]string, *screen) {
	t.Helper()
	sc := newScreen(mustRules(t), w, h)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	user := newTestTerminal(t, w, h)
	for _, c := range chunks {
		sc.Write([]byte(c))
		user.Write(sc.Frame())
	}
	return screenLines(user), sc
}

// newTestTerminal 造一台「用户终端」，并排掉它对能力查询的回复（没人读会死锁）。
func newTestTerminal(t *testing.T, w, h int) *vt.Emulator {
	t.Helper()
	em := vt.NewEmulator(w, h)
	go func() { _, _ = io.Copy(io.Discard, em) }()
	return em
}

// TestEndToEndCollapses 差量重绘的工具块，在用户终端上必须真的变成一行折叠文案。
func TestEndToEndCollapses(t *testing.T) {
	got, sc := pipe(t, 100, 24, diffRepaint())
	joined := strings.Join(got, "\n")

	if sc.stats.Hits == 0 && sc.liveHits == 0 {
		t.Fatalf("一次都没折叠\n%s", joined)
	}
	for _, gone := range []string{"total 8", "+78 lines", "Bash("} {
		if strings.Contains(joined, gone) {
			t.Errorf("用户屏幕上不该还有 %q\n%s", gone, joined)
		}
	}
	if !strings.Contains(joined, "running") {
		t.Errorf("折叠行没出现在用户屏幕上\n%s", joined)
	}
	if !strings.Contains(joined, "答案在这里，必须活下来") {
		t.Errorf("正文被吃了\n%s", joined)
	}
}

// TestEndToEndKeepsHistory 锁住退行 bug：每帧擦活动区，绝不能把上面已落地的历史一起擦掉。
//
// 之前用「活动区有几行」当退行量，而收尾时光标停在输入框（不是最后一行），
// 于是每帧都往上多退几行，ESC[J 一路吃掉历史。屏幕上表现为内容凭空消失。
func TestEndToEndKeepsHistory(t *testing.T) {
	var chunks []string
	for i := 0; i < 40; i++ { // 逼出足量历史
		chunks = append(chunks, "history line "+itoa(i)+"\r\r\n")
	}
	// 再画一个把光标停在中间的输入框：光标不在最后一行，正是踩雷的形状
	chunks = append(chunks, "> prompt\r\r\n\x1b[1A\x1b[3G")
	for i := 0; i < 5; i++ { // 多跑几帧，退行错的话每帧吃掉一点
		chunks = append(chunks, "")
	}

	got, _ := pipe(t, 60, 10, chunks...)
	joined := strings.Join(got, "\n")
	for _, must := range []string{"history line 30", "history line 35", "history line 39"} {
		if !strings.Contains(joined, must) {
			t.Errorf("历史被自己擦掉了，%q 不见了\n%s", must, joined)
		}
	}
}

// TestEndToEndNoDuplicateHistory 已经落地的行不能因为重绘又出现一遍。
func TestEndToEndNoDuplicateHistory(t *testing.T) {
	chunks := []string{}
	for i := 0; i < 30; i++ {
		chunks = append(chunks, "unique-"+itoa(i)+"\r\r\n")
	}
	chunks = append(chunks, "", "", "") // 空帧：只重绘，不该多出内容
	got, _ := pipe(t, 60, 8, chunks...)

	seen := map[string]int{}
	for _, l := range got {
		if s := strings.TrimSpace(l); strings.HasPrefix(s, "unique-") {
			seen[s]++
		}
	}
	for line, n := range seen {
		if n > 1 {
			t.Errorf("%q 出现了 %d 次，重绘把历史又吐了一遍", line, n)
		}
	}
}

// TestEndToEndBurstKeepsEveryLine 一次灌进远超暂存区容量的行，一行都不能丢。
//
// 模拟器只替我们暂存「滚出屏幕、还没取走」的行，满了会淘汰最老的——而且是无声的。
// agent 输出一个大文件时一帧滚上千行是常事，所以 drive 会在攒满前提前画一帧。
// 这个用例直接走那条路径：不提前画就会丢中间那几百行。
func TestEndToEndBurstKeepsEveryLine(t *testing.T) {
	// 必须超过暂存区容量才谈得上淘汰。反证过：不提前画的话 8192 行只剩 4105 行。
	const n = scrollbackWindow * 2
	sc := newScreen(mustRules(t), 60, 10)
	go func() { _, _ = io.Copy(io.Discard, sc.em) }()
	user := newTestTerminal(t, 60, 10)
	user.Scrollback().SetMaxLines(n * 2) // 用户终端得存得下，否则测的是它的容量

	for i := 0; i < n; i++ {
		sc.Write([]byte("burst-" + itoa(i) + "\r\r\n"))
		if sc.PendingHistory() >= historyFlushAt { // 和 drive 里同一条规则
			user.Write(sc.Frame())
		}
	}
	user.Write(sc.Frame())

	seen := map[string]bool{}
	for _, l := range screenLines(user) {
		if s := strings.TrimSpace(l); strings.HasPrefix(s, "burst-") {
			seen[s] = true
		}
	}
	for i := 0; i < n-10; i++ { // 末尾几行还在活动区里，不计
		if !seen["burst-"+itoa(i)] {
			t.Fatalf("丢了 burst-%d（一共 %d 行，只收到 %d 行）", i, n, len(seen))
		}
	}
}

// TestEndToEndMergesToolRuns 连着调用同一个工具，屏幕上只占一行。
//
// agent 一口气跑七条 sed 时，七行「正在查找」没有信息量——知道它在跑 shell 就够了。
// 并出来的那行要带次数和最新在做什么：数字往上走，就说明它还在动。
func TestEndToEndMergesToolRuns(t *testing.T) {
	var frames []string
	for i := 0; i < 7; i++ {
		frames = append(frames, "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G\x1b[1mBash\x1b[22m(sed -n "+
			itoa(i)+",99p file"+itoa(i)+".go)\r\r\n  \x1b[2m⎿\x1b[22m output preview\r\r\n\r\r\n")
	}
	got, _ := pipe(t, 100, 30, frames...)

	runs := 0
	for _, l := range got {
		if strings.Contains(l, "running") {
			runs++
		}
	}
	if runs != 1 {
		t.Errorf("7 次连续 Bash 应该只占 1 行，实际 %d 行:\n%s", runs, strings.Join(got, "\n"))
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "×7") {
		t.Errorf("并出来的行要标次数，否则看不出它还在动:\n%s", joined)
	}
	if !strings.Contains(joined, "file6.go") {
		t.Errorf("要显示最新在做什么（file6.go），不是第一条:\n%s", joined)
	}
}

// TestEndToEndDifferentToolsStaySeparate 换了工具就得另起一行，否则等于把信息抹平。
func TestEndToEndDifferentToolsStaySeparate(t *testing.T) {
	head := func(tool, arg string) string {
		return "\x1b[2G\x1b[92m⏺\x1b[39m\x1b[4G\x1b[1m" + tool + "\x1b[22m(" + arg + ")\r\r\n"
	}
	got, _ := pipe(t, 100, 30, head("Bash", "ls"), head("Bash", "pwd"), head("Read", "a.go"), head("Bash", "date"))
	joined := strings.Join(got, "\n")
	for _, must := range []string{"running", "reading"} {
		if !strings.Contains(joined, must) {
			t.Errorf("少了 %q:\n%s", must, joined)
		}
	}
	if n := strings.Count(joined, "running"); n != 2 {
		t.Errorf("Bash 被 Read 隔开成两串，应有 2 行 running，实际 %d:\n%s", n, joined)
	}
}
