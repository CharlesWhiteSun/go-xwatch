package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Event represents a single filesystem change.
type Event struct {
	Path  string
	Op    fsnotify.Op
	IsDir bool
	Size  int64
	TS    time.Time
}

func Run(ctx context.Context, root string, logger *slog.Logger, onEvent func(Event)) error {
	if logger == nil {
		logger = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("root is not a directory: %s", root)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addRecursive(watcher, root); err != nil {
		return err
	}
	logger.Info("watcher started", "root", root)

	for {
		select {
		case <-ctx.Done():
			logger.Info("watcher stopping", "reason", ctx.Err())
			return nil
		case err := <-watcher.Errors:
			if err != nil {
				logger.Error("watcher error", "err", err)
			}
		case event := <-watcher.Events:
			if shouldIgnore(event.Name) {
				continue
			}
			info := Event{Path: event.Name, Op: event.Op, TS: time.Now()}
			if fi, statErr := os.Stat(event.Name); statErr == nil {
				info.IsDir = fi.IsDir()
				info.Size = fi.Size()
			}
			logger.Info("fs event", "op", event.Op.String(), "path", event.Name)
			if onEvent != nil {
				onEvent(info)
			}
			if event.Has(fsnotify.Create) {
				if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
					if err := addRecursive(watcher, event.Name); err != nil {
						logger.Error("failed to add created dir", "path", event.Name, "err", err)
					}
				}
			}
		}
	}
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if shouldIgnore(path) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

func shouldIgnore(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(clean, "/.git/") || strings.HasSuffix(clean, "/.git") {
		return true
	}
	if strings.Contains(clean, "/node_modules/") || strings.HasSuffix(clean, "/node_modules") {
		return true
	}
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, "~") {
		return true
	}
	return false
}
