package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setConfirmForTest(t *testing.T, h ConfirmHandler) {
	t.Helper()
	SetConfirmHandler(h)
	t.Cleanup(func() { SetConfirmHandler(nil) })
}

func TestDeleteFileAsksUserAndIgnoresModelConfirm(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()
	target := filepath.Join(workspace, "a.txt")
	os.WriteFile(target, []byte("hello"), 0o644)

	var prompts []string
	setConfirmForTest(t, func(p string) (bool, error) {
		prompts = append(prompts, p)
		return false, nil
	})

	// 模型传 confirm=true 也不能绕过用户
	_, err := DeleteFile(DeleteFileArguments{Path: "a.txt", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "用户拒绝") {
		t.Fatalf("用户拒绝时应报错, 得到 %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatal("用户拒绝后文件不应被删除")
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "a.txt") || !strings.Contains(prompts[0], "5 字节") {
		t.Errorf("提示应包含路径与大小: %v", prompts)
	}
}

func TestDeleteFileUserApprovesWithoutModelConfirm(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()
	target := filepath.Join(workspace, "a.txt")
	os.WriteFile(target, []byte("x"), 0o644)

	setConfirmForTest(t, func(string) (bool, error) { return true, nil })

	if _, err := DeleteFile(DeleteFileArguments{Path: "a.txt"}); err != nil {
		t.Fatalf("用户同意后应删除成功: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("文件应已删除")
	}
}

func TestDeleteDirectoryPromptShowsFileCount(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()
	dir := filepath.Join(workspace, "d", "sub")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(workspace, "d", "1.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "2.txt"), nil, 0o644)

	var prompt string
	setConfirmForTest(t, func(p string) (bool, error) { prompt = p; return false, nil })

	DeleteFile(DeleteFileArguments{Path: "d", Recursive: true})
	if !strings.Contains(prompt, "递归删除") || !strings.Contains(prompt, "2 个文件") {
		t.Errorf("目录提示应包含文件数: %q", prompt)
	}
}

func TestDeleteFileConfirmHandlerError(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()
	target := filepath.Join(workspace, "a.txt")
	os.WriteFile(target, []byte("x"), 0o644)

	setConfirmForTest(t, func(string) (bool, error) { return false, errors.New("stdin closed") })

	if _, err := DeleteFile(DeleteFileArguments{Path: "a.txt", Confirm: true}); err == nil {
		t.Fatal("确认失败时应报错")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("确认失败时不应删除")
	}
}

// 并发工具调用时确认请求必须串行。
func TestAskUserSerializesPrompts(t *testing.T) {
	var active, maxActive int
	var mu sync.Mutex
	setConfirmForTest(t, func(string) (bool, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		for i := 0; i < 1000; i++ {
			_ = i * i
		}
		mu.Lock()
		active--
		mu.Unlock()
		return true, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); askUser("p") }()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Errorf("确认请求应串行, 最大并发 %d", maxActive)
	}
}
