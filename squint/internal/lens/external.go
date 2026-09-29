package lens

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mayangzz/squint/internal/event"
)

// external 把镜片外包给一个子进程：squint 每行喂一个 JSON 事件到它的 stdin，
// 它往 stdout 打什么就显示什么（不打就是不显示）。
//
// 这样「自定义看什么」不绑语言、不用重编译 squint：丢个可执行文件到
// ~/.squint/lenses/ 就是一种新看法。
type external struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
	dead bool
}

// lensDir 是外部镜片的存放目录。
func lensDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home dir for ~/.squint/lenses: %w", err)
	}
	return filepath.Join(home, ".squint", "lenses"), nil
}

func newExternal(name string) (*external, error) {
	// 镜片名要拼进路径，`../..` 会逃出 lenses 目录。本地工具算不上提权，但名字也可能
	// 来自 config.json 或别人写的封装脚本，三行就能堵住。
	if name != filepath.Base(name) || name == "." || name == ".." {
		return nil, fmt.Errorf("lens name %q must not contain a path separator", name)
	}
	dir, err := lensDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no builtin lens and no executable at %s", path)
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not an executable file (chmod +x it?)", path)
	}
	c := exec.Command(path)
	in, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &external{cmd: c, in: in, out: bufio.NewScanner(out)}, nil
}

func (e *external) View(ev event.Event) (string, bool) {
	if e.dead {
		return "", false
	}
	line, _ := json.Marshal(ev)
	if _, err := fmt.Fprintf(e.in, "%s\n", line); err != nil {
		e.dead = true
		return "", false
	}
	// 约定：一个事件回一行。空行 = 不显示；`\n` 转义还原成真换行，方便外部脚本出多行。
	if !e.out.Scan() {
		e.dead = true
		return "", false
	}
	s := e.out.Text()
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return strings.ReplaceAll(s, `\n`, "\n"), true
}

// Close 收尾，让子进程正常退出。
func (e *external) Close() {
	if e.in != nil {
		_ = e.in.Close()
	}
	if e.cmd != nil {
		_ = e.cmd.Wait()
	}
}
