// squint —— 眯着眼看 agent 干活：细节糊掉，只留你想看的那部分。
//
// 它不改 agent 怎么跑，只换一层显示。agent 后端（source）和看法（lens）各自可插拔，
// 用一个配置开关选用哪个。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mayangzz/squint/internal/event"
	"github.com/mayangzz/squint/internal/lens"
	"github.com/mayangzz/squint/internal/relay"
	"github.com/mayangzz/squint/internal/source"
)

// exitCode 由 run 结束时的 os.Exit 消费；不能在中途 os.Exit，否则 --save 的 defer Close 不跑。
var exitCode int

func main() {
	defer func() { os.Exit(exitCode) }()
	cfg := loadConfig()
	var (
		lensName = flag.String("lens", cfg.Lens, "what to show: "+strings.Join(lens.Names(), " / ")+", or an executable in ~/.squint/lenses/")
		srcName  = flag.String("source", cfg.Source, "agent backend: "+source.Names())
		model    = flag.String("model", cfg.Model, "model; empty uses the backend default")
		replay   = flag.String("replay", "", "re-render a saved stream-json file instead of running the agent")
		save     = flag.String("save", "", "also write the raw event stream to this file")
	)
	relayMode := flag.Bool("relay", false, "relay mode: run the interactive agent in a PTY and collapse its tool blocks")
	check := flag.Bool("check", false, "self-check the collapse rules against a real sample")
	flag.Parse()

	if *check {
		report, ok := relay.Check()
		fmt.Print(report)
		if !ok {
			os.Exit(1)
		}
		return
	}

	if *relayMode {
		// 位置参数就是要包住的命令行：`squint --relay claude --resume` → 跑 `claude --resume`。
		// 一个都不给就用配置里的后端名。
		name, argv := *srcName, flag.Args()
		if len(argv) > 0 {
			name, argv = argv[0], argv[1:]
		}
		code, err := relay.Run(name, argv)
		if err != nil {
			die(err)
		}
		os.Exit(code)
	}

	l, err := lens.New(*lensName)
	if err != nil {
		die(err)
	}
	src, err := source.Get(*srcName)
	if err != nil {
		die(err)
	}

	stream, wait, err := open(src, *replay, *model, strings.Join(flag.Args(), " "))
	if err != nil {
		die(err)
	}
	if *save != "" {
		f, err := os.Create(*save)
		if err != nil {
			die(err)
		}
		defer func() {
			if err := f.Close(); err != nil { // 磁盘满会静默截断归档
				fmt.Fprintln(os.Stderr, "squint: save:", err)
			}
		}()
		stream = io.TeeReader(stream, f)
	}

	events := make(chan event.Event, 64)
	go src.Parse(stream, events)
	for ev := range events {
		if out, ok := l.View(ev); ok {
			fmt.Println(out)
		}
	}
	if c, ok := l.(interface{ Close() }); ok {
		c.Close()
	}
	if wait != nil {
		if err := wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exitCode = ee.ExitCode() // relay 路径会透传退出码，headless 也得一致
			} else {
				exitCode = 1
			}
		}
	}
}

// open 要么回放文件，要么起 agent 子进程；两条路都返回一个可读的事件流。
func open(src source.Source, replay, model, prompt string) (io.Reader, func() error, error) {
	if replay != "" {
		f, err := os.Open(replay)
		return f, nil, err
	}
	if prompt == "" {
		return nil, nil, fmt.Errorf("give a prompt, or use --replay to re-render a saved run; see `squint -h`")
	}
	cmd := src.Cmd(prompt, model, nil)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return out, cmd.Wait, nil
}

// config 是 ~/.squint/config.json。字段都有合理默认，没这个文件也能跑。
type config struct {
	Lens   string `json:"lens"`
	Source string `json:"source"`
	Model  string `json:"model"`
}

func loadConfig() config {
	c := config{Lens: "minimal", Source: "claude"}
	home, err := os.UserHomeDir()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(home + "/.squint/config.json")
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c) // 配置坏了就用默认值，不该因为一个可选文件起不来
	return c
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "squint:", err)
	os.Exit(1)
}
