package mailcmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
)

// ── Runner 介面配適測試 ───────────────────────────────────────────────

// TestRunner_ServiceRequiredFor 確認 ServiceRequiredFor 回傳正確功能名稱與子指令清單。
func TestRunner_ServiceRequiredFor(t *testing.T) {
	feature, subcmds := Runner.ServiceRequiredFor()
	if feature == "" {
		t.Error("feature 名稱不應為空")
	}
	if !strings.Contains(feature, "郵件") {
		t.Errorf("feature 應包含「郵件」，實際：%q", feature)
	}
	if len(subcmds) == 0 {
		t.Error("subcmds 不應為空")
	}
	found := false
	for _, s := range subcmds {
		if s == "enable" {
			found = true
		}
	}
	if !found {
		t.Errorf("subcmds 應包含 \"enable\"，實際：%v", subcmds)
	}
}

// TestRunner_ServiceRequiredFor_Idempotent 確認多次呼叫回傳相同結果。
func TestRunner_ServiceRequiredFor_Idempotent(t *testing.T) {
	f1, s1 := Runner.ServiceRequiredFor()
	f2, s2 := Runner.ServiceRequiredFor()
	if f1 != f2 {
		t.Errorf("多次呼叫 feature 不一致：%q vs %q", f1, f2)
	}
	if len(s1) != len(s2) {
		t.Errorf("多次呼叫 subcmds 長度不一致：%d vs %d", len(s1), len(s2))
	}
}

// TestRunner_Run_HelpSubcmd 確認 Runner.Run 可正確委派至 Run("help")。
func TestRunner_Run_HelpSubcmd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	if err := config.Save(config.Settings{RootDir: filepath.Join(tmp, "root")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Runner.Run([]string{"help"}); err != nil {
		t.Errorf("Runner.Run([help]) 不應回傳錯誤，實際：%v", err)
	}
}

// TestRunner_Run_StatusSubcmd 確認 Runner.Run 可正確委派至 Run("status")。
func TestRunner_Run_StatusSubcmd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	if err := config.Save(config.Settings{RootDir: filepath.Join(tmp, "root")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Runner.Run([]string{"status"}); err != nil {
		t.Errorf("Runner.Run([status]) 不應回傳錯誤，實際：%v", err)
	}
}

// TestRunner_Run_UnknownSubcmd 確認 Runner.Run 對未知子指令回傳錯誤。
func TestRunner_Run_UnknownSubcmd(t *testing.T) {
	// 需要基本 config 環境
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	_ = os.MkdirAll(filepath.Join(tmp, "go-xwatch"), 0o755)
	err := Runner.Run([]string{"nonexistent-cmd-xyz"})
	if err == nil {
		t.Error("對未知子指令應回傳錯誤，但得到 nil")
	}
}

// TestRunner_SendSubcmd_CallsInjectedSender 確認 Runner.Run("send") 使用注入的 GmailSender（ISP 驗證）。
// 這選話看出了 ISP 動機：縖譯期就能測試、不需起真實 SMTP。
func TestRunner_SendSubcmd_CallsInjectedSender(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	if err := config.Save(config.Settings{
		RootDir: filepath.Join(tmp, "root"),
		Mail: config.MailSettings{
			To:              []string{"test@example.com"},
			SMTPDialTimeout: 5,
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var called bool
	r := mailCmdRunner{sender: &mockGmailSender{fn: func(
		_ context.Context, _ mailer.SMTPConfig, _ mailer.ReportOptions, _ mailer.SendMailFunc,
	) error {
		called = true
		return nil
	}}}

	if err := r.Run([]string{"send", "--to", "test@example.com"}); err != nil {
		t.Fatalf("r.Run send 不應回傳錯誤，got %v", err)
	}
	if !called {
		t.Error("runner.Run 應使用注入的 GmailSender，但 mock 未被呼叫")
	}
}
