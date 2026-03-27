package ui

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// EscMonitor 监控 Esc 按键，支持双击 Esc 中止任务
type EscMonitor struct {
	cancelFunc      context.CancelFunc
	lastEscTime     time.Time
	mu              sync.Mutex
	enabled         bool
	doubleEscWindow time.Duration // 两次 Esc 之间的最大间隔
	running         bool
	stopChan        chan struct{}
	oldState        *term.State
	cancelled       bool // 标记是否已取消
	stdinFd         int
	doneChan        chan struct{} // 用于等待 goroutine 完全退出
}

// GlobalEscMonitor 全局 Esc 监控器实例
var GlobalEscMonitor = NewEscMonitor()

// NewEscMonitor 创建新的 Esc 监控器
func NewEscMonitor() *EscMonitor {
	return &EscMonitor{
		doubleEscWindow: 1500 * time.Millisecond, // 1.5 秒内按两次 Esc
		stopChan:        make(chan struct{}),
		stdinFd:        int(os.Stdin.Fd()),
		doneChan:       make(chan struct{}),
	}
}

// SetCancelFunc 设置取消函数
func (m *EscMonitor) SetCancelFunc(cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelFunc = cancel
}

// Start 启动监控（在任务执行期间调用）
func (m *EscMonitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.enabled = true
	m.cancelled = false
	m.lastEscTime = time.Time{}
	m.stopChan = make(chan struct{})
	m.doneChan = make(chan struct{})
	m.mu.Unlock()

	// 启动键盘监听 goroutine
	go m.monitorKeyboard()
}

// Stop 停止监控
func (m *EscMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}

	m.running = false
	m.enabled = false

	// 关闭 stopChan 来通知 goroutine 退出
	select {
	case <-m.stopChan:
		// 已经关闭
	default:
		close(m.stopChan)
	}
	m.mu.Unlock()

	// 等待 goroutine 完全退出
	<-m.doneChan

	// 确保终端状态已恢复
	m.mu.Lock()
	if m.oldState != nil {
		term.Restore(m.stdinFd, m.oldState)
		m.oldState = nil
	}
	m.mu.Unlock()
}

// IsCancelled 返回是否已取消
func (m *EscMonitor) IsCancelled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancelled
}

// monitorKeyboard 监听键盘输入
func (m *EscMonitor) monitorKeyboard() {
	defer close(m.doneChan)

	// 获取原始终端状态
	fd := m.stdinFd

	// 检查是否是终端
	if !term.IsTerminal(fd) {
		return
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// 无法设置原始模式（可能是在非终端环境），放弃监听
		return
	}

	m.mu.Lock()
	m.oldState = oldState
	m.mu.Unlock()

	// 确保在退出时恢复终端状态
	defer func() {
		m.mu.Lock()
		if m.oldState != nil {
			term.Restore(fd, m.oldState)
			m.oldState = nil
		}
		m.mu.Unlock()
	}()

	// 使用 ticker 定期检查
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 1)

	for {
		select {
		case <-m.stopChan:
			// 收到停止信号，退出
			return
		case <-ticker.C:
			// 尝试非阻塞读取
			n, err := nonBlockingRead(fd, buf)
			if err != nil {
				// 读取错误，继续
				continue
			}
			if n > 0 && buf[0] == 27 { // ESC 键的 ASCII 码是 27
				if m.HandleEsc() {
					// 双击 Esc 已触发，停止监控
					return
				}
			}
		}
	}
}

// HandleEsc 处理 Esc 按键事件
func (m *EscMonitor) HandleEsc() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return false
	}

	now := time.Now()
	if !m.lastEscTime.IsZero() && now.Sub(m.lastEscTime) < m.doubleEscWindow {
		// 双击 Esc，触发取消
		m.lastEscTime = time.Time{} // 重置
		m.enabled = false           // 防止重复触发
		m.cancelled = true

		fmt.Print("\r\033[K") // 清除当前行
		PrintWarning("检测到双击 Esc，正在中止任务...")

		if m.cancelFunc != nil {
			go m.cancelFunc()
		}
		return true
	}

	// 第一次 Esc
	m.lastEscTime = now
	// 打印提示信息
	fmt.Print("\r\033[K") // 清除当前行
	PrintDim("  [再按一次 Esc 可中止任务]")
	return false
}

// Reset 重置状态
func (m *EscMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastEscTime = time.Time{}
	m.cancelled = false
}

// IsEnabled 返回是否启用
func (m *EscMonitor) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}