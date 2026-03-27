package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func workspaceRoot() (string, error) {
	root, err := os.Getwd()

	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = resolvedRoot
	}

	return filepath.Clean(root), nil
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
