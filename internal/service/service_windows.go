//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/paths"
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

func Run(serviceName string, settings config.Settings) error {
	return svc.Run(serviceName, &handler{settings: settings})
}

type handler struct {
	settings config.Settings
}

var watchLogRotator = NewRotatingLogger("watch", "xwatch-watch-logs", paths.EnsureDataDir, watcher.NewLogger)

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	logger, closeLogger, err := watchLogRotator.Logger()
	if err != nil {
		return false, 1
	}
	defer closeLogger()

	ctx, cancel := context.WithCancel(context.Background())
	// Runner.Run 內部的 runMailSchedulerManager 負責郵件排程熱重載，
	// 不需在此額外啟動 runMailScheduler（避免靜態快照問題）。
	runner := &Runner{Settings: h.settings, Logger: logger}
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
