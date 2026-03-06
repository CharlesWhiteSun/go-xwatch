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

// mockGmailSender 是 GmailSender 介面的測試 mock。
// fn 欄位不為 nil 時呼叫 fn；否則回傳 err。
type mockGmailSender struct {
	fn  func(ctx context.Context, cfg mailer.SMTPConfig, opts mailer.ReportOptions, sendFn mailer.SendMailFunc) error
	err error
}

func (m *mockGmailSender) SendGmail(ctx context.Context, cfg mailer.SMTPConfig, opts mailer.ReportOptions, sendFn mailer.SendMailFunc) error {
	if m.fn != nil {
		return m.fn(ctx, cfg, opts, sendFn)
	}
	return m.err
}

// 確保 mockGmailSender 編譯期即符合 GmailSender 介面。
var _ GmailSender = &mockGmailSender{}

func TestRunHelp(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("Run help should not error: %v", err)
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

// TestSendWithSender_FailLogShowsAttachmentStatus 確認 send() 失敗時：
// 若 watch log 存在，mail log 的附件欄位顯示「已附檔」而非「失敗」。
func TestSendWithSender_FailLogShowsAttachmentStatus(t *testing.T) {
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

	// Mock struct：總是失敗，避免真實 SMTP 連線
	mock := &mockGmailSender{err: errors.New("mock SMTP 連線失敗")}

	err := sendWithSender([]string{"--to", "test@example.com"}, mock)
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

// TestSendWithSender_DialTimeoutFromConfig 確認 SMTPConfig.DialTimeout 從 config 讀取。
func TestSendWithSender_DialTimeoutFromConfig(t *testing.T) {
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
	mock := &mockGmailSender{fn: func(_ context.Context, cfg mailer.SMTPConfig, _ mailer.ReportOptions, _ mailer.SendMailFunc) error {
		capturedTimeout = cfg.DialTimeout
		return errors.New("stop after capture")
	}}

	_ = sendWithSender([]string{"--to", "test@example.com"}, mock)

	want := time.Duration(wantTimeoutSec) * time.Second
	if capturedTimeout != want {
		t.Errorf("SMTPConfig.DialTimeout = %s，期望 %s", capturedTimeout, want)
	}
}

// TestMailEnable_SetsEnabledAndDefaultTo 確認 mail enable 不帶 --to 時：
// - Enabled 自動設為 true
// - To 使用 dev 環境預設清單首位（e003@httc.com.tw）
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
	wantFirst := config.DefaultMailToListForEnv(config.EnvDev)[0]
	if loaded.Mail.To[0] != wantFirst {
		t.Fatalf("預期 To[0]=%q，實際=%q", wantFirst, loaded.Mail.To[0])
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
