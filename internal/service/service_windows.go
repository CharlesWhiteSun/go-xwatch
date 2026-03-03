//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/watcher"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var ErrAlreadyRunning = errors.New("service is already running")

func IsWindowsServiceProcess() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

func InstallOrUpdate(name, exePath string, args ...string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err == nil {
		defer s.Close()
		_ = Stop(name)
		cfg, cfgErr := s.Config()
		if cfgErr != nil {
			return fmt.Errorf("read service config failed: %w", cfgErr)
		}
		cfg.DisplayName = "Go XWatch Service"
		cfg.Description = "Watch filesystem changes under configured root directory"
		cfg.StartType = mgr.StartAutomatic
		cfg.DelayedAutoStart = true
		cfg.BinaryPathName = formatBinaryPath(exePath, args)
		return s.UpdateConfig(cfg)
	}

	config := mgr.Config{
		DisplayName:      "Go XWatch Service",
		Description:      "Watch filesystem changes under configured root directory",
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		BinaryPathName:   formatBinaryPath(exePath, args),
	}
	s, err = m.CreateService(name, exePath, config, args...)
	if err != nil {
		return fmt.Errorf("create service failed: %w", err)
	}
	defer s.Close()
	return nil
}

func formatBinaryPath(exePath string, args []string) string {
	quoted := fmt.Sprintf("\"%s\"", exePath)
	if len(args) == 0 {
		return quoted
	}
	return quoted + " " + strings.Join(args, " ")
}

func Start(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()

	st, err := s.Query()
	if err == nil && st.State == svc.Running {
		return ErrAlreadyRunning
	}

	return s.Start()
}

func Stop(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for service stop")
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return err
		}
	}
	return nil
}

func Uninstall(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Delete()
}

func Status(name string) (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return "", err
	}
	defer s.Close()
	status, err := s.Query()
	if err != nil {
		return "", err
	}
	switch status.State {
	case svc.Stopped:
		return "stopped", nil
	case svc.StartPending:
		return "start pending", nil
	case svc.StopPending:
		return "stop pending", nil
	case svc.Running:
		return "running", nil
	default:
		return fmt.Sprintf("state(%d)", status.State), nil
	}
}

func Run(serviceName string, settings config.Settings) error {
	return svc.Run(serviceName, &handler{settings: settings})
}

type handler struct {
	settings config.Settings
}

var watchLog struct {
	mu     sync.Mutex
	logger *slog.Logger
	file   *os.File
	date   string
	err    error
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	logger, closeLogger, err := getWatchLogger()
	if err != nil {
		return false, 1
	}
	defer closeLogger()

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		logger.Error(fmt.Sprintf("無法建立資料目錄：%v", err))
		return false, 1
	}
	root := h.settings.RootDir
	if root == "" {
		logger.Error("設定中的 root 路徑為空")
		return false, 1
	}
	root, err = filepath.Abs(root)
	if err != nil {
		logger.Error(fmt.Sprintf("解析 root 路徑失敗：%v", err))
		return false, 1
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		logger.Error(fmt.Sprintf("載入或建立金鑰失敗：%v", err))
		return false, 1
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	j, err := journal.Open(journalPath, key)
	if err != nil {
		logger.Error(fmt.Sprintf("開啟事件日誌失敗：%v", err))
		return false, 1
	}
	defer j.Close()

	eventCh := make(chan watcher.Event, 256)
	ctx, cancel := context.WithCancel(context.Background())

	agg := pipeline.NewAggregator()
	sinks := pipeline.MultiSink{
		pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
			return j.Append(ctx, entries)
		}),
	}
	if h.settings.DailyCSVEnabled {
		dir := h.settings.DailyCSVDir
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
	writer := pipeline.NewWriter(sinks, logger, 500*time.Millisecond, 5*time.Second)

	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		flush := func(now time.Time) {
			entries := agg.Flush()
			if len(entries) > 0 {
				writer.Enqueue(entries)
			}
			writer.Flush(context.Background(), now)
		}
		for {
			select {
			case ev := <-eventCh:
				agg.Add(ev)
			case <-ticker.C:
				flush(time.Now())
			case <-ctx.Done():
				flush(time.Now())
				return
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Run(ctx, root, logger, func(ev watcher.Event) {
			select {
			case eventCh <- ev:
			default:
				logger.Warn(fmt.Sprintf("事件通道已滿，丟棄：%s", ev.Path))
			}
		})
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				return false, 0
			default:
			}
		case err := <-errCh:
			if err != nil {
				logger.Error(fmt.Sprintf("檔案監視停止，錯誤：%v", err))
				return false, 2
			}
			return false, 0
		}
	}
}

func getWatchLogger() (*slog.Logger, func(), error) {
	watchLog.mu.Lock()
	defer watchLog.mu.Unlock()

	now := time.Now()
	day := now.In(time.Local).Format("2006-01-02")
	if watchLog.logger != nil && watchLog.date == day && watchLog.err == nil {
		return watchLog.logger, func() {}, nil
	}

	if watchLog.file != nil {
		_ = watchLog.file.Close()
		watchLog.file = nil
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		watchLog.err = err
		return nil, func() {}, err
	}
	logDir := filepath.Join(dataDir, "xwatch-watch-logs")
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		watchLog.err = mkErr
		return nil, func() {}, mkErr
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("watch_%s.log", day))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		watchLog.err = err
		return nil, func() {}, err
	}
	watchLog.logger = watcher.NewLogger(f)
	watchLog.file = f
	watchLog.date = day
	watchLog.err = nil
	return watchLog.logger, func() { _ = f.Close() }, nil
}
