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

	"go-xwatch/internal/humanize"

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
		logger = NewLogger(os.Stdout)
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
	logger.Info(fmt.Sprintf("監視開始，根目錄：%s", root))

	for {
		select {
		case <-ctx.Done():
			logger.Info(fmt.Sprintf("監視結束，原因：%v", ctx.Err()))
			return nil
		case err := <-watcher.Errors:
			if err != nil {
				logger.Error(fmt.Sprintf("監視器錯誤：%v", err))
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
			friendly := humanize.Format(humanize.Input{TS: info.TS, Op: info.Op.String(), Path: info.Path, IsDir: info.IsDir, Size: info.Size}, humanize.Options{Root: root, ShowSize: true, ShowOp: true, HideTime: true})
			logger.Info(friendly, slog.String("路徑", info.Path), slog.String("動作", info.Op.String()))
			if onEvent != nil {
				onEvent(info)
			}
			if event.Has(fsnotify.Create) {
				if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
					if err := addRecursive(watcher, event.Name); err != nil {
						logger.Error(fmt.Sprintf("加入新資料夾失敗：%s，錯誤：%v", event.Name, err))
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
