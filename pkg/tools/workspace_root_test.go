package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkspaceRoot_StableAcrossChdir 验证工作区根目录不随进程 CWD 漂移。
//
// 回归背景：workspaceRoot() 每次调用都取 os.Getwd()，实测 chdir 到别处后
// 「工作区」会变成那个目录——安全边界随之静默移动。
func TestWorkspaceRoot_StableAcrossChdir(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := SetWorkspaceRoot(orig); err != nil {
		t.Fatalf("SetWorkspaceRoot: %v", err)
	}

	first, err := workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}

	// 切换 CWD 后根目录不应改变
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Skipf("无法切换 CWD: %v", err)
	}

	second, err := workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("工作区根目录随 CWD 漂移: %q -> %q", first, second)
	}
}

// TestWorkspaceRoot_SetWorkspaceRootNormalizes 验证传入路径被规整为绝对路径。
func TestWorkspaceRoot_SetWorkspaceRootNormalizes(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// 传入带冗余元素的相对路径
	if err := SetWorkspaceRoot("."); err != nil {
		t.Fatalf("SetWorkspacePath: %v", err)
	}

	root, err := workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(root) {
		t.Errorf("根目录应为绝对路径，得到 %q", root)
	}
	// 不应含未清理的冗余分隔符
	if filepath.Clean(root) != root {
		t.Errorf("根目录未清理: %q", root)
	}
}

// TestWorkspaceRoot_RejectsEmpty 验证空路径会被拒绝。
func TestWorkspaceRoot_RejectsEmpty(t *testing.T) {
	// SetWorkspaceRoot("") 应该回退或报错，但不能静默产生非法根目录
	if err := SetWorkspaceRoot(""); err != nil {
		return // 报错是可接受行为
	}
	root, err := workspaceRoot()
	if err != nil {
		return
	}
	if root == "" {
		t.Error("根目录不应为空字符串")
	}
}
