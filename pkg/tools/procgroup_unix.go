//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup 让子进程进入独立进程组。
//
// 这样超时终止时只杀掉该命令及其子进程，不会波及调用方自身的进程组。
// 需要配合 Setpgid，Stop() 才能按 -pid 终止整个组。
func isolateProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}
