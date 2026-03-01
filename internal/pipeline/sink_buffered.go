package pipeline

import (
	"context"
	"sync"
	"time"

	"go-xwatch/internal/journal"
)

// BufferedSink batches entries before forwarding to an underlying sink.
// Flush triggers when maxBatch is reached or window elapses since the first buffered item.
type BufferedSink struct {
	sink     EventSink
	window   time.Duration
	maxBatch int

	mu      sync.Mutex
	buf     []journal.Entry
	firstAt time.Time
}

func NewBufferedSink(sink EventSink, window time.Duration, maxBatch int) *BufferedSink {
	if window <= 0 {
		window = 5 * time.Second
	}
	if maxBatch <= 0 {
		maxBatch = 512
	}
	return &BufferedSink{sink: sink, window: window, maxBatch: maxBatch}
}

func (b *BufferedSink) Handle(ctx context.Context, entries []journal.Entry) error {
	now := time.Now()
	b.mu.Lock()
	if len(entries) > 0 {
		b.buf = append(b.buf, entries...)
		if b.firstAt.IsZero() {
			b.firstAt = now
		}
	}

	shouldFlush := false
	if b.maxBatch > 0 && len(b.buf) >= b.maxBatch {
		shouldFlush = true
	}
	if !shouldFlush && b.window > 0 && !b.firstAt.IsZero() && now.Sub(b.firstAt) >= b.window {
		shouldFlush = true
	}

	if !shouldFlush || len(b.buf) == 0 {
		b.mu.Unlock()
		return nil
	}

	pending := append([]journal.Entry(nil), b.buf...)
	b.buf = b.buf[:0]
	b.firstAt = time.Time{}
	b.mu.Unlock()

	if err := b.sink.Handle(ctx, pending); err != nil {
		b.mu.Lock()
		b.buf = append(pending, b.buf...)
		if b.firstAt.IsZero() {
			b.firstAt = now
		}
		b.mu.Unlock()
		return err
	}

	return nil
}
