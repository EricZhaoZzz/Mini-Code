//go:build windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// pathEscapesRoot 判断路径是否逃出工作区根目录（Windows 实现）。
//
// 两道防线，均经本机实测验证：
//
//  1. 逐组件 reparse point 检查。
//     junction 是 FILE_ATTRIBUTE_REPARSE_POINT，不是符号链接——
//     os.Lstat 返回的 Mode 不含 ModeSymlink，filepath.EvalSymlinks
//     也无法解析 junction 内部的文件，所以「Lstat 检查符号链接」这种
//     常见加固在这里完全失效。必须查 reparse point 属性。
//
//  2. 句柄真实路径权威判定。
//     GetFinalPathNameByHandle 返回 junction 解析后的规范路径，
//     因此即使校验与访问之间发生 TOCTOU 竞态，也无法落到工作区外。
//
// 目标不存在时（如 write_file 创建新文件）无法开句柄，
// 退化为字符串比较——这时由第一道防线覆盖父目录是 junction 的情形。
func pathEscapesRoot(root, resolved string) (bool, error) {
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return true, nil // Rel 失败（如 volume 不匹配）视为越界
	}

	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)

		isReparse, err := isReparsePoint(current)
		if err != nil {
			// 路径不存在等情况交由后续处理，不在此处判越界
			continue
		}
		if !isReparse {
			continue
		}

		// reparse point 存在时，必须确认它指向哪里
		target, terr := resolveReparseTarget(current)
		if terr != nil {
			return true, nil // 无法确认目标 → 保守拒绝
		}
		if !isWithinRoot(root, target) {
			return true, nil
		}
	}

	// 第二道：句柄真实路径权威判定
	return !handleConfirmsInsideRoot(root, resolved), nil
}

// isReparsePoint 判断路径是否为 reparse point（junction / symlink / mount point）。
func isReparsePoint(path string) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false, err
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

// resolveReparseTarget 取 reparse point 的真实目标。
func resolveReparseTarget(path string) (string, error) {
	target, err := finalPathByHandle(path)
	if err != nil {
		return "", err
	}
	return normalizeWindowsPath(target), nil
}

// handleConfirmsInsideRoot 用句柄真实路径确认仍在 root 内。
func handleConfirmsInsideRoot(root, path string) bool {
	final, err := finalPathByHandle(path)
	if err != nil {
		return isWithinRoot(root, path)
	}
	return isWithinRoot(root, normalizeWindowsPath(final))
}

// finalPathByHandle 打开路径并返回其真实规范路径。
func finalPathByHandle(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	// FILE_FLAG_BACKUP_SEMANTICS 允许对目录（及 junction）打开句柄。
	// 不指定 FILE_FLAG_OPEN_REPARSE_POINT，从而让 NTFS 跟随链接，
	// 拿到的句柄指向真正的目标。
	handle, err := windows.CreateFile(
		p,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("打开路径句柄失败: %w", err)
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", fmt.Errorf("取真实路径失败: %w", err)
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// normalizeWindowsPath 去掉 GetFinalPathNameByHandle 返回的 \\?\ 前缀。
func normalizeWindowsPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		p = p[4:]
	}
	// UNC 形式（\\?\UNC\server\share）还原为 \\server\share，
	// 使其无法落入任何本地 root
	if strings.HasPrefix(p, `UNC\`) {
		return `\\` + p[4:]
	}
	return filepath.Clean(p)
}
