package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/watcher"

	"github.com/fsnotify/fsnotify"
)

func TestRunnerFlushesAggregatedEvents(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	received := make([]string, 0)

	sink := pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
		return nil
	})

	sink2 := pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range entries {
			received = append(received, e.Op+"|"+e.Path)
		}
		return nil
	})

	watchCalled := 0
	watchFn := func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
		watchCalled++
		p := filepath.Join(root, "a.txt")
		onEvent(watcher.Event{Path: p, Op: fsnotify.Create, TS: time.Unix(0, 0)})
		onEvent(watcher.Event{Path: p, Op: fsnotify.Write, TS: time.Unix(1, 0)})
		return nil
	}

	r := &Runner{
		Settings:  config.Settings{RootDir: tmp},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: watchFn,
		Sinks:     []pipeline.EventSink{sink, sink2},
		Now:       func() time.Time { return time.Unix(2, 0) },
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if watchCalled != 1 {
		t.Fatalf("expected watcher called once, got %d", watchCalled)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(received))
	}
	if received[0] != "WRITE|"+filepath.Join(tmp, "a.txt") {
		t.Fatalf("unexpected entry: %v", received[0])
	}
}

func TestRunnerReturnsErrorOnEmptyRoot(t *testing.T) {
	r := &Runner{Settings: config.Settings{RootDir: ""}, Logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))}
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("expected error for empty root dir")
	}
}
