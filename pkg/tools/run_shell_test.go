package tools

import (
	"runtime"
	"strings"
	"testing"
)

func TestRunShellReturnsStdoutAndStderr(t *testing.T) {
	workspace := t.TempDir()
	restore := chdirForTest(t, workspace)
	defer restore()

	var command string
	if runtime.GOOS == "windows" {
		command = `echo hello&& echo warn 1>&2`
	} else {
		command = `printf 'hello\n'; printf 'warn\n' >&2`
	}

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
