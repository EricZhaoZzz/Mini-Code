//go:build !windows
// +build !windows

package ui

import (
	"syscall"
)

// nonBlockingRead 非阻塞读取（Unix 版本）
func nonBlockingRead(fd int, buf []byte) (int, error) {
	// 获取当前文件描述符标志
	oldFlags, err := syscall.FcntlInt(uintptr(fd), syscall.F_GETFL, 0)
	if err != nil {
		return 0, err
	}

	// 设置非阻塞标志
	if err := syscall.SetNonblock(fd, true); err != nil {
		return 0, err
	}

	// 尝试读取
	n, err := syscall.Read(fd, buf)

	// 恢复原始标志
	syscall.SetNonblock(fd, (oldFlags&syscall.O_NONBLOCK) != 0)

	return n, err
}