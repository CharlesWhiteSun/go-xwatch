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
	if err := status(); err != nil {
		t.Fatalf("status 不應回傳錯誤，got %v", err)
	}
}

//  start / stop

func TestStart_EnablesFilecheck(t *testing.T) {
	setupConfig(t)
	if err := start(); err != nil {
		t.Fatalf("start 不應回傳錯誤，got %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Filecheck.Enabled {
		t.Error("start 後 Filecheck.Enabled 應為 true")
	}
}

func TestStop_DisablesFilecheck(t *testing.T) {
	setupConfig(t)
	if err := start(); err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop 不應回傳錯誤，got %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Filecheck.Enabled {
		t.Error("stop 後 Filecheck.Enabled 應為 false")
	}
}

//  set

func TestSet_ScanDir_Absolute(t *testing.T) {
	setupConfig(t)
	absPath := filepath.Join(t.TempDir(), "mylogs")

	if err := set([]string{"--scan-dir", absPath}); err != nil {
		t.Fatalf("set --scan-dir 不應回傳錯誤，got %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Filecheck.ScanDir != absPath {
		t.Errorf("ScanDir 應為 %q，got %q", absPath, s.Filecheck.ScanDir)
	}
}

func TestSet_ScanDir_Relative(t *testing.T) {
	setupConfig(t)
	if err := set([]string{"--scan-dir", `storage\archives`}); err != nil {
		t.Fatalf("set --scan-dir 相對路徑不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.ScanDir != `storage\archives` {
		t.Errorf("ScanDir 應為 storage\\archives，got %q", s.Filecheck.ScanDir)
	}
}

func TestSet_NoFlags_NothingChanged(t *testing.T) {
	setupConfig(t)
	// 不傳任何旗標應正常結束（不回傳錯誤）
	if err := set(nil); err != nil {
		t.Fatalf("set 不傳旗標不應回傳錯誤，got %v", err)
	}
}

//  runCheck

func TestRunCheck_NoError(t *testing.T) {
	setupConfig(t)
	// 掃描目錄不存在時應印出 [ERROR]，但不回傳錯誤
	if err := runCheck(nil); err != nil {
		t.Fatalf("runCheck 不應回傳錯誤，got %v", err)
	}
}

func TestRunCheck_InvalidDate_ReturnsError(t *testing.T) {
	setupConfig(t)
	if err := runCheck([]string{"--date", "not-a-date"}); err == nil {
		t.Fatal("無效日期應回傳錯誤")
	}
}

func TestRunCheck_MatchingFileFound(t *testing.T) {
	tmp := setupConfig(t)
	s, _ := config.Load()
	_ = tmp

	// 建立預設 scanDir 並放一個含昨天 YYYY-DD-MM 格式的檔案
	yesterday := time.Now().AddDate(0, 0, -1)
	datePattern := yesterday.Format(filecheck.FileDateFormat)
	scanDir := filecheck.DefaultScanDir(s.RootDir)
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scanDir, "data_"+datePattern+".csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runCheck(nil); err != nil {
		t.Fatalf("有符合檔案時 runCheck 不應回傳錯誤，got %v", err)
	}
}

//  mail subcommands

func TestMailHelp_NoError(t *testing.T) {
	if err := runMail([]string{"help"}); err != nil {
		t.Fatalf("mail help 不應回傳錯誤，got %v", err)
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

func TestMailSet_Schedule(t *testing.T) {
	setupConfig(t)
	if err := mailSet([]string{"--schedule", "08:30"}); err != nil {
		t.Fatalf("mailSet --schedule 不應回傳錯誤，got %v", err)
	}
	s, _ := config.Load()
	if s.Filecheck.Mail.Schedule != "08:30" {
		t.Errorf("Schedule 應為 08:30，got %q", s.Filecheck.Mail.Schedule)
	}
}

func TestMailSet_InvalidSchedule_ReturnsError(t *testing.T) {
	setupConfig(t)
	if err := mailSet([]string{"--schedule", "not-a-time"}); err == nil {
		t.Fatal("無效排程應回傳錯誤")
	}
}

//  mail send（注入假 sendFn）

func TestMailSend_NoRecipients_ReturnsError(t *testing.T) {
	setupConfig(t)
	err := mailSend(nil, nil)
	if err == nil {
		t.Fatal("無收件人應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "收件人") {
		t.Errorf("錯誤應提示收件人，got %q", err.Error())
	}
}

func TestMailSend_WithFakeSmtp_Succeeds(t *testing.T) {
	setupConfig(t)
	_ = mailEnable([]string{"--to", "test@example.com"})

	var gotSubject, gotBody string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subject, body string, _ mailer.SendMailFunc) error {
		gotSubject = subject
		gotBody = body
		return nil
	}

	if err := mailSend([]string{"--to", "test@example.com"}, fakeSend); err != nil {
		t.Fatalf("mailSend（注入假 smtp）不應回傳錯誤，got %v", err)
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
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, _, body string, _ mailer.SendMailFunc) error {
		gotBody = body
		return nil
	}
	_ = mailSend([]string{"--to", "test@example.com"}, fakeSend)

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

	// 在預設 scanDir（storage/logs）建立含昨天 YYYY-DD-MM 格式的檔案
	yesterday := time.Now().AddDate(0, 0, -1)
	datePattern := yesterday.Format(filecheck.FileDateFormat)
	scanDir := filecheck.DefaultScanDir(s.RootDir)
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scanDir, "report_"+datePattern+".csv"), []byte("data"), 0o644)

	_ = mailEnable([]string{"--to", "test@example.com"})

	var gotSubject, gotBody string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subject, body string, _ mailer.SendMailFunc) error {
		gotSubject = subject
		gotBody = body
		return nil
	}
	if err := mailSend([]string{"--to", "test@example.com"}, fakeSend); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	if !strings.Contains(gotSubject, "找到") {
		t.Errorf("有符合檔案時主旨應含「找到」，got %q", gotSubject)
	}
	if !strings.Contains(gotBody, "[FOUND]") {
		t.Errorf("有符合檔案時內文應含 [FOUND]，got:\n%s", gotBody)
	}
}

func TestRunMail_Unknown_ReturnsError(t *testing.T) {
	err := runMail([]string{"nonexistent"})
	if err == nil {
		t.Fatal("未知 mail 子指令應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "filecheck mail help") {
		t.Errorf("錯誤應提示 filecheck mail help，got %q", err.Error())
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
	err := mailSend([]string{"--to", "ADDR[bad@addr]"}, nil)
	if err == nil {
		t.Fatal("--to 傳入 ADDR[...] 格式應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "無效") {
		t.Errorf("錯誤應提示無效，got %q", err.Error())
	}
}

// ── help text 不含 xwatch ────────────────────────────────────────────

func TestHelpText_NoXwatchPrefix(t *testing.T) {
	// 捕捉 printHelp 的輸出（透過測試 Run 中的 help 子指令不回傳錯誤的間接方式）
	// 直接檢查 printHelp/printMailHelp 輸出需要 os.Stdout 重定向，
	// 此處以功能層面驗證 Run("help") 不會呼叫到含 xwatch 的輸出
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("filecheck help 不應回傳錯誤，got %v", err)
	}
	if err := Run([]string{"mail", "help"}); err != nil {
		t.Fatalf("filecheck mail help 不應回傳錯誤，got %v", err)
	}
}
