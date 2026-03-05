package envcmd

import (
	"path/filepath"
	"testing"

	"go-xwatch/internal/config"
)

// ── Run 單元測試 ──────────────────────────────────────────────────────────────

func TestRun_HelpDoesNotError(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Errorf("env help 不應回傳錯誤，got %v", err)
	}
}

func TestRun_UnknownSubcommandReturnsError(t *testing.T) {
	if err := Run([]string{"unknown"}); err == nil {
		t.Error("未知子指令應回傳錯誤")
	}
}

func TestRun_SetMissingArgReturnsError(t *testing.T) {
	if err := Run([]string{"set"}); err == nil {
		t.Error("env set 未給環境名稱應回傳錯誤")
	}
}

func TestRun_SetInvalidEnvReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	// 先寫入 config
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	if err := Run([]string{"set", "staging"}); err == nil {
		t.Error("不支援的環境名稱應回傳錯誤")
	}
}

// ── setEnv 整合測試 ────────────────────────────────────────────────────────────

func TestSetEnv_SwitchesToDev(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	if err := setEnv(config.EnvDev); err != nil {
		t.Fatalf("setEnv dev 不應回傳錯誤，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if s.Environment != config.EnvDev {
		t.Errorf("環境應為 dev，實際 %q", s.Environment)
	}
}

func TestSetEnv_SwitchesToProd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	// 先設定為 dev
	if err := config.Save(config.Settings{RootDir: root, Environment: config.EnvDev}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	if err := setEnv(config.EnvProd); err != nil {
		t.Fatalf("setEnv prod 不應回傳錯誤，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if s.Environment != config.EnvProd {
		t.Errorf("環境應為 prod，實際 %q", s.Environment)
	}
}

func TestSetEnv_SameEnvNoChange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, Environment: config.EnvDev}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	// 設定相同環境不應報錯
	if err := setEnv(config.EnvDev); err != nil {
		t.Fatalf("相同環境不應報錯，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if s.Environment != config.EnvDev {
		t.Errorf("環境應維持 dev，實際 %q", s.Environment)
	}
}

// TestSetEnv_DoesNotOverwriteExistingRecipients 切換環境不會改寫已設定的收件人。
func TestSetEnv_DoesNotOverwriteExistingRecipients(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	// 已手動設定收件人
	customRecipients := []string{"custom@example.com"}
	initial := config.Settings{
		RootDir:     root,
		Environment: config.EnvProd,
	}
	initial.Mail.To = customRecipients
	if err := config.Save(initial); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	// 切換到 dev
	if err := setEnv(config.EnvDev); err != nil {
		t.Fatalf("setEnv dev 失敗: %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	// 環境已切換
	if s.Environment != config.EnvDev {
		t.Errorf("環境應為 dev，實際 %q", s.Environment)
	}
	// 收件人仍為原始設定，未被覆蓋
	if len(s.Mail.To) != 1 || s.Mail.To[0] != "custom@example.com" {
		t.Errorf("切換環境不應改寫已設定的收件人，實際 %v", s.Mail.To)
	}
}
