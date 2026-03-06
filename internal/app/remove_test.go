package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go-xwatch/internal/config"
)

// mockLogger 收集 op log 訊息供測試驗證。
type mockLogger struct {
	mu   sync.Mutex
	msgs []string
	args [][]any
}

func (m *mockLogger) Info(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
	m.args = append(m.args, args)
}

func (m *mockLogger) hasMsg(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.msgs {
		if v == msg {
			return true
		}
	}
	return false
}

func (m *mockLogger) anyArgContains(sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, argSlice := range m.args {
		for _, a := range argSlice {
			if s, ok := a.(string); ok && strings.Contains(s, sub) {
				return true
			}
		}
	}
	return false
}

// setupRemoveTestConfig 建立包含啟用郵件、心跳與 filecheck 的 config 檔案供測試使用。
func setupRemoveTestConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	tr := true
	if err := config.Save(config.Settings{
		RootDir:           root,
		HeartbeatEnabled:  true,
		HeartbeatInterval: 60,
		Mail: config.MailSettings{
			Enabled: &tr,
			To:      []string{"r021@httc.com.tw"},
		},
		Filecheck: config.FilecheckSettings{
			Enabled: true,
			Mail: config.FilecheckMailSettings{
				Enabled: &tr,
				To:      []string{"admin@example.com"},
			},
		},
	}); err != nil {
		t.Fatalf("setupRemoveTestConfig: Save failed: %v", err)
	}
	return tmp
}

// TestBuildRemoveFeatures_ContainsExpectedCommands 確認 buildRemoveFeatures 回傳
// 所有預期的 CLI 指令項目（heartbeat / mail / filecheck / env / db / export）。
func TestBuildRemoveFeatures_ContainsExpectedCommands(t *testing.T) {
	wantCmds := []string{"heartbeat", "mail", "filecheck", "env", "db / export"}
	features := buildRemoveFeatures()
	got := make(map[string]bool, len(features))
	for _, f := range features {
		got[f.CmdName] = true
	}
	for _, cmd := range wantCmds {
		if !got[cmd] {
			t.Errorf("buildRemoveFeatures 應包含指令 %q，但未找到", cmd)
		}
	}
}

// TestBuildRemoveFeatures_DisableFeatures_HaveDisableFn 確認所有
// removeActionDisable 項目都有非 nil 的 Disable 函式。
func TestBuildRemoveFeatures_DisableFeatures_HaveDisableFn(t *testing.T) {
	for _, f := range buildRemoveFeatures() {
		if f.Action == removeActionDisable && f.Disable == nil {
			t.Errorf("removeFeature %q: removeActionDisable 必須有 Disable 函式", f.CmdName)
		}
	}
}

// TestBuildRemoveFeatures_NonDisableFeatures_HaveNote 確認非 removeActionDisable
// 項目（env、db / export）都有非空的 Note 說明。
func TestBuildRemoveFeatures_NonDisableFeatures_HaveNote(t *testing.T) {
	for _, f := range buildRemoveFeatures() {
		if f.Action != removeActionDisable && f.Note == "" {
			t.Errorf("removeFeature %q: action=%d 必須有 Note 說明", f.CmdName, f.Action)
		}
	}
}

// TestBuildRemoveFeatures_Disable_ActuallyModifiesSettings 針對每個
// removeActionDisable 項目，確認 Disable 函式確實修改了對應的 Settings 欄位。
func TestBuildRemoveFeatures_Disable_ActuallyModifiesSettings(t *testing.T) {
	tr := true
	for _, f := range buildRemoveFeatures() {
		if f.Action != removeActionDisable {
			continue
		}
		s := config.Settings{
			HeartbeatEnabled: true,
			Mail:             config.MailSettings{Enabled: &tr},
			Filecheck: config.FilecheckSettings{
				Enabled: true,
				Mail:    config.FilecheckMailSettings{Enabled: &tr},
			},
		}
		f.Disable(&s)
		switch f.CmdName {
		case "heartbeat":
			if s.HeartbeatEnabled {
				t.Errorf("heartbeat Disable 後 HeartbeatEnabled 應為 false")
			}
		case "mail":
			if s.Mail.IsEnabled() {
				t.Errorf("mail Disable 後 Mail.IsEnabled() 應為 false")
			}
		case "filecheck":
			if s.Filecheck.Enabled {
				t.Errorf("filecheck Disable 後 Filecheck.Enabled 應為 false")
			}
			if s.Filecheck.Mail.IsEnabled() {
				t.Errorf("filecheck Disable 後 Filecheck.Mail.IsEnabled() 應為 false")
			}
		}
	}
}

// TestFilecheckEnabledDefaultFalse 確認新建立的 config Filecheck.Enabled 預設為 false。
func TestFilecheckEnabledDefaultFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")

	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Filecheck.Enabled {
		t.Fatal("Filecheck.Enabled 預設應為 false，但實際為 true")
	}
	if loaded.Filecheck.Mail.IsEnabled() {
		t.Fatal("Filecheck.Mail.IsEnabled() 預設應為 false，但實際為 true")
	}
}

// ── stopAndUninstall 整合測試（XWATCH_SKIP_SERVICE_OPS=1 略過 SCM 呼叫）────────

// TestStopAndUninstall_WithConfig_DisablesAndDeletesConfig 執行完整 remove 流程，
// 確認設定檔已被刪除（IsInitialized = false）。
func TestStopAndUninstall_WithConfig_DisablesAndDeletesConfig(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}

	if config.IsInitialized() {
		t.Fatal("stopAndUninstall 後 IsInitialized() 應為 false")
	}
}

// TestStopAndUninstall_NoConfig_CompletesWithoutError 模擬重複執行 remove
// （設定檔已不存在）的情境，確認不 panic、不回傳錯誤。
func TestStopAndUninstall_NoConfig_CompletesWithoutError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall（無 config）失敗：%v", err)
	}
}

// TestStopAndUninstall_OpsLog_ContainsAllFeatureEntries 確認 ops-log 包含
// buildRemoveFeatures 中所有 CmdName 的相關記錄。
func TestStopAndUninstall_OpsLog_ContainsAllFeatureEntries(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}

	for _, f := range buildRemoveFeatures() {
		if !ml.anyArgContains(f.CmdName) {
			t.Errorf("ops-log 應包含功能 %q 的相關記錄，但未找到", f.CmdName)
		}
	}
}

// TestStopAndUninstall_NoConfig_OpsLog_ContainsSkipForAllDisableFeatures
// 確認 config 不存在時，ops-log 也有為 heartbeat/mail/filecheck 各寫入「略過」記錄。
func TestStopAndUninstall_NoConfig_OpsLog_ContainsSkipForAllDisableFeatures(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall（無 config）失敗：%v", err)
	}

	for _, f := range buildRemoveFeatures() {
		if f.Action == removeActionDisable && !ml.anyArgContains(f.CmdName) {
			t.Errorf("ops-log 應包含 %q 的略過記錄，但未找到", f.CmdName)
		}
	}
}

// ── remove 後設定刪除相關整合測試 ─────────────────────────────────────────────

// TestRemove_DisableThenDeleteConfig_IsInitializedFalse
// 執行完整 stopAndUninstall 流程後確認 config.IsInitialized() = false。
func TestRemove_DisableThenDeleteConfig_IsInitializedFalse(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}

	if config.IsInitialized() {
		t.Fatal("stopAndUninstall 後 IsInitialized() 應為 false")
	}
}

// TestRemove_DisableThenDeleteConfig_LoadReturnsErrNotInitialized
// 執行完整 remove 後確認 config.Load() 回傳 config.ErrNotInitialized，
// 讓 mail/filecheck/heartbeat status 等子指令顯示友善錯誤，而非原始 os 錯誤。
func TestRemove_DisableThenDeleteConfig_LoadReturnsErrNotInitialized(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}

	_, err := config.Load()
	if err == nil {
		t.Fatal("刪除設定後 Load() 應回傳錯誤")
	}
	if !isErrNotInitialized(err) {
		t.Fatalf("期得 config.ErrNotInitialized，實際：%v", err)
	}
}

// TestRemove_FreshExeAfterRemove_StatusCommandShowsNotInitialized
// 模擬「remove 後在同一資料夾放新 exe 並執行 status 相關子指令」的情境：
// config 已刪除，Load() 應回傳 ErrNotInitialized（顯示友善錯誤，不顯示舊設定）。
func TestRemove_FreshExeAfterRemove_StatusCommandShowsNotInitialized(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "test-svc", opsLogger: ml}

	// Step 1: remove 流程
	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}

	// Step 2: 模擬新 exe 啟動（suffix 重新設定，與舊 exe 相同）
	// suffix 由 main.go 的 deriveServiceContext 依 exe 父目錄決定，
	// 相同資料夾 → 相同 suffix → 相同 config 路徑
	// 此處 suffix = "" (測試環境不設 suffix，確認路徑一致性)

	// Step 3: 執行 status 相關指令 → 應取得 ErrNotInitialized
	_, loadErr := config.Load()
	if loadErr == nil {
		t.Fatal("新 exe 啟動後 Load() 應回傳錯誤（設定已刪除）")
	}
	if !isErrNotInitialized(loadErr) {
		t.Fatalf("期得 ErrNotInitialized，實際：%v", loadErr)
	}
}

// isErrNotInitialized 是輔助斷言，縮短測試中的 errors.Is 呼叫。
func isErrNotInitialized(err error) bool {
	return errors.Is(err, config.ErrNotInitialized)
}

// setupRemoveTestConfigWithSuffix 建立含服務後綴的測試環境。
func setupRemoveTestConfigWithSuffix(t *testing.T, suffix string) (tmp string) {
	t.Helper()
	tmp = t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	config.SetServiceSuffix(suffix)
	t.Cleanup(config.ResetServiceSuffix)
	root := filepath.Join(tmp, "root")
	tr := true
	if err := config.Save(config.Settings{
		RootDir:          root,
		HeartbeatEnabled: true,
		Mail:             config.MailSettings{Enabled: &tr},
	}); err != nil {
		t.Fatalf("setupRemoveTestConfigWithSuffix: Save failed: %v", err)
	}
	return tmp
}

// TestStopAndUninstall_WithSuffix_RemovesConfigDir 確認有服務後綴時，
// stopAndUninstall 會移除整個設定資料夾。
func TestStopAndUninstall_WithSuffix_RemovesConfigDir(t *testing.T) {
	suffix := "test-plant"
	tmp := setupRemoveTestConfigWithSuffix(t, suffix)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch-test-plant", opsLogger: ml}

	dir := filepath.Join(tmp, "go-xwatch", suffix)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("測試前資料夾應存在：%v", err)
	}
	if err := app.stopAndUninstall(); err != nil {
		t.Fatalf("stopAndUninstall 失敗：%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("stopAndUninstall 後設定資料夾應已移除，實際 err：%v", err)
	}
}
