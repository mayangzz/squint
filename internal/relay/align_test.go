package relay

import (
	"io"
	"os"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// TestCollapsedRowFitsWidth 折叠行宽出去一格，真终端就会折行——那一行于是占两行，
// 而退行量是按行数算的，从此每帧擦错地方。窄终端里 ×n 后缀尤其容易顶出去。
func TestCollapsedRowFitsWidth(t *testing.T) {
	rules := mustRules(t)
	long := strings.Repeat("sed -n 1,200p internal/relay/screen.go; ", 8)
	for _, w := range []int{40, 60, 72, 80, 100, 196} {
		f := newFilter(rules, w)
		d := f.Judge("⏺ Bash(" + long + ")")
		if got := ansi.StringWidth(d.Text); got > w {
			t.Errorf("w=%d: 折叠行宽 %d，超了 %d 格\n%q", w, got, got-w, d.Text)
		}
		if got := ansi.StringWidth(f.runLine(d.Tool, d.Arg, 12)); got > w {
			t.Errorf("w=%d: ×12 的合并行宽 %d，超了 %d 格", w, got, got-w)
		}
	}
}

// TestUserScreenMatchesIntent 每画完一帧，用户终端上活动区那几行必须逐行等于
// squint 打算画的那几行（screen.prevRows）。
//
// 这是「错位重叠」唯一测得出来的地方：光标算术一旦跟真屏幕对不上，squint 自己
// 察觉不到——它照着自己的模型继续画，屏幕上却是新内容压在旧内容上。
// 想验自己那场会话：SQUINT_CAPTURE=/tmp/x.raw 跑一次，再
// SQUINT_TEST_CAPTURE=/tmp/x.raw go test ./internal/relay -run Intent。
func TestUserScreenMatchesIntent(t *testing.T) {
	path := realCapture
	if p := os.Getenv("SQUINT_TEST_CAPTURE"); p != "" {
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rules := mustRules(t)
	for _, w := range []int{60, 80, 120, 196} {
		for _, chunk := range []int{512, 4096, 32768} {
			sc := newScreen(rules, w, 40)
			user := vt.NewEmulator(w, 40)
			go func() { _, _ = io.Copy(io.Discard, sc.em) }()
			go func() { _, _ = io.Copy(io.Discard, user) }()

			for off := 0; off < len(data); off += chunk {
				end := min(off+chunk, len(data))
				sc.Write(data[off:end])
				_, _ = user.Write(sc.Frame())

				top := user.CursorPosition().Y - sc.cursorRow
				for i, row := range sc.prevRows {
					y := top + i
					if y < 0 || y >= user.Height() {
						continue // 已经滚出屏幕的行没法比
					}
					want := normalize(ansi.Strip(row))
					if got := normalize(userRow(user, y)); got != want {
						t.Fatalf("w=%d chunk=%d: 第 %d 行画到屏幕上不是我们要的\n want=%q\n got =%q",
							w, chunk, i, want, got)
					}
				}
			}
		}
	}
}

func userRow(em *vt.Emulator, y int) string {
	line := make(uv.Line, em.Width())
	for x := 0; x < em.Width(); x++ {
		if c := em.CellAt(x, y); c != nil {
			line[x] = *c
		}
	}
	return lineText(line)
}

// normalize 对齐 lineText 的口径：NBSP 当空格，尾部空白不计。
func normalize(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, " ", " "), " \t")
}

// TestHidesInputHint 输入框里光标之后的暗色文字（CC 的输入提示 / 历史补全）不该画出来，
// 光标之前自己敲的字必须原样留着。
func TestHidesInputHint(t *testing.T) {
	// 真实形状（抓包里抠的）：自己敲的字是常色，补全提示是 SGR 2，光标停在两者之间
	const input = "❯ 我敲的字\x1b[2m剩下这截是补全提示\x1b[22m\x1b[2;13H"
	got, _ := pipe(t, 60, 6, "上一条正文\r\r\n", input)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "剩下这截是补全提示") {
		t.Errorf("输入提示没被抹掉:\n%s", joined)
	}
	if !strings.Contains(joined, "我敲的字") || !strings.Contains(joined, "上一条正文") {
		t.Errorf("自己敲的字被误伤:\n%s", joined)
	}
}

// TestHidesColoredInputHint CC 有时不用 SGR 2、只给提示一个灰前景色，这种也得抹掉，
// 否则它就压在自己敲的字后面（只在输入行这么判，别的行的彩色正文不许动）。
func TestHidesColoredInputHint(t *testing.T) {
	const input = "❯ 我敲的字\x1b[38;5;242m剩下这截是补全提示\x1b[39m\x1b[2;13H"
	got, _ := pipe(t, 60, 6, "上一条正文\r\r\n", input)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "剩下这截是补全提示") {
		t.Errorf("灰色输入提示没被抹掉:\n%s", joined)
	}
	if !strings.Contains(joined, "我敲的字") {
		t.Errorf("自己敲的字被误伤:\n%s", joined)
	}
}

// TestHidesHintWhenCursorParkedElsewhere CC 收尾常把光标挪回别处，
// 按光标列切会整条漏掉提示 —— 输入行要从「敲的最后一个字」之后开始抹。
func TestHidesHintWhenCursorParkedElsewhere(t *testing.T) {
	const input = "❯ 我敲的字\x1b[38;5;242m剩下这截是补全提示\x1b[39m\x1b[2;40H"
	got, _ := pipe(t, 60, 6, "上一条正文\r\r\n", input)
	if joined := strings.Join(got, "\n"); strings.Contains(joined, "剩下这截是补全提示") {
		t.Errorf("光标不在字尾时提示没被抹掉:\n%s", joined)
	}
}

// TestKeepsColoredMentionInInput 输入行里自己敲的 @提及 / 斜杠命令是彩色（非灰），不能当提示抹掉。
func TestKeepsColoredMentionInInput(t *testing.T) {
	const input = "❯ 看下 \x1b[38;5;39m@main.go\x1b[39m\x1b[2;8H"
	got, _ := pipe(t, 60, 6, "上一条正文\r\r\n", input)
	if joined := strings.Join(got, "\n"); !strings.Contains(joined, "@main.go") {
		t.Errorf("彩色 @提及 被误伤:\n%s", joined)
	}
}

// TestKeepsColoredTextOnNonInputRow 光标停在正文行时，后面的彩色输出不能被当提示抹掉。
func TestKeepsColoredTextOnNonInputRow(t *testing.T) {
	const line = "正文开头\x1b[38;5;242m后面这截是正文\x1b[39m\x1b[1;9H"
	got, _ := pipe(t, 60, 6, line)
	if joined := strings.Join(got, "\n"); !strings.Contains(joined, "后面这截是正文") {
		t.Errorf("正文行的彩色内容被误伤:\n%s", joined)
	}
}

// TestResizeForcesRepaint 改窗口大小后必须整片重画：留着旧宽度算出来的 prevRows 去做
// 差量比对，会把「其实已经被终端重排」的行当成没变而跳过，屏幕上就是新旧两版叠在一起。
func TestResizeForcesRepaint(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 6)
	sc.Write([]byte("第一行内容\r\n第二行内容\r\n"))
	_ = sc.Frame()
	if sc.prevRows == nil {
		t.Fatal("正常帧之后应该记下 prevRows")
	}
	sc.Resize(40, 6)
	if sc.prevRows != nil {
		t.Error("Resize 后必须丢掉 prevRows，否则下一帧会走差量比对")
	}
	// 重画那一帧要带整片擦除（\x1b[J），不能只改几行
	if out := string(sc.Frame()); !strings.Contains(out, "\x1b[J") {
		t.Errorf("Resize 后的第一帧应整片重画:\n%q", out)
	}
}

// TestDiffRunSelfHeals 差量重绘连着走太久就该强制整片重画一次：错位一旦发生，
// 只靠差量永远回不来，这条保险丝保证最多烂几百毫秒。
func TestDiffRunSelfHeals(t *testing.T) {
	sc := newScreen(mustRules(t), 60, 6)
	sc.Write([]byte("一行内容\r\n"))
	_ = sc.Frame()
	full := 0
	for i := 0; i < maxDiffRun+5; i++ {
		sc.Write([]byte("\x1b[1;1H一行内容")) // 每帧原地重写，行数不变 → 本该一直走差量
		if strings.Contains(string(sc.Frame()), "\x1b[J") {
			full++
		}
	}
	if full == 0 {
		t.Errorf("连着 %d 帧差量后应该强制整片重画一次，实际一次都没有", maxDiffRun)
	}
}
