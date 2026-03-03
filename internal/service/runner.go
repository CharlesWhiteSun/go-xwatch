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
	"go-xwatch/internal/heartbeat"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/watcher"
)

// Runner 封裝服務監看流程，便於測試與重用。
type Runner struct {
	Settings config.Settings
	Logger   *slog.Logger

	DataDirFn               func() (string, error)
	WatcherFn               func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error
	Sinks                   []pipeline.EventSink
	Now                     func() time.Time
	HeartbeatLogDirFn       func() (string, error)          // 測試時可覆寫，預設使用 heartbeat.DefaultLogDir
	ConfigLoadFn            func() (config.Settings, error) // 測試時可覆寫，預設使用 config.Load
	HeartbeatReloadInterval time.Duration                   // 熱重載間隔，預設 30s，測試時可縮短
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

	// 心跳管理（支援熱重載）：服務啟動後當設定改變，不需重啟服務即可生效
	go r.runHeartbeatManager(ctx, logger)

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
		_ = writer.Close(context.Background())
		return nil
	case err := <-errCh:
		cancel()
		<-aggDone
		_ = writer.Close(context.Background())
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

func (r *Runner) configLoadFn() func() (config.Settings, error) {
	if r.ConfigLoadFn != nil {
		return r.ConfigLoadFn
	}
	return config.Load
}

func (r *Runner) heartbeatReloadInterval() time.Duration {
	if r.HeartbeatReloadInterval > 0 {
		return r.HeartbeatReloadInterval
	}
	return 30 * time.Second
}

// runHeartbeatManager 在背景 goroutine 中运行心跳管理，支援熱重載設定。
// 服務啟動後若繳聽設定改變（heartbeatEnabled/heartbeatInterval），
// 不需重啟服務即可自動嗚動或關閉心跳。
func (r *Runner) runHeartbeatManager(ctx context.Context, logger *slog.Logger) {
	var hb *heartbeat.Heartbeat

	stopHB := func() {
		if hb != nil {
			hb.Stop()
			hb = nil
		}
	}

	startHBFromSettings := func(s config.Settings) {
		stopHB()
		if !s.HeartbeatEnabled {
			return
		}
		hbLogDir, err := r.heartbeatLogDir()
		if err != nil {
			logger.Warn(fmt.Sprintf("無法取得心跳 log 目錄，心跳記錄停用：%v", err))
			return
		}
		iv := time.Duration(s.HeartbeatInterval) * time.Second
		if iv <= 0 {
			iv = time.Duration(heartbeat.DefaultInterval) * time.Second
		}
		hb = heartbeat.New(iv, heartbeat.NewFileLogFunc(hbLogDir, iv))
		hb.Start(ctx)
		logger.Info(fmt.Sprintf("已啟用心跳 log，位置：%s，間隔：%v", hbLogDir, iv))
	}

	// 依啟動時的設定決定是否立即啟動
	startHBFromSettings(r.Settings)

	curEnabled := r.Settings.HeartbeatEnabled
	curInterval := r.Settings.HeartbeatInterval
	cfgFn := r.configLoadFn()

	reloadTicker := time.NewTicker(r.heartbeatReloadInterval())
	defer reloadTicker.Stop()

	for {
		select {
		case <-reloadTicker.C:
			newSettings, err := cfgFn()
			if err != nil {
				continue
			}
			if newSettings.HeartbeatEnabled != curEnabled || newSettings.HeartbeatInterval != curInterval {
				startHBFromSettings(newSettings)
				curEnabled = newSettings.HeartbeatEnabled
				curInterval = newSettings.HeartbeatInterval
			}
		case <-ctx.Done():
			stopHB()
			return
		}
	}
}

func (r *Runner) heartbeatLogDir() (string, error) {
	if r.HeartbeatLogDirFn != nil {
		return r.HeartbeatLogDirFn()
	}
	return heartbeat.DefaultLogDir()
}
