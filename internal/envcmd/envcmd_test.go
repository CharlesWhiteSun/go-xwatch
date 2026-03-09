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

func TestRun_StatusDoesNotError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	if err := Run([]string{"status"}); err != nil {
		t.Errorf("env status 不應回傳錯誤，got %v", err)
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
	// 切換後 mail 收件人應更新為 dev 預設清單
	wantTo := config.DefaultMailToListForEnv(config.EnvDev)
	if len(s.Mail.To) != len(wantTo) {
		t.Errorf("mail.To 長度應為 %d，實際 %d: %v", len(wantTo), len(s.Mail.To), s.Mail.To)
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
	// 切換後 mail 收件人應更新為 prod 預設清單（含 589497@cpc.com.tw）
	wantTo := config.DefaultMailToListForEnv(config.EnvProd)
	if len(s.Mail.To) != len(wantTo) || s.Mail.To[0] != wantTo[0] {
		t.Errorf("mail.To 應為 prod 預設清單，實際 %v", s.Mail.To)
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

// TestSetEnv_OverwritesRecipientsWithEnvDefault 切換環境時，會以目標環境預設清單覆蓋現有收件人設定。
func TestSetEnv_OverwritesRecipientsWithEnvDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	// 已手動設定自訂收件人
	initial := config.Settings{
		RootDir:     root,
		Environment: config.EnvProd,
	}
	initial.Mail.To = []string{"custom@example.com"}
	initial.Filecheck.Mail.To = []string{"custom@example.com"}
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
	if s.Environment != config.EnvDev {
		t.Errorf("環境應為 dev，實際 %q", s.Environment)
	}
	// mail.To 應被覆蓋為 dev 預設清單
	wantTo := config.DefaultMailToListForEnv(config.EnvDev)
	if len(s.Mail.To) != len(wantTo) {
		t.Errorf("切換環境應覆蓋 mail.To 為環境預設，實際 %v", s.Mail.To)
	}
	// filecheck.mail.To 應被覆蓋為 dev 預設清單
	if len(s.Filecheck.Mail.To) != len(wantTo) {
		t.Errorf("切換環境應覆蓋 filecheck.mail.To 為環境預設，實際 %v", s.Filecheck.Mail.To)
	}
}

// TestSetEnv_UpdatesFilecheckMailRecipients 切換環境時，filecheck.mail.to 也應同步更新。
func TestSetEnv_UpdatesFilecheckMailRecipients(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	// 從 dev 出發，filecheck.mail.to 為 dev 清單
	if err := config.Save(config.Settings{RootDir: root, Environment: config.EnvDev}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	// 切換到 prod
	if err := setEnv(config.EnvProd); err != nil {
		t.Fatalf("setEnv prod 失敗: %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	wantTo := config.DefaultMailToListForEnv(config.EnvProd)
	if len(s.Filecheck.Mail.To) != len(wantTo) {
		t.Errorf("filecheck.mail.To 應為 prod 預設清單長度 %d，實際 %d: %v",
			len(wantTo), len(s.Filecheck.Mail.To), s.Filecheck.Mail.To)
	}
	if len(s.Filecheck.Mail.To) > 0 && s.Filecheck.Mail.To[0] != wantTo[0] {
		t.Errorf("filecheck.mail.To 首位應為 %q，實際 %q", wantTo[0], s.Filecheck.Mail.To[0])
	}
}

// ── Charles 隱藏環境測試 ──────────────────────────────────────────────────────

func TestSetEnv_SwitchesToCharles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, Environment: config.EnvProd}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	if err := setEnv(config.EnvCharles); err != nil {
		t.Fatalf("setEnv charles 不應回傳錯誤，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	if s.Environment != config.EnvCharles {
		t.Errorf("環境應為 charles，實際 %q", s.Environment)
	}
	// mail.To 應更新為 charles 專屬清單
	wantTo := config.DefaultMailToListForEnv(config.EnvCharles)
	if len(s.Mail.To) != len(wantTo) {
		t.Fatalf("mail.To 應有 %d 位，實際 %d: %v", len(wantTo), len(s.Mail.To), s.Mail.To)
	}
	for i, addr := range wantTo {
		if s.Mail.To[i] != addr {
			t.Errorf("mail.To[%d] 應為 %q，實際 %q", i, addr, s.Mail.To[i])
		}
	}
	// filecheck.mail.To 應同步更新
	if len(s.Filecheck.Mail.To) != len(wantTo) {
		t.Fatalf("filecheck.mail.To 應有 %d 位，實際 %d: %v", len(wantTo), len(s.Filecheck.Mail.To), s.Filecheck.Mail.To)
	}
}

func TestRun_SetCharlesWorks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, Environment: config.EnvDev}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	// env set Charles（大小寫不限）應成功
	if err := Run([]string{"set", "Charles"}); err != nil {
		t.Fatalf("env set Charles 不應回傳錯誤，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	wantTo := config.DefaultMailToListForEnv(config.EnvCharles)
	if len(s.Mail.To) != len(wantTo) {
		t.Errorf("切換 charles 後 mail.To 應有 %d 位，實際 %d: %v", len(wantTo), len(s.Mail.To), s.Mail.To)
	}
}

func TestRun_SetCharlesRecipientsContainExpectedAddresses(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	if err := Run([]string{"set", "charles"}); err != nil {
		t.Fatalf("env set charles 不應回傳錯誤，got %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}
	// 驗證兩個預期收件人均出現在 mail.To 清單中
	expectedAddrs := config.DefaultMailToListCharles
	for _, want := range expectedAddrs {
		found := false
		for _, got := range s.Mail.To {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mail.To 應含 %q，實際 %v", want, s.Mail.To)
		}
	}
}

func TestRun_SetInvalidEnvStillRejectsUnknown(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	// staging 不在任何環境清單中，應被拒絕
	if err := Run([]string{"set", "staging"}); err == nil {
		t.Error("不支援的環境名稱 staging 應回傳錯誤")
	}
}
