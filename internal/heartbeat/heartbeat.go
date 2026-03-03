// Package heartbeat 提供週期性心跳訊號功能，可用於確認服務正常運作。
package heartbeat

import (
	"context"
	"sync"
	"time"
)

const DefaultInterval = 60 // 預設心跳間隔（秒）

// Heartbeat 以固定間隔定期呼叫回呼函式。
type Heartbeat struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	interval time.Duration
	onTick   func(time.Time)
}

// New 建立一個 Heartbeat，interval 為心跳間隔，onTick 為每次心跳時呼叫的函式。
// 若 interval <= 0，使用 DefaultInterval 秒。
func New(interval time.Duration, onTick func(time.Time)) *Heartbeat {
	if interval <= 0 {
		interval = time.Duration(DefaultInterval) * time.Second
	}
	return &Heartbeat{
		interval: interval,
		onTick:   onTick,
	}
}

// Start 在背景 goroutine 中啟動心跳迴圈。若已在執行則為 no-op。
func (h *Heartbeat) Start(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return
	}
	ctx, h.cancel = context.WithCancel(ctx)
	h.running = true
	go h.loop(ctx)
}

// Stop 停止心跳迴圈。
func (h *Heartbeat) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	h.running = false
}

// Running 回傳心跳是否正在執行中。
func (h *Heartbeat) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// Interval 回傳心跳間隔。
func (h *Heartbeat) Interval() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interval
}

func (h *Heartbeat) loop(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	defer func() {
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
	}()
	for {
		select {
		case t := <-ticker.C:
			if h.onTick != nil {
				h.onTick(t)
			}
		case <-ctx.Done():
			return
		}
	}
}
