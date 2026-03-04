package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
)

func TestMailHeartbeatInterval(t *testing.T) {
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "5")
	if got := mailHeartbeatInterval(); got != 5*time.Second {
		t.Fatalf("heartbeat interval = %s, want 5s", got)
	}
}

func TestMailHeartbeatIntervalInvalid(t *testing.T) {
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "abc")
	if got := mailHeartbeatInterval(); got != 0 {
		t.Fatalf("heartbeat interval for invalid env should be 0, got %s", got)
	}
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "0")
	if got := mailHeartbeatInterval(); got != 0 {
		t.Fatalf("heartbeat interval for zero should be 0, got %s", got)
	}
}

// ── sendDailyMail 重試邏輯測試 ──────────────────────────────────────

// buildTestMailSettings 建立可測試的最小 MailSettings（使用 tmp 作為 LogDir）。
func buildTestMailSettings(t *testing.T) config.MailSettings {
	t.Helper()
	tmp := t.TempDir()
	return config.MailSettings{
		Enabled:         config.BoolPtr(true),
		To:              []string{"test@example.com"},
		Schedule:        "10:00",
		Subject:         "Test 日誌",
		Body:            "測試內容",
		LogDir:          tmp,
		MailLogDir:      tmp,
		SMTPHost:        "smtp.test.local",
		SMTPPort:        25,
		SMTPUser:        "user@test.local",
		SMTPPass:        "pass",
		SMTPFrom:        "user@test.local",
		SMTPDialTimeout: 5,
		SMTPRetries:     2,
		SMTPRetryDelay:  1, // 1 秒，加速測試
	}
}

// fakeSendFn 建立一個可計數的假 sendFn，在第 n 次呼叫前回傳錯誤，之後回傳 nil。
func fakeSendFn(succeedOnAttempt int) (mailer.SendMailFunc, *atomic.Int32) {
	var calls atomic.Int32
	fn := func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		n := int(calls.Add(1))
		if n < succeedOnAttempt {
			return fmt.Errorf("mock SMTP error on attempt %d", n)
		}
		return nil
	}
	return fn, &calls
}

// TestSendDailyMail_SucceedsFirstAttempt 確認第一次就成功時不會重試。
func TestSendDailyMail_SucceedsFirstAttempt(t *testing.T) {
	mail := buildTestMailSettings(t)
	fn, calls := fakeSendFn(1) // 第 1 次呼叫即成功
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loc := time.UTC
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, loc)

	err := sendDailyMail(context.Background(), logger, mail, loc, now, fn)
	if err != nil {
		t.Fatalf("expected success on first attempt, got: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected exactly 1 call, got %d", n)
	}
}

// TestSendDailyMail_RetrySucceedsOnSecondAttempt 確認第一次失敗後，
// 重試第二次成功時函式回傳 nil。
func TestSendDailyMail_RetrySucceedsOnSecondAttempt(t *testing.T) {
	mail := buildTestMailSettings(t)
	mail.SMTPRetries = 2
	mail.SMTPRetryDelay = 1 // 1 秒

	fn, calls := fakeSendFn(2) // 第 2 次呼叫才成功
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loc := time.UTC
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, loc)

	start := time.Now()
	err := sendDailyMail(context.Background(), logger, mail, loc, now, fn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success on second attempt, got: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected 2 calls (1 fail + 1 succeed), got %d", n)
	}
	// 應等待約 1 秒之後重試
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected at least ~1s wait before retry, elapsed=%s", elapsed)
	}
}

// TestSendDailyMail_RetryExhausted 確認重試次數耗盡後回傳最後錯誤。
func TestSendDailyMail_RetryExhausted(t *testing.T) {
	mail := buildTestMailSettings(t)
	mail.SMTPRetries = 2
	mail.SMTPRetryDelay = 1 // 1 秒，加速測試

	alwaysFail := mailer.SendMailFunc(func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		return errors.New("persistent SMTP error")
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loc := time.UTC
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, loc)

	err := sendDailyMail(context.Background(), logger, mail, loc, now, alwaysFail)
	if err == nil {
		t.Fatal("expected error after all retries exhausted, got nil")
	}
	if !strings.Contains(err.Error(), "persistent SMTP error") {
		t.Fatalf("expected last error to be returned, got: %v", err)
	}
}

// TestSendDailyMail_ContextCancelledDuringRetry 確認 ctx 被取消時，
// 重試等待期間立即停止並回傳 context.Canceled。
func TestSendDailyMail_ContextCancelledDuringRetry(t *testing.T) {
	mail := buildTestMailSettings(t)
	mail.SMTPRetries = 5
	mail.SMTPRetryDelay = 60 // 60 秒 — 不應真正等待

	alwaysFail := mailer.SendMailFunc(func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		return errors.New("fail")
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loc := time.UTC
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, loc)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sendDailyMail(ctx, logger, mail, loc, now, alwaysFail)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context error, got: %v", err)
	}
	// 應在 ctx 逾時不久後返回，而非等待 60 秒
	if elapsed > 2*time.Second {
		t.Fatalf("should respect ctx cancellation quickly, elapsed=%s", elapsed)
	}
}

// ── 附件欄位正確性測試 ────────────────────────────────────────────────

// readMailLogLastLine 讀取 mail log 檔最後一行內容。
func readMailLogLastLine(t *testing.T, mailLogDir string, now time.Time) string {
	t.Helper()
	loc := time.FixedZone("CST", 8*60*60)
	file := fmt.Sprintf("%s/mail_%s.log", mailLogDir, now.In(loc).Format("2006-01-02"))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("讀取 mail log 失敗：%v (path=%s)", err, file)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[len(lines)-1]
}

// TestWriteMailLog_SendFailWithAttachment 確認寄信失敗但日誌檔存在時，
// 附件欄位顯示「已附檔」而非「失敗」。
func TestWriteMailLog_SendFailWithAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := writeMailLog(tmp, now, "fail", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "attached", "SMTP 連線失敗")
	if err != nil {
		t.Fatalf("writeMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=已附檔") {
		t.Errorf("寄信失敗時附件欄位應為「已附檔」，實際 log 行：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際 log 行：%s", line)
	}
	if !strings.Contains(line, "錯誤=SMTP 連線失敗") {
		t.Errorf("錯誤原因應記錄在「錯誤=」欄位，實際 log 行：%s", line)
	}
	if !strings.Contains(line, "狀態=失敗") {
		t.Errorf("狀態欄位應為失敗，實際 log 行：%s", line)
	}
}

// TestWriteMailLog_SendFailWithoutAttachment 確認寄信失敗且日誌檔不存在時，
// 附件欄位顯示「未附檔」而非「失敗」。
func TestWriteMailLog_SendFailWithoutAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := writeMailLog(tmp, now, "fail", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "missing", "SMTP 連線失敗")
	if err != nil {
		t.Fatalf("writeMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=未附檔") {
		t.Errorf("寄信失敗且無附件時附件欄位應為「未附檔」，實際 log 行：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際 log 行：%s", line)
	}
}

// TestSendDailyMail_FailLogShowsAttachmentStatus 整合驗證：
// sendDailyMail 在寄信失敗時，mail log 的附件欄位應反映日誌檔實際存在狀態。
func TestSendDailyMail_FailLogShowsAttachmentStatus(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	// 建立前一天的 watch log，確保附件存在
	watchLog := fmt.Sprintf("%s/watch_2026-03-02.log", tmp)
	if err := os.WriteFile(watchLog, []byte("some log content"), 0o644); err != nil {
		t.Fatalf("建立 watch log 失敗: %v", err)
	}

	mail := config.MailSettings{
		Enabled:         config.BoolPtr(true),
		To:              []string{"test@example.com"},
		Subject:         "Test",
		Body:            "Body",
		LogDir:          tmp,
		MailLogDir:      tmp,
		SMTPHost:        "smtp.test.local",
		SMTPPort:        25,
		SMTPUser:        "user@test.local",
		SMTPPass:        "pass",
		SMTPFrom:        "user@test.local",
		SMTPDialTimeout: 5,
		SMTPRetries:     0, // 不重試，快速失敗
		SMTPRetryDelay:  1,
	}

	alwaysFail := mailer.SendMailFunc(func(_ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
		return errors.New("mock SMTP 連線失敗")
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sendErr := sendDailyMail(context.Background(), logger, mail, time.UTC, now, alwaysFail)
	if sendErr == nil {
		t.Fatal("預期寄信失敗，但回傳 nil")
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=已附檔") {
		t.Errorf("日誌檔存在時寄信失敗，附件欄位應為「已附檔」，實際：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "狀態=失敗") {
		t.Errorf("狀態欄位應為「失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "錯誤=") {
		t.Errorf("應有「錯誤=」欄位記錄原因，實際：%s", line)
	}
}

// TestRunMailScheduler_WritesScheduledToMailLog 確認 runMailScheduler 啟動後
// 立即在 mail log 寫入 scheduled 記錄，讓使用者可查詢排程狀態。
func TestRunMailScheduler_WritesScheduledToMailLog(t *testing.T) {
	tmp := t.TempDir()
	mail := config.MailSettings{
		Enabled:    config.BoolPtr(true),
		To:         []string{"test@example.com"},
		Schedule:   "23:59",
		Subject:    "Test 主旨",
		Body:       "Test 內文",
		LogDir:     tmp,
		MailLogDir: tmp,
		SMTPHost:   "smtp.test.local",
		SMTPPort:   25,
		SMTPUser:   "user@test.local",
		SMTPPass:   "pass",
		SMTPFrom:   "user@test.local",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 排程時間 23:59，不會在測試期間觸發；ctx 逾時後 scheduler 結束
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	runMailScheduler(ctx, logger, mail, time.Now)

	// mail log 應含 scheduled 記錄
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("讀取 tmp 目錄失敗：%v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "mail_") && strings.HasSuffix(e.Name(), ".log") {
			data, _ := os.ReadFile(filepath.Join(tmp, e.Name()))
			if strings.Contains(string(data), "scheduled") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("runMailScheduler 啟動後應在 mail log 寫入 scheduled 記錄，但未找到")
	}
}
