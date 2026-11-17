package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveWorkspacePath_RejectsDangerousPrefixes 危险前缀必须被拒。
//
// 这些路径形式都有明确定义的危险语义，不是「看起来可疑」：
//   - ADS（ok.txt:evil）：隐藏数据流，fastwalk 不会列出，可绕过内容审计
//   - UNC（\\server\share）：跨网络边界
//   - device（\\.\PhysicalDrive0）：直接访问设备，绕过一切文件语义
//   - NUL：Go 的 os 包在 NUL 处截断，会使校验路径与实际访问路径不一致
func TestResolveWorkspacePath_RejectsDangerousPrefixes(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	cases := []struct {
		name string
		path string
	}{
		{"ADS 数据流", "ok.txt:evil"},
		{"ADS 显式 DATA", "ok.txt:$DATA"},
		{"UNC 前缀", `\\localhost\C$\Users`},
		{"UNC 双斜杠", `//server/share/file.txt`},
		{"device 路径", `\\.\PhysicalDrive0`},
		{"空字节", "ok.txt\x00.txt"},
	}
	for _, c := range cases {
		if _, err := resolveWorkspacePath(c.path); err == nil {
			t.Errorf("%s: 路径 %q 未被拒绝", c.name, c.path)
		} else if !strings.Contains(err.Error(), "非法") && !strings.Contains(err.Error(), "不允许") {
			t.Errorf("%s: 错误信息应说明原因，得到 %v", c.name, err)
		}
	}
}

// TestResolveWorkspacePath_AllowsLongPath 不应误杀合法长路径与深层路径。
func TestResolveWorkspacePath_AllowsLongPath(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	deep := strings.Repeat("sub/", 40) + "file.txt"
	if _, err := resolveWorkspacePath(deep); err != nil {
		t.Errorf("合法深层路径被拒: %v", err)
	}
}

// TestResolveWorkspacePath_AllowsDriveLetterPath Windows 盘符形式不能误杀。
//
// 若工作区恰好就在另一个盘符上，绝对路径应正常通过（或按超出工作区拒绝），
// 但不能报「不允许包含冒号」——那是 ADS 的判断，误伤正常盘符路径。
func TestResolveWorkspacePath_AllowsDriveLetterPath(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	abs := filepath.Join(ws, "notes.txt")
	_, err := resolveWorkspacePath(abs)
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "不允许") {
		t.Errorf("盘符绝对路径被 ADS 规则误伤: %v", err)
	}
}

// TestResolveWorkspacePath_BlocksJunctionEscape NTFS junction 越权回归测试。
//
// 这是已实测可利用的漏洞：junction 是 reparse point 而非符号链接，
// os.Lstat 的 Mode 不含 ModeSymlink，因此常见的 Lstat 检查完全失效。
// 只有打开句柄取真实路径才能确认它的目标。
func TestResolveWorkspacePath_BlocksJunctionEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 特有：NTFS junction")
	}
	if os.Getenv("CI") != "" {
		t.Skip("CI 环境不保证可创建 junction")
	}

	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET-CONTENT-abc123"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 建立 junction 指向工作区外
	junc := filepath.Join(ws, "junc")
	if err := exec.Command("cmd", "/C", "mklink", "/J", junc, outside).Run(); err != nil {
		t.Skipf("无法创建 junction（需管理员或开发者模式）: %v", err)
	}

	restore := chdirForTest(t, ws)
	defer restore()

	// 穿越 junction 访问外部文件必须被拒绝
	if _, err := resolveWorkspacePath(filepath.Join("junc", "secret.txt")); err == nil {
		t.Error("junction 越权未被拦截：resolveWorkspacePath 允许了工作区外路径")

		// 若确实放行，直接证明可读写外部文件（用于人工确认严重性）
		if got, err2 := resolveWorkspacePath(filepath.Join("junc", "secret.txt")); err2 == nil {
			if data, rerr := os.ReadFile(got); rerr == nil {
				t.Errorf("已读取到工作区外内容: %q", string(data))
			}
		}
	}

	// 直接访问外部绝对路径同样必须被拒
	if _, err := resolveWorkspacePath(secret); err == nil {
		t.Error("外部绝对路径未被拦截")
	}
}

// TestResolveWorkspacePath_AllowsJunctionInsideWorkspace 指向工作区内的 junction 应放行。
func TestResolveWorkspacePath_AllowsJunctionInsideWorkspace(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 特有")
	}
	if os.Getenv("CI") != "" {
		t.Skip("CI 环境不保证可创建 junction")
	}

	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	inner := filepath.Join(ws, "real")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	junc := filepath.Join(ws, "link")
	if err := exec.Command("cmd", "/C", "mklink", "/J", junc, inner).Run(); err != nil {
		t.Skipf("无法创建 junction: %v", err)
	}

	restore := chdirForTest(t, ws)
	defer restore()

	if _, err := resolveWorkspacePath(filepath.Join("link", "ok.txt")); err != nil {
		t.Errorf("指向工作区内的 junction 被误拒: %v", err)
	}
}

// TestResolveWorkspacePath_BlocksSymlinkEscape Unix 符号链接越权回归。
func TestResolveWorkspacePath_BlocksSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix 特有：符号链接")
	}

	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(ws, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	restore := chdirForTest(t, ws)
	defer restore()

	if _, err := resolveWorkspacePath(filepath.Join("escape", "secret.txt")); err == nil {
		t.Error("符号链接越权未被拦截")
	}
}
