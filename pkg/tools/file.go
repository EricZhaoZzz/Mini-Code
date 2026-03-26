package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type WriteFileAugments struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func WriteFile(args interface{}) (interface{}, error) {
	var params WriteFileAugments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
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
	var stdout, stderr strings.Builder
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
