package mailcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
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

// ── writeMailLog 失敗路徑測試 ───────────────────────────────────────

// TestWriteMailLog_FailWithExistingAttachment 確認寄信失敗但日誌檔存在時，
// writeMailLog 記錄「附件=已附檔」而非「附件=失敗」。
func TestWriteMailLog_FailWithExistingAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if err := writeMailLog(tmp, now, "fail", "2026-03-02", []string{"a@example.com"}, "subject", "attached", "SMTP 連線失敗"); err != nil {
		t.Fatalf("writeMailLog error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "mail_2026-03-03.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "狀態=失敗") {
		t.Errorf("期望狀態=失敗，實際：%s", s)
	}
	if !strings.Contains(s, "附件=已附檔") {
		t.Errorf("寄信失敗時日誌存在，附件欄位應為「已附檔」，實際：%s", s)
	}
	if strings.Contains(s, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", s)
	}
	if !strings.Contains(s, "錯誤=SMTP 連線失敗") {
		t.Errorf("期望包含錯誤=SMTP 連線失敗，實際：%s", s)
	}
}

// TestWriteMailLog_FailWithMissingAttachment 確認寄信失敗且日誌檔不存在時，
// writeMailLog 記錄「附件=未附檔」而非「附件=失敗」。
func TestWriteMailLog_FailWithMissingAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if err := writeMailLog(tmp, now, "fail", "2026-03-02", []string{"a@example.com"}, "subject", "missing", "SMTP 連線失敗"); err != nil {
		t.Fatalf("writeMailLog error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "mail_2026-03-03.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "附件=未附檔") {
		t.Errorf("寄信失敗且日誌不存在，附件欄位應為「未附檔」，實際：%s", s)
	}
	if strings.Contains(s, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", s)
	}
}

// ── sendWithGmailFn 整合測試 ─────────────────────────────────────────

// setupTestMailConfig 在 tmp 目錄下建立測試用 config，並設 ProgramData 環境變數。
func setupTestMailConfig(t *testing.T, tmp string, mail config.MailSettings) {
	t.Helper()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(config.Settings{RootDir: root, Mail: mail}); err != nil {
		t.Fatalf("config.Save 失敗：%v", err)
	}
}

// readLastMailLogLine 讀取 mail log 目錄中今日 mail log 的最後一行。
func readLastMailLogLine(t *testing.T, mailLogDir string) string {
	t.Helper()
	loc := time.FixedZone("CST", 8*60*60)
	file := filepath.Join(mailLogDir, fmt.Sprintf("mail_%s.log", time.Now().In(loc).Format("2006-01-02")))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("讀取 mail log 失敗：%v (path=%s)", err, file)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[len(lines)-1]
}

// TestSendWithGmailFn_FailLogShowsAttachmentStatus 確認 send() 失敗時：
// 若 watch log 存在，mail log 的附件欄位顯示「已附檔」而非「失敗」。
func TestSendWithGmailFn_FailLogShowsAttachmentStatus(t *testing.T) {
	tmp := t.TempDir()

	mailSettings := config.MailSettings{
		Enabled:         config.BoolPtr(true),
		To:              []string{"test@example.com"},
		Schedule:        "10:00",
		SMTPDialTimeout: 10,
		SMTPRetries:     0,
	}
	setupTestMailConfig(t, tmp, mailSettings)

	// 建立前一天的 watch log（讓附件狀態為 "attached"）
	watchLogDir := filepath.Join(tmp, "go-xwatch", "xwatch-watch-logs")
	if err := os.MkdirAll(watchLogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("CST", 8*60*60)
	yesterday := time.Now().In(loc).AddDate(0, 0, -1).Format("2006-01-02")
	watchLog := filepath.Join(watchLogDir, fmt.Sprintf("watch_%s.log", yesterday))
	if err := os.WriteFile(watchLog, []byte("some event log"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mock：總是失敗，避免真實 SMTP 連線
	mockFail := func(_ context.Context, _ mailer.SMTPConfig, _ mailer.ReportOptions, _ mailer.SendMailFunc) error {
		return errors.New("mock SMTP 連線失敗")
	}

	err := sendWithGmailFn([]string{"--to", "test@example.com"}, mockFail)
	if err == nil {
		t.Fatal("預期回傳錯誤，但得到 nil")
	}

	mailLogDir := filepath.Join(tmp, "go-xwatch", "xwatch-mail-logs")
	line := readLastMailLogLine(t, mailLogDir)

	if !strings.Contains(line, "附件=已附檔") {
		t.Errorf("watch log 存在時寄信失敗，附件欄位應為「已附檔」，實際：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "狀態=失敗") {
		t.Errorf("狀態欄位應為「失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "錯誤=") {
		t.Errorf("應有「錯誤=」欄位，實際：%s", line)
	}
}

// TestSendWithGmailFn_DialTimeoutFromConfig 確認 SMTPConfig.DialTimeout 從 config 讀取。
func TestSendWithGmailFn_DialTimeoutFromConfig(t *testing.T) {
	tmp := t.TempDir()

	const wantTimeoutSec = 15
	mailSettings := config.MailSettings{
		Enabled:         config.BoolPtr(true),
		To:              []string{"test@example.com"},
		Schedule:        "10:00",
		SMTPDialTimeout: wantTimeoutSec,
		SMTPRetries:     0,
	}
	setupTestMailConfig(t, tmp, mailSettings)

	var capturedTimeout time.Duration
	mockCapture := func(_ context.Context, cfg mailer.SMTPConfig, _ mailer.ReportOptions, _ mailer.SendMailFunc) error {
		capturedTimeout = cfg.DialTimeout
		return errors.New("stop after capture")
	}

	_ = sendWithGmailFn([]string{"--to", "test@example.com"}, mockCapture)

	want := time.Duration(wantTimeoutSec) * time.Second
	if capturedTimeout != want {
		t.Errorf("SMTPConfig.DialTimeout = %s，期望 %s", capturedTimeout, want)
	}
}

// TestMailEnable_SetsEnabledAndDefaultTo 確認 mail enable 不帶 --to 時：
// - Enabled 自動設為 true
// - To 使用 DefaultMailTo（r021@httc.com.tw）
func TestMailEnable_SetsEnabledAndDefaultTo(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	// 先建立最小 config（只有 rootDir，未設 to）
	setupTestMailConfig(t, tmp, config.MailSettings{})

	// 不帶任何 flag 執行 mail enable
	if err := Run([]string{"enable"}); err != nil {
		t.Fatalf("mail enable failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	_ = root
	if !loaded.Mail.IsEnabled() {
		t.Fatal("mail enable 後 IsEnabled() 應回傳 true")
	}
	if len(loaded.Mail.To) == 0 {
		t.Fatal("mail enable 不帶 --to 時應自動填入 DefaultMailTo，實際 To 為空")
	}
	if loaded.Mail.To[0] != config.DefaultMailTo {
		t.Fatalf("預期 To[0]=%q，實際=%q", config.DefaultMailTo, loaded.Mail.To[0])
	}
}

// TestMailSet_ScheduleSaved 確認 mail set --schedule HH:MM 能正確儲存到 config，
// 模擬「服務安裝前」的設定流程（存入 config.json，服務啟動時讀取）。
func TestMailSet_ScheduleSaved(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{})

	if err := Run([]string{"set", "--schedule", "09:30"}); err != nil {
		t.Fatalf("mail set --schedule failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if loaded.Mail.Schedule != "09:30" {
		t.Fatalf("預期 Schedule=09:30，實際=%q", loaded.Mail.Schedule)
	}
}

// TestMailSet_InvalidScheduleRejected 確認 mail set --schedule 給出非法格式時回傳錯誤。
func TestMailSet_InvalidScheduleRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{})

	if err := Run([]string{"set", "--schedule", "25:99"}); err == nil {
		t.Fatal("非法 schedule 應回傳錯誤")
	}
}

// TestMailDisable_SetsFalse 確認 mail disable 後 IsEnabled()=false，
// 且再次 Load 不會自動回復為 true。
func TestMailDisable_SetsFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{})

	// 先 enable，再 disable
	if err := Run([]string{"enable"}); err != nil {
		t.Fatalf("mail enable failed: %v", err)
	}
	if err := Run([]string{"disable"}); err != nil {
		t.Fatalf("mail disable failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if loaded.Mail.IsEnabled() {
		t.Fatal("mail disable 後 IsEnabled() 應回傳 false，不應被預設值覆蓋")
	}
}

// TestPrintMailHelp_DynamicDate 確認 help 輸出包含前一天日期（動態），且不含舊固定日期。
func TestPrintMailHelp_DynamicDate(t *testing.T) {
	now := time.Date(2030, 6, 15, 12, 0, 0, 0, time.UTC)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printMailHelp(now)

	w.Close()
	os.Stdout = old

	var buf strings.Builder
	io.Copy(&buf, r)
	out := buf.String()

	want := now.AddDate(0, 0, -1).Format("2006-01-02") // "2030-06-14"
	if !strings.Contains(out, want) {
		t.Errorf("help 輸出應含 %q，實際輸出：\n%s", want, out)
	}
	if strings.Contains(out, "2026-03-02") {
		t.Errorf("help 輸出不應含舊固定日期 2026-03-02，實際輸出：\n%s", out)
	}
}
