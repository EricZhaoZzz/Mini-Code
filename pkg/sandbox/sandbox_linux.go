//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// linuxMechanism 标识 Landlock。
const linuxMechanism = "landlock"

// Landlock syscall 号（amd64 与 arm64 相同）。
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

// landlockRuleTypePathBeneath 是 LANDLOCK_RULE_PATH_BENEATH。
const landlockRuleTypePathBeneath = 1

// landlockCreateRulesetVersion 用于探测内核支持的 ABI 版本。
const landlockCreateRulesetVersion = 1 << 0

// prSetNoNewPrivs：调用 landlock_restrict_self 前必须先设置，否则返回 EPERM。
const prSetNoNewPrivs = 38

// AccessFS 权限位（ABI v1 起）。
const (
	fsExecute   = 1 << 0
	fsWriteFile = 1 << 1
	fsReadFile  = 1 << 2
	fsReadDir   = 1 << 3

	fsRemoveDir  = 1 << 4
	fsRemoveFile = 1 << 5
	fsMakeChar   = 1 << 6
	fsMakeDir    = 1 << 7
	fsMakeReg    = 1 << 8
	fsMakeSock   = 1 << 9
	fsMakeFifo   = 1 << 10
	fsMakeBlock  = 1 << 11
	fsMakeSym    = 1 << 12

	fsRefer    = 1 << 13 // ABI v2
	fsTruncate = 1 << 14 // ABI v3
)

// rulesetAttr 对应 struct landlock_ruleset_attr。
// 只用 8 字节（ABI v1 布局），避免触碰 ABI v4 的 net 字段。
type rulesetAttr struct {
	handledAccessFS uint64
}

// pathBeneathAttr 对应 struct landlock_path_beneath_attr。
type pathBeneathAttr struct {
	allowedAccess uint64
	parentFD      int32
	_             int32 // 显式补齐对齐填充
}

// GetStatus 探测 Landlock 是否可用。
func GetStatus(cfg Config) Status {
	st := Status{
		Mechanism:     linuxMechanism,
		WritableRoots: cfg.ExtraWritableRoots,
	}

	abi, err := abiVersion()
	if err != nil || abi < 1 {
		st.Reason = fmt.Sprintf("Landlock 不可用（内核 < 5.13 或未启用）: %v", err)
		return st
	}

	st.Available = true
	return st
}

// abiVersion 返回内核支持的 Landlock ABI 版本。
func abiVersion() (int, error) {
	r0, _, errno := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if errno != 0 {
		return 0, errno
	}
	return int(r0), nil
}

// Wrap 为命令注入 Landlock 规则。
//
// ⚠️ 关键前提：Landlock 限制整个调用线程且不可逆。在主进程收紧会连带
// 限制 mini-code 自身的文件写入（记忆库、审计日志、构建产物都在工作区外）。
//
// 因此这里**不**在主进程调用 restrict_self，而是把命令转交给本进程的
// runner 子进程：runner 启动后先收紧、再 exec 真正的 shell。
// 这样限制只作用于那条命令，且自动继承给它的全部子进程。
func Wrap(_ context.Context, cmd *exec.Cmd, cfg Config) error {
	if cfg.WorkspaceRoot == "" {
		return fmt.Errorf("沙箱配置缺少 WorkspaceRoot")
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法确定 mini-code 可执行文件路径: %w", err)
	}

	orig := append([]string(nil), cmd.Args...)

	cmd.Path = self
	cmd.Args = append([]string{self, RunnerFlag}, orig...)
	cmd.Env = append(cmd.Env,
		"LM_SANDBOX_ROOT="+cfg.WorkspaceRoot,
		"LM_SANDBOX_WRITABLE="+strings.Join(cfg.ExtraWritableRoots, string(os.PathListSeparator)),
		"LM_SANDBOX_READONLY="+strings.Join(cfg.ReadOnlyPaths, string(os.PathListSeparator)),
	)

	return nil
}

// RunSandboxed 作为 runner 子进程运行：收紧 Landlock 后 exec 目标命令。
//
// args[0] 是 RunnerFlag，其后为原始命令及其参数。
func RunSandboxed(args []string) error {
	if len(args) == 0 || args[0] != RunnerFlag {
		return fmt.Errorf("runner 参数格式错误: %v", args)
	}

	cmdArgs := args[1:]
	if len(cmdArgs) == 0 {
		return fmt.Errorf("runner 缺少待执行命令")
	}

	cfg := Config{
		WorkspaceRoot:      os.Getenv("LM_SANDBOX_ROOT"),
		ExtraWritableRoots: splitPathList(os.Getenv("LM_SANDBOX_WRITABLE")),
		ReadOnlyPaths:      splitPathList(os.Getenv("LM_SANDBOX_READONLY")),
	}
	if cfg.WorkspaceRoot == "" {
		// 没有显式配置时退化为「当前目录」，runner 只由主进程唤起，
		// 正常情况下不会走到这里。
		if wd, err := os.Getwd(); err == nil {
			cfg.WorkspaceRoot = wd
		}
	}

	if err := restrictSelf(cfg); err != nil {
		return err
	}

	path, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("runner 找不到可执行文件 %q: %w", cmdArgs[0], err)
	}

	// 进程被替换，成功后不再返回
	return syscall.Exec(path, cmdArgs, os.Environ())
}

// restrictSelf 收紧当前进程的文件访问。
//
// 顺序要求：先 PR_SET_NO_NEW_PRIVS，再 create_ruleset → add_rule → restrict_self。
// 顺序颠倒会直接 EPERM。
func restrictSelf(cfg Config) error {
	if cfg.WorkspaceRoot == "" {
		return fmt.Errorf("沙箱配置缺少 WorkspaceRoot")
	}

	abi, err := abiVersion()
	if err != nil || abi < 1 {
		return fmt.Errorf("Landlock 不可用（内核 < 5.13 或未启用）: %v", err)
	}

	// 收紧后不可逆，且会限制后续所有文件访问，
	// 因此必须在此之前完成一切需要的准备工作。
	if _, _, errno := syscall.Syscall(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0); errno != 0 {
		return fmt.Errorf("设置 PR_SET_NO_NEW_PRIVS 失败: %w", errno)
	}

	handled := supportedAccess(abi)
	attr := rulesetAttr{handledAccessFS: handled}

	fd, err := landlockCreateRuleset(unsafe.Pointer(&attr), unsafe.Sizeof(attr), 0)
	if err != nil {
		return fmt.Errorf("创建 Landlock ruleset 失败: %w", err)
	}
	defer syscall.Close(fd)

	readWrite := append([]string{cfg.WorkspaceRoot}, cfg.ExtraWritableRoots...)
	for _, p := range readWrite {
		if err := addPathRule(fd, p, handled); err != nil {
			return fmt.Errorf("添加可读写规则失败 %q: %w", p, err)
		}
	}

	readOnly := readOnlyAccess(abi)
	for _, p := range cfg.ReadOnlyPaths {
		if err := addPathRule(fd, p, readOnly); err != nil {
			return fmt.Errorf("添加只读规则失败 %q: %w", p, err)
		}
	}

	if err := landlockRestrictSelf(fd, 0); err != nil {
		return fmt.Errorf("启用 Landlock 沙箱失败: %w", err)
	}

	return nil
}

// supportedAccess 按 ABI 版本裁剪可处理的权限位。
//
// 传入内核不支持的位会让 create_ruleset 返回 EINVAL，必须裁剪。
func supportedAccess(abi int) uint64 {
	access := uint64(fsExecute | fsWriteFile | fsReadFile | fsReadDir |
		fsRemoveDir | fsRemoveFile | fsMakeChar | fsMakeDir | fsMakeReg |
		fsMakeSock | fsMakeFifo | fsMakeBlock | fsMakeSym)

	// REFER（跨目录 rename/link，ABI ≥ 2）刻意**不**授予：
	// 授予它会让工作区内的文件被移动到工作区外，破坏边界。
	// 代价是部分构建脚本的跨目录移动会被拒绝，安全性优先。

	if abi >= 3 {
		// TRUNCATE 是独立权限：只给 WRITE_FILE 时，覆盖写现有文件
		//（O_TRUNC）仍然会失败
		access |= fsTruncate
	}

	return access
}

// readOnlyAccess 返回只读规则所需的权限位。
func readOnlyAccess(abi int) uint64 {
	// EXECUTE 必须显式授予，否则二进制无法执行——
	// 但只对只读工具链路径授予，不给工作区，避免在工作区内执行下载物。
	access := uint64(fsReadFile | fsReadDir | fsExecute)
	if abi >= 3 {
		access |= fsTruncate
	}
	return access
}

// addPathRule 为路径添加一条规则。路径不存在时静默跳过
// （工具链路径在不同环境下可能缺失）。
func addPathRule(rulesetFD int, path string, access uint64) error {
	abs := filepath.Clean(path)
	if abs == "" {
		return nil
	}

	fd, err := syscall.Open(abs, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		// 路径不存在或不是目录 → 跳过
		return nil
	}
	defer syscall.Close(fd)

	attr := pathBeneathAttr{allowedAccess: access, parentFD: int32(fd)}
	_, _, errno := syscall.Syscall6(sysLandlockAddRule,
		uintptr(rulesetFD), uintptr(landlockRuleTypePathBeneath),
		uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// landlockCreateRuleset 创建 ruleset 并返回其文件描述符。
func landlockCreateRuleset(attr unsafe.Pointer, size uintptr, flags uint32) (int, error) {
	r0, _, errno := syscall.Syscall(sysLandlockCreateRuleset,
		uintptr(attr), size, uintptr(flags))
	if errno != 0 {
		return 0, errno
	}
	return int(r0), nil
}

// landlockRestrictSelf 把 ruleset 应用到当前线程。
func landlockRestrictSelf(rulesetFD int, flags uint32) error {
	_, _, errno := syscall.Syscall(sysLandlockRestrictSelf,
		uintptr(rulesetFD), uintptr(flags), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// CleanupProfiles 在 Linux 上无操作（不生成 profile 文件）。
func CleanupProfiles() {}

// DefaultWritableRoots 返回默认额外可写目录。
//
// 这些是构建工具必需的缓存目录，否则 go build / npm install 会失败。
// TMPDIR 在多数 Linux 发行版上并未设置，因此额外兜底 /tmp 与 /var/tmp。
func DefaultWritableRoots() []string {
	var roots []string
	for _, env := range []string{"GOCACHE", "GOMODCACHE", "TMPDIR", "TEMP", "TMP"} {
		if p := os.Getenv(env); p != "" {
			roots = append(roots, p)
		}
	}
	for _, p := range []string{"/tmp", "/var/tmp"} {
		if _, err := os.Stat(p); err == nil {
			roots = append(roots, p)
		}
	}
	return roots
}

// DefaultReadOnlyPaths 返回默认只读工具链目录。
func DefaultReadOnlyPaths() []string {
	var paths []string
	if goroot := runtime.GOROOT(); goroot != "" {
		paths = append(paths, goroot)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		paths = append(paths, gopath)
	}
	for _, p := range []string{"/usr", "/lib", "/lib64", "/bin", "/sbin", "/etc", "/dev"} {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}
