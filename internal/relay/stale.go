package relay

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// 升级 squint 之后，已经开着的窗口还在跑旧代码 —— 花屏十有八九是它们。
// 进程换不了自己的代码，只能重开窗口，所以至少要让人一眼看出该重开哪几个。

// StaleReport 列出还在跑旧版二进制的 squint 窗口。
func StaleReport() string {
	exe, err := os.Executable()
	if err != nil {
		return "squint: cannot locate own binary: " + err.Error() + "\n"
	}
	st, err := os.Stat(exe)
	if err != nil {
		return "squint: cannot stat own binary: " + err.Error() + "\n"
	}
	built := st.ModTime()

	procs, err := runningSquints()
	if err != nil {
		return "squint: cannot list processes: " + err.Error() + "\n"
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].started.Before(procs[j].started) })

	var sb strings.Builder
	fmt.Fprintf(&sb, "binary on disk: %s (built %s)\n", exe, built.Format("01-02 15:04"))
	self := os.Getpid()
	var stale, fresh int
	for _, p := range procs {
		if p.pid == self {
			continue // --stale 自己不算窗口
		}
		// 给 2s 容差：进程和二进制同一秒落地时，先后顺序没有意义
		if p.started.Add(2 * time.Second).Before(built) {
			stale++
			fmt.Fprintf(&sb, "  ⚠️  pid %-6d %-9s started %s, running an older build: reopen this window\n",
				p.pid, p.tty, p.started.Format("01-02 15:04"))
			continue
		}
		fresh++
	}
	if stale == 0 {
		fmt.Fprintf(&sb, "  ✅ all %d windows are up to date\n", fresh)
		return sb.String()
	}
	fmt.Fprintf(&sb, "%d windows: %d to reopen, %d up to date\n", stale+fresh, stale, fresh)
	return sb.String()
}

// SelfStaleNote 这个进程启动之后二进制又被换过的话，返回一句提示。
// 进程没法给自己换代码，只能在退出时提醒一句「重开就生效」——正好是要重开的时机。
func SelfStaleNote(started time.Time) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	st, err := os.Stat(exe)
	if err != nil || !started.Add(2*time.Second).Before(st.ModTime()) {
		return ""
	}
	return fmt.Sprintf("squint: this window runs the build from %s, the binary on disk is from %s. Reopen the window to pick it up (squint --stale lists the others)",
		started.Format("01-02 15:04"), st.ModTime().Format("01-02 15:04"))
}

type squintProc struct {
	pid     int
	tty     string
	started time.Time
}

// runningSquints 用 ps 拿 relay 模式的 squint 进程。lstart 是「Tue Aug 25 20:24:08 2026」这种格式。
func runningSquints() ([]squintProc, error) {
	out, err := exec.Command("ps", "-eo", "pid=,tty=,lstart=,command=").Output()
	if err != nil {
		return nil, err
	}
	var procs []squintProc
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "squint") || !strings.Contains(line, "--relay") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(f[0], "%d", &pid); err != nil {
			continue
		}
		started, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.Join(f[2:7], " "), time.Local)
		if err != nil {
			continue
		}
		procs = append(procs, squintProc{pid: pid, tty: f[1], started: started})
	}
	return procs, nil
}
