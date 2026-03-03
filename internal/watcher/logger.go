package watcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var tzUTC8 = time.FixedZone("UTC+8", 8*3600)

// NewLogger builds a slog logger with Traditional Chinese keys, UTC+8 time format, and pipe delimiters.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(newLineHandler(w, slog.LevelInfo))
}

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
	base := append([]slog.Attr{}, h.attrs...)
	base = append(base, attrs...)
	return &lineHandler{w: h.w, level: h.level, attrs: base}
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
