package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/watcher"
)

// Runner 封裝服務監看流程，便於測試與重用。
type Runner struct {
	Settings config.Settings
	Logger   *slog.Logger

	DataDirFn func() (string, error)
	WatcherFn func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error
	Sinks     []pipeline.EventSink
	Now       func() time.Time
}

func (r *Runner) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger := r.logger()
	dataDirFn := r.dataDirFn()
	nowFn := r.nowFn()

	dataDir, err := dataDirFn()
	if err != nil {
		logger.Error(fmt.Sprintf("無法建立資料目錄：%v", err))
		return err
	}
	root := strings.TrimSpace(r.Settings.RootDir)
	if root == "" {
		logger.Error("設定中的 root 路徑為空")
		return errors.New("empty root dir in config")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		logger.Error(fmt.Sprintf("解析 root 路徑失敗：%v", err))
		return err
	}

	sinks, closeFn, err := r.buildSinks(dataDir, logger)
	if err != nil {
		return err
	}
	defer closeFn()

	agg := pipeline.NewAggregator()
	writer := pipeline.NewWriter(pipeline.MultiSink(sinks), logger, 500*time.Millisecond, 5*time.Second)
	eventCh := make(chan watcher.Event, 256)

	aggDone := make(chan struct{})
	go func() {
		defer close(aggDone)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		flush := func(now time.Time) {
			entries := agg.Flush()
			if len(entries) > 0 {
				writer.Enqueue(entries)
			}
			writer.Flush(ctx, now)
		}
		for {
			select {
			case ev := <-eventCh:
				agg.Add(ev)
			case <-ticker.C:
				flush(nowFn())
			case <-ctx.Done():
				for {
					select {
					case ev := <-eventCh:
						agg.Add(ev)
					default:
						flush(nowFn())
						return
					}
				}
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- r.watcherFn()(ctx, root, logger, func(ev watcher.Event) {
			select {
			case eventCh <- ev:
			default:
				logger.Warn(fmt.Sprintf("事件通道已滿，丟棄：%s", ev.Path))
			}
		})
	}()

	select {
	case <-ctx.Done():
		<-aggDone
		return nil
	case err := <-errCh:
		cancel()
		<-aggDone
		if err != nil {
			logger.Error(fmt.Sprintf("檔案監視停止，錯誤：%v", err))
		}
		return err
	}
}

func (r *Runner) buildSinks(dataDir string, logger *slog.Logger) ([]pipeline.EventSink, func(), error) {
	if r.Sinks != nil {
		return r.Sinks, func() {}, nil
	}

	var closers []func()
	closeAll := func() {
		for _, c := range closers {
			c()
		}
	}

	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		logger.Error(fmt.Sprintf("載入或建立金鑰失敗：%v", err))
		return nil, closeAll, err
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	j, err := journal.Open(journalPath, key)
	if err != nil {
		logger.Error(fmt.Sprintf("開啟事件日誌失敗：%v", err))
		return nil, closeAll, err
	}
	closers = append(closers, func() { _ = j.Close() })

	sinks := pipeline.MultiSink{
		pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
			return j.Append(ctx, entries)
		}),
	}

	if r.Settings.DailyCSVEnabled {
		dir := r.Settings.DailyCSVDir
		if dir == "" {
			dir = filepath.Join(dataDir, "daily")
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(dataDir, dir)
		}
		dailySink, err := pipeline.NewDailyFileSink(dir, pipeline.NewCSVRecorder)
		if err != nil {
			logger.Error(fmt.Sprintf("建立每日 CSV 寫入器失敗（目錄：%s）：%v", dir, err))
		} else {
			sinks = append(sinks, pipeline.NewBufferedSink(dailySink, 5*time.Second, 1024))
			logger.Info(fmt.Sprintf("已啟用每日 CSV 輸出，位置：%s", dir))
		}
	}

	return sinks, closeAll, nil
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func (r *Runner) dataDirFn() func() (string, error) {
	if r.DataDirFn != nil {
		return r.DataDirFn
	}
	return paths.EnsureDataDir
}

func (r *Runner) watcherFn() func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
	if r.WatcherFn != nil {
		return r.WatcherFn
	}
	return watcher.Run
}

func (r *Runner) nowFn() func() time.Time {
	if r.Now != nil {
		return r.Now
	}
	return time.Now
}
