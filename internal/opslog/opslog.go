package opslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-xwatch/internal/paths"
)

// Logger writes daily ops logs with friendly formatting.
type Logger struct {
	mu        sync.Mutex
	logger    *slog.Logger
	file      *os.File
	date      string
	err       error
	dataDirFn func() (string, error)
}

// New creates a Logger; dataDirFn may be nil to use paths.EnsureDataDir.
func New(dataDirFn func() (string, error)) *Logger {
	if dataDirFn == nil {
		dataDirFn = paths.EnsureDataDir
	}
	return &Logger{dataDirFn: dataDirFn}
}

// Info logs an informational message after formatting.
func (l *Logger) Info(msg string, args ...any) {
	l.log(time.Now(), msg, args...)
}

// FormatOpsMessage converts structured args into a readable Traditional Chinese line.
func FormatOpsMessage(msg string, args ...any) string {
	kv := make(map[string]any)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		kv[key] = args[i+1]
	}

	switch msg {
	case "cli start":
		return fmt.Sprintf("CLI 啟動；版本=%v；PID=%v；參數=%v", kv["version"], kv["pid"], kv["args"])
	case "command":
		cmd := kv["cmd"]
		return fmt.Sprintf("收到指令：%v；參數=%v", cmd, kv["args"])
	case "command ok":
		return "指令已完成"
	case "command error":
		return fmt.Sprintf("指令失敗：%v", kv["err"])
	case "cli exit":
		if reason, ok := kv["reason"]; ok {
			return fmt.Sprintf("CLI 結束；代碼=%v；原因=%v", kv["code"], reason)
		}
		return fmt.Sprintf("CLI 結束；代碼=%v", kv["code"])
	case "service error":
		return fmt.Sprintf("服務錯誤：%v", kv["err"])
	case "cli signal":
		return fmt.Sprintf("收到訊號：%v；即將結束", kv["signal"])
	default:
		if len(args) == 0 {
			return msg
		}
		return fmt.Sprintf("%s；內容=%v", msg, kv)
	}
}

func (l *Logger) log(now time.Time, msg string, args ...any) {
	logger, err := l.getLogger(now)
	if err != nil || logger == nil {
		return
	}
	logger.Info(FormatOpsMessage(msg, args...))
}

func (l *Logger) getLogger(now time.Time) (*slog.Logger, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	day := now.In(time.Local).Format("2006-01-02")
	if l.logger != nil && l.date == day && l.err == nil {
		return l.logger, nil
	}

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}

	dataDir, err := l.dataDirFn()
	if err != nil {
		l.err = err
		return nil, err
	}
	logDir := filepath.Join(dataDir, "xwatch-ops-logs")
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		l.err = mkErr
		return nil, mkErr
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("operations_%s.log", day))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		l.err = err
		return nil, err
	}
	l.logger = slog.New(newLineHandler(f, slog.LevelInfo))
	l.file = f
	l.date = day
	l.err = nil
	return l.logger, nil
}

var tzUTC8 = time.FixedZone("UTC+8", 8*3600)

type lineHandler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Leveler
	attrs []slog.Attr
}

func newLineHandler(w io.Writer, level slog.Level) *lineHandler {
	return &lineHandler{w: w, level: level}
}

func (h *lineHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level.Level()
}

func (h *lineHandler) Handle(_ context.Context, rec slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	parts := make([]string, 0, 6+rec.NumAttrs())
	parts = append(parts, fmt.Sprintf("時間=%s", rec.Time.In(tzUTC8).Format("2006-01-02 15:04:05.000")))
	parts = append(parts, fmt.Sprintf("層級=%s", strings.ToUpper(rec.Level.String())))
	parts = append(parts, fmt.Sprintf("訊息=%s", rec.Message))

	attrs := append([]slog.Attr{}, h.attrs...)
	rec.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	for _, a := range attrs {
		key := translateKey(a.Key)
		parts = append(parts, fmt.Sprintf("%s=%v", key, a.Value))
	}

	_, err := fmt.Fprintln(h.w, strings.Join(parts, " | "))
	return err
}

func (h *lineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copyBase := append([]slog.Attr{}, h.attrs...)
	copyBase = append(copyBase, attrs...)
	return &lineHandler{w: h.w, level: h.level, attrs: copyBase}
}

func (h *lineHandler) WithGroup(_ string) slog.Handler { return h }

func translateKey(key string) string {
	switch key {
	case slog.TimeKey:
		return "時間"
	case slog.LevelKey:
		return "層級"
	case slog.MessageKey:
		return "訊息"
	case "path":
		return "路徑"
	case "op":
		return "動作"
	default:
		return key
	}
}

// Close releases any open file handle. Safe to call multiple times.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var err error
	if l.file != nil {
		err = l.file.Close()
		l.file = nil
	}
	l.logger = nil
	l.date = ""
	l.err = nil
	return err
}
