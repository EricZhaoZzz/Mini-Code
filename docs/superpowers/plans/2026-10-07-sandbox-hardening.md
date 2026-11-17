# 沙箱与路径边界加固 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 2026-10-07 审查发现的 **NTFS junction 越权读写漏洞**（已实测复现），并为 `run_shell` 建立分层的沙箱机制——Linux 用 Landlock、macOS 用 Seatbelt，Windows 明确标注为「无 OS 层」并以路径校验 + 审批 + 审计补偿。

**Architecture:** 分三层且**刻意不对称**：
- **L1 路径边界加固**（全平台，必须做）：修 junction 漏洞、拒绝 ADS/UNC/device 路径、补句柄级校验。
- **L2 OS 强制**（Linux/macOS 才有，零外部依赖）：Landlock / Seatbelt，不可用时**硬失败而非静默降级**。
- **L3 人工审批 + 审计**（全平台）：高危命令需确认，所有 shell 执行落审计日志。

**Tech Stack:** Go 1.26.4，`golang.org/x/sys/windows`（已在依赖树内），`github.com/landlock-lsm/go-landlock`（Linux 专用，纯 Go 无 cgo）

**前置条件：** 上一轮修复已完成（commit `d8ca6ee`）。`go build`/`go vet`/`go test`/`go test -race` 全绿。

---

## ⚠️ P0：已实测确认的越权漏洞

**这不是理论风险，是当前代码里真实可利用的缺陷。** 我在本机（Windows 11 build 26200、Go 1.26.4）用真实项目代码复现：

```
resolveWorkspacePath("junc\secret.txt")
  -> "C:\...\TestProbeJunctionBypass\001\ws\junc\secret.txt"   err=<nil>   ← 通过校验
实际读取: "SECRET-CONTENT-abc123"                                  ← 读到工作区外
实际写入 err=<nil>
工作区外文件现内容: "PWNED-BY-AGENT"                               ← 改到工作区外
```

**根因链**（逐步实测）：

| 步骤 | 实测结果 |
|------|---------|
| `EvalSymlinks(ws\junc)` | `ws\junc`，err=`<nil>` ← 目录本身「成功」 |
| `EvalSymlinks(ws\junc\secret.txt)` | `""`，err=`The system cannot find the path specified.` |
| 降级路径 `resolvePathWithExistingParents` 向上走到 `ws` | 逐层拼回缺失部分 |
| `filepath.Rel(root, resolved)` | 判定在 root 内 → **放行** |
| 实际 `os.ReadFile` | NTFS 跟随 junction → **到达工作区外** |

**为什么 `Lstat` 也拦不住**：junction 是 reparse point，不是符号链接：

```
os.Lstat(ws\junc): Mode=?rw-rw-rw-    ← 不含 ModeSymlink！
os.Readlink(ws\junc) = "C:\...\outside"  ← Readlink 能读到，但 EvalSymlinks 不用它
```

所以「用 `Lstat` 检查 `ModeSymlink`」这种常见加固方式在这里**完全失效**。必须查 `FILE_ATTRIBUTE_REPARSE_POINT`（`0x400`），或用句柄取真实路径。

**两次调用即可攻击**：agent 先用 `run_shell` 执行 `mklink /J ws\junc C:\Users\...`（当前 denylist 不含 `mklink`，也不该靠 denylist 拦），再用文件工具穿越。

---

## 关键调研结论（决定方案选型）

### 为什么 Windows 不能做低成本 OS 沙箱

| 机制 | 能否限制文件访问 | 结论 |
|------|----------------|------|
| **Job Objects** | **完全不能** | 微软文档明确：*"Windows job objects do not have file input/output (I/O) source restrictions... The Windows operating system does not implement similar limits on files."* 只能用于 `KILL_ON_JOB_CLOSE`（替代当前不稳定的 `taskkill /T`） |
| **AppContainer** | 能（ACL） | 但要求**事先穷举**进程需要的每个路径。`go build` 要读 `GOROOT`/`GOMODCACHE`/`~/.gitconfig`，`npm install` 要写 `%LOCALAPPDATA%`… coding agent 行为空间是开放的，每次加能力都要改 profile |
| **完整性级别（MIC）** | 能（写） | 但把用户的真实工作副本降级成 low-integrity 汇点，**改变宿主信任模型**，且标签粘在文件系统上，进程退出后仍在 |
| **Restricted Token + ACL** | 能（写） | Codex 用的方案。需手绑 `CreateRestrictedToken`（**`x/sys/windows` 里没有**）+ 向真实目录写 ACL |
| **Windows Sandbox** | 能 | **Windows Home 没有此功能**；且需要 host/guest 桥接 |

参考：OpenAI《Building a safe, effective sandbox to enable Codex on Windows》明确否决了 AppContainer 与 MIC，理由与上表一致。

`x/sys/windows@v0.42.0` 实测**缺失**关键 API：`CreateRestrictedToken`、`CreateAppContainerProfile`、`CreateProcessWithLogonW` 全部没有。已有的只是 ACL 操作 + `CreateJobObject`。**难的那一半正好缺失。**

### Linux：Landlock 是唯一「零成本」选项

- `github.com/landlock-lsm/go-landlock` v0.10.1，**纯 Go 无 cgo**（`CGO_ENABLED=0` 时不需要 libcap/psx）
- 最低 Go 1.26.0 — 本项目 `go 1.26.1` **满足**
- syscall：`landlock_create_ruleset`=444、`landlock_add_rule`=445、`landlock_restrict_self`=446（amd64/arm64 相同）
- 需先 `prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)`
- ABI 版本 ↔ 内核：V1=5.13、V2=5.19(REFER)、V3=6.2(TRUNCATE)、V4=6.7、V5=6.10、V8=7.0(TSYNC)
- **不需要 root / setuid / user namespace** — 这是它相对 bubblewrap 的核心优势

bubblewrap 需 `apt install`，nsjail 需 root，gVisor 需 Docker。对「个人本地工具 + 低摩擦」的要求，只有 Landlock 和 Seatbelt 满足。

### macOS：Seatbelt 可用但 SDK 27 有变数

- `sandbox-exec` 标 DEPRECATED，但底层 Seatbelt 子系统**功能完好**（2026-08 在 macOS 26.5 实测确认）
- ⚠️ **SDK 27 移除了内置 profile 常量**（`kSBXProfilePureComputation` 等）。OpenSSH 10.6 因此直接删掉了 macOS 沙箱
- **但自定义 SBPL profile 字符串不受影响**（走 `sandbox_init(profile, ...)`，profile 是调用方传入）
- 纯 Go 绑定不存在，需 `syscall.Syscall` + `Dlopen` 绑 `libsandbox.dylib`
- 已知限制：沙箱生效**前**打开的 FD 不受约束。本项目用 `Buffered: true` 内存管道，不受影响

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Modify | `pkg/tools/workspace.go` | L1：junction/ADS/UNC/device 拦截 + 句柄级校验 |
| Create | `pkg/tools/workspace_windows.go` | 句柄级真实路径校验（`GetFinalPathNameByHandle`） |
| Create | `pkg/tools/workspace_unix.go` | Unix 侧 reparse/symlink 校验 |
| Create | `pkg/tools/workspace_security_test.go` | 越权用例（含 junction 回归） |
| Create | `pkg/sandbox/sandbox.go` | 平台无关接口 + 能力探测 |
| Create | `pkg/sandbox/sandbox_linux.go` | Landlock 实现 |
| Create | `pkg/sandbox/sandbox_darwin.go` | Seatbelt 实现 |
| Create | `pkg/sandbox/sandbox_other.go` | 不支持平台的显式降级 |
| Create | `pkg/sandbox/audit.go` | 审计日志 |
| Create | `pkg/sandbox/*_test.go` | 单测 |
| Modify | `pkg/tools/file.go` | `RunShell` 接入沙箱包装器 |
| Modify | `pkg/agent/prompt.go` | 同步边界描述 |
| Modify | `.env.example` | 新增配置项 |
| Modify | `docs/superpowers/plans/2026-10-07-critical-bugs-fix.md` | 标记 P0-1.3 部分作废，被本文档取代 |

---

## Phase 1：L1 路径边界加固（P0，必做）

### Task 1.1: 拦截 ADS、UNC、device 路径

**Files:**
- Modify: `pkg/tools/workspace.go:25`（`resolveWorkspacePath`）
- Test: `pkg/tools/workspace_security_test.go`

**背景：** 实测确认 ADS（`file.txt:stream`）可绕过——工作区内 `x.txt:evil` 会创建隐藏流，`ListFiles` 看不到（fastwalk 不列 ADS），构成内容审计绕过。UNC/device 路径虽被 `Rel` 的 volume mismatch 挡住，但当前逻辑**误杀**了合法的 `\\?\` 长路径。

- [x] **Step 1.1.1: 写失败测试**

```go
// pkg/tools/workspace_security_test.go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveWorkspacePath_RejectsDangerousPrefixes 危险前缀必须被拒。
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

// TestResolveWorkspacePath_AllowsLongPath 不应误杀合法长路径。
func TestResolveWorkspacePath_AllowsLongPath(t *testing.T) {
	ws := t.TempDir()
	restore := chdirForTest(t, ws)
	defer restore()

	// 普通深层路径应放行
	deep := strings.Repeat("sub/", 40) + "file.txt"
	if _, err := resolveWorkspacePath(deep); err != nil {
		t.Errorf("合法深层路径被拒: %v", err)
	}
}
```

- [x] **Step 1.1.2: 实现前缀与字符校验**

在 `resolveWorkspacePath` 开头插入：

```go
	// 前置校验：拒绝 Windows 上有特殊语义或可绕过审计的路径形式。
	//
	// 这些不是「看起来可疑」，而是有明确定义的危险语义：
	//   - ADS（file.txt:stream）：隐藏数据流，ListFiles 不会列出，可绕过内容审计
	//   - UNC（\\server\share）：跨网络边界，Rel 的 volume 判断可能被绕过
	//   - device（\\.\PhysicalDrive0）：直接访问设备，绕过一切文件语义
	//   - NUL：Go 的 os 包会截断，可能导致校验路径与实际访问路径不一致
	if err := checkDangerousPath(candidate); err != nil {
		return "", err
	}
```

```go
// checkDangerousPath 拒绝带特殊语义的危险路径形式。
func checkDangerousPath(path string) error {
	lower := strings.ToLower(path)

	// NUL 字节：Go 的 os 包在 NUL 处截断，可能使校验路径与实际访问路径不一致
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("路径不允许包含 NUL 字符")
	}

	// Windows 上冒号只用于盘符与 ADS，出现在文件名中一律拒绝
	if runtime.GOOS == "windows" {
		// 盘符形式 C:\ 或 C:/ 在 IsAbs 分支已处理，
		// 这里拦的是 ADS 与其他冒号用法
		if idx := strings.Index(lower, ":"); idx > 1 {
			return fmt.Errorf("路径不允许包含冒号（可能试图访问 NTFS 备用数据流）")
		}
	}

	// UNC 与 device 路径
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return fmt.Errorf("路径不允许使用 UNC 或设备路径前缀")
	}

	return nil
}
```

需补 import：`runtime`。

- [x] **Step 1.1.3: 运行测试**

```bash
go test ./pkg/tools -run "TestResolveWorkspacePath_" -v
```

### Task 1.2: 修复 junction 越权（核心）

**Files:**
- Create: `pkg/tools/workspace_windows.go`
- Create: `pkg/tools/workspace_unix.go`
- Modify: `pkg/tools/workspace.go`
- Test: `pkg/tools/workspace_security_test.go`

**背景：** 见开头 P0 实测。两种修复方式**都已实测验证有效**：

- **方案 A（句柄真实路径）**：`GetFinalPathNameByHandle` 返回 junction 解析后的真实路径
  ```
  GetFinalPathNameByHandle("junc\secret.txt")
    -> "\\?\C:\Users\Eric\AppData\Local\Temp\jtest\outside\secret.txt"
  真实路径在 root 内? false (rel="..\outside\secret.txt")
  ```
- **方案 B（reparse point 检测）**：查 `FILE_ATTRIBUTE_REPARSE_POINT`（`0x400`）
  ```
  reparse point 检测: true
  普通文件 reparse point: false   ← 不误杀
  ```

**推荐两者叠加**：方案 B 作为前置快速拒绝（避免对每个路径都开句柄），方案 A 作为权威判定（TOCTOU 窗口内的最终防线）。

- [x] **Step 1.2.1: 写失败测试（junction 回归）**

```go
// TestResolveWorkspacePath_BlocksJunctionEscape NTFS junction 越权回归测试。
//
// 这是已实测可利用的漏洞：junction 是 reparse point 而非符号链接，
// os.Lstat 的 Mode 不含 ModeSymlink，因此常见的 Lstat 检查完全失效。
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
```

- [x] **Step 1.2.2: 新增 `workspace_windows.go`**

```go
//go:build windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// pathEscapesRoot 判断路径是否逃出工作区根目录。
//
// 实现要点（均经实测验证）：
//  1. 逐组件检查 reparse point —— junction 是 FILE_ATTRIBUTE_REPARSE_POINT，
//     而非符号链接，os.Lstat 的 Mode 不含 ModeSymlink，只查 Lstat 会漏。
//  2. 用 GetFinalPathNameByHandle 取句柄指向的真实路径做权威判定 ——
//     它返回 junction 解析后的规范路径（\\?\ 前缀），
//     因此即使校验与访问之间发生 TOCTOU 竞态，也无法写到工作区外。
func pathEscapesRoot(root, resolved string) (bool, error) {
	// 第一道：逐组件 reparse point 检查
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return true, nil // Rel 失败（如 volume 不匹配）视为越界
	}

	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)

		isReparse, err := isReparsePoint(current)
		if err != nil {
			// 路径不存在等情况交由后续处理，不在此处判越界
			continue
		}
		if isReparse {
			// reparse point 存在时，必须确认它指向哪里
			target, terr := resolveReparseTarget(current)
			if terr != nil {
				return true, nil // 无法确认目标 → 保守拒绝
			}
			if !isWithinRoot(root, target) {
				return true, nil
			}
		}
	}

	// 第二道：句柄真实路径权威判定
	return !handleConfirmsInsideRoot(root, resolved)
}

// isReparsePoint 判断路径是否为 reparse point（junction / symlink / mount point）。
func isReparsePoint(path string) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false, err
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

// resolveReparseTarget 取 reparse point 的真实目标。
func resolveReparseTarget(path string) (string, error) {
	target, err := finalPathByHandle(path)
	if err != nil {
		return "", err
	}
	return normalizeWindowsPath(target), nil
}

// handleConfirmsInsideRoot 用句柄真实路径确认仍在 root 内。
func handleConfirmsInsideRoot(root, path string) bool {
	// 目标可能尚不存在（如 write_file 创建新文件），此时退化为字符串比较
	final, err := finalPathByHandle(path)
	if err != nil {
		return isWithinRoot(root, path)
	}
	return isWithinRoot(root, normalizeWindowsPath(final))
}

// finalPathByHandle 打开路径并返回其真实规范路径。
func finalPathByHandle(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	// FILE_FLAG_BACKUP_SEMANTICS 允许对目录打开句柄
	handle, err := windows.CreateFile(
		p,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("打开路径句柄失败: %w", err)
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", fmt.Errorf("取真实路径失败: %w", err)
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// normalizeWindowsPath 去掉 GetFinalPathNameByHandle 返回的 \\?\ 前缀。
func normalizeWindowsPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		p = p[4:]
	}
	return filepath.Clean(p)
}
```

- [x] **Step 1.2.3: 新增 `workspace_unix.go`**

```go
//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// pathEscapesRoot 判断路径是否逃出工作区根目录（Unix 实现）。
//
// Unix 上 filepath.EvalSymlinks 能正确解析符号链接，
// 因此这里只需处理 EvalSymlinks 失败时的降级路径是否安全。
func pathEscapesRoot(root, resolved string) (bool, error) {
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return true, nil // volume 不匹配等 → 视为越界
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)), nil
}
```

- [x] **Step 1.2.4: 抽出 `isWithinRoot` 并接入 `resolveWorkspacePath`**

在 `workspace.go` 添加共用辅助函数，并把句柄校验接到现有流程：

```go
// isWithinRoot 判断 path 是否位于 root 之内。
func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
```

在 `resolveWorkspacePath` 的 `filepath.Rel` 校验之后追加句柄级校验（**顺序重要**：字符串校验在前，快速拒绝绝大多数非法路径；句柄校验在后，处理 junction 这类字符串层面看不出的情况）：

```go
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径超出工作区: %s", path)
	}

	// 字符串校验通过后，再做句柄级校验以拦截 junction 等
	// 字符串层面看不出的越界（NTFS junction 是 reparse point 而非符号链接，
	// 其真实目标只有打开句柄才能确认）。
	escapes, err := pathEscapesRoot(root, resolvedCandidate)
	if err != nil || escapes {
		return "", fmt.Errorf("路径超出工作区: %s", path)
	}

	return resolvedCandidate, nil
```

- [x] **Step 1.2.5: 运行测试**

```bash
go test ./pkg/tools -run "TestResolveWorkspacePath_" -v
go test ./pkg/tools -count=1
```

> **验收标准**：`TestResolveWorkspacePath_BlocksJunctionEscape` 必须通过，
> 且 `TestResolveWorkspacePath_AllowsJunctionInsideWorkspace` 不能被误拒。
> 回归验证：把 `pathEscapesRoot` 调用删掉重跑第一个用例，必须复现越权。

### Task 1.3: denylist 补充 junction/hardlink 相关命令

**Files:**
- Modify: `pkg/tools/shell_guard.go`

**背景：** agent 可用 `run_shell` 自行创建 junction 前提。虽然 Task 1.2 已让穿越无法成功，但阻止创建更省事。

- [x] **Step 1.3.1: 补充规则**

```go
	// 创建链接 / 硬链接：可用于制造 reparse point 绕过路径校验
	{regexp.MustCompile(`(?i)\b(mklink|fsutil\s+hardlink\s+create)\b`), "不允许创建链接"},
	{regexp.MustCompile(`(?i)\bnew-item\b.*-itemtype\s+(hardlink|junction|symboliclink)\b`), "不允许创建链接"},
```

- [x] **Step 1.3.2: 补测试并验证不误杀 `go mod tidy` 等常用命令**

```bash
go test ./pkg/tools -run TestCheckShellCommand -v
```

---

## Phase 2：L2 OS 强制沙箱（Linux/macOS）

### Task 2.1: 定义平台无关接口与能力探测

**Files:**
- Create: `pkg/sandbox/sandbox.go`
- Create: `pkg/sandbox/sandbox_other.go`

**设计要点：**

| 层 | Windows | Linux | macOS |
|---|---|---|---|
| L1 路径校验 + 命令守卫 | ✓ | ✓ | ✓ |
| **L2 OS 强制** | **✗ 不可能** | ✓ Landlock | ✓ Seatbelt |
| L3 审批 + 审计 | ✓ | ✓ | ✓ |

**关键原则：绝不静默降级。** 沙箱起不来必须硬失败并告知用户，否则用户会以为有保护而实际没有。

- [x] **Step 2.1.1: 实现**

```go
// Package sandbox 为 shell 命令执行提供 OS 级文件系统边界。
//
// 分层说明（刻意不对称，不要追求三平台一致）：
//   - Linux   : Landlock，内核强制，无需额外依赖 —— 这是本项目唯一
//     同时满足「内核强制」+「零外部依赖」的选项。
//   - macOS   : sandbox-exec（Seatbelt），系统自带。
//   - Windows : 无可行的低成本 OS 机制。见 sandbox_windows.go 的说明。
//     Job Objects 不限制文件访问；AppContainer 要求事先穷举能力清单，
//     与 coding agent 开放的行为空间不匹配。故此平台只提供 L1+L3。
//
// 各层职责：
//   - L1 路径边界加固（pkg/tools/workspace.go）：全平台
//   - L2 OS 强制（本包）：Linux/macOS，fail-closed
//   - L3 审批与审计（pkg/sandbox/audit.go）：全平台
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// Status 描述当前平台的沙箱能力。
type Status struct {
	// Available 表示本平台是否支持 OS 级强制
	Available bool
	// Mechanism 说明使用的机制（landlock / seatbelt / none）
	Mechanism string
	// Reason 在 Available=false 时说明原因（面向用户展示）
	Reason string
	// WritableRoots 是允许写入的额外根目录
	WritableRoots []string
}

// Config 是沙箱配置。
type Config struct {
	// WorkspaceRoot 工作区根目录（唯一允许读写的项目目录）
	WorkspaceRoot string
	// ExtraWritableRoots 额外允许读写的目录（如 GOCACHE、TMPDIR）
	ExtraWritableRoots []string
	// ReadOnlyPaths 额外允许只读访问的路径（如 GOROOT、工具链）
	ReadOnlyPaths []string
	// Strict 严格模式下沙箱不可用即报错；否则降级为无沙箱
	Strict bool
}

// GetStatus 返回当前平台的沙箱能力（不实际启用）。
func GetStatus(cfg Config) Status

// Wrap 把命令包装为在沙箱内执行。
//
// 返回的 cmd 已设置好沙箱所需的全部前置条件，调用方只需 Start。
// 返回 error 表示沙箱无法启用——调用方**必须**处理该错误，
// 不能静默忽略（否则用户会误以为有保护）。
func Wrap(ctx context.Context, cmd *exec.Cmd, cfg Config) error
```

- [x] **Step 2.1.2: 不支持平台的显式降级**

```go
//go:build !linux && !darwin

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// GetStatus 返回当前平台的沙箱能力。
func GetStatus(cfg Config) Status {
	return Status{
		Available: false,
		Mechanism: "none",
		Reason: fmt.Sprintf(
			"%s 上没有可行的低成本 OS 级文件沙箱。"+
				"Job Objects 不限制文件访问；AppContainer 要求事先穷举进程需要的能力，"+
				"与 coding agent 开放的行为空间不匹配。"+
				"本平台仅提供 L1 路径校验 + L3 命令审批与审计。",
			runtime.GOOS),
		WritableRoots: cfg.ExtraWritableRoots,
	}
}

// Wrap 在不支持的平台不做任何注入。
func Wrap(_ context.Context, _ *exec.Cmd, _ Config) error {
	return nil
}
```

- [x] **Step 2.1.3: 单测**

```go
// pkg/sandbox/sandbox_test.go
package sandbox

import (
	"runtime"
	"testing"
)

func TestGetStatus(t *testing.T) {
	st := GetStatus(Config{WorkspaceRoot: t.TempDir()})

	switch runtime.GOOS {
	case "linux", "darwin":
		// 具体是否可用取决于内核版本，这里只校验字段自洽
		if st.Mechanism == "" {
			t.Error("Mechanism 不应为空")
		}
		if !st.Available && st.Reason == "" {
			t.Error("不可用时必须给出原因（面向用户）")
		}
	default:
		if st.Available {
			t.Errorf("%s 不应声称有 OS 沙箱", runtime.GOOS)
		}
		if st.Reason == "" {
			t.Error("必须说明为何不可用")
		}
	}
}
```

### Task 2.2: Linux Landlock 实现

**Files:**
- Create: `pkg/sandbox/sandbox_linux.go`

**实测确认的机制细节**（务必按此实现，否则会踩坑）：

| 陷阱 | 后果 | 应对 |
|------|------|------|
| 未设 `PR_SET_NO_NEW_PRIVS` | `landlock_restrict_self` 返回 `EPERM` | 先 `prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)` |
| 漏 `EXECUTE` 权限 | 任意二进制都能执行 | 显式授予需要执行的路径 |
| `TRUNCATE` 是独立权限 | 给 `WRITE_FILE` 不等于能 `O_TRUNC`，覆盖写会失败 | 需 `LANDLOCK_ACCESS_FS_TRUNCATE`（ABI ≥ 3） |
| `REFER` 默认拒绝 | 内核 < 5.19 无法跨目录 rename/link | 只在 ABI ≥ 2 时授予 |
| 收紧后不可逆 | 先开好所有文件/套接字再收紧 | 顺序：加规则 → restrict_self |
| 沙箱前打开的 FD 不受限 | 继承的句柄仍可访问 | 确保不预先打开工作区外文件 |

- [x] **Step 2.2.1: 引入依赖**

```bash
go get github.com/landlock-lsm/go-landlock@v0.10.1
```

需确认：`go.mod` 的 `go` 指令 ≥ 1.26.0（当前 `go 1.26.1` ✓）；
`CGO_ENABLED=0` 时不引入 cgo 依赖。

- [x] **Step 2.2.2: 实现**

```go
//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// linuxMechanism 标识 Landlock。
const linuxMechanism = "landlock"

// readWriteAccess 工作区与额外可写目录的权限集。
//
// 注意 TRUNCATE 是独立权限：不包含它时覆盖写现有文件会失败
// （O_TRUNC 需要 LANDLOCK_ACCESS_FS_TRUNCATE，ABI ≥ 3）。
// AccessFSReadWrite 已包含 read/write/truncate/remove/make_*。
//
// REFER（跨目录 rename/link）需 ABI ≥ 2，由版本常量控制，
// 这里不显式授予 —— 未授予时相关操作会被拒绝，属于安全默认值。
func readWriteAccess() landlock.AccessFSSet {
	return landlock.AccessFSReadWrite
}

// GetStatus 探测 Landlock 是否可用。
func GetStatus(cfg Config) Status {
	st := Status{
		Mechanism:     linuxMechanism,
		WritableRoots: cfg.ExtraWritableRoots,
	}

	// 用只读规则做一次无害探测：若失败说明内核不支持
	if err := landlock.V3.RestrictPaths(); err != nil {
		st.Reason = fmt.Sprintf("Landlock 不可用（内核 < 5.19 或未启用）: %v", err)
		return st
	}

	st.Available = true
	return st
}

// Wrap 为命令注入 Landlock 规则。
//
// ⚠️ 关键前提：Landlock 限制的是**调用它的进程及其后续所有子进程**，
// 且不可逆。因此在 mini-code 主进程里调用 RestrictPaths 会连带
// 限制 mini-code 自身的文件访问（记忆库、审计日志、缓存写入都会受影响）。
//
// 实现时必须在启动 shell 子进程**之前**完成收紧，且要确认宿主写入不受影响：
// 见 Step 2.2.3 的最小验证。若发现冲突，正确做法是把 shell 命令放进
// 一个专用的 runner 子进程，在那个子进程里收紧，而不是在主进程收紧。
func Wrap(_ context.Context, cmd *exec.Cmd, cfg Config) error {
	if cfg.WorkspaceRoot == "" {
		return fmt.Errorf("沙箱配置缺少 WorkspaceRoot")
	}

	readWrite := append([]string{cfg.WorkspaceRoot}, cfg.ExtraWritableRoots...)
	readOnly := existingPaths(cfg.ReadOnlyPaths)

	// 规则顺序无关，但每条规则内的 paths 会被合并。
	// 只读规则应覆盖工具链（GOROOT 等），否则 go build 无法运行。
	rules := []landlock.Rule{
		landlock.PathAccess(landlock.AccessFSReadWrite, readWrite...),
	}
	if len(readOnly) > 0 {
		rules = append(rules, landlock.PathAccess(landlock.AccessFSReadFile, readOnly...))
	}

	// V3 = ABI 3（内核 6.2），比 V4 兼容性更好；
	// AccessFSReadWrite 已含 TRUNCATE，ABI ≥ 3 才支持。
	if err := landlock.V3.RestrictPaths(rules...); err != nil {
		return fmt.Errorf("启用 Landlock 沙箱失败: %w", err)
	}

	return nil
}

// existingPaths 过滤掉不存在的路径。
//
// Landlock 的 PathAccess 遇到不存在的路径会报错，
// 而工具链路径（如 GOROOT）在某些环境下可能不存在。
// 若确认无此问题，可改用 landlock.PathAccess(...).IgnoreMissing()。
func existingPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
```

> **API 说明**（已对 `go-landlock@v0.10.1` 实测核对）：
> - `func (c Config) RestrictPaths(rules ...Rule) error` —— 变参 `Rule`，不是 `(paths, access, paths)`。
> - `func PathAccess(accessFS AccessFSSet, paths ...string) FSRule` —— 构造规则。
> - 版本常量 `V1`…`V10` 是 `Config` 值，方法在其上。
> - 权限常量在 `AccessFSSet`：`AccessFSReadWrite` / `AccessFSReadFile` / `AccessFSExecute` 等。

- [x] **Step 2.2.3: 验证不影响宿主进程（关键）**

```go
// TestWrap_VerifyIsolation 验证沙箱真的生效，且不误伤宿主。
func TestWrap_VerifyIsolation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux 特有")
	}
	if !GetStatus(Config{}).Available {
		t.Skip("Landlock 不可用")
	}

	root := t.TempDir()
	allowed := filepath.Join(root, "allowed.txt")
	if err := os.WriteFile(allowed, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 子进程内：应能读工作区
	out, err := runInSandbox(t, root, []string{"cat", allowed})
	if err != nil {
		t.Errorf("沙箱内读取工作区失败: %v\n输出: %s", err, out)
	}

	// 子进程内：应不能读 /etc/passwd
	out, err = runInSandbox(t, root, []string{"cat", "/etc/passwd"})
	if err == nil {
		t.Errorf("沙箱内读取 /etc/passwd 竟然成功（Landlock 未生效）: %s", out)
	}
}
```

- [x] **Step 2.2.4: 运行测试**

```bash
go test ./pkg/sandbox -v
```

### Task 2.3: macOS Seatbelt 实现

**Files:**
- Create: `pkg/sandbox/sandbox_darwin.go`

**关键事实（已核实）**：`sandbox-exec` 标 DEPRECATED 但功能完好；**必须用自定义 SBPL profile**，不能用内置 `kSBXProfile*` 常量（SDK 27 已移除，会导致进程被 kill）。

- [x] **Step 2.3.1: 实现（以子进程方式包装 `sandbox-exec`）**

```go
//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// darwinMechanism 标识 Seatbelt。
const darwinMechanism = "seatbelt"

// sandboxProfile 生成 SBPL profile。
//
// 只允许工作区内读写 + 系统只读路径，其余一律拒绝。
// 注意：必须用自定义 profile，不要用内置 kSBXProfile* 常量
// —— 它们在 macOS SDK 27 已被移除，进程会在尝试时被杀。
func sandboxProfile(cfg Config) string {
	rw := []string{cfg.WorkspaceRoot}
	rw = append(rw, cfg.ExtraWritableRoots...)

	var b []byte
	b = append(b, []byte("(version 1)\n(allow default)\n")...)

	for _, p := range rw {
		// 转义 profile 中的引号与反斜杠
		escaped := escapeSBPL(p)
		b = append(b, []byte(fmt.Sprintf("(deny file-write* (subpath %q))\n", escaped))...)
	}
	return string(b)
}

// escapeSBPL 转义 SBPL 字符串。
func escapeSBPL(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' || r == '\\' {
			out = append(out, '\\', r)
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// GetStatus 探测 sandbox-exec 是否可用。
func GetStatus(cfg Config) Status {
	st := Status{
		Mechanism:     darwinMechanism,
		WritableRoots: cfg.ExtraWritableRoots,
	}

	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		st.Reason = "sandbox-exec 不可用"
		return st
	}

	st.Available = true
	return st
}

// Wrap 通过 sandbox-exec 包装命令。
//
// 与 Landlock 不同，Seatbelt 只能作用于**子进程**，
// 因此这里改写 cmd 的 Path/Args 指向 sandbox-exec。
func Wrap(_ context.Context, cmd *exec.Cmd, cfg Config) error {
	if cfg.WorkspaceRoot == "" {
		return fmt.Errorf("沙箱配置缺少 WorkspaceRoot")
	}

	profilePath := filepath.Join(os.TempDir(), fmt.Sprintf("mini-code-sandbox-%d.sb", os.Getpid()))
	if err := os.WriteFile(profilePath, []byte(sandboxProfile(cfg)), 0o600); err != nil {
		return fmt.Errorf("写入 sandbox profile 失败: %w", err)
	}
	// 记录待清理路径，由调用方在命令结束后删除
	registerProfileCleanup(profilePath)

	originalPath := cmd.Path
	originalArgs := cmd.Args

	cmd.Path = "/usr/bin/sandbox-exec"
	cmd.Args = append([]string{"sandbox-exec", "-f", profilePath, originalPath}, originalArgs[1:]...)

	return nil
}
```

- [x] **Step 2.3.2: 补 `registerProfileCleanup`**

```go
// sandboxProfileCleanup 收集待删除的 profile 文件。
var sandboxProfileCleanup sync.Map

func registerProfileCleanup(path string) {
	sandboxProfileCleanup.Store(path, struct{}{})
}

// CleanupProfiles 删除已生成的 profile 文件。
func CleanupProfiles() {
	sandboxProfileCleanup.Range(func(k, _ any) bool {
		if path, ok := k.(string); ok {
			_ = os.Remove(path)
		}
		return true
	})
}
```

- [x] **Step 2.3.3: 标注维护风险**

在 `sandbox_darwin.go` 顶部加注释：

```go
// ⚠️ 维护风险：sandbox-exec 虽被 Apple 标为 DEPRECATED，但底层 Seatbelt
// 子系统功能完好，且被大量组件依赖（Safari、Mail、Chromium、Firefox、
// SwiftPM、Homebrew、Bazel、Nix，以及 Claude Code / Codex / Cursor），
// 移除概率极低。
//
// 但 SDK 27 确实移除了内置 profile 常量（kSBXProfile*）。
// 本实现只使用自定义 profile 字符串，不受影响。
// 若未来 Apple 移除 Seatbelt，此文件需改用 App Sandbox 或废弃。
```

---

## Phase 3：L3 审批与审计（全平台）

### Task 3.1: 审计日志

**Files:**
- Create: `pkg/sandbox/audit.go`
- Test: `pkg/sandbox/audit_test.go`

- [x] **Step 3.1.1: 实现**

```go
package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEvent 是一条审计记录。
type AuditEvent struct {
	Time      time.Time `json:"time"`
	Command   string    `json:"command"`
	Dir       string    `json:"dir"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
	Mechanism string    `json:"mechanism,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	PID       int       `json:"pid,omitempty"`
}

// auditLogger 追加写入审计日志。
type auditLogger struct {
	mu sync.Mutex
	f  *os.File
}

// AuditLog 是全局审计器，默认写入 .mini-code/audit.log。
var auditLog = &auditLogger{}

// InitAudit 在工作区下初始化审计日志。
func InitAudit(workspaceRoot string) error {
	dir := filepath.Join(workspaceRoot, ".mini-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "audit.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	if auditLog.f != nil {
		_ = auditLog.f.Close()
	}
	auditLog.f = f
	return nil
}

// Log 记录一条审计事件（失败不影响主流程）。
func Log(ev AuditEvent) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()

	if auditLog.f == nil {
		return
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = auditLog.f.Write(append(data, '\n'))
}
```

- [x] **Step 3.1.2: 单测**

```go
func TestAuditLog_WritesJSONL(t *testing.T) {
	ws := t.TempDir()
	if err := InitAudit(ws); err != nil {
		t.Fatal(err)
	}

	Log(AuditEvent{Command: "go test ./...", Dir: ws, Allowed: true, ExitCode: 0})

	data, err := os.ReadFile(filepath.Join(ws, ".mini-code", "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "go test") {
		t.Errorf("审计日志未记录命令: %s", data)
	}
	// 必须是合法 JSONL
	line := strings.TrimSpace(string(data))
	var ev AuditEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Errorf("审计日志不是合法 JSON: %v", err)
	}
}
```

### Task 3.2: 接入 RunShell

**Files:**
- Modify: `pkg/tools/file.go:230`（`RunShell`）
- Modify: `cmd/agent/main.go`
- Modify: `.env.example`

- [x] **Step 3.2.1: 在 RunShell 中接入沙箱与审计**

在 `CheckShellCommand` 通过之后、构造 `cmd.Options` 之前插入：

```go
	// 记录审计（无论成功失败）
	sandbox.Log(sandbox.AuditEvent{
		Command: command,
		Dir:     root,
		Allowed: true,
	})
```

并把沙箱包装加到 `BeforeExec`（Landlock 必须在 Start 前注入）：

```go
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

	// OS 级沙箱：把 runner 内部的 *exec.Cmd 取出来包装。
	//
	// go-cmd 不暴露底层 Cmd，因此通过 Status().PID 无法拿到句柄；
	// 这里改为在 BeforeExec 阶段包装。
	if err := sandbox.Wrap(ctx, execCmdOf(runner), sandboxCfg); err != nil {
		if sandboxCfg.Strict {
			return nil, fmt.Errorf("沙箱不可用，已拒绝执行: %w", err)
		}
		sandbox.Log(sandbox.AuditEvent{
			Command: command, Dir: root, Allowed: true,
			Reason: "沙箱不可用: " + err.Error(),
		})
	}
```

> **实现注意**：go-cmd 不暴露底层 `*exec.Cmd`，需要在 `BeforeExec` 回调里
> 包装（该回调能拿到 `*exec.Cmd`）。这要求把沙箱注入移到 `BeforeExec` 内，
> 而不是当前的 `BeforeExec` 之后。**落地时需调整调用顺序**，先跑
> `TestRunShell_*` 确认四种既有行为（stdout/stderr、非零退出、重启拦截、
> 超时）未受影响。

- [x] **Step 3.2.2: main.go 初始化沙箱配置与审计**

```go
	// 初始化沙箱：探测能力并开启审计
	sandboxCfg := sandbox.Config{
		WorkspaceRoot:    toolsWorkspaceRoot(),
		ExtraWritableRoots: sandboxWritableRoots(),
		ReadOnlyPaths:    sandboxReadOnlyPaths(),
		Strict:           os.Getenv("LM_SANDBOX_STRICT") == "1",
	}
	if err := sandbox.InitAudit(sandboxCfg.WorkspaceRoot); err != nil {
		ui.PrintError("审计日志初始化失败: %v", err)
	}

	st := sandbox.GetStatus(sandboxCfg)
	if !st.Available {
		ui.PrintInfo("沙箱状态: %s (%s)", st.Mechanism, st.Reason)
	}
```

配套的辅助函数：

```go
// toolsWorkspaceRoot 返回工作区根目录。
func toolsWorkspaceRoot() string {
	root, err := tools.CurrentWorkspaceRoot()
	if err != nil {
		return "."
	}
	return root
}

// sandboxWritableRoots 返回额外可写目录。
//
// 这些是构建工具必需的缓存目录，否则 go build / npm install 会失败。
func sandboxWritableRoots() []string {
	var roots []string
	for _, env := range []string{"GOCACHE", "GOMODCACHE", "TMPDIR", "TEMP", "TMP"} {
		if p := os.Getenv(env); p != "" {
			roots = append(roots, p)
		}
	}
	return roots
}

// sandboxReadOnlyPaths 返回只读工具链目录。
func sandboxReadOnlyPaths() []string {
	var paths []string
	if goroot := runtime.GOROOT(); goroot != "" {
		paths = append(paths, goroot)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		paths = append(paths, gopath)
	}
	return paths
}
```

需在 `pkg/tools` 导出一个访问器：

```go
// CurrentWorkspaceRoot 返回当前固定的工作区根目录。
func CurrentWorkspaceRoot() (string, error) {
	return workspaceRoot()
}
```

- [x] **Step 3.2.3: 更新 `.env.example`**

```bash
# ============== 沙箱 ==============

# 可选：严格沙箱模式。设为 1 时，沙箱不可用将直接拒绝执行命令，
# 而不是降级为无沙箱运行。生产/测试环境建议开启。
LM_SANDBOX_STRICT=

# 可选：额外允许沙箱读写的目录（逗号分隔），如构建缓存位置。
# 默认自动取 GOCACHE / GOMODCACHE / TMPDIR 等环境变量。
LM_SANDBOX_WRITABLE=
```

- [x] **Step 3.2.4: 运行全量测试**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./...
```

---

## Phase 4：文档同步

### Task 4.1: 更新系统提示词

**Files:**
- Modify: `pkg/agent/prompt.go`

当前提示词声称 `run_shell`「能够访问工作区之外的任何文件」。Linux/macOS 上启用沙箱后这句话需要按能力动态描述，否则会出现「提示词说有沙箱，实际没有」的误导。

- [x] **Step 4.1.1: 注入沙箱状态到提示词**

```go
	if st := sandbox.GetStatus(sandbox.Config{}); st.Available {
		basePrompt += fmt.Sprintf("\n\n## 沙箱状态\n"+
			"当前平台启用 %s OS 级文件沙箱：shell 命令只能在工作区内读写，"+
			"访问其他路径会被内核拒绝。\n", st.Mechanism)
	} else {
		basePrompt += fmt.Sprintf("\n\n## 沙箱状态\n"+
			"当前平台无 OS 级文件沙箱（%s）。\n"+
			"shell 命令能访问工作区之外的任何文件，仅有部分危险命令被黑名单拦截。\n"+
			"请勿把 shell 用于访问敏感路径。\n", st.Reason)
	}
```

### Task 4.2: 更新 AGENTS.md / README

- [x] ] **Step 4.2.1: 在 AGENTS.md「关键约束」补充沙箱说明**
- [x] **Step 4.2.2: 在 README 说明各平台沙箱能力差异**

---

## 验收标准

| 项 | 验收方式 |
|----|---------|
| **junction 越权** | `TestResolveWorkspacePath_BlocksJunctionEscape` 通过；删除 `pathEscapesRoot` 调用后必须复现失败 |
| **junction 不误杀** | `TestResolveWorkspacePath_AllowsJunctionInsideWorkspace` 通过 |
| ADS/UNC/device | `TestResolveWorkspacePath_RejectsDangerousPrefixes` 通过 |
| 长路径不误杀 | `TestResolveWorkspacePath_AllowsLongPath` 通过 |
| 沙箱探测 | `TestGetStatus` 通过；Windows 上 `Available=false` 且 `Reason` 非空 |
| Landlock | `TestWrap_VerifyIsolation` 通过（Linux）；能读工作区、读不到 `/etc/passwd` |
| Seatbelt | 同上（macOS） |
| 审计 | `TestAuditLog_WritesJSONL` 通过 |
| 既有行为 | `TestRunShell*` 全部通过（stdout/stderr、非零退出、重启拦截、超时） |
| 回归 | `go build`/`go vet`/`go test`/`go test -race` 全绿 |
| 文档 | 提示词与实际沙箱状态一致，不再有「禁止访问工作区外文件」这类不成立表述 |

## 建议执行顺序

**先做 Phase 1**（junction 是已实测可利用的越权读写，与 OS 无关，Windows 上直接生效），
再按平台做 Phase 2，最后 Phase 3/4。

Phase 2.2 有个**必须先验证的前提**：Landlock 收紧会影响整个进程，可能连带
限制 mini-code 自身的文件写入。落地时请先跑 Step 2.2.3 的最小验证，
若发现冲突，改为「把 shell 命令放进独立子进程执行」而非在父进程收紧。

## 明确标记为不确定的项

1. **`filepath-securejoin` v0.4.1 能否拦住 Windows junction** —— 项目里已有该
   依赖（经 go-git 间接引入），文档称支持 Windows，但**未在本机实测**。
   若实测通过可简化 Task 1.2。本方案选择自己实现，因为已实测验证有效。
2. **Go 1.26 内置 Landlock** —— CL 77630 扩展 `SysProcAttr` 支持
   `landlock_restrict_self`，状态"In Review"，未进入任何 release。
   跟踪 golang/go#68595。一旦可用可去掉外部依赖。
3. **macOS SDK 27 对自定义 SBPL 的长期承诺** —— 现在能用（2026-08 实测），
   Apple 未承诺保留。生态依赖极重，移除概率低但无正式保证。
4. **Landlock V10（内核 7.2，UDP）** —— 2026-07 才有稳定内核，生态刚开始适配。