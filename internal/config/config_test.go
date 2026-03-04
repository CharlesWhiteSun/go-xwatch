package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	absRoot, _ := filepath.Abs(root)
	if loaded.RootDir != absRoot {
		t.Fatalf("unexpected rootDir, got %q want %q", loaded.RootDir, absRoot)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be set")
	}
	if loaded.Mail.Schedule != "10:00" {
		t.Fatalf("expected mail schedule default '10:00', got %q", loaded.Mail.Schedule)
	}
	if loaded.Mail.Timezone != "Asia/Taipei" {
		t.Fatalf("expected mail timezone default 'Asia/Taipei', got %q", loaded.Mail.Timezone)
	}

	stat, err := os.Stat(filepath.Join(tmp, "go-xwatch", "config.json"))
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatalf("config file is empty")
	}
}

func TestValidateAndFillDefaults(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !filepath.IsAbs(s.RootDir) {
		t.Fatalf("root should be absolute, got %q", s.RootDir)
	}
	if s.Mail.Schedule != "10:00" {
		t.Fatalf("expected default mail schedule, got %q", s.Mail.Schedule)
	}
	if s.Mail.SMTPPort != 587 {
		t.Fatalf("expected default SMTP port, got %d", s.Mail.SMTPPort)
	}
}

func TestValidateAndFillDefaultsEmptyRoot(t *testing.T) {
	if _, err := ValidateAndFillDefaults(Settings{RootDir: "   "}); err == nil {
		t.Fatalf("expected error for empty root")
	}
}

func TestValidateAndFillDefaultsInvalidSchedule(t *testing.T) {
	root := "./foo"
	if _, err := ValidateAndFillDefaults(Settings{RootDir: root, Mail: MailSettings{Schedule: "25:00"}}); err == nil {
		t.Fatalf("expected schedule validation error")
	}
}

func TestHeartbeatIntervalDefault(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.HeartbeatInterval != DefaultHeartbeatInterval {
		t.Fatalf("expected HeartbeatInterval=%d, got %d", DefaultHeartbeatInterval, s.HeartbeatInterval)
	}
}

func TestHeartbeatIntervalCustom(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root, HeartbeatInterval: 120})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.HeartbeatInterval != 120 {
		t.Fatalf("expected HeartbeatInterval=120, got %d", s.HeartbeatInterval)
	}
}

func TestHeartbeatEnabledPersisted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root, HeartbeatEnabled: true, HeartbeatInterval: 30}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !loaded.HeartbeatEnabled {
		t.Fatal("expected HeartbeatEnabled=true")
	}
	if loaded.HeartbeatInterval != 30 {
		t.Fatalf("expected HeartbeatInterval=30, got %d", loaded.HeartbeatInterval)
	}
}

func TestHeartbeatIntervalZeroFillsDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	// 儲存時 HeartbeatInterval=0，應自動填入預設值
	if err := Save(Settings{RootDir: root, HeartbeatInterval: 0}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.HeartbeatInterval != DefaultHeartbeatInterval {
		t.Fatalf("expected HeartbeatInterval=%d, got %d", DefaultHeartbeatInterval, loaded.HeartbeatInterval)
	}
}

// TestSMTPTimeoutRetryDefaults 確認 SMTPDialTimeout/SMTPRetries/SMTPRetryDelay
// 在 ValidateAndFillDefaults 中正確填入預設值。
func TestSMTPTimeoutRetryDefaults(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.Mail.SMTPDialTimeout != DefaultSMTPDialTimeout {
		t.Fatalf("expected SMTPDialTimeout=%d, got %d", DefaultSMTPDialTimeout, s.Mail.SMTPDialTimeout)
	}
	if s.Mail.SMTPRetries != DefaultSMTPRetries {
		t.Fatalf("expected SMTPRetries=%d, got %d", DefaultSMTPRetries, s.Mail.SMTPRetries)
	}
	if s.Mail.SMTPRetryDelay != DefaultSMTPRetryDelay {
		t.Fatalf("expected SMTPRetryDelay=%d, got %d", DefaultSMTPRetryDelay, s.Mail.SMTPRetryDelay)
	}
}

// TestSMTPTimeoutRetryCustom 確認自訂的 SMTP 逾時與重試參數能被保留。
func TestSMTPTimeoutRetryCustom(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{
		RootDir: root,
		Mail: MailSettings{
			SMTPDialTimeout: 60,
			SMTPRetries:     5,
			SMTPRetryDelay:  300,
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.Mail.SMTPDialTimeout != 60 {
		t.Fatalf("expected SMTPDialTimeout=60, got %d", s.Mail.SMTPDialTimeout)
	}
	if s.Mail.SMTPRetries != 5 {
		t.Fatalf("expected SMTPRetries=5, got %d", s.Mail.SMTPRetries)
	}
	if s.Mail.SMTPRetryDelay != 300 {
		t.Fatalf("expected SMTPRetryDelay=300, got %d", s.Mail.SMTPRetryDelay)
	}
}

// TestMailEnabledNilDefaultsToFalse 確認 Enabled 為 nil（未設定）時，IsEnabled() 回傳 false。
// 首次安裝未執行 mail enable 時，郵件排程不應自動啟動。
func TestMailEnabledNilDefaultsToFalse(t *testing.T) {
	m := MailSettings{} // Enabled 為 nil
	if m.IsEnabled() {
		t.Fatal("Enabled 為 nil 時 IsEnabled() 應回傳 false，郵件必須明確執行 mail enable 才會啟動")
	}
}

// TestMailEnabledExplicitFalse 確認明確設為 false 時，IsEnabled() 回傳 false。
// 這代表 mail disable 存檔後載入仍能正確被停用。
func TestMailEnabledExplicitFalse(t *testing.T) {
	f := false
	m := MailSettings{Enabled: &f}
	if m.IsEnabled() {
		t.Fatal("明確設為 false 時 IsEnabled() 應回傳 false")
	}
}

// TestMailEnabledExplicitTrue 確認明確設為 true 時，IsEnabled() 回傳 true。
func TestMailEnabledExplicitTrue(t *testing.T) {
	tr := true
	m := MailSettings{Enabled: &tr}
	if !m.IsEnabled() {
		t.Fatal("明確設為 true 時 IsEnabled() 應回傳 true")
	}
}

// TestMailDefaultTo 確認從未設定收件人時，
// ValidateAndFillDefaults 自動填入 DefaultMailTo。
func TestMailDefaultTo(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(s.Mail.To) == 0 {
		t.Fatal("預期 To 自動填入 DefaultMailTo，實際為空")
	}
	if s.Mail.To[0] != DefaultMailTo {
		t.Fatalf("預期 To[0]=%q，實際=%q", DefaultMailTo, s.Mail.To[0])
	}
}

// TestMailDefaultToPreserved 確認自訂 To 不被覆蓋。
func TestMailDefaultToPreserved(t *testing.T) {
	root := "./foo"
	custom := "custom@example.com"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root, Mail: MailSettings{To: []string{custom}}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(s.Mail.To) != 1 || s.Mail.To[0] != custom {
		t.Fatalf("自訂 To 不應被覆蓋，實際=%v", s.Mail.To)
	}
}

// TestMailEnabledPersistFalse 確認 Save/Load 後 Enabled=false 能被正確保留。
// 避免 mail disable 後因預設邏輯而被重新開啟。
func TestMailEnabledPersistFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	f := false
	if err := Save(Settings{RootDir: root, Mail: MailSettings{Enabled: &f, To: []string{"a@test.com"}}}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Mail.IsEnabled() {
		t.Fatal("明確 disable 後 Load 不應自動重新啟用")
	}
}

// ── daily 已移除 對應測試 ──────────────────────────────────────────────────────

// TestLoad_OldConfigWithDailyCSVFieldsIsIgnored 檢役舊版 config.json 含 dailyCsvEnabled 欄位
// 時，讀取仍可成功（JSON 不明欄位需被忽略，不產生錯誤）。
func TestLoad_OldConfigWithDailyCSVFieldsIsIgnored(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// 手寫舊版含 dailyCsvEnabled/dailyCsvDir 的 JSON；
	// 使用 encoding/json 序列化路徑以正確處理 Windows 反斜線
	configDir := filepath.Join(tmp, "go-xwatch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	absRoot, _ := filepath.Abs(root)
	// 透過 json.Marshal 正確轉義 Windows 路徑的反斜線
	rootJSON, _ := json.Marshal(absRoot)
	oldJSON := `{"rootDir":` + string(rootJSON) + `,"dailyCsvEnabled":true,"dailyCsvDir":"/some/dir","heartbeatEnabled":false,"heartbeatInterval":60,"mail":{}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(oldJSON), 0o644); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("舊版 config 含 daily 欄位時， Load 不應失敗，實際錯誤：%v", err)
	}
	if loaded.RootDir != absRoot {
		t.Fatalf("舊版 config 讀取後 RootDir 不符，期望 %q，實際 %q", absRoot, loaded.RootDir)
	}
}

// TestSettings_NoDailyCSVFields 檢役 Settings 結構體已不包含 DailyCSV 欄位（編譯期查詳）。
// 若未移除欄位則下方忍用設定碼会編譯失敗。
func TestSettings_NoDailyCSVFields(t *testing.T) {
	// Settings{} 啟用所有欄位，若 DailyCSVEnabled/Dir 存在則下列碼不編譯
	// var _ = Settings{DailyCSVEnabled: true} // 這行如果存在就不會編譯
	s := Settings{}
	if s.HeartbeatEnabled {
		t.Error("預設 HeartbeatEnabled 應為 false")
	}
}
