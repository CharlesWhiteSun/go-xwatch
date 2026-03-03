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

type Options struct {
	Logger    *slog.Logger
	Formatter func(root string, ev Event) string
	OnEvent   func(Event)
	WatcherFn func() (*fsnotify.Watcher, error)
	Now       func() time.Time
}

// Run 保留舊介面，委派到 RunWithOptions。
func Run(ctx context.Context, root string, logger *slog.Logger, onEvent func(Event)) error {
	return RunWithOptions(ctx, root, Options{Logger: logger, OnEvent: onEvent})
}

// RunWithOptions 允許注入 formatter/hook，提升解耦。
func RunWithOptions(ctx context.Context, root string, opt Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	logger := opt.Logger
	if logger == nil {
		logger = NewLogger(os.Stdout)
	}
	nowFn := opt.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	formatFn := opt.Formatter
	if formatFn == nil {
		formatFn = func(root string, ev Event) string {
			return humanize.Format(humanize.Input{TS: ev.TS, Op: ev.Op.String(), Path: ev.Path, IsDir: ev.IsDir, Size: ev.Size}, humanize.Options{Root: root, ShowSize: true, ShowOp: true, HideTime: true})
		}
	}
	watchFactory := opt.WatcherFn
	if watchFactory == nil {
		watchFactory = fsnotify.NewWatcher
	}

	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("root is not a directory: %s", root)
	}

	watcher, err := watchFactory()
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
			info := Event{Path: event.Name, Op: event.Op, TS: nowFn()}
			if fi, statErr := os.Stat(event.Name); statErr == nil {
				info.IsDir = fi.IsDir()
				info.Size = fi.Size()
			}
			msg := formatFn(root, info)
			logger.Info(msg, slog.String("路徑", info.Path), slog.String("動作", info.Op.String()))
			if opt.OnEvent != nil {
				opt.OnEvent(info)
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
