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

// ── mail add-to 測試 ───────────────────────────────────────────────

// TestMailAddTo_AppendsRecipients 確認 mail add-to 追加收件人而不覆蓋現有清單。
func TestMailAddTo_AppendsRecipients(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{
		To: []string{"alice@example.com"},
	})

	if err := Run([]string{"add-to", "--to", "bob@example.com"}); err != nil {
		t.Fatalf("mail add-to failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(loaded.Mail.To) != 2 {
		t.Fatalf("期望 2 位收件人，實際 %d：%v", len(loaded.Mail.To), loaded.Mail.To)
	}
	if loaded.Mail.To[0] != "alice@example.com" {
		t.Errorf("原有收件人應保留，實際 To[0]=%q", loaded.Mail.To[0])
	}
	if loaded.Mail.To[1] != "bob@example.com" {
		t.Errorf("新收件人應追加，實際 To[1]=%q", loaded.Mail.To[1])
	}
}

// TestMailAddTo_DeduplicatesRecipients 確認重複地址只保留一份。
func TestMailAddTo_DeduplicatesRecipients(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{
		To: []string{"alice@example.com", "bob@example.com"},
	})

	// 再次追加已存在的地址
	if err := Run([]string{"add-to", "--to", "alice@example.com,carol@example.com"}); err != nil {
		t.Fatalf("mail add-to failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	// alice 不應重複，carol 才是新增的
	if len(loaded.Mail.To) != 3 {
		t.Fatalf("期望 3 位收件人（alice/bob/carol），實際 %d：%v", len(loaded.Mail.To), loaded.Mail.To)
	}
	found := map[string]bool{}
	for _, r := range loaded.Mail.To {
		found[r] = true
	}
	for _, want := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		if !found[want] {
			t.Errorf("收件人清單缺少 %q，實際：%v", want, loaded.Mail.To)
		}
	}
}

// TestMailAddTo_PositionalArgs 確認 mail add-to 支援直接傳入位置參數（不需 --to）。
func TestMailAddTo_PositionalArgs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{
		To: []string{"alice@example.com"},
	})

	if err := Run([]string{"add-to", "bob@example.com,carol@example.com"}); err != nil {
		t.Fatalf("mail add-to positional args failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(loaded.Mail.To) != 3 {
		t.Fatalf("期望 3 位收件人，實際 %d：%v", len(loaded.Mail.To), loaded.Mail.To)
	}
}

// TestMailAddTo_NoArgsReturnsError 確認未提供收件人時回傳錯誤。
func TestMailAddTo_NoArgsReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{})

	if err := Run([]string{"add-to"}); err == nil {
		t.Fatal("未提供收件人時應回傳錯誤")
	}
}

// TestMailSet_ToReplacesRecipients 確認 mail set --to 仍是「覆蓋」語意。
func TestMailSet_ToReplacesRecipients(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	setupTestMailConfig(t, tmp, config.MailSettings{
		To: []string{"alice@example.com", "bob@example.com"},
	})

	if err := Run([]string{"set", "--to", "carol@example.com"}); err != nil {
		t.Fatalf("mail set --to failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(loaded.Mail.To) != 1 || loaded.Mail.To[0] != "carol@example.com" {
		t.Fatalf("set --to 應覆蓋原有清單，實際：%v", loaded.Mail.To)
	}
}

// TestPrintMailHelp_RemovedAddToFlagExample 確認 help 輸出不含已移除的
// 「mail add-to --to colleague@example.com」重複範例。
func TestPrintMailHelp_RemovedAddToFlagExample(t *testing.T) {
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

	// 移除的範例不應出現
	if strings.Contains(out, "mail add-to --to colleague@example.com") {
		t.Errorf("help 輸出不應包含已移除的 '--to colleague@example.com' 範例，實際輸出：\n%s", out)
	}
	// 保留的位置參數範例應仍存在
	if !strings.Contains(out, "mail add-to a@example.com,b@example.com") {
		t.Errorf("help 輸出應仍含 'mail add-to a@example.com,b@example.com' 範例，實際輸出：\n%s", out)
	}
}

// ── buildMailContent 單元測試 ─────────────────────────────────────────────────

func TestBuildMailContent_LogMissing_Immediate(t *testing.T) {
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
	if !strings.Contains(subject, "MyProject") {
		t.Errorf("主旨應含目錄名稱，實際：%q", subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "特此通知") {
		t.Errorf("內文應含「特此通知」，實際：%q", body)
	}
}

func TestBuildMailContent_LogMissing_Scheduled(t *testing.T) {
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

func TestBuildMailContent_EmptyLog_TreatedAsMissing(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	// 建立空檔案（size=0）
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeImmediate)
	if !missing {
		t.Fatal("空日誌檔應被視為 missing，attachmentMissing 應為 true")
	}
}

func TestBuildMailContent_LogExists_Immediate_NoColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeImmediate)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	// 即時模式：主旨不含冒號（用空格連接）
	if strings.Contains(subject, ":") {
		t.Errorf("即時模式有日誌時主旨不應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.Contains(subject, sendModeImmediate) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", sendModeImmediate, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}

func TestBuildMailContent_LogExists_Scheduled_HasColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject, body, missing := buildMailContent("MyProject", "2026-03-02", logPath, sendModeScheduled)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	// 排程模式：主旨含冒號
	if !strings.Contains(subject, ":") {
		t.Errorf("排程模式有日誌時主旨應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.Contains(subject, sendModeScheduled) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", sendModeScheduled, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}

func TestBuildMailContent_RootDirNameInSubjectAndBody(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rootDirName := "UniqueProjectName"
	subject, body, _ := buildMailContent(rootDirName, "2026-03-02", logPath, sendModeImmediate)

	if !strings.Contains(subject, rootDirName) {
		t.Errorf("主旨應含目錄名稱 %q，實際：%q", rootDirName, subject)
	}
	if !strings.Contains(body, rootDirName) {
		t.Errorf("內文應含目錄名稱 %q，實際：%q", rootDirName, body)
	}
}

func TestBuildMailContent_DayStrInSubjectAndBody(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dayStr := "2026-03-02"
	subject, body, _ := buildMailContent("Proj", dayStr, logPath, sendModeScheduled)

	if !strings.Contains(subject, dayStr) {
		t.Errorf("主旨應含日期字串 %q，實際：%q", dayStr, subject)
	}
	if !strings.Contains(body, dayStr) {
		t.Errorf("內文應含日期字串 %q，實際：%q", dayStr, body)
	}
}
