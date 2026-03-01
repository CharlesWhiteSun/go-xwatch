package pipeline

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"go-xwatch/internal/journal"
	"go-xwatch/internal/watcher"
)

// Aggregator coalesces rapid events by path; caller controls flush cadence.
type Aggregator struct {
	m map[string]journal.Entry
}

func NewAggregator() *Aggregator {
	return &Aggregator{m: make(map[string]journal.Entry)}
}

func (a *Aggregator) Add(ev watcher.Event) {
	if a.m == nil {
		a.m = make(map[string]journal.Entry)
	}
	entry := journal.Entry{TS: ev.TS, Op: ev.Op.String(), Path: ev.Path, IsDir: ev.IsDir, Size: ev.Size}
	// keep last event per path within flush window
	a.m[ev.Path] = entry
}

// Flush returns aggregated entries sorted by timestamp and clears internal state.
func (a *Aggregator) Flush() []journal.Entry {
	if len(a.m) == 0 {
		return nil
	}
	out := make([]journal.Entry, 0, len(a.m))
	for _, v := range a.m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	a.m = make(map[string]journal.Entry)
	return out
}

// Appender defines the Append contract used by Writer.
type Appender interface {
	Append(ctx context.Context, entries []journal.Entry) error
}

// Writer batches entries and applies backoff on append failures.
type Writer struct {
	appender    Appender
	logger      *slog.Logger
	baseBackoff time.Duration
	maxBackoff  time.Duration

	pending []journal.Entry
	nextTry time.Time
	backoff time.Duration
}

func NewWriter(appender Appender, logger *slog.Logger, baseBackoff, maxBackoff time.Duration) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	if baseBackoff <= 0 {
		baseBackoff = 500 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = 5 * time.Second
	}
	return &Writer{appender: appender, logger: logger, baseBackoff: baseBackoff, maxBackoff: maxBackoff}
}

// Enqueue appends entries to the pending buffer.
func (w *Writer) Enqueue(entries []journal.Entry) {
	if len(entries) == 0 {
		return
	}
	w.pending = append(w.pending, entries...)
}

// Flush attempts to write pending entries if backoff has elapsed.
func (w *Writer) Flush(ctx context.Context, now time.Time) {
	if len(w.pending) == 0 {
		return
	}
	if !w.nextTry.IsZero() && now.Before(w.nextTry) {
		return
	}
	if err := w.appender.Append(ctx, w.pending); err != nil {
		if w.backoff == 0 {
			w.backoff = w.baseBackoff
		} else {
			w.backoff *= 2
			if w.backoff > w.maxBackoff {
				w.backoff = w.maxBackoff
			}
		}
		w.nextTry = now.Add(w.backoff)
		w.logger.Error("journal append failed", "err", err, "retry_in", w.backoff.String(), "pending", len(w.pending))
		return
	}
	// success
	w.pending = w.pending[:0]
	w.backoff = 0
	w.nextTry = time.Time{}
}
