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

	err := sendDailyMail(context.Background(), logger, mail, mail.LogDir, loc, now, fn)
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
	err := sendDailyMail(context.Background(), logger, mail, mail.LogDir, loc, now, fn)
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

	err := sendDailyMail(context.Background(), logger, mail, mail.LogDir, loc, now, alwaysFail)
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
	err := sendDailyMail(ctx, logger, mail, mail.LogDir, loc, now, alwaysFail)
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

	sendErr := sendDailyMail(context.Background(), logger, mail, tmp, time.UTC, now, alwaysFail)
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

// TestRunMailScheduler_NoScheduledEntryInMailLog 確認排程器啟動後，
// mail log 不寫入 scheduled 或 heartbeat 記錄（避免使用者誤以為郵件已寄出）。
// mail log 只記錄實際寄信結果（ok / fail）。
func TestRunMailScheduler_NoScheduledEntryInMailLog(t *testing.T) {
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
	runMailScheduler(ctx, logger, mail, tmp, time.Now)

	// mail log 不應含 scheduled 或 heartbeat 記錄
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("讀取 tmp 目錄失敗：%v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "mail_") && strings.HasSuffix(e.Name(), ".log") {
			data, _ := os.ReadFile(filepath.Join(tmp, e.Name()))
			content := string(data)
			if strings.Contains(content, "scheduled") {
				t.Fatalf("排程器啟動不應在 mail log 寫入 scheduled 記錄，實際內容：\n%s", content)
			}
			if strings.Contains(content, "heartbeat") {
				t.Fatalf("排程器啟動不應在 mail log 寫入 heartbeat 記錄，實際內容：\n%s", content)
			}
		}
	}
}

// ── 問題 1&3 防迴歸測試 ─────────────────────────────────────────────────────────

// TestNextSendTime_AlwaysReturnsFutureTime 確認 nextSendTime 不論何時呼叫都回傳
// 嚴格在未來的時間點。防迴歸問題 3：排程器不應在啟動時立即觸發寄信。
func TestNextSendTime_AlwaysReturnsFutureTime(t *testing.T) {
	tests := []struct {
		name     string
		now      string // "HH:MM:SS"
		schedule string // "HH:MM"
	}{
		{"排程未到今日", "08:00:00", "10:49"},
		{"排程已過今日", "11:00:00", "10:49"},
		{"排程恰好這一分鐘", "10:49:00", "10:49"},
		{"排程同分鐘但秒數已過", "10:49:30", "10:49"},
		{"午夜排程（上午未觸發）", "08:00:00", "00:00"},
		{"午夜排程（已過）", "01:00:00", "00:00"},
	}

	loc := time.UTC
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nowParsed, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-03-04 "+tc.now, loc)
			if err != nil {
				t.Fatalf("parse now: %v", err)
			}
			next, err := nextSendTime(nowParsed, tc.schedule, loc)
			if err != nil {
				t.Fatalf("nextSendTime error: %v", err)
			}
			if !next.After(nowParsed) {
				t.Errorf("nextSendTime(now=%q, schedule=%q)=%v，應嚴格大於 now=%v",
					tc.now, tc.schedule, next, nowParsed)
			}
		})
	}
}

// TestRunMailScheduler_DoesNotSendImmediately 確認排程器啟動後在排程時間到來之前不寄信。
// 防迴歸問題 3：mail enable 不應立即觸發寄信功能。
func TestRunMailScheduler_DoesNotSendImmediately(t *testing.T) {
	tmp := t.TempDir()
	mail := config.MailSettings{
		Enabled:    config.BoolPtr(true),
		To:         []string{"test@example.com"},
		Schedule:   "23:59", // 遠未來，不會在 200ms 內觸發
		Subject:    "Test",
		Body:       "body",
		LogDir:     tmp,
		MailLogDir: tmp,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// nowFn 固定回傳早上 10:55，排程 23:59 絕對在未來
	nowFn := func() time.Time {
		return time.Date(2026, 3, 4, 10, 55, 0, 0, time.UTC)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	runMailScheduler(ctx, logger, mail, tmp, nowFn)

	// 若有實際寄信，mail log 應有 ok 或 fail 記錄
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "mail_") && strings.HasSuffix(e.Name(), ".log") {
			data, _ := os.ReadFile(filepath.Join(tmp, e.Name()))
			content := string(data)
			if strings.Contains(content, "狀態=ok") || strings.Contains(content, "狀態=fail") {
				t.Fatalf("排程時間未到不應寄信，但 mail log 出現 ok/fail 記錄：\n%s", content)
			}
		}
	}
}

// TestRunMailScheduler_NoHeartbeatFileCreated 確認 mail 排程器執行期間
// 不產生 heartbeat 開頭的任何檔案。
// 防迴歸問題 2：啟動 mail 功能不應啟動 heartbeat。
func TestRunMailScheduler_NoHeartbeatFileCreated(t *testing.T) {
	tmp := t.TempDir()
	mail := config.MailSettings{
		Enabled:    config.BoolPtr(true),
		To:         []string{"test@example.com"},
		Schedule:   "23:59",
		Subject:    "Test",
		Body:       "body",
		LogDir:     tmp,
		MailLogDir: tmp,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	runMailScheduler(ctx, logger, mail, tmp, time.Now)

	// heartbeat 檔案（格式：heartbeat_YYYY-MM-DD.log）絕對不應出現
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "heartbeat_") {
			t.Fatalf("mail 排程器不應產生 heartbeat 檔案，但找到：%s", e.Name())
		}
	}
}

// ── buildMailContent 單元測試 ─────────────────────────────────────────────────

func TestServiceBuildMailContent_LogMissing_Immediate(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log") // 不存在

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeImmediate)

	if !missing {
		t.Fatal("日誌不存在時 attachmentMissing 應為 true")
	}
	if !strings.Contains(subject, "無資料夾異動紀錄") {
		t.Errorf("主旨應含「無資料夾異動紀錄」，實際：%q", subject)
	}
	if !strings.Contains(subject, sendModeImmediate) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", sendModeImmediate, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "特此通知") {
		t.Errorf("內文應含「特此通知」，實際：%q", body)
	}
}

func TestServiceBuildMailContent_LogMissing_Scheduled(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeScheduled)

	if !missing {
		t.Fatal("日誌不存在時 attachmentMissing 應為 true")
	}
	if !strings.Contains(subject, "無資料夾異動紀錄") {
		t.Errorf("主旨應含「無資料夾異動紀錄」，實際：%q", subject)
	}
	if !strings.Contains(subject, sendModeScheduled) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", sendModeScheduled, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "特此通知") {
		t.Errorf("內文應含「特此通知」，實際：%q", body)
	}
}

func TestServiceBuildMailContent_EmptyLog_TreatedAsMissing(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeImmediate)
	if !missing {
		t.Fatal("空日誌檔應被視為 missing，attachmentMissing 應為 true")
	}
}

func TestServiceBuildMailContent_LogExists_Immediate_NoColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeImmediate)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	if strings.Contains(subject, ":") {
		t.Errorf("即時模式有日誌時主旨不應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}

func TestServiceBuildMailContent_LogExists_Scheduled_HasColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeScheduled)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	if !strings.Contains(subject, ":") {
		t.Errorf("排程模式有日誌時主旨應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}
