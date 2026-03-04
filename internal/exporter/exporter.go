package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
)

type options struct {
	now        func() time.Time
	stdout     io.Writer
	stderr     io.Writer
	createFile func(string) (io.WriteCloser, error)
}

type Option func(*options)

func WithNow(fn func() time.Time) Option { return func(o *options) { o.now = fn } }
func WithStdout(w io.Writer) Option      { return func(o *options) { o.stdout = w } }
func WithStderr(w io.Writer) Option      { return func(o *options) { o.stderr = w } }
func WithCreateFile(fn func(string) (io.WriteCloser, error)) Option {
	return func(o *options) { o.createFile = fn }
}

func defaultOptions() options {
	return options{
		now:        time.Now,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		createFile: func(p string) (io.WriteCloser, error) { return os.Create(p) },
	}
}

// Export writes journal entries to the desired format.
func Export(sinceStr, untilStr string, limit int, format string, all, bom bool, outPath string, opts ...Option) error {
	optsState := defaultOptions()
	for _, opt := range opts {
		opt(&optsState)
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		return err
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		return err
	}
	defer j.Close()

	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339, s)
	}
	since, err := parse(sinceStr)
	if err != nil {
		return fmt.Errorf("invalid since: %w", err)
	}
	until, err := parse(untilStr)
	if err != nil {
		return fmt.Errorf("invalid until: %w", err)
	}
	if all {
		since = time.Time{}
		until = time.Time{}
	}

	entries, err := j.Query(context.Background(), since, until, limit)
	if err != nil {
		return err
	}

	ext := "json"
	switch strings.ToLower(format) {
	case "jsonl":
		ext = "jsonl"
	case "json":
		ext = "json"
	case "text":
		ext = "txt"
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	var out io.Writer = optsState.stdout
	var closeFn func() error
	if outPath == "" {
		outPath = filepath.Join(dataDir, "xwatch-export-files", fmt.Sprintf("export_%s.%s", optsState.now().Format("20060102_150405"), ext))
	}
	if outPath != "-" {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		f, err := optsState.createFile(outPath)
		if err != nil {
			return err
		}
		out = f
		closeFn = f.Close
	}
	if bom {
		if _, err := out.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			if closeFn != nil {
				_ = closeFn()
			}
			return err
		}
	}

	switch strings.ToLower(format) {
	case "jsonl", "json":
		enc := json.NewEncoder(out)
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				if closeFn != nil {
					_ = closeFn()
				}
				return err
			}
		}
	case "text":
		for _, e := range entries {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%d\t%t\n", e.TS.Format(time.RFC3339Nano), e.Op, e.Path, e.Size, e.IsDir); err != nil {
				if closeFn != nil {
					_ = closeFn()
				}
				return err
			}
		}
	}
	if closeFn != nil {
		_ = closeFn()
		fmt.Fprintf(optsState.stderr, "已匯出 %d 筆事件到 %s。\n", len(entries), outPath)
	} else {
		fmt.Fprintf(optsState.stderr, "已匯出 %d 筆事件。\n", len(entries))
	}
	return nil
}
