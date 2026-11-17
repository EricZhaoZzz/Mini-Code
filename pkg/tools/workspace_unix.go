//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// pathEscapesRoot 判断路径是否逃出工作区根目录（Unix 实现）。
//
// Unix 上 filepath.EvalSymlinks 能正确解析符号链接，
// 因此这里只需处理 EvalSymlinks 失败时的降级路径是否安全。
func pathEscapesRoot(root, resolved string) (bool, error) {
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return true, nil // volume 不匹配等 → 视为越界
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)), nil
}
