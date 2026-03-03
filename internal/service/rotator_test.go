package service

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotatingLoggerReturnsSameLoggerSameDay(t *testing.T) {
	tmp := t.TempDir()
	rot := NewRotatingLogger("watch", "logs", func() (string, error) { return tmp, nil }, func(w io.Writer) *slog.Logger {
		return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{}))
	})

	day1 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.Local)
	l1, c1, err := rot.LoggerFor(day1)
	if err != nil {
		t.Fatalf("logger error: %v", err)
	}
	l2, c2, err := rot.LoggerFor(day1.Add(2 * time.Hour))
	if err != nil {
		t.Fatalf("logger error: %v", err)
	}
	if l1 != l2 {
		t.Fatalf("expected same logger for same day")
	}
	defer c1()
	defer c2()
	if _, err := os.Stat(filepath.Join(tmp, "logs", "watch_2024-01-02.log")); err != nil {
		t.Fatalf("expected log file exists: %v", err)
	}
}

func TestRotatingLoggerRotatesOnNewDay(t *testing.T) {
	tmp := t.TempDir()
	rot := NewRotatingLogger("watch", "logs", func() (string, error) { return tmp, nil }, func(w io.Writer) *slog.Logger {
		return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{}))
	})

	day1 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.Local)
	l1, c1, err := rot.LoggerFor(day1)
	if err != nil {
		t.Fatalf("logger error: %v", err)
	}
	day2 := day1.Add(24 * time.Hour)
	l2, c2, err := rot.LoggerFor(day2)
	if err != nil {
		t.Fatalf("logger error: %v", err)
	}
	defer c1()
	defer c2()
	if l1 == l2 {
		t.Fatalf("expected different logger after rotation")
	}
	if _, err := os.Stat(filepath.Join(tmp, "logs", "watch_2024-01-03.log")); err != nil {
		t.Fatalf("expected rotated log file exists: %v", err)
	}
}
