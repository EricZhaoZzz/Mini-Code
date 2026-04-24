package tools

import (
	"runtime"
	"strings"
	"testing"
)

func TestRunShellInterceptsMiniCodeRestart(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	called := false
	SetRestartHandler(func() (string, error) {
		called = true
		return "restart-scheduled", nil
	})
	defer SetRestartHandler(nil)

	result, err := RunShell(RunShellArguments{Command: "mini-code restart"})
	if err != nil {
		t.Fatalf("expected restart interception to succeed, got error: %v", err)
	}
	if !called {
		t.Fatal("expected restart handler to be called")
	}
	if result.(string) != "restart-scheduled" {
		t.Fatalf("expected restart handler output, got %q", result.(string))
	}
}

func TestRunShellKeepsNormalCommandsUntouchedWhenRestartHandlerExists(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	called := false
	SetRestartHandler(func() (string, error) {
		called = true
		return "restart-scheduled", nil
	})
	defer SetRestartHandler(nil)

	result, err := RunShell(RunShellArguments{Command: normalEchoCommand()})
	if err != nil {
		t.Fatalf("expected normal shell command to succeed, got error: %v", err)
	}
	if called {
		t.Fatal("expected normal shell command not to call restart handler")
	}
	if !strings.Contains(result.(string), "hello") {
		t.Fatalf("expected normal shell output to be preserved, got %q", result.(string))
	}
}

func TestRunShellReturnsStdoutAndStderr(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	command := stderrEchoCommand()

	result, err := RunShell(RunShellArguments{Command: command})
	if err != nil {
		t.Fatalf("expected shell command to succeed, got error: %v", err)
	}

	output := result.(string)
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected stdout in output, got: %q", output)
	}
	if !strings.Contains(output, "[stderr]: warn") {
		t.Fatalf("expected stderr in output, got: %q", output)
	}
}

func TestRunShellReturnsErrorOnNonZeroExit(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	var command string
	if runtime.GOOS == "windows" {
		command = `echo fail&& exit /b 7`
	} else {
		command = `printf 'fail\n'; exit 7`
	}

	result, err := RunShell(RunShellArguments{Command: command})
	if err == nil {
		t.Fatal("expected shell command to fail")
	}
	if !strings.Contains(err.Error(), "命令执行错误") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.(string), "fail") {
		t.Fatalf("expected stdout to be preserved on failure, got: %q", result)
	}
}

func normalEchoCommand() string {
	if runtime.GOOS == "windows" {
		return `echo hello`
	}
	return `printf 'hello\n'`
}

func stderrEchoCommand() string {
	if runtime.GOOS == "windows" {
		return `echo hello&& echo warn 1>&2`
	}
	return `printf 'hello\n'; printf 'warn\n' >&2`
}
