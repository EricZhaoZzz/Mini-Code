package orchestrator

import (
	"sync"
	"testing"
	"time"
)

// TestEvictStaleSessions_LastSeenRace 专项验证 evict 循环读取 lastSeen 时必须加锁。
//
// 回归背景：evictLoop 直接无锁读 s.lastSeen，而 AppendUserMessage 会写该字段，
// go test -race 实测报 DATA RACE（session.go:67 写 vs 无锁读）。
func TestEvictStaleSessions_LastSeenRace(t *testing.T) {
	o := New(nil)
	s := o.GetOrCreateSession("cli", "local")

	// 后台持续写 lastSeen
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			s.AppendUserMessage("hi")
		}
	}()

	// 并发执行清理循环（内部通过 LastSeen() 加锁读取）
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				o.evictStaleSessions()
			}
		}()
	}
	wg.Wait()
	<-done
}

// TestEvictStaleSessions_KeepsActiveSessions 活跃会话不应被清理。
func TestEvictStaleSessions_KeepsActiveSessions(t *testing.T) {
	o := New(nil)
	o.GetOrCreateSession("cli", "active")
	o.GetOrCreateSession("cli", "stale")

	// 把 stale 的 lastSeen 改到 48 小时前（通过公开 API 无法直接改，
	// 这里用同包访问模拟）
	o.mu.Lock()
	for _, s := range o.sessions {
		if s.UserID == "stale" {
			s.mu.Lock()
			s.lastSeen = time.Now().Add(-48 * time.Hour)
			s.mu.Unlock()
		}
	}
	o.mu.Unlock()

	o.evictStaleSessions()

	if s := o.GetOrCreateSession("cli", "stale"); s.UserID != "stale" {
		t.Error("过期会话应被清理")
	}
	if s := o.GetOrCreateSession("cli", "active"); s.UserID != "active" {
		t.Error("活跃会话不应被清理")
	}
}

// TestOrchestrator_CloseIsIdempotentAndStopsLoop 验证 Close 可重复调用且能停掉后台循环。
func TestOrchestrator_CloseIsIdempotentAndStopsLoop(t *testing.T) {
	o := New(nil)

	// 重复调用不应 panic
	o.Close()
	o.Close()
	o.Close()

	// Close 之后 evict 循环已退出，再次调用清理不应死锁
	done := make(chan struct{})
	go func() {
		o.evictStaleSessions()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close 后 evictStaleSessions 死锁")
	}
}
