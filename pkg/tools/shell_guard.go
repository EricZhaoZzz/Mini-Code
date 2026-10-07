package tools

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// shellDenylist 不可逆 / 破坏性命令黑名单，命中即拒绝，不提供绕过开关。
//
// 目标不是穷尽所有危险写法（做不到），而是拦住手滑和低级误操作。
// 正则注意点：Windows 盘符用 [\\/]? 而非 \\?，后者只能匹配 `C:\`，
// 会漏掉 `C:/` 和 `C:` 两种写法。
var shellDenylist = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	// Unix 递归删除根目录 / 家目录
	{regexp.MustCompile(`(?i)\brm\s+(-[a-z]*[rf][a-z]*\s+)+(/|/\*|~|~/\*|\$HOME)\s*$`), "递归删除根目录或家目录"},
	{regexp.MustCompile(`(?i)\brm\s+(-[a-z]*[rf][a-z]*\s+)+/[a-z]+\s*$`), "递归删除根目录"},
	// 磁盘与文件系统
	{regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`), "格式化文件系统"},
	{regexp.MustCompile(`(?i)\bdd\b[^|]*\bof=/dev/`), "向块设备写入"},
	{regexp.MustCompile(`(?i)>\s*/dev/(sd|nvme|hd|vd)[a-z0-9]*`), "向块设备写入"},
	// fork bomb
	{regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`), "fork bomb"},
	// 关机重启
	{regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`), "关机或重启"},
	// Windows 盘符级破坏（del / rd / rmdir / removedir + 盘符结尾）
	{regexp.MustCompile(`(?i)\b(del|rd|rmdir|removedir)\b[^|]*\b[a-z]:[\\/]?\s*$`), "删除整个盘符根目录"},
	{regexp.MustCompile(`(?i)\bformat\s+[a-z]:`), "格式化磁盘"},
	// 注册表与系统配置
	{regexp.MustCompile(`(?i)\breg\s+delete\s+HK(LM|CRU)`), "删除注册表机器/用户级键"},
	// 权限与属主
	{regexp.MustCompile(`(?i)\bchmod\s+-R\s+[0-7]*7[0-7]*\s+/`), "递归开放根目录权限"},
	{regexp.MustCompile(`(?i)\bchown\s+-R\b.*\s+/`), "递归修改根目录属主"},
	// 管道执行远程脚本
	{regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba)?sh`), "管道执行远程脚本"},
	{regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(python|perl|ruby)\b`), "管道执行远程脚本"},
}

// shellAllowlist 严格模式（LM_SHELL_STRICT=1）下的命令白名单。
// 只有首词命中其中之一才放行；组合命令（&& / || / ;）可命中任意一段。
var shellAllowlist = map[string]bool{
	// 版本控制与构建
	"git": true, "go": true, "gofmt": true, "goimports": true,
	"npm": true, "npx": true, "node": true, "pnpm": true, "yarn": true,
	"python": true, "python3": true, "pip": true, "pytest": true,
	"cargo": true, "rustc": true, "make": true, "cmake": true,
	"mvn": true, "gradle": true, "dotnet": true, "java": true,
	// 只读与浏览
	"ls": true, "dir": true, "cat": true, "type": true,
	"head": true, "tail": true, "wc": true, "grep": true, "rg": true,
	"find": true, "fd": true, "tree": true, "diff": true, "jq": true,
	"echo": true, "pwd": true, "sort": true, "uniq": true, "sed": true, "awk": true,
	"which": true, "where": true, "whoami": true, "date": true,
}

// CheckShellCommand 校验命令是否允许执行。
//
// 默认仅拦截黑名单中的破坏性命令；设置 LM_SHELL_STRICT=1 后改为白名单模式。
// 黑名单优先级更高：即使命令首词在白名单内，命中黑名单仍会被拒绝。
//
// 注意：这不是沙箱。通过检查的命令依然能访问工作区之外的文件
// （run_shell 只设置工作目录，不限制路径），因此本函数用于防止误操作，
// 不能当作对抗恶意输入的安全边界。
func CheckShellCommand(command string) error {
	normalized := strings.TrimSpace(command)
	if normalized == "" {
		return fmt.Errorf("命令为空")
	}

	// 展开行尾续行符，避免用 `\` + 换行拆开命令绕过匹配
	normalized = strings.ReplaceAll(normalized, "\\\r\n", " ")
	normalized = strings.ReplaceAll(normalized, "\\\n", " ")
	lower := strings.ToLower(normalized)

	for _, entry := range shellDenylist {
		if entry.pattern.MatchString(lower) || entry.pattern.MatchString(normalized) {
			return fmt.Errorf("命令被安全策略拒绝（%s）。如确需执行，请手动在终端运行。\n被拒绝的命令: %s",
				entry.reason, normalized)
		}
	}

	if os.Getenv("LM_SHELL_STRICT") == "1" && !shellCommandAllowed(lower) {
		return fmt.Errorf("严格模式（LM_SHELL_STRICT=1）下只允许白名单命令，被拒绝: %s", normalized)
	}

	return nil
}

// shellCommandAllowed 判断命令是否命中白名单。
// 组合命令（cd pkg && go test）只要有一段命中即放行。
func shellCommandAllowed(lower string) bool {
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}

	for _, field := range fields {
		// 允许包装器与环境切换指令
		if field == "&&" || field == "||" || field == ";" || field == "|" {
			continue
		}
		if shellAllowlist[field] {
			return true
		}
	}

	return false
}
