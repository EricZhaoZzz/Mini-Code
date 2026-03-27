package ui

import (
	"testing"
	"time"
)

func TestEscMonitor_New(t *testing.T) {
	m := NewEscMonitor()
	if m == nil {
		t.Fatal("NewEscMonitor returned nil")
	}
	if m.doubleEscWindow != 1500*time.Millisecond {
		t.Errorf("expected doubleEscWindow to be 1.5s, got %v", m.doubleEscWindow)
	}
}

func TestEscMonitor_SetCancelFunc(t *testing.T) {
	m := NewEscMonitor()
	cancelCalled := false
	cancelFunc := func() {
		cancelCalled = true
	}

	m.SetCancelFunc(cancelFunc)
	if m.cancelFunc == nil {
		t.Error("cancelFunc should not be nil after SetCancelFunc")
	}

	// 调用设置的 cancel 函数
	m.cancelFunc()
	if !cancelCalled {
		t.Error("cancelFunc was not called")
	}
}

func TestEscMonitor_StartStop(t *testing.T) {
	m := NewEscMonitor()

	// 启动监控
	m.Start()
	if !m.running {
		t.Error("monitor should be running after Start")
	}
	if !m.enabled {
		t.Error("monitor should be enabled after Start")
	}

	// 等待一小段时间让 goroutine 启动
	time.Sleep(50 * time.Millisecond)

	// 停止监控
	m.Stop()
	if m.running {
		t.Error("monitor should not be running after Stop")
	}
	if m.enabled {
		t.Error("monitor should not be enabled after Stop")
	}
}

func TestEscMonitor_DoubleEscTrigger(t *testing.T) {
	m := NewEscMonitor()
	cancelCalled := false
	m.SetCancelFunc(func() {
		cancelCalled = true
	})

	// 启用监控
	m.enabled = true

	// 第一次 Esc
	triggered := m.HandleEsc()
	if triggered {
		t.Error("first Esc should not trigger cancel")
	}
	if m.lastEscTime.IsZero() {
		t.Error("lastEscTime should be set after first Esc")
	}

	// 第二次 Esc（立即）
	triggered = m.HandleEsc()
	if !triggered {
		t.Error("second Esc should trigger cancel")
	}
	if m.enabled {
		t.Error("monitor should be disabled after double Esc")
	}
	
	// cancel 函数在 goroutine 中调用，等待一下
	time.Sleep(10 * time.Millisecond)
	if !cancelCalled {
		t.Error("cancel function should have been called")
	}
}

func TestEscMonitor_SlowDoubleEscNoTrigger(t *testing.T) {
	m := NewEscMonitor()
	m.doubleEscWindow = 100 * time.Millisecond // 设置较短的窗口

	m.enabled = true

	// 第一次 Esc
	triggered := m.HandleEsc()
	if triggered {
		t.Error("first Esc should not trigger cancel")
	}

	// 等待超过窗口时间
	time.Sleep(150 * time.Millisecond)

	// 第二次 Esc（超过窗口时间）- 应该被视为第一次
	triggered = m.HandleEsc()
	if triggered {
		t.Error("slow double Esc should not trigger cancel")
	}
}

func TestEscMonitor_DisabledNoTrigger(t *testing.T) {
	m := NewEscMonitor()
	m.enabled = false // 禁用状态

	// 即使快速按两次也不应该触发
	triggered := m.HandleEsc()
	if triggered {
		t.Error("disabled monitor should not trigger")
	}
}

func TestEscMonitor_Reset(t *testing.T) {
	m := NewEscMonitor()
	m.lastEscTime = time.Now()
	m.cancelled = true

	m.Reset()

	if !m.lastEscTime.IsZero() {
		t.Error("lastEscTime should be zero after Reset")
	}
	if m.cancelled {
		t.Error("cancelled should be false after Reset")
	}
}

func TestEscMonitor_IsEnabled(t *testing.T) {
	m := NewEscMonitor()
	m.enabled = true
	if !m.IsEnabled() {
		t.Error("IsEnabled should return true")
	}

	m.enabled = false
	if m.IsEnabled() {
		t.Error("IsEnabled should return false")
	}
}

func TestEscMonitor_IsCancelled(t *testing.T) {
	m := NewEscMonitor()
	m.cancelled = true
	if !m.IsCancelled() {
		t.Error("IsCancelled should return true")
	}

	m.cancelled = false
	if m.IsCancelled() {
		t.Error("IsCancelled should return false")
	}
}

func TestGlobalEscMonitor(t *testing.T) {
	// 测试全局实例
	if GlobalEscMonitor == nil {
		t.Fatal("GlobalEscMonitor should not be nil")
	}
}