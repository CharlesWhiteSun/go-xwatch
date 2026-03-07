//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/heartbeat"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/watcher"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var ErrAlreadyRunning = errors.New("service is already running")

// IsInstalled 回傳 Windows 服務是否已安裝（不代表正在執行）。
func IsInstalled(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	defer s.Close()
	return true
}

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

	displayName := buildDisplayName(name)

	s, err := m.OpenService(name)
	if err == nil {
		defer s.Close()
		_ = Stop(name)
		cfg, cfgErr := s.Config()
		if cfgErr != nil {
			return fmt.Errorf("read service config failed: %w", cfgErr)
		}
		cfg.DisplayName = displayName
		cfg.Description = "Watch filesystem changes under configured root directory"
		cfg.StartType = mgr.StartAutomatic
		cfg.DelayedAutoStart = true
		cfg.BinaryPathName = formatBinaryPath(exePath, args)
		return s.UpdateConfig(cfg)
	}

	config := mgr.Config{
		DisplayName:      displayName,
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

// buildDisplayName 從服務名稱建立人類可讀的顯示名稱。
// "GoXWatch"           → "Go XWatch Service"
// "GoXWatch-plant-A"   → "Go XWatch Service (plant-A)"
func buildDisplayName(name string) string {
	suffix := SuffixFromServiceName(name)
	if suffix == "" {
		return "Go XWatch Service"
	}
	return fmt.Sprintf("Go XWatch Service (%s)", suffix)
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

// ServiceAccount 查詢指定 Windows 服務登錄的執行帳戶。
// 回傳空字串或 "LocalSystem" 代表以 SYSTEM 身份執行。
func ServiceAccount(name string) (string, error) {
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
	cfg, err := s.Config()
	if err != nil {
		return "", err
	}
	account := cfg.ServiceStartName
	if account == "" {
		account = "LocalSystem"
	}
	return account, nil
}

// RegisteredExePath 從 Windows SCM 讀取指定服務已登錄的執行檔絕對路徑。
// 若服務不存在或無法連線 SCM，回傳錯誤。
func RegisteredExePath(name string) (string, error) {
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
	cfg, err := s.Config()
	if err != nil {
		return "", err
	}
	return parseExeFromBinaryPath(cfg.BinaryPathName), nil
}

func Run(serviceName string, settings config.Settings) error {
	return svc.Run(serviceName, &handler{settings: settings})
}

type handler struct {
	settings config.Settings
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// 依服務名稱推導後綴，確保各實例的資料目錄完全隔離。
	suffix := SuffixFromServiceName(h.settings.ServiceName)
	dataDirFn := func() (string, error) { return paths.EnsureDataDirForSuffix(suffix) }

	watchLogRotator := NewRotatingLogger("watch", "xwatch-watch-logs", dataDirFn, watcher.NewLogger)
	logger, closeLogger, err := watchLogRotator.Logger()
	if err != nil {
		return false, 1
	}
	defer closeLogger()

	ctx, cancel := context.WithCancel(context.Background())
	// Runner.Run 內部的 runMailSchedulerManager 負責郵件排程熱重載，
	// 不需在此額外啟動 runMailScheduler（避免靜態快照問題）。
	runner := &Runner{
		Settings:  h.settings,
		Logger:    logger,
		DataDirFn: dataDirFn,
		HeartbeatLogDirFn: func() (string, error) {
			dir, err := dataDirFn()
			if err != nil {
				return "", err
			}
			return heartbeat.LogDirForDataDir(dir), nil
		},
	}
	runCh := make(chan error, 1)
	go func() {
		runCh <- runner.Run(ctx)
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
				err := <-runCh
				if err != nil {
					logger.Error(fmt.Sprintf("檔案監視停止，錯誤：%v", err))
					return false, 2
				}
				return false, 0
			default:
			}
		case err := <-runCh:
			if err != nil {
				logger.Error(fmt.Sprintf("檔案監視停止，錯誤：%v", err))
				return false, 2
			}
			return false, 0
		}
	}
}
