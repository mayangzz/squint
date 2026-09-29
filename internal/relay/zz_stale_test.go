package relay

import (
	"strings"
	"testing"
)

// ps 的 lstart 是「Tue Aug 25 20:24:08 2026」这种格式，解析歪了整个判定就没意义。
func TestRunningSquintsParsesPS(t *testing.T) {
	procs, err := runningSquints()
	if err != nil {
		t.Skipf("环境里跑不了 ps: %v", err)
	}
	for _, p := range procs {
		if p.pid <= 0 || p.started.IsZero() {
			t.Errorf("解析出无效进程: %+v", p)
		}
	}
}

func TestStaleReportShape(t *testing.T) {
	out := StaleReport()
	if !strings.Contains(out, "binary on disk") {
		t.Errorf("报告缺少二进制信息:\n%s", out)
	}
}
