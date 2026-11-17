package tools

import (
	"fmt"
	"sync"
)

// ConfirmHandler 向用户发起确认，返回用户是否同意。
// 由 main.go 注入（CLI 下从终端读取 y/n）；不能交给模型自己决定。
type ConfirmHandler func(prompt string) (bool, error)

var (
	confirmHandlerMu sync.RWMutex
	confirmHandler   ConfirmHandler

	// confirmSerial 串行化确认请求：同一轮多个工具并发执行时，
	// 交互提示必须逐个出现，否则用户的回答会对应到错误的操作上。
	confirmSerial sync.Mutex
)

// SetConfirmHandler 注入用户确认处理器；传 nil 取消注入。
//
// 未注入时（单测、无交互终端）危险操作回退为旧行为：
// 仅校验模型传入的 confirm 参数。
func SetConfirmHandler(h ConfirmHandler) {
	confirmHandlerMu.Lock()
	defer confirmHandlerMu.Unlock()
	confirmHandler = h
}

func currentConfirmHandler() ConfirmHandler {
	confirmHandlerMu.RLock()
	defer confirmHandlerMu.RUnlock()
	return confirmHandler
}

// askUser 请求用户确认。interactive=false 表示未注入处理器，调用方应走回退逻辑。
func askUser(prompt string) (interactive bool, approved bool, err error) {
	h := currentConfirmHandler()
	if h == nil {
		return false, false, nil
	}

	confirmSerial.Lock()
	defer confirmSerial.Unlock()

	approved, err = h(prompt)
	if err != nil {
		return true, false, fmt.Errorf("获取用户确认失败: %w", err)
	}
	return true, approved, nil
}
