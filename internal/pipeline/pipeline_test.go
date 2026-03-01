package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"go-xwatch/internal/journal"
	"go-xwatch/internal/watcher"

	"github.com/fsnotify/fsnotify"
)

type stubSink struct {
	failures int
	calls    int
	records  [][]journal.Entry
}

func (s *stubSink) Handle(_ context.Context, entries []journal.Entry) error {
	s.calls++
	s.records = append(s.records, append([]journal.Entry(nil), entries...))
	if s.failures > 0 {
		s.failures--
		return errors.New("fail")
	}
	return nil
}

func TestAggregatorDedupeKeepsLastPerPath(t *testing.T) {
	agg := NewAggregator()
	t0 := time.Now()
	agg.Add(watcher.Event{Path: "a", Op: fsnotify.Create, TS: t0})
	agg.Add(watcher.Event{Path: "a", Op: fsnotify.Write, TS: t0.Add(time.Millisecond)})
	agg.Add(watcher.Event{Path: "b", Op: fsnotify.Create, TS: t0})

	out := agg.Flush()
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	var a journal.Entry
	for _, e := range out {
		if e.Path == "a" {
			a = e
		}
	}
	if a.Op != fsnotify.Write.String() {
		t.Fatalf("expected last op for a to be WRITE, got %s", a.Op)
	}
}

func TestWriterBackoffAndRecovery(t *testing.T) {
	stub := &stubSink{failures: 2}
	w := NewWriter(stub, slog.New(slog.NewTextHandler(io.Discard, nil)), 10*time.Millisecond, 50*time.Millisecond)

	now := time.Now()
	w.Enqueue([]journal.Entry{{Path: "a"}})

	w.Flush(context.Background(), now)
	if stub.calls != 1 {
		t.Fatalf("expected 1 call, got %d", stub.calls)
	}
	if w.backoff == 0 {
		t.Fatalf("expected backoff to be set")
	}

	// before nextTry -> should not retry
	w.Flush(context.Background(), now.Add(5*time.Millisecond))
	if stub.calls != 1 {
		t.Fatalf("expected no retry before nextTry")
	}

	// after nextTry -> second failure
	w.Flush(context.Background(), now.Add(20*time.Millisecond))
	if stub.calls != 2 {
		t.Fatalf("expected second call, got %d", stub.calls)
	}

	// success after failures
	w.Flush(context.Background(), now.Add(100*time.Millisecond))
	if stub.calls != 3 {
		t.Fatalf("expected third call, got %d", stub.calls)
	}
	if len(w.pending) != 0 {
		t.Fatalf("pending not cleared after success")
	}
	if w.backoff != 0 {
		t.Fatalf("backoff not reset after success")
	}
}
