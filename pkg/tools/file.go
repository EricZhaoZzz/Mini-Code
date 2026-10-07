package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"
	gitignore "github.com/denormal/go-gitignore"
	"github.com/gabriel-vasile/mimetype"
	"github.com/go-cmd/cmd"
)

type WriteFileArguments struct {
	Path    string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要写入的工作区内相对路径"`
	Content string `json:"content" jsonschema:"required" jsonschema_description:"文件完整内容"`
}

func WriteFile(args interface{}) (interface{}, error) {
	var params WriteFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	if dir := filepath.Dir(targetPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目录失败: %w", err)
		}
	}

	if err := writeFileAtomically(targetPath, []byte(params.Content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	return fmt.Sprintf("文件已写入: %s", displayPath(targetPath)), nil
}

type ReadFileArguments struct {
	Path string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要读取的工作区内相对路径"`
}

func ReadFile(args interface{}) (interface{}, error) {
	var params ReadFileArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	targetPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	if isBinary, err := isBinaryFile(targetPath); err == nil && isBinary {
		return nil, fmt.Errorf("不支持读取二进制文件: %s", displayPath(targetPath))
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	return string(data), nil
}

type ListFilesArguments struct {
	Path string `json:"path" jsonschema_description:"要列出的目录路径，留空表示当前工作区"`
}

func ListFiles(args interface{}) (interface{}, error) {
	var params ListFilesArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	rootPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	workspacePath, err := workspaceRoot()
	if err != nil {
		return nil, err
	}

	ignoreMatcher := newRepositoryIgnoreMatcher(workspacePath)
	var (
		files []string
		mu    sync.Mutex
	)

	err = walkWorkspace(rootPath, ignoreMatcher, func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}

		mu.Lock()
		files = append(files, displayPath(path))
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("列出文件失败: %w", err)
	}

	sort.Strings(files)
	if len(files) == 0 {
		return "未找到任何文件", nil
	}

	return strings.Join(files, "\n"), nil
}

type SearchInFilesArguments struct {
	Path  string `json:"path" jsonschema_description:"要搜索的目录路径，留空表示当前工作区"`
	Query string `json:"query" validate:"required" jsonschema:"required" jsonschema_description:"要搜索的文本内容"`
}

func SearchInFiles(args interface{}) (interface{}, error) {
	var params SearchInFilesArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	rootPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(params.Query)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}

	workspacePath, err := workspaceRoot()
	if err != nil {
		return nil, err
	}

	ignoreMatcher := newRepositoryIgnoreMatcher(workspacePath)
	const limit = 50

	var (
		matches []string
		mu      sync.Mutex
		stopped atomic.Bool
	)

	err = walkWorkspace(rootPath, ignoreMatcher, func(path string, d fs.DirEntry) error {
		if stopped.Load() {
			return fs.SkipAll
		}
		if d.IsDir() || shouldSkipSearchFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		localMatches := make([]string, 0, 4)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, query) {
				localMatches = append(localMatches, fmt.Sprintf("%s:%d: %s", displayPath(path), i+1, strings.TrimSpace(line)))
			}
		}
		if len(localMatches) == 0 {
			return nil
		}

		mu.Lock()
		defer mu.Unlock()

		if stopped.Load() {
			return fs.SkipAll
		}

		remaining := limit - len(matches)
		if remaining <= 0 {
			stopped.Store(true)
			return fs.SkipAll
		}
		if len(localMatches) > remaining {
			localMatches = localMatches[:remaining]
		}
		matches = append(matches, localMatches...)
		if len(matches) >= limit {
			stopped.Store(true)
			return fs.SkipAll
		}

		return nil
	})
	if err != nil && err != fs.SkipAll {
		return nil, fmt.Errorf("搜索文件失败: %w", err)
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return "未找到匹配内容", nil
	}

	return strings.Join(matches, "\n"), nil
}

func shouldSkipSearchFile(path string) bool {
	isBinary, err := isBinaryFile(path)
	return err == nil && isBinary
}

type RunShellArguments struct {
	Command string `json:"command" validate:"required" jsonschema:"required" jsonschema_description:"要执行的 shell 命令"`
}

func RunShell(args interface{}) (interface{}, error) {
	var params RunShellArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	command := strings.TrimSpace(params.Command)
	if result, handled, err := handleRestartCommand(command); handled {
		if result == "" && err == nil {
			return "", fmt.Errorf("重启处理器未初始化")
		}
		return result, err
	}

	// 安全策略：黑名单常开，LM_SHELL_STRICT=1 时切换为白名单。
	// 注意这只能防误操作，run_shell 本身不限制路径（见 shell_guard.go 说明）。
	if err := CheckShellCommand(command); err != nil {
		return nil, err
	}

	root, err := workspaceRoot()
	if err != nil {
		return nil, err
	}

	// 超时保护：防止长驻命令卡死整个会话。
	//
	// go-cmd v1.4.3 不支持 CommandContext（设置 exec.Cmd.Cancel 会直接报错），
	// 且默认不为子进程创建独立进程组——其 Stop() 会 taskkill /T 整个进程树，
	// 可能连带杀掉调用方自身的进程。因此这里显式要求独立进程组，
	// 让超时终止只影响这条命令，不波及其他进程。
	timeout := shellTimeout()
	options := cmd.Options{
		Buffered:       true,
		CombinedOutput: false,
		BeforeExec: []func(*exec.Cmd){
			func(execCmd *exec.Cmd) {
				execCmd.Dir = root
				isolateProcessGroup(execCmd)
			},
		},
	}

	var runner *cmd.Cmd
	if runtime.GOOS == "windows" {
		runner = cmd.NewCmdOptions(options, "cmd", "/C", command)
	} else {
		runner = cmd.NewCmdOptions(options, "sh", "-c", command)
	}

	statusChan := runner.Start()

	// 等待结果：正常结束，或超时后返回已累积的部分输出。
	// 即便终止失败也不能无限期阻塞，因此这里用 select 而不是裸接收。
	var status cmd.Status
	var timedOut bool
	select {
	case status = <-statusChan:
	case <-time.After(timeout):
		timedOut = true
		// 尽力终止，但不阻塞等待它真的结束
		go func() {
			_ = terminateRunner(runner)
		}()
		status = runner.Status()
	}

	output := strings.Join(status.Stdout, "\n")
	if len(status.Stderr) > 0 {
		stderrText := strings.Join(status.Stderr, "\n")
		if output != "" {
			output += "\n"
		}
		output += "[stderr]: " + stderrText
	}

	// 超时优先于 exit code 报告，避免把超时误报成普通失败
	if timedOut {
		if output == "" {
			output = "(命令超时，未产生输出)"
		}
		return output, fmt.Errorf("命令执行超时（超过 %s），已终止", timeout)
	}
	if errors.Is(status.Error, context.DeadlineExceeded) {
		return output, fmt.Errorf("命令执行超时（超过 %s），已终止", timeout)
	}
	if status.Error != nil {
		return output, fmt.Errorf("命令执行错误: %w", status.Error)
	}
	if status.Exit != 0 {
		return output, fmt.Errorf("命令执行错误: exit status %d", status.Exit)
	}

	return output, nil
}

// terminateRunner 终止仍在运行的命令。
// go-cmd 的 Stop() 存在启动竞态：命令尚未真正启动时调用会返回 ErrNotStarted，
// 因此先等待 Status.PID 被设置再终止。
func terminateRunner(runner *cmd.Cmd) error {
	deadline := time.Now().Add(2 * time.Second)
	for runner.Status().PID == 0 {
		if time.Now().After(deadline) {
			return cmd.ErrNotStarted
		}
		time.Sleep(10 * time.Millisecond)
	}
	return runner.Stop()
}

// shellTimeout 返回命令执行超时。
// 默认 120 秒，可用 LM_SHELL_TIMEOUT 覆盖（单位：秒）。
func shellTimeout() time.Duration {
	if v := os.Getenv("LM_SHELL_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 120 * time.Second
}

func parseArgs(args interface{}, out interface{}) error {
	switch v := args.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), out); err != nil {
			return err
		}
	case []byte:
		if err := json.Unmarshal(v, out); err != nil {
			return err
		}
	default:
		data, err := json.Marshal(args)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}

	return validateToolArguments(out)
}

type repositoryIgnoreMatcher struct {
	root  string
	match gitignore.GitIgnore
}

func newRepositoryIgnoreMatcher(root string) *repositoryIgnoreMatcher {
	// 用 ReadFile 读取后关闭文件，避免 Windows 上文件句柄泄漏
	gitignorePath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return nil
	}
	ignore := gitignore.New(bytes.NewReader(data), root, nil)

	return &repositoryIgnoreMatcher{
		root:  root,
		match: ignore,
	}
}

func (m *repositoryIgnoreMatcher) ignores(path string, isDir bool) bool {
	if m == nil || m.match == nil {
		return false
	}

	rel, err := filepath.Rel(m.root, path)
	if err != nil || rel == "." {
		return false
	}

	match := m.match.Relative(filepath.ToSlash(rel), isDir)
	return match != nil && match.Ignore()
}

func walkWorkspace(rootPath string, ignoreMatcher *repositoryIgnoreMatcher, walkFn func(path string, d fs.DirEntry) error) error {
	conf := fastwalk.Config{}
	return fastwalk.Walk(&conf, rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if shouldSkipWalkEntry(path, d, ignoreMatcher) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		return walkFn(path, d)
	})
}

func shouldSkipWalkEntry(path string, d fs.DirEntry, ignoreMatcher *repositoryIgnoreMatcher) bool {
	name := d.Name()
	if name == ".git" || name == "node_modules" {
		return true
	}

	return ignoreMatcher != nil && ignoreMatcher.ignores(path, d.IsDir())
}

func isBinaryFile(path string) (bool, error) {
	mtype, err := mimetype.DetectFile(path)
	if err != nil {
		return false, err
	}

	mimeName := strings.ToLower(mtype.String())
	if strings.HasPrefix(mimeName, "text/") {
		return false, nil
	}

	textApplications := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-javascript",
		"application/x-sh",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
	}
	for _, prefix := range textApplications {
		if strings.HasPrefix(mimeName, prefix) {
			return false, nil
		}
	}

	return true, nil
}
