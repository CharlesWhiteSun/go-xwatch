package mailcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareBodyMissingAddsNote(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")

	defaultBody := "附件為 2026-03-02 的監控日誌。"
	missingBody := "沒有可用的監控日誌（2026-03-02），未附檔。"
	body, missing := prepareBody(logPath, "2026-03-02", "自訂內容 {day}", defaultBody, missingBody)
	if !missing {
		t.Fatalf("expected missing")
	}
	if !strings.Contains(body, "未附檔") {
		t.Fatalf("expected body to mention missing attachment, got %q", body)
	}
}

func TestPrepareBodyWithExistingLog(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	defaultBody := "附件為 2026-03-02 的監控日誌。"
	missingBody := "沒有可用的監控日誌（2026-03-02），未附檔。"
	body, missing := prepareBody(logPath, "2026-03-02", "", defaultBody, missingBody)
	if missing {
		t.Fatalf("expected not missing")
	}
	if body != defaultBody {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestWriteMailLog(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if err := writeMailLog(tmp, now, "ok", "2026-03-02", []string{"a@example.com"}, "subject", "attached", ""); err != nil {
		t.Fatalf("writeMailLog error: %v", err)
	}

	path := filepath.Join(tmp, "mail_2026-03-03.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "2026-03-03 10:00:00.000") {
		t.Fatalf("expected timestamp format, got %s", s)
	}
	if !strings.Contains(s, "狀態=成功") || !strings.Contains(s, "日期=2026-03-02") || !strings.Contains(s, "主旨=subject") || !strings.Contains(s, "附件=已附檔") {
		t.Fatalf("unexpected log content: %s", s)
	}
}

func TestRunHelp(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("Run help should not error: %v", err)
	}
}
