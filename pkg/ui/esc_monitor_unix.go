//go:build !windows
// +build !windows

package ui

import (
	"golang.org/x/sys/unix"
)

// nonBlockingRead 非阻塞读取（Unix 版本）
//
// 注意必须用 golang.org/x/sys/unix 而不是标准库 syscall：
// 标准库 syscall 在 Linux 上没有 FcntlInt（只有 Fcntl），
// 这个错在 Windows 上不会暴露，因为本文件带 !windows 构建约束。
func nonBlockingRead(fd int, buf []byte) (int, error) {
	// 获取当前文件描述符标志
	oldFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return 0, err
	}

	// 设置非阻塞标志
	if err := unix.SetNonblock(fd, true); err != nil {
		return 0, err
	}

	// 尝试读取
	n, err := unix.Read(fd, buf)

	// 恢复原始标志
	_ = unix.SetNonblock(fd, (oldFlags&unix.O_NONBLOCK) != 0)

	return n, err
}
