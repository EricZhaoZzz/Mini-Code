package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type WriteFileArguments struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func WriteFile(args interface{}) (interface{}, error) {
	var params WriteFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	if dir := filepath.Dir(params.Path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目录失败: %w", err)
		}
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	return fmt.Sprintf("文件已写入: %s", params.Path), nil
}

type ReadFileArguments struct {
	Path string `json:"path"`
}

func ReadFile(args interface{}) (interface{}, error) {
	var params ReadFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	return string(data), nil
}

type ListFilesArguments struct {
	Path string `json:"path"`
}

func ListFiles(args interface{}) (interface{}, error) {
	var params ListFilesArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	root := strings.TrimSpace(params.Path)
	if root == "" {
		root = "."
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("列出文件失败: %w", err)
	}

	if len(files) == 0 {
		return "未找到任何文件", nil
	}

	return strings.Join(files, "\n"), nil
}

type SearchInFilesArguments struct {
	Path  string `json:"path"`
	Query string `json:"query"`
}

func SearchInFiles(args interface{}) (interface{}, error) {
	var params SearchInFilesArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	root := strings.TrimSpace(params.Path)
	if root == "" {
		root = "."
	}

	query := strings.TrimSpace(params.Query)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}

	var matches []string
	const limit = 50

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if shouldSkipSearchFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, query) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
				if len(matches) >= limit {
					return fs.SkipAll
				}
			}
		}

		return nil
	})
	if err != nil && err != fs.SkipAll {
		return nil, fmt.Errorf("搜索文件失败: %w", err)
	}

	if len(matches) == 0 {
		return "未找到匹配内容", nil
	}

	return strings.Join(matches, "\n"), nil
}

func shouldSkipSearchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".exe", ".dll", ".so":
		return true
	default:
		return false
	}
}

type RunShellArguments struct {
	Command string `json:"command"`
}

func RunShell(args interface{}) (interface{}, error) {
	var params RunShellArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", params.Command)
	} else {
		cmd = exec.Command("sh", "-c", params.Command)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n[stderr]: " + stderr.String()
	}
	if err != nil {
		return output, fmt.Errorf("命令执行错误: %w", err)
	}

	return output, nil
}

func parseArgs(args interface{}, out interface{}) error {
	switch v := args.(type) {
	case string:
		return json.Unmarshal([]byte(v), out)
	case []byte:
		return json.Unmarshal(v, out)
	default:
		data, err := json.Marshal(args)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, out)
	}
}
