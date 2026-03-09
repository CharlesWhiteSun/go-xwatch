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

	DataDirFn                  func() (string, error)
	WatcherFn                  func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error
	Sinks                      []pipeline.EventSink
	Now                        func() time.Time
	HeartbeatLogDirFn          func() (string, error)                                                                                         // 測試時可覆寫，預設使用 heartbeat.DefaultLogDir
	ConfigLoadFn               func() (config.Settings, error)                                                                                // 測試時可覆寫，預設使用 config.Load
	HeartbeatReloadInterval    time.Duration                                                                                                  // 心跳熱重載間隔，預設 30s，測試時可縮短
	MailReloadInterval         time.Duration                                                                                                  // 郵件排程熱重載間隔，預設 30s，測試時可縮短
	FilecheckReloadInterval    time.Duration                                                                                                  // filecheck 熱重載間隔，預設 30s，測試時可縮短
	WatchExcludeReloadInterval time.Duration                                                                                                  // WatchExclude 熱重載間隔，預設 30s，測試時可縮短
	MailSchedulerFn            func(ctx context.Context, logger *slog.Logger, mail config.MailSettings, rootDir string, now func() time.Time) // 測試時可覆寫，預設使用 runMailScheduler
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

	// 郵件排程管理（支援熱重載）：服務啟動後當 mail 設定改變，不需重啟服務即可生效
	go r.runMailSchedulerManager(ctx, logger)

	// 目錄檔案檢查管理（支援熱重載）：服務啟動後當 filecheck 設定改變，不需重啟服務即可生效
	go r.runFilecheckManager(ctx, logger)

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

	onEvent := func(ev watcher.Event) {
		select {
		case eventCh <- ev:
		default:
			logger.Warn(fmt.Sprintf("事件通道已滿，丟棄：%s", ev.Path))
		}
	}

	// WatchExclude 管理器（支援熱重載）：服務啟動後當排除清單或啟用狀態改變，
	// 不需重啟服務即可自動套用新設定。CLI 指令執行後的異動也會在下一輪輪詢自動生效。
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.runWatchManager(ctx, root, logger, onEvent)
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
	// 防穩性 fallback：若呼叫端未注入 DataDirFn，則根據全域設定的 suffix 推導正確資料目錄。
	return func() (string, error) {
		return paths.EnsureDataDirForSuffix(config.GetServiceSuffix())
	}
}

// buildWatcherForSettings 依傳入的 Settings 建立對應的 watcher 函式。
// WatcherFn 注入優先（測試），其次依 WatchExclude 設定動態建立，最後 fallback 到 watcher.Run。
func (r *Runner) buildWatcherForSettings(s config.Settings) func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
	if r.WatcherFn != nil {
		return r.WatcherFn
	}
	we := s.WatchExclude
	if we.IsEnabled() && len(we.Dirs) > 0 {
		dirs := append([]string(nil), we.Dirs...)
		return func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
			return watcher.RunWithOptions(ctx, root, watcher.Options{
				Logger:       logger,
				OnEvent:      onEvent,
				ShouldSkipFn: buildExcludeSkipFn(root, dirs),
			})
		}
	}
	return watcher.Run
}

// watchExcludeKey 記錄影響 WatchExclude 行為的欄位，用於偵測設定變更。
type watchExcludeKey struct {
	enabled bool
	dirs    string // 以逗號 join 後比對
}

func watchExcludeKeyFromSettings(s config.Settings) watchExcludeKey {
	return watchExcludeKey{
		enabled: s.WatchExclude.IsEnabled(),
		dirs:    strings.Join(s.WatchExclude.Dirs, ","),
	}
}

// runWatchManager 在服務主迴圈內管理 watcher 的生命週期，支援 WatchExclude 熱重載。
// 當 WatchExclude 設定（啟用狀態或排除目錄清單）發生變更時，自動取消舊 watcher 並以
// 新設定重新啟動，事件通道（eventCh）持續運作不中斷。
// CLI 指令（如 watchexclude enable/disable/add-to/set）修改 config.json 後，
// 會在下一輪輪詢（預設 30s）自動生效，無需重啟服務。
func (r *Runner) runWatchManager(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
	cfgFn := r.configLoadFn()
	curSettings := r.Settings
	curKey := watchExcludeKeyFromSettings(curSettings)

	reloadTicker := time.NewTicker(r.watchExcludeReloadInterval())
	defer reloadTicker.Stop()

	for {
		watchCtx, watchCancel := context.WithCancel(ctx)
		watchFn := r.buildWatcherForSettings(curSettings)

		watchErrCh := make(chan error, 1)
		go func() {
			watchErrCh <- watchFn(watchCtx, root, logger, onEvent)
		}()

		restarted := false
	inner:
		for {
			select {
			case <-reloadTicker.C:
				newSettings, err := cfgFn()
				if err != nil {
					continue
				}
				newKey := watchExcludeKeyFromSettings(newSettings)
				if newKey != curKey {
					logger.Info(fmt.Sprintf("WatchExclude 設定已變更（enabled=%v dirs=%v），重新啟動監控...",
						newSettings.WatchExclude.IsEnabled(), newSettings.WatchExclude.Dirs))
					watchCancel()
					<-watchErrCh
					curKey = newKey
					curSettings = newSettings
					restarted = true
					break inner
				}
			case err := <-watchErrCh:
				watchCancel()
				return err
			case <-ctx.Done():
				watchCancel()
				<-watchErrCh
				return nil
			}
		}
		if !restarted {
			watchCancel()
			return nil
		}
	}
}

func (r *Runner) watchExcludeReloadInterval() time.Duration {
	if r.WatchExcludeReloadInterval > 0 {
		return r.WatchExcludeReloadInterval
	}
	return 30 * time.Second
}

// buildExcludeSkipFn 依 rootDir 與目錄名稱清單建立路徑排除函式。
// 相對路徑名稱會與 root 合併為絕對路徑後進行前綴比對，達到子目錄完整排除。
func buildExcludeSkipFn(root string, dirs []string) func(string) bool {
	abs := make([]string, 0, len(dirs))
	for _, d := range dirs {
		var full string
		if filepath.IsAbs(d) {
			full = d
		} else {
			full = filepath.Join(root, d)
		}
		abs = append(abs, filepath.ToSlash(strings.ToLower(full)))
	}
	return func(path string) bool {
		clean := filepath.ToSlash(strings.ToLower(path))
		for _, excl := range abs {
			if clean == excl || strings.HasPrefix(clean, excl+"/") {
				return true
			}
		}
		return false
	}
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

func (r *Runner) mailReloadInterval() time.Duration {
	if r.MailReloadInterval > 0 {
		return r.MailReloadInterval
	}
	return 30 * time.Second
}

func (r *Runner) filecheckReloadInterval() time.Duration {
	if r.FilecheckReloadInterval > 0 {
		return r.FilecheckReloadInterval
	}
	return 30 * time.Second
}

// mailSchedulerKey 記錄影響郵件排程行為的關鍵欄位，用於偵測設定變更。
// 包含 SMTP 設定，確保 SMTP 變更能觸發熱重載。
type mailSchedulerKey struct {
	enabled    bool
	schedule   string
	to         string // Join 後比對
	timezone   string
	smtpHost   string
	smtpPort   int
	smtpUser   string
	smtpPass   string
	logDir     string
	mailLogDir string
}

func mailKeyFromSettings(s config.Settings) mailSchedulerKey {
	return mailSchedulerKey{
		enabled:    s.Mail.IsEnabled(),
		schedule:   s.Mail.Schedule,
		to:         strings.Join(s.Mail.To, ","),
		timezone:   s.Mail.Timezone,
		smtpHost:   s.Mail.SMTPHost,
		smtpPort:   s.Mail.SMTPPort,
		smtpUser:   s.Mail.SMTPUser,
		smtpPass:   s.Mail.SMTPPass,
		logDir:     s.Mail.LogDir,
		mailLogDir: s.Mail.MailLogDir,
	}
}

// runMailSchedulerManager 在背景 goroutine 中管理郵件排程，支援熱重載設定。
// 服務啟動後若郵件設定改變（enabled/schedule/to/timezone），
// 不需重啟服務即可自動啟動或停止郵件排程。
func (r *Runner) runMailSchedulerManager(ctx context.Context, logger *slog.Logger) {
	var mailCancel context.CancelFunc

	stopMail := func() {
		if mailCancel != nil {
			mailCancel()
			mailCancel = nil
		}
	}

	startMailFromSettings := func(s config.Settings) {
		stopMail()
		if !s.Mail.IsEnabled() {
			return
		}
		mailCtx, cancel := context.WithCancel(ctx)
		mailCancel = cancel
		go r.mailSchedulerFn()(mailCtx, logger, s.Mail, s.RootDir, time.Now)
		logger.Info(fmt.Sprintf("已啟用郵件排程器，排程時間：%s", s.Mail.Schedule))
	}

	// 依啟動時的設定決定是否立即啟動
	startMailFromSettings(r.Settings)

	curKey := mailKeyFromSettings(r.Settings)
	cfgFn := r.configLoadFn()

	reloadTicker := time.NewTicker(r.mailReloadInterval())
	defer reloadTicker.Stop()

	for {
		select {
		case <-reloadTicker.C:
			newSettings, err := cfgFn()
			if err != nil {
				continue
			}
			newKey := mailKeyFromSettings(newSettings)
			if newKey != curKey {
				startMailFromSettings(newSettings)
				curKey = newKey
			}
		case <-ctx.Done():
			stopMail()
			return
		}
	}
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
	// 防穩性 fallback：依直尤全域 suffix 推導正確心跳 log 目錄。
	dataDir, err := paths.EnsureDataDirForSuffix(config.GetServiceSuffix())
	if err != nil {
		return "", err
	}
	return heartbeat.LogDirForDataDir(dataDir), nil
}

func (r *Runner) mailSchedulerFn() func(ctx context.Context, logger *slog.Logger, mail config.MailSettings, rootDir string, now func() time.Time) {
	if r.MailSchedulerFn != nil {
		return r.MailSchedulerFn
	}
	return runMailScheduler
}
