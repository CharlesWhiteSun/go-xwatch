package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go-xwatch/internal/cli"
	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/exporter"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/service"

	"golang.org/x/sys/windows"
)

const elevationPrompt = "偵測到目前非系統管理員，是否重新以系統管理員執行？(Y/n): "

func resolveRoot(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
	}

	settings, err := config.Load()
	if err == nil && settings.RootDir != "" {
		return resolveAndEnsureDir(settings.RootDir, "根目錄")
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveAndEnsureDir(filepath.Dir(exePath), "根目錄")
}

func resolveAndEnsureDir(path string, purpose string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("%s 不可為空", purpose)
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s 不是資料夾: %s", purpose, absPath)
		}
		return absPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if mkErr := os.MkdirAll(absPath, 0o755); mkErr != nil {
		return "", mkErr
	}
	return absPath, nil
}

func exportJournal(since string, until string, limit int, format string, all bool, bom bool, out string) error {
	return exporter.Export(since, until, limit, format, all, bom, out)
}

func clearJournal() error {
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Stop(serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		return err
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	for _, p := range []string{journalPath, journalPath + "-wal", journalPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("無法刪除 %s: %w", filepath.Base(p), err)
		}
	}

	j, err := journal.Open(journalPath, key)
	if err != nil {
		return fmt.Errorf("重建日誌資料庫失敗: %w", err)
	}
	_ = j.Close()
	return nil
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "access is denied")
}

func isServiceMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "service does not exist") || strings.Contains(msg, "does not exist")
}

func evaluateElevation(skipEnv, interactive, elevated bool, ask func(string) bool, relaunch func([]string) error, args []string) (string, error) {
	if skipEnv || !interactive || elevated {
		return "continue", nil
	}

	if ask(elevationPrompt) {
		if err := relaunch(args); err != nil {
			return "continue", err
		}
		return "relaunch", nil
	}

	return "exit", nil
}

func promptYes(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			return true
		}
		if strings.EqualFold(line, "n") || strings.EqualFold(line, "no") {
			return false
		}
	}
}

func buildCommandRegistry() *cli.Registry {
	reg := cli.NewRegistry()
	for _, name := range []string{"init", "help", "status", "start", "stop", "uninstall", "cleanup", "remove", "clear", "purge", "wipe", "export", "daily", "run"} {
		reg.Register(cli.CommandFunc{CommandName: name, Fn: func([]string) error { return nil }})
	}
	return reg
}
