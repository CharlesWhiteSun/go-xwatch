//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
		s.Close()
		if stopErr := Stop(name); stopErr != nil {
			_ = stopErr
		}
		if uninstallErr := Uninstall(name); uninstallErr != nil {
			return uninstallErr
		}
	}

	config := mgr.Config{
		DisplayName:      "Go XWatch Service",
		Description:      "Watch filesystem changes under configured root directory",
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
	}
	s, err = m.CreateService(name, exePath, config, args...)
	if err != nil {
		return fmt.Errorf("create service failed: %w", err)
	}
	defer s.Close()
	return nil
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

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	logFilePath := filepath.Join(os.Getenv("ProgramData"), "go-xwatch", "xwatch.log")
	_ = os.MkdirAll(filepath.Dir(logFilePath), 0o755)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, 1
	}
	defer logFile.Close()
	logger := slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		logger.Error("ensure data dir", "err", err)
		return false, 1
	}
	root := h.settings.RootDir
	if root == "" {
		logger.Error("empty root dir in config")
		return false, 1
	}
	root, err = filepath.Abs(root)
	if err != nil {
		logger.Error("resolve root", "err", err)
		return false, 1
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		logger.Error("load/create key", "err", err)
		return false, 1
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	j, err := journal.Open(journalPath, key)
	if err != nil {
		logger.Error("open journal", "err", err)
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
			logger.Error("create daily sink", "err", err, "dir", dir)
		} else {
			sinks = append(sinks, pipeline.NewBufferedSink(dailySink, 5*time.Second, 1024))
			logger.Info("daily csv sink enabled", "dir", dir)
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
				logger.Warn("journal channel full, dropping event", "path", ev.Path)
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
				logger.Error("watcher stopped with error", "err", err)
				return false, 2
			}
			return false, 0
		}
	}
}
