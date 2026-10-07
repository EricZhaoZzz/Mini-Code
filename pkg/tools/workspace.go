package tools

import (
	"fmt"
	"os"
	"path/filepath"
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

	return resolvedCandidate, nil
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
