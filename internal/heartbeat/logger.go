package heartbeat

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const defaultLogSubDir = "xwatch-heartbeat"

// DefaultLogDir 回傳預設心跳 log 目錄路徑：
// %ProgramData%\go-xwatch\xwatch-heartbeat
func DefaultLogDir() (string, error) {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		return "", fmt.Errorf("ProgramData 環境變數未設定")
	}
	return filepath.Join(pd, "go-xwatch", defaultLogSubDir), nil
}

// WriteEntry 將一筆心跳記錄附加到日期分檔的 log 中。
// 檔案路徑：<logDir>/heartbeat_YYYY-MM-DD.log
// 格式：2026-03-03 10:00:00.000 心跳 #1 正常 (間隔: 60s)
func WriteEntry(logDir string, t time.Time, seq int64, interval time.Duration) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("無法建立心跳 log 目錄 %s: %w", logDir, err)
	}
	fileName := fmt.Sprintf("heartbeat_%s.log", t.Format("2006-01-02"))
	path := filepath.Join(logDir, fileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("無法開啟心跳 log 檔 %s: %w", path, err)
	}
	defer f.Close()
	line := fmt.Sprintf("%s 心跳 #%d 正常 (間隔: %s)\n",
		t.Format("2006-01-02 15:04:05.000"),
		seq,
		intervalLabel(interval),
	)
	_, err = fmt.Fprint(f, line)
	return err
}

// NewFileLogFunc 回傳一個 onTick 回呼，每次呼叫時自動遞增序號並將心跳記錄寫入 logDir。
// 寫入失敗時靜默忽略（best-effort）。
func NewFileLogFunc(logDir string, interval time.Duration) func(time.Time) {
	var seq atomic.Int64
	return func(t time.Time) {
		n := seq.Add(1)
		_ = WriteEntry(logDir, t, n, interval)
	}
}

// intervalLabel 將 Duration 格式化為友善字串，例如 60s、120s。
func intervalLabel(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	secs := int64(d.Seconds())
	return fmt.Sprintf("%ds", secs)
}
