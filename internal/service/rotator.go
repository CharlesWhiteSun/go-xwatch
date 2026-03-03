package service

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RotatingLogger 以日期切換 log 檔，並透過 builder 建立 slog.Logger。
type RotatingLogger struct {
	mu     sync.Mutex
	dirFn  func() (string, error)
	subdir string
	prefix string
	build  func(io.Writer) *slog.Logger

	logger *slog.Logger
	file   *os.File
	date   string
	err    error
}

func NewRotatingLogger(prefix, subdir string, dirFn func() (string, error), build func(io.Writer) *slog.Logger) *RotatingLogger {
	return &RotatingLogger{prefix: prefix, subdir: subdir, dirFn: dirFn, build: build}
}

// Logger 取得今日的 logger。
func (r *RotatingLogger) Logger() (*slog.Logger, func(), error) {
	return r.LoggerFor(time.Now())
}

// LoggerFor 依指定時間決定檔名，方便測試。
func (r *RotatingLogger) LoggerFor(now time.Time) (*slog.Logger, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	day := now.In(time.Local).Format("2006-01-02")
	if r.logger != nil && r.date == day && r.err == nil {
		return r.logger, func() {}, nil
	}

	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}

	dir, err := r.dirFn()
	if err != nil {
		r.err = err
		return nil, func() {}, err
	}
	logDir := filepath.Join(dir, r.subdir)
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		r.err = mkErr
		return nil, func() {}, mkErr
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", r.prefix, day))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		r.err = err
		return nil, func() {}, err
	}

	builder := r.build
	if builder == nil {
		builder = func(w io.Writer) *slog.Logger {
			return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
		}
	}
	r.logger = builder(f)
	r.file = f
	r.date = day
	r.err = nil
	return r.logger, func() { _ = f.Close() }, nil
}
