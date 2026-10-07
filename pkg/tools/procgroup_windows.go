//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup 让子进程进入独立进程组。
//
// go-cmd v1.4.3 默认不设置任何 SysProcAttr，其 Stop() 会用 taskkill /T
// 按进程树终止——这会连带杀掉调用方自身的进程（实测把 go test 进程一起干掉）。
// CREATE_NEW_PROCESS_GROUP 让终止只影响该命令的进程组。
func isolateProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}
