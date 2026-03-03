package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type channelHandler struct {
	ch chan string
}

func (h *channelHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *channelHandler) Handle(_ context.Context, rec slog.Record) error {
	var path string
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "path", "路徑":
			path = fmt.Sprint(a.Value)
		}
		return true
	})
	if path != "" {
		select {
		case h.ch <- path:
		default:
		}
	}
	return nil
}

func (h *channelHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *channelHandler) WithGroup(_ string) slog.Handler      { return h }

type msgHandler struct {
	ch chan string
}

func (h *msgHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *msgHandler) Handle(_ context.Context, rec slog.Record) error {
	select {
	case h.ch <- rec.Message:
	default:
	}
	return nil
}
func (h *msgHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *msgHandler) WithGroup(_ string) slog.Handler      { return h }

func TestWatcherDetectsFileCreate(t *testing.T) {
	tmp := t.TempDir()
	ch := make(chan string, 10)
	logger := slog.New(&channelHandler{ch: ch})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, tmp, logger, nil) }()
	defer func() {
		cancel()
		_ = <-errCh
	}()

	// give watcher time to register initial directories
	time.Sleep(250 * time.Millisecond)

	path := filepath.Join(tmp, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	select {
	case p := <-ch:
		if filepath.Clean(p) != filepath.Clean(path) {
			t.Fatalf("unexpected path: %s", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for file create event")
	}
}

func TestWatcherAddsNewDirectories(t *testing.T) {
	tmp := t.TempDir()
	ch := make(chan string, 10)
	logger := slog.New(&channelHandler{ch: ch})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, tmp, logger, nil) }()
	defer func() {
		cancel()
		_ = <-errCh
	}()

	time.Sleep(250 * time.Millisecond)

	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	fileInSub := filepath.Join(sub, "b.txt")
	if err := os.WriteFile(fileInSub, []byte("world"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	deadline := time.After(7 * time.Second)
	for {
		select {
		case p := <-ch:
			if filepath.Clean(p) == filepath.Clean(fileInSub) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for event in new dir")
		}
	}
}

func TestShouldIgnore(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/a/.git", true},
		{"/a/.git/config", true},
		{"/a/node_modules", true},
		{"/a/node_modules/pkg", true},
		{"/a/file.tmp", true},
		{"/a/file.swp", true},
		{"/a/file.txt", false},
	}

	for _, c := range cases {
		if got := shouldIgnore(c.path); got != c.want {
			t.Fatalf("shouldIgnore(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

func TestRunWithOptionsFormatterAndHook(t *testing.T) {
	tmp := t.TempDir()
	msgCh := make(chan string, 4)

	var hookPath string
	logger := slog.New(&msgHandler{ch: msgCh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger: logger,
			Formatter: func(_ string, ev Event) string {
				return "CUSTOM:" + filepath.Base(ev.Path)
			},
			OnEvent: func(ev Event) { hookPath = ev.Path },
		})
	}()

	time.Sleep(200 * time.Millisecond)
	path := filepath.Join(tmp, "c.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	want := "CUSTOM:c.txt"
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-msgCh:
			if msg == want {
				cancel()
				goto done
			}
		case <-deadline:
			cancel()
			t.Fatalf("timeout waiting for formatted message; last got %q", hookPath)
		}
	}
done:

	if hookPath == "" || filepath.Clean(hookPath) != filepath.Clean(path) {
		t.Fatalf("hook path not set, got %q", hookPath)
	}
	_ = <-errCh
}
