//go:build windows
// +build windows

package ui

import (
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	getNumberOfConsoleInputEventsProc = kernel32.NewProc("GetNumberOfConsoleInputEvents")
	readConsoleInputProc   = kernel32.NewProc("ReadConsoleInputW")
	getStdHandleProc       = kernel32.NewProc("GetStdHandle")
)

const (
	STD_INPUT_HANDLE = ^uint32(0) - 10 // -10
	KEY_EVENT        = 0x0001
)

type inputRecord struct {
	eventType uint16
	padding   uint16
	keyEvent  keyEventRecord
}

type keyEventRecord struct {
	bKeyDown         int32
	wRepeatCount     uint16
	wVirtualKeyCode  uint16
	wVirtualScanCode uint16
	unicodeChar      uint16
	dwControlKeyState uint32
}

// nonBlockingRead 非阻塞读取（Windows 版本）
func nonBlockingRead(fd int, buf []byte) (int, error) {
	// 获取标准输入句柄
	stdinHandle, _, _ := getStdHandleProc.Call(uintptr(STD_INPUT_HANDLE))

	// 检查是否有输入事件
	var numEvents uint32
	ret, _, _ := getNumberOfConsoleInputEventsProc.Call(
		stdinHandle,
		uintptr(unsafe.Pointer(&numEvents)),
	)

	if ret == 0 || numEvents == 0 {
		// 没有输入事件
		return 0, nil
	}

	// 读取输入事件
	var inputRecord inputRecord
	var numRead uint32

	for numEvents > 0 {
		ret, _, err := readConsoleInputProc.Call(
			stdinHandle,
			uintptr(unsafe.Pointer(&inputRecord)),
			1,
			uintptr(unsafe.Pointer(&numRead)),
		)

		if ret == 0 {
			return 0, err
		}

		// 检查是否是按键按下事件
		if inputRecord.eventType == KEY_EVENT && inputRecord.keyEvent.bKeyDown != 0 {
			// 获取按键字符
			char := byte(inputRecord.keyEvent.unicodeChar)
			if char != 0 {
				buf[0] = char
				return 1, nil
			}
		}

		numEvents--
	}

	return 0, nil
}