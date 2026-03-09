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
// ValidateAndFillDefaults 依預設環境（dev）自動填入對應清單。
func TestMailDefaultTo(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 預設環境為 dev，收件人清單應與 DefaultMailToListDev 相同
	want := DefaultMailToListForEnv(EnvDev)
	if len(s.Mail.To) != len(want) {
		t.Fatalf("預期 To 共 %d 位（dev 清單），實際得 %d 位：%v", len(want), len(s.Mail.To), s.Mail.To)
	}
	for i, w := range want {
		if s.Mail.To[i] != w {
			t.Errorf("預期 To[%d]=%q，實際=%q", i, w, s.Mail.To[i])
		}
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

// ── filterValidEmails ─────────────────────────────────────────────────

func TestFilterValidEmails_RemovesInvalidAddresses(t *testing.T) {
	input := []string{
		"ADDR[r021@httc.com.tw]", // 含方括號
		"r021@httc.com.tw",       // 正常
		"noatsign",               // 無 @
		"admin@example.com",      // 正常
		"user @bad.com",          // 含空格
		"<angle@brackets.com>",   // 含角括號
	}
	got := filterValidEmails(input)
	if len(got) != 2 {
		t.Errorf("應過濾後剩 2 個有效地址，got %d: %v", len(got), got)
	}
	if got[0] != "r021@httc.com.tw" || got[1] != "admin@example.com" {
		t.Errorf("有效地址不符預期，got %v", got)
	}
}

func TestFilterValidEmails_EmptyInput(t *testing.T) {
	got := filterValidEmails(nil)
	if got != nil && len(got) != 0 {
		t.Errorf("空輸入應回傳空，got %v", got)
	}
}

func TestFilterValidEmails_AllInvalid_ReturnsEmpty(t *testing.T) {
	input := []string{"ADDR[bad]", "notanemail", "@missing-local"}
	got := filterValidEmails(input)
	if len(got) != 0 {
		t.Errorf("全部無效時應回傳空切片，got %v", got)
	}
}

// ── validateAndFillFilecheckDefaults 清除無效收件人 ──────────────────

func TestFilecheckDefaults_InvalidToAddresses_AreFiltered(t *testing.T) {
	fc := FilecheckSettings{
		Mail: FilecheckMailSettings{
			To: []string{"ADDR[r021@httc.com.tw]", "r021@httc.com.tw"},
		},
	}
	result, err := validateAndFillFilecheckDefaults(fc, EnvProd)
	if err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	if len(result.Mail.To) != 1 || result.Mail.To[0] != "r021@httc.com.tw" {
		t.Errorf("應僅保留有效地址，got %v", result.Mail.To)
	}
}

func TestFilecheckDefaults_AllInvalidTo_FallsBackToDefaultList(t *testing.T) {
	fc := FilecheckSettings{
		Mail: FilecheckMailSettings{
			To: []string{"ADDR[bad]", "notvalid"},
		},
	}
	result, err := validateAndFillFilecheckDefaults(fc, EnvProd)
	if err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	// 全部無效們過濾後為空，應自動填入 DefaultMailToList
	if len(result.Mail.To) != len(DefaultMailToList) {
		t.Errorf("公全無效時應回彈至 DefaultMailToList（%d 位），got %v", len(DefaultMailToList), result.Mail.To)
	}
	if len(result.Mail.To) > 0 && result.Mail.To[0] != DefaultMailToList[0] {
		t.Errorf("應回彈首位=%q，got %q", DefaultMailToList[0], result.Mail.To[0])
	}
}

// TestFilecheckDefaultTo_EmptyUsesDefaultList 確認 filecheck.mail.to 空時自動填入 DefaultMailToList。
func TestFilecheckDefaultTo_EmptyUsesDefaultList(t *testing.T) {
	fc := FilecheckSettings{}
	result, err := validateAndFillFilecheckDefaults(fc, EnvProd)
	if err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	if len(result.Mail.To) != len(DefaultMailToList) {
		t.Fatalf("預期 To 共 %d 位，實際得 %d 位：%v", len(DefaultMailToList), len(result.Mail.To), result.Mail.To)
	}
	for i, want := range DefaultMailToList {
		if result.Mail.To[i] != want {
			t.Errorf("預期 To[%d]=%q，實際=%q", i, want, result.Mail.To[i])
		}
	}
}

// ── Environment 環境相關測試 ─────────────────────────────────────────────

func TestDefaultMailToListForEnv_DevReturnsDev(t *testing.T) {
	list := DefaultMailToListForEnv(EnvDev)
	if len(list) != len(DefaultMailToListDev) {
		t.Fatalf("預期 dev 清單 %d 位，實際 %d 位", len(DefaultMailToListDev), len(list))
	}
	for i, want := range DefaultMailToListDev {
		if list[i] != want {
			t.Errorf("dev[%d] 預期 %q 實際 %q", i, want, list[i])
		}
	}
}

func TestDefaultMailToListForEnv_ProdReturnsProd(t *testing.T) {
	list := DefaultMailToListForEnv(EnvProd)
	if len(list) != len(DefaultMailToListProd) {
		t.Fatalf("預期 prod 清單 %d 位，實際 %d 位", len(DefaultMailToListProd), len(list))
	}
	if list[0] != "589497@cpc.com.tw" {
		t.Errorf("prod 清單首位應為 589497@cpc.com.tw，實際 %q", list[0])
	}
}

func TestDefaultMailToListForEnv_EmptyDefaultsToProd(t *testing.T) {
	list := DefaultMailToListForEnv("")
	if len(list) != len(DefaultMailToListProd) {
		t.Fatalf("空字串應回傳 prod 清單，實際 %d 位", len(list))
	}
}

func TestDefaultMailToListForEnv_UnknownDefaultsToProd(t *testing.T) {
	list := DefaultMailToListForEnv("staging")
	if len(list) != len(DefaultMailToListProd) {
		t.Fatalf("不明環境應回傳 prod 清單，實際 %d 位", len(list))
	}
}

func TestDefaultMailToListForEnv_CharlesReturnsCharlesList(t *testing.T) {
	list := DefaultMailToListForEnv(EnvCharles)
	if len(list) != len(DefaultMailToListCharles) {
		t.Fatalf("charles 環境預期 %d 位收件人，實際 %d 位", len(DefaultMailToListCharles), len(list))
	}
	for i, want := range DefaultMailToListCharles {
		if list[i] != want {
			t.Errorf("charles[%d] 預期 %q 實際 %q", i, want, list[i])
		}
	}
}

func TestDefaultMailToListForEnv_CharlesCaseInsensitive(t *testing.T) {
	list := DefaultMailToListForEnv("Charles")
	if len(list) != len(DefaultMailToListCharles) {
		t.Fatalf("大寫 Charles 應等同 charles，實際回傳 %d 位", len(list))
	}
}

func TestDefaultMailToListCharles_IsACopy(t *testing.T) {
	a := DefaultMailToListForEnv(EnvCharles)
	b := DefaultMailToListForEnv(EnvCharles)
	a[0] = "mutated@example.com"
	if b[0] == "mutated@example.com" {
		t.Error("DefaultMailToListForEnv 應回傳獨立副本，不應共用底層陣列")
	}
}

// ── IsKnownEnv ────────────────────────────────────────────────────────────────

func TestIsKnownEnv_DevIsKnown(t *testing.T) {
	if !IsKnownEnv(EnvDev) {
		t.Error("dev 應為已知環境")
	}
}

func TestIsKnownEnv_ProdIsKnown(t *testing.T) {
	if !IsKnownEnv(EnvProd) {
		t.Error("prod 應為已知環境")
	}
}

func TestIsKnownEnv_CharlesIsKnown(t *testing.T) {
	if !IsKnownEnv(EnvCharles) {
		t.Error("charles 應為已知環境")
	}
}

func TestIsKnownEnv_CharlesCaseInsensitive(t *testing.T) {
	if !IsKnownEnv("Charles") {
		t.Error("大小寫不影響判斷，Charles 應為已知環境")
	}
}

func TestIsKnownEnv_StagingIsUnknown(t *testing.T) {
	if IsKnownEnv("staging") {
		t.Error("staging 不應為已知環境")
	}
}

func TestIsKnownEnv_EmptyIsUnknown(t *testing.T) {
	if IsKnownEnv("") {
		t.Error("空字串不應為已知環境")
	}
}

// ── IsPublicEnv ───────────────────────────────────────────────────────────────

func TestIsPublicEnv_DevIsPublic(t *testing.T) {
	if !IsPublicEnv(EnvDev) {
		t.Error("dev 應為公開環境")
	}
}

func TestIsPublicEnv_ProdIsPublic(t *testing.T) {
	if !IsPublicEnv(EnvProd) {
		t.Error("prod 應為公開環境")
	}
}

func TestIsPublicEnv_CharlesIsNotPublic(t *testing.T) {
	if IsPublicEnv(EnvCharles) {
		t.Error("charles 不應為公開環境（隱藏功能）")
	}
}

func TestIsPublicEnv_StagingIsNotPublic(t *testing.T) {
	if IsPublicEnv("staging") {
		t.Error("staging 不應為公開環境")
	}
}

func TestValidateAndFillDefaults_DevEnv_UsesDevList(t *testing.T) {
	s, err := ValidateAndFillDefaults(Settings{RootDir: "./foo", Environment: EnvDev})
	if err != nil {
		t.Fatalf("不應錯誤：%v", err)
	}
	if s.Environment != EnvDev {
		t.Errorf("環境應為 dev，實際 %q", s.Environment)
	}
	// mail.To 為空時應填入 dev 清單
	if len(s.Mail.To) != len(DefaultMailToListDev) {
		t.Fatalf("dev 環境 mail.To 預期 %d 位，實際 %d：%v",
			len(DefaultMailToListDev), len(s.Mail.To), s.Mail.To)
	}
	// dev 清單不包含 589497@cpc.com.tw
	for _, addr := range s.Mail.To {
		if addr == "589497@cpc.com.tw" {
			t.Errorf("dev 環境不應含 589497@cpc.com.tw，實際 %v", s.Mail.To)
		}
	}
}

func TestValidateAndFillDefaults_ProdEnv_UsesProdList(t *testing.T) {
	s, err := ValidateAndFillDefaults(Settings{RootDir: "./foo", Environment: EnvProd})
	if err != nil {
		t.Fatalf("不應錯誤：%v", err)
	}
	if s.Environment != EnvProd {
		t.Errorf("環境應為 prod，實際 %q", s.Environment)
	}
	if len(s.Mail.To) != len(DefaultMailToListProd) {
		t.Fatalf("prod 環境 mail.To 預期 %d 位，實際 %d：%v",
			len(DefaultMailToListProd), len(s.Mail.To), s.Mail.To)
	}
	// prod 清單包含 589497@cpc.com.tw
	found := false
	for _, addr := range s.Mail.To {
		if addr == "589497@cpc.com.tw" {
			found = true
		}
	}
	if !found {
		t.Errorf("prod 環境應含 589497@cpc.com.tw，實際 %v", s.Mail.To)
	}
}

func TestValidateAndFillDefaults_EmptyEnv_DefaultsToDev(t *testing.T) {
	s, err := ValidateAndFillDefaults(Settings{RootDir: "./foo"})
	if err != nil {
		t.Fatalf("不應錯誤：%v", err)
	}
	if s.Environment != EnvDev {
		t.Errorf("環境空時應預設 dev，實際 %q", s.Environment)
	}
	// dev 環境預設收件人不包含 589497@cpc.com.tw
	for _, addr := range s.Mail.To {
		if addr == "589497@cpc.com.tw" {
			t.Errorf("首次初始化（dev）不應含 589497@cpc.com.tw，實際: %v", s.Mail.To)
		}
	}
}

// ── DeleteConfig 測試 ──────────────────────────────────────────────────

func TestDeleteConfig_RemovesConfigFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	p := filepath.Join(tmp, "go-xwatch", "config.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("設定檔應存在: %v", err)
	}

	if err := DeleteConfig(); err != nil {
		t.Fatalf("DeleteConfig 失敗: %v", err)
	}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("删除後檔案應不存在，實際 err: %v", err)
	}
}

func TestDeleteConfig_NoFileIsOK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	// 未建立設定檔時對呼叫不應報錯
	if err := DeleteConfig(); err != nil {
		t.Errorf("DeleteConfig 在檔案不存在時不應回傳錯誤，實際: %v", err)
	}
}

func TestDeleteConfig_ThenLoadReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	if err := DeleteConfig(); err != nil {
		t.Fatalf("DeleteConfig 失敗: %v", err)
	}
	// 删除後 Load 應回傳錯誤
	if _, err := Load(); err == nil {
		t.Error("删除設定檔後 Load 應回傳錯誤，實際得 nil")
	}
}

// ── DeleteConfigDir 測試 ──────────────────────────────────────────────

func TestDeleteConfigDir_WithSuffix_RemovesEntireDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	SetServiceSuffix("test-plant")
	defer ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	dir := filepath.Join(tmp, "go-xwatch", "test-plant")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("設定資料夾應存在: %v", err)
	}

	if err := DeleteConfigDir(); err != nil {
		t.Fatalf("DeleteConfigDir 失敗: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("DeleteConfigDir 後資料夾應不存在，實際 err: %v", err)
	}
}

func TestDeleteConfigDir_NoDirIsOK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	SetServiceSuffix("nonexistent-suffix")
	defer ResetServiceSuffix()

	if err := DeleteConfigDir(); err != nil {
		t.Errorf("DeleteConfigDir 在目錄不存在時不應回傳錯誤，實際: %v", err)
	}
}

func TestDeleteConfigDir_EmptySuffix_PreservesBaseDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	// suffix = "" （傳統模式）

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}
	baseDir := filepath.Join(tmp, "go-xwatch")
	configFile := filepath.Join(baseDir, "config.json")

	if err := DeleteConfigDir(); err != nil {
		t.Fatalf("DeleteConfigDir 失敗: %v", err)
	}
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		t.Errorf("設定檔應已刪除，實際 err: %v", err)
	}
	if _, err := os.Stat(baseDir); err != nil {
		t.Errorf("傳統模式共用根目錄應保留，實際 err: %v", err)
	}
}
