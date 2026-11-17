//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

// TestProfileQuotesEscaped SBPL profile 中的引号与反斜杠必须被转义。
//
// 路径来自用户环境，未转义会破坏 profile 语法，甚至被注入额外规则。
func TestProfileQuotesEscaped(t *testing.T) {
	if got := quoteSBPL(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("quoteSBPL 转义错误，得到 %s", got)
	}
}

// TestSandboxProfileContainsWorkspace profile 必须显式允许工作区读写。
func TestSandboxProfileContainsWorkspace(t *testing.T) {
	ws := t.TempDir()
	profile := sandboxProfile(Config{WorkspaceRoot: ws})
	if !strings.Contains(profile, ws) {
		t.Errorf("profile 未包含工作区路径: %s", profile)
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Errorf("profile 应以 deny default 开头: %s", profile)
	}
}
