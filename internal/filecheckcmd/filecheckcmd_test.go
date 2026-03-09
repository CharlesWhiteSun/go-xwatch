package filecheckcmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/filecheck"
	"go-xwatch/internal/mailer"
)

// mockTextMailSender 是 TextMailSender 介面的測試 mock。
// fn 欄位不為 nil 時呼叫 fn；否則回傳 err。
type mockTextMailSender struct {
	fn  func(ctx context.Context, cfg mailer.SMTPConfig, subject, body string, fn mailer.SendMailFunc) error
	err error
}

func (m *mockTextMailSender) SendTextMail(ctx context.Context, cfg mailer.SMTPConfig, subject, body string, fn mailer.SendMailFunc) error {
	if m.fn != nil {
		return m.fn(ctx, cfg, subject, body, fn)
	}
	return m.err
}

// 確保 mockTextMailSender 編譯期即符合 TextMailSender 介面。
var _ TextMailSender = &mockTextMailSender{}

// setupConfig 在暫存目錄建立最小測試設定。
func setupConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("setupConfig: %v", err)
	}
	return tmp
}

//  Run dispatch

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	if err := Run(nil); err != nil {
		t.Fatalf("Run 無參數不應回傳錯誤，got %v", err)
	}
}

func TestRun_Help(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("filecheck help 不應回傳錯誤，got %v", err)
	}
}

func TestRun_Unknown_ReturnsError(t *testing.T) {
	err := Run([]string{"unknowncmd"})
	if err == nil {
		t.Fatal("未知子指令應回傳錯誤，got nil")
	}
	if !strings.Contains(err.Error(), "filecheck help") {
		t.Errorf("錯誤應提示 filecheck help，got %q", err.Error())
	}
}

//  status

func TestStatus_ReadsConfig(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("filecheck status 不應回傳錯誤，got %v", err)
	}
}

//  enable / disable

func TestRun_Enable_SetsFlags(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"enable", "--to", "a@example.com"}); err != nil {
		t.Fatalf("filecheck enable 不應回傳錯誤，got %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Filecheck.Enabled {
		t.Error("enable 後 Filecheck.Enabled 應為 true")
	}
	if !s.Filecheck.Mail.IsEnabled() {
		t.Error("enable 後 Filecheck.Mail.IsEnabled() 應為 true")
	}
	if len(s.Filecheck.Mail.To) != 1 || s.Filecheck.Mail.To[0] != "a@example.com" {
		t.Errorf("收件人應為 a@example.com，got %v", s.Filecheck.Mail.To)
	}
}

func TestRun_Disable_ClearsFlags(t *testing.T) {
	setupConfig(t)
	// 先啟用
	_ = Run([]string{"enable", "--to", "a@example.com"})
	// 再停用
	if err := Run([]string{"disable"}); err != nil {
		t.Fatalf("filecheck disable 不應回傳錯誤，got %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Filecheck.Enabled {
		t.Error("disable 後 Filecheck.Enabled 應為 false")
	}
	if s.Filecheck.Mail.IsEnabled() {
		t.Error("disable 後 Filecheck.Mail.IsEnabled() 應為 false")
	}
}

func TestEnable_AlsoSetsFilecheckEnabled(t *testing.T) {
	setupConfig(t)
	if err := mailEnable([]string{"--to", "test@example.com"}); err != nil {
		t.Fatalf("mailEnable 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if !s.Filecheck.Enabled {
		t.Error("啟用 filecheck mail 時 Filecheck.Enabled 也應同步為 true")
	}
}

func TestDisable_AlsoDisablesFilecheckEnabled(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "test@example.com"})
	if err := mailDisable(); err != nil {
		t.Fatalf("mailDisable 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.Enabled {
		t.Error("停用 filecheck mail 時 Filecheck.Enabled 也應同步為 false")
	}
}

//  mail subcommands

func TestHelp_NoError(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("filecheck help 不應回傳錯誤，got %v", err)
	}
}

func TestMailEnable_SetsFlag(t *testing.T) {
	setupConfig(t)
	if err := mailEnable([]string{"--to", "a@example.com"}); err != nil {
		t.Fatalf("mailEnable 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if !s.Filecheck.Mail.IsEnabled() {
		t.Error("mailEnable 後 Filecheck.Mail.Enabled 應為 true")
	}
	if len(s.Filecheck.Mail.To) != 1 || s.Filecheck.Mail.To[0] != "a@example.com" {
		t.Errorf("收件人應為 a@example.com，got %v", s.Filecheck.Mail.To)
	}
}

func TestMailDisable_ClearsFlag(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "a@example.com"})
	if err := mailDisable(); err != nil {
		t.Fatalf("mailDisable 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.Mail.IsEnabled() {
		t.Error("mailDisable 後 Filecheck.Mail.Enabled 應為 false")
	}
}

// set 子指令已移除，應回傳未知子指令錯誤
func TestRun_Set_IsRemovedAndReturnsError(t *testing.T) {
	err := Run([]string{"set", "--schedule", "08:30"})
	if err == nil {
		t.Fatal("'filecheck set' 已移除，應回傳錯誤，got nil")
	}
	if !strings.Contains(err.Error(), "filecheck help") {
		t.Errorf("錯誤應提示 'filecheck help'，got %q", err.Error())
	}
}

// enable --schedule 取代原 set --schedule
func TestEnable_WithSchedule_SetsSchedule(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"enable", "--to", "a@example.com", "--schedule", "08:30"}); err != nil {
		t.Fatalf("filecheck enable --schedule 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.Mail.Schedule != "08:30" {
		t.Errorf("Schedule 應為 08:30，got %q", s.Filecheck.Mail.Schedule)
	}
}

func TestEnable_WithInvalidSchedule_ReturnsError(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"enable", "--to", "a@example.com", "--schedule", "not-a-time"}); err == nil {
		t.Fatal("無效排程應回傳錯誤")
	}
}

// enable --tz 取代原 set --tz
func TestEnable_WithTimezone_SetsTimezone(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"enable", "--to", "a@example.com", "--tz", "UTC"}); err != nil {
		t.Fatalf("filecheck enable --tz 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.Mail.Timezone != "UTC" {
		t.Errorf("Timezone 應為 UTC，got %q", s.Filecheck.Mail.Timezone)
	}
}

//  mail send（注入假 TextMailSender mock）

// TestMailSend_DefaultRecipients_UsedWhenNoFlag 確認未指定 --to 時自動使用 config 預設收件人清單。
func TestMailSend_DefaultRecipients_UsedWhenNoFlag(t *testing.T) {
	setupConfig(t)
	var gotTo []string
	mock := &mockTextMailSender{fn: func(_ context.Context, cfg mailer.SMTPConfig, _, _ string, _ mailer.SendMailFunc) error {
		gotTo = cfg.To
		return nil
	}}
	if err := mailSendWithSender(nil, mock); err != nil {
		t.Fatalf("使用預設收件人時不應失敗，got %v", err)
	}
	wantTo := config.DefaultMailToListForEnv(config.EnvDev)
	if len(gotTo) != len(wantTo) {
		t.Errorf("預期 %d 位收件人，got %d: %v", len(wantTo), len(gotTo), gotTo)
	}
	if len(gotTo) > 0 && gotTo[0] != wantTo[0] {
		t.Errorf("預期首位收件人 %q，got %q", wantTo[0], gotTo[0])
	}
}

func TestMailSend_WithFakeSmtp_Succeeds(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "test@example.com"})

	var gotSubject, gotBody string
	mock := &mockTextMailSender{fn: func(_ context.Context, _ mailer.SMTPConfig, subject, body string, _ mailer.SendMailFunc) error {
		gotSubject = subject
		gotBody = body
		return nil
	}}

	if err := mailSendWithSender([]string{"--to", "test@example.com"}, mock); err != nil {
		t.Fatalf("mailSendWithSender（注入假 smtp）不應回傳錯誤，got %v", err)
	}
	// 主旨應含「目錄檔案存在性報告」關鍵字
	if !strings.Contains(gotSubject, "目錄") {
		t.Errorf("主旨應含「目錄」關鍵字，got %q", gotSubject)
	}
	if gotBody == "" {
		t.Error("郵件內容不應為空")
	}
}

func TestMailSend_BodyContainsStatus(t *testing.T) {
	// 無論路徑是否存在，內文應包含 [FOUND]/[NOT FOUND]/[ERROR] 狀態
	setupConfig(t)
	_ = mailEnable([]string{"--to", "test@example.com"})

	var gotBody string
	mock := &mockTextMailSender{fn: func(_ context.Context, _ mailer.SMTPConfig, _, body string, _ mailer.SendMailFunc) error {
		gotBody = body
		return nil
	}}
	_ = mailSendWithSender([]string{"--to", "test@example.com"}, mock)

	hasStatus := strings.Contains(gotBody, "[FOUND]") ||
		strings.Contains(gotBody, "[NOT FOUND]") ||
		strings.Contains(gotBody, "[ERROR]")
	if !hasStatus {
		t.Errorf("內文應含 [FOUND]/[NOT FOUND]/[ERROR] 狀態標籤，got:\n%s", gotBody)
	}
}

func TestMailSend_BodyContainsFoundWhenFileMatchesPattern(t *testing.T) {
	setupConfig(t)
	s, _ := config.Load()

	// 在預設 scanDir（storage/logs）建立符合 laravel-{YYYY-MM-DD}.log 格式的檔案
	yesterday := time.Now().AddDate(0, 0, -1)
	targetFile := filecheck.TargetFileName(yesterday) // e.g. "laravel-2026-03-08.log"
	scanDir := filecheck.DefaultScanDir(s.RootDir)
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scanDir, targetFile), []byte("data"), 0o644)

	_ = mailEnable([]string{"--to", "test@example.com"})

	var gotSubject, gotBody string
	mock := &mockTextMailSender{fn: func(_ context.Context, _ mailer.SMTPConfig, subject, body string, _ mailer.SendMailFunc) error {
		gotSubject = subject
		gotBody = body
		return nil
	}}
	if err := mailSendWithSender([]string{"--to", "test@example.com"}, mock); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	if !strings.Contains(gotSubject, "找到") {
		t.Errorf("有符合檔案時主旨應含「找到」，got %q", gotSubject)
	}
	if !strings.Contains(gotBody, "[FOUND]") {
		t.Errorf("有符合檔案時內文應含 [FOUND]，got:\n%s", gotBody)
	}
}

func TestRun_Unknown_Subcommand_MailIsNoLongerValid(t *testing.T) {
	// 原本的 'filecheck mail' 層級已移除，'mail' 現在是未知子指令
	err := Run([]string{"mail", "help"})
	if err == nil {
		t.Fatal("預期回傳錯誤，'mail' 已不再是有效子指令")
	}
	if !strings.Contains(err.Error(), "filecheck help") {
		t.Errorf("錯誤應提示 'filecheck help'，got %q", err.Error())
	}
}

func TestRun_Status_Dispatch(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("Run status 不應回傳錯誤，got %v", err)
	}
}

// TestRun_Send_Dispatch_SmtpRouteError 確認 filecheck send 路由至 mailSend
// （使用無效 SMTP 主機確保連線失敗，避免测試時送出真實郵件）。
func TestRun_Send_Dispatch_SmtpRouteError(t *testing.T) {
	setupConfig(t)
	// 覆寫 SMTP 為無效主機，確保對外連線失敗
	s, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	s.Mail.SMTPHost = "127.0.0.1"
	s.Mail.SMTPPort = 19999
	s.Mail.SMTPDialTimeout = 2
	if err := config.Save(s); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	err = Run([]string{"send"})
	if err == nil {
		t.Fatal("無效 SMTP 時 filecheck send 應回傳錯誤")
	}
}

// ── looksLikeEmail ────────────────────────────────────────────────────

func TestLooksLikeEmail_Valid(t *testing.T) {
	cases := []string{
		"user@example.com",
		"r021@httc.com.tw",
		"admin+tag@mail.company.org",
	}
	for _, c := range cases {
		if !looksLikeEmail(c) {
			t.Errorf("應為有效 email：%q", c)
		}
	}
}

func TestLooksLikeEmail_Invalid(t *testing.T) {
	cases := []string{
		"ADDR[r021@httc.com.tw]", // 含方括號
		"noatsign",               // 無 @
		"@nodomain",              // @ 在開頭
		"user@",                  // @ 在結尾
		"user @example.com",      // 含空格
		"<user@example.com>",     // 含角括號
	}
	for _, c := range cases {
		if looksLikeEmail(c) {
			t.Errorf("應為無效 email：%q", c)
		}
	}
}

// ── applyMailFlags email 驗證 ─────────────────────────────────────────

func TestApplyMailFlags_InvalidEmail_ReturnsError(t *testing.T) {
	var m config.FilecheckMailSettings
	err := applyMailFlags(&m, []string{"--to", "ADDR[r021@httc.com.tw]"})
	if err == nil {
		t.Fatal("傳入 ADDR[...] 格式應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "無效") {
		t.Errorf("錯誤應提示無效地址，got %q", err.Error())
	}
}

func TestApplyMailFlags_ValidEmail_Succeeds(t *testing.T) {
	var m config.FilecheckMailSettings
	if err := applyMailFlags(&m, []string{"--to", "r021@httc.com.tw"}); err != nil {
		t.Fatalf("有效 email 不應回傳錯誤，got %v", err)
	}
	if len(m.To) != 1 || m.To[0] != "r021@httc.com.tw" {
		t.Errorf("收件人應為 r021@httc.com.tw，got %v", m.To)
	}
}

func TestApplyMailFlags_MultipleEmails(t *testing.T) {
	var m config.FilecheckMailSettings
	if err := applyMailFlags(&m, []string{"--to", "a@x.com,b@y.com"}); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	if len(m.To) != 2 {
		t.Errorf("應設定 2 位收件人，got %d", len(m.To))
	}
}

// ── mailAddTo ─────────────────────────────────────────────────────────

func TestMailAddTo_AppendsRecipient(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "first@example.com"})

	if err := mailAddTo([]string{"second@example.com"}); err != nil {
		t.Fatalf("mailAddTo 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if len(s.Filecheck.Mail.To) != 2 {
		t.Errorf("應有 2 位收件人，got %d: %v", len(s.Filecheck.Mail.To), s.Filecheck.Mail.To)
	}
}

func TestMailAddTo_DuplicateSkipped(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "same@example.com"})
	_ = mailAddTo([]string{"same@example.com"})

	s, _ := config.Load()
	if len(s.Filecheck.Mail.To) != 1 {
		t.Errorf("重複地址應被略過，應有 1 位收件人，got %d", len(s.Filecheck.Mail.To))
	}
}

func TestMailAddTo_InvalidEmail_ReturnsError(t *testing.T) {
	setupConfig(t)
	err := mailAddTo([]string{"ADDR[bad@email]"})
	if err == nil {
		t.Fatal("無效 email 應回傳錯誤")
	}
}

func TestMailAddTo_NoArgs_ReturnsError(t *testing.T) {
	setupConfig(t)
	if err := mailAddTo([]string{}); err == nil {
		t.Fatal("無收件人應回傳錯誤")
	}
}

func TestMailAddTo_WithToFlag(t *testing.T) {
	setupConfig(t)
	if err := mailAddTo([]string{"--to", "flagged@example.com"}); err != nil {
		t.Fatalf("--to 旗標方式不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	found := false
	for _, a := range s.Filecheck.Mail.To {
		if a == "flagged@example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("應含 flagged@example.com，got %v", s.Filecheck.Mail.To)
	}
}

// ── mailSend --to 格式驗證 ────────────────────────────────────────────

func TestMailSend_InvalidToFlag_ReturnsError(t *testing.T) {
	setupConfig(t)
	err := mailSendWithSender([]string{"--to", "ADDR[bad@addr]"}, nil)
	if err == nil {
		t.Fatal("--to 傳入 ADDR[...] 格式應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "無效") {
		t.Errorf("錯誤應提示無效，got %q", err.Error())
	}
}

// ── help text 子指令檢查 ────────────────────────────────────────────────

func TestHelpText_DoesNotAcceptOldMailSubcommand(t *testing.T) {
	// 'filecheck help' 應正常執行
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("filecheck help 不應回傳錯誤，got %v", err)
	}
	// '層級已移除的 filecheck mail help' 應回傳錯誤
	if err := Run([]string{"mail", "help"}); err == nil {
		t.Fatal("預期 'filecheck mail help' 回傳錯誤，因為 'mail' 已不再是有效子指令")
	}
}
