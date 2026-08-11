package source

import (
	"io"
	"strings"
)

// stringReader 把 prompt 从 stdin 喂给子进程，避开超长命令行参数和引号转义。
func stringReader(s string) io.Reader { return strings.NewReader(s) }
