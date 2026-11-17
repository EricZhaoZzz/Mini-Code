package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	// rootOnce 保证根目录只解析一次
	rootOnce sync.Once
	// fixedRoot 是显式设置的工作区根目录（优先于 CWD）
	fixedRoot string
	// rootErr 记录首次解析时的错误
	rootErr error
)

// SetWorkspaceRoot 显式固定工作区根目录。
//
// 必须在进程启动时调用一次（cmd/agent 已接入）。不调用时退化为
// 「首次调用时的 os.Getwd()」。
//
// 为什么要固定：workspaceRoot 决定所有文件工具的安全边界，
// 若每次都取 os.Getwd()，任何 chdir（子进程、或库内部行为）都会
// 静默移动边界——实测 chdir 到用户主目录后「工作区」就变成了主目录。
func SetWorkspaceRoot(root string) error {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return fmt.Errorf("工作区根目录不能为空")
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return fmt.Errorf("解析工作区路径失败: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	fixedRoot = filepath.Clean(abs)
	// 重置缓存，使新的固定值立即生效
	rootOnce = sync.Once{}
	rootErr = nil
	return nil
}

// workspaceRoot 返回固定的工作区根目录。
// 固定后不再随进程 CWD 变化，避免安全边界漂移。
func workspaceRoot() (string, error) {
	rootOnce.Do(func() {
		if fixedRoot != "" {
			return
		}
		cwd, err := os.Getwd()
		if err != nil {
			rootErr = fmt.Errorf("获取工作区失败: %w", err)
			return
		}
		resolved, err := filepath.EvalSymlinks(cwd)
		if err == nil {
			cwd = resolved
		}
		fixedRoot = filepath.Clean(cwd)
	})

	if rootErr != nil {
		return "", rootErr
	}
	return fixedRoot, nil
}

func resolveWorkspacePath(path string) (string, error) {
	root, err := workspaceRoot()
	if err != nil {
		return "", err
	}

	candidate := strings.TrimSpace(path)
	if candidate == "" {
		candidate = "."
	}

	// 前置校验：拒绝带特殊语义、可绕过审计的路径形式（见 checkDangerousPath）
	if err := checkDangerousPath(candidate); err != nil {
		return "", err
	}

	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}

	candidate = filepath.Clean(candidate)

	resolvedCandidate, err := resolvePathWithExistingParents(candidate)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %w", err)
	}

	rel, err := filepath.Rel(root, resolvedCandidate)
	if err != nil {
		return "", fmt.Errorf("计算相对路径失败: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径超出工作区: %s", path)
	}

	// 字符串校验通过后，再做句柄级校验。
	//
	// 为什么必须多这一步：NTFS junction 是 reparse point 而非符号链接，
	// os.Lstat 返回的 Mode 不含 ModeSymlink，EvalSymlinks 又无法解析
	// junction 内部的文件，因此字符串层面的 Rel 判断会把它当成普通子目录放行，
	// 而实际 os.ReadFile/WriteFile 时 NTFS 会跟随 junction 到达工作区外。
	escapes, err := pathEscapesRoot(root, resolvedCandidate)
	if err != nil || escapes {
		return "", fmt.Errorf("路径超出工作区: %s", path)
	}

	return resolvedCandidate, nil
}

// checkDangerousPath 拒绝带特殊语义的危险路径形式。
//
// 每一项都有明确定义的危险语义，不是「看起来可疑」：
//   - NUL 字节：Go 的 os 包在 NUL 处截断，会使校验路径与实际访问路径不一致
//   - UNC（\\server\share）：跨网络边界，Rel 的 volume 判断可能失效
//   - device（\\.\PhysicalDrive0）：直接访问设备，绕过一切文件语义
//   - 冒号（ADS）：file.txt:stream 是隐藏数据流，ListFiles 不会列出，可绕过内容审计
//
// 注意 `\\?\` 是例外：它是 Windows 的扩展长度前缀，语义等同普通绝对路径，
// 必须放行——否则会误杀合法的超长路径（GetFinalPathNameByHandle 也返回这种形式）。
func checkDangerousPath(path string) error {
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("路径不允许包含 NUL 字符")
	}

	candidate := path
	if runtime.GOOS == "windows" && strings.HasPrefix(candidate, `\\?\`) {
		candidate = candidate[4:]
	}

	if strings.HasPrefix(candidate, `\\`) || strings.HasPrefix(candidate, "//") {
		return fmt.Errorf("路径不允许使用 UNC 或设备路径前缀")
	}

	// Windows 上冒号只用于盘符与 ADS。盘符形如 C:\，冒号下标为 1；
	// 其它位置的冒号一律视为试图访问备用数据流。
	if runtime.GOOS == "windows" {
		if idx := strings.Index(candidate, ":"); idx > 1 {
			return fmt.Errorf("路径不允许包含冒号（可能试图访问 NTFS 备用数据流）")
		}
	}

	return nil
}

// isWithinRoot 判断 path 是否位于 root 之内（含 root 自身）。
func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func displayPath(path string) string {
	root, err := workspaceRoot()
	if err != nil {
		return filepath.Clean(path)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	if rel == "." {
		return "."
	}

	return rel
}

func resolvePathWithExistingParents(path string) (string, error) {
	current := filepath.Clean(path)
	var missingParts []string

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missingParts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missingParts[i])
			}
			return filepath.Clean(resolved), nil
		}

		// 失败降级，使用原始路径继续
		// 如果路径不存在或解析失败，都尝试向上查找已存在的父路径
		parent := filepath.Dir(current)
		if parent == current {
			// 已到达根路径，使用原始路径
			for i := len(missingParts) - 1; i >= 0; i-- {
				current = filepath.Join(current, missingParts[i])
			}
			return filepath.Clean(current), nil
		}

		missingParts = append(missingParts, filepath.Base(current))
		current = parent
	}
}
