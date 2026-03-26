package tools

import (
	"fmt"
	"os"
	"strings"
)

type ReplaceInFileArguments struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

func ReplaceInFile(args interface{}) (interface{}, error) {
	var params ReplaceInFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	if strings.TrimSpace(params.Path) == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	if params.Old == "" {
		return nil, fmt.Errorf("old 不能为空")
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, params.Old) {
		return nil, fmt.Errorf("未找到要替换的内容")
	}

	updated := strings.Replace(content, params.Old, params.New, 1)
	if err := os.WriteFile(targetPath, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("写回文件失败: %w", err)
	}

	return fmt.Sprintf("已替换文件内容: %s", displayPath(targetPath)), nil
}
