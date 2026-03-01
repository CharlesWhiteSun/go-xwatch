package pipeline

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go-xwatch/internal/journal"
)

// EventRecorder defines how entries are persisted for a single output target.
type EventRecorder interface {
	Record(entries []journal.Entry) error
	Close() error
}

// CSVRecorder appends entries as CSV with header; creates file if missing.
type CSVRecorder struct {
	file *os.File
	w    *csv.Writer
}

// NewCSVRecorder opens/creates path and prepares a CSV writer with header when file is new/empty.
func NewCSVRecorder(path string) (EventRecorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	rec := &CSVRecorder{file: f, w: csv.NewWriter(f)}
	fi, statErr := f.Stat()
	if statErr == nil && fi.Size() == 0 {
		// header
		if err := rec.w.Write([]string{"ts", "op", "path", "size", "is_dir"}); err != nil {
			f.Close()
			return nil, err
		}
		rec.w.Flush()
	}
	return rec, nil
}

// Record writes entries to CSV.
func (r *CSVRecorder) Record(entries []journal.Entry) error {
	for _, e := range entries {
		if err := r.w.Write([]string{
			e.TS.Format(time.RFC3339Nano),
			e.Op,
			e.Path,
			strconv.FormatInt(e.Size, 10),
			strconv.FormatBool(e.IsDir),
		}); err != nil {
			return err
		}
	}
	r.w.Flush()
	return r.w.Error()
}

// Close flushes and closes the underlying file.
func (r *CSVRecorder) Close() error {
	r.w.Flush()
	if err := r.w.Error(); err != nil {
		_ = r.file.Close()
		return err
	}
	return r.file.Close()
}

// DailyFileSink groups events by local-date and writes via provided recorder factory.
type DailyFileSink struct {
	dir     string
	factory func(path string) (EventRecorder, error)
	mu      sync.Mutex
	perDay  map[string]EventRecorder
}

// NewDailyFileSink creates a sink writing one file per day (YYYY-MM-DD.ext based on factory caller choice).
func NewDailyFileSink(dir string, factory func(path string) (EventRecorder, error)) (*DailyFileSink, error) {
	if dir == "" {
		return nil, fmt.Errorf("dir is required")
	}
	if factory == nil {
		return nil, fmt.Errorf("factory is required")
	}
	return &DailyFileSink{dir: dir, factory: factory, perDay: make(map[string]EventRecorder)}, nil
}

// Handle routes entries by day and records them.
func (s *DailyFileSink) Handle(_ context.Context, entries []journal.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		day := e.TS.In(time.Local).Format("2006-01-02")
		rec, ok := s.perDay[day]
		if !ok {
			path := filepath.Join(s.dir, day+".csv")
			var err error
			rec, err = s.factory(path)
			if err != nil {
				return err
			}
			s.perDay[day] = rec
		}
		if err := rec.Record([]journal.Entry{e}); err != nil {
			return err
		}
	}
	return nil
}

// Close closes all active recorders.
func (s *DailyFileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for day, rec := range s.perDay {
		if rec == nil {
			continue
		}
		if err := rec.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", day, err)
		}
		delete(s.perDay, day)
	}
	return firstErr
}
