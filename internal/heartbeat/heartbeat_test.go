package heartbeat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_DefaultInterval(t *testing.T) {
	h := New(0, nil)
	if h.Interval() != time.Duration(DefaultInterval)*time.Second {
		t.Fatalf("expected default interval %v, got %v", time.Duration(DefaultInterval)*time.Second, h.Interval())
	}
}

func TestNew_CustomInterval(t *testing.T) {
	h := New(5*time.Second, nil)
	if h.Interval() != 5*time.Second {
		t.Fatalf("expected 5s interval, got %v", h.Interval())
	}
}

func TestHeartbeat_NotRunningBeforeStart(t *testing.T) {
	h := New(time.Minute, nil)
	if h.Running() {
		t.Fatal("should not be running before Start()")
	}
}

func TestHeartbeat_RunningAfterStart(t *testing.T) {
	h := New(time.Minute, nil)
	ctx := context.Background()
	h.Start(ctx)
	defer h.Stop()

	if !h.Running() {
		t.Fatal("should be running after Start()")
	}
}

func TestHeartbeat_NotRunningAfterStop(t *testing.T) {
	h := New(time.Minute, nil)
	ctx := context.Background()
	h.Start(ctx)
	h.Stop()

	// 給 goroutine 時間更新狀態
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !h.Running() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("should not be running after Stop()")
}

func TestHeartbeat_TickCalled(t *testing.T) {
	var count atomic.Int64
	h := New(20*time.Millisecond, func(t time.Time) {
		count.Add(1)
	})
	ctx := context.Background()
	h.Start(ctx)
	defer h.Stop()

	time.Sleep(120 * time.Millisecond)

	got := count.Load()
	if got < 2 {
		t.Fatalf("expected at least 2 ticks in 120ms with 20ms interval, got %d", got)
	}
}

func TestHeartbeat_StopStopsTicks(t *testing.T) {
	var count atomic.Int64
	h := New(20*time.Millisecond, func(t time.Time) {
		count.Add(1)
	})
	ctx := context.Background()
	h.Start(ctx)

	time.Sleep(60 * time.Millisecond)
	h.Stop()
	countAfterStop := count.Load()

	// 停止後等一段時間，確認 count 不再增加
	time.Sleep(60 * time.Millisecond)
	if count.Load() > countAfterStop+1 {
		t.Fatalf("ticks should stop after Stop(); count before=%d after=%d", countAfterStop, count.Load())
	}
}

func TestHeartbeat_StartIdempotent(t *testing.T) {
	var count atomic.Int64
	h := New(20*time.Millisecond, func(t time.Time) {
		count.Add(1)
	})
	ctx := context.Background()
	// 重複呼叫 Start 不應同時啟動多個 goroutine
	h.Start(ctx)
	h.Start(ctx)
	h.Start(ctx)
	defer h.Stop()

	if !h.Running() {
		t.Fatal("should be running")
	}
}

func TestHeartbeat_ContextCancel(t *testing.T) {
	h := New(time.Minute, nil)
	ctx, cancel := context.WithCancel(context.Background())
	h.Start(ctx)
	cancel()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !h.Running() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("heartbeat should stop when context is cancelled")
}

func TestHeartbeat_NilOnTick(t *testing.T) {
	h := New(20*time.Millisecond, nil)
	ctx := context.Background()
	h.Start(ctx)
	time.Sleep(60 * time.Millisecond)
	h.Stop() // 不應 panic
}
