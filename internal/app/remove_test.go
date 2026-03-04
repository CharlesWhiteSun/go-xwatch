package app

import (
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

// TestDisableAllFeaturesOnRemove_DisablesHeartbeatMailAndFilecheck 確認呼叫後
// config 中的心跳、郵件排程與 filecheck 排程均已停用。
func TestDisableAllFeaturesOnRemove_DisablesHeartbeatMailAndFilecheck(t *testing.T) {
	setupRemoveTestConfig(t)

	ml := &mockLogger{}
	app := &cliApp{opsLogger: ml}

	if err := app.disableAllFeaturesOnRemove(); err != nil {
		t.Fatalf("disableAllFeaturesOnRemove failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.HeartbeatEnabled {
		t.Fatal("預期 HeartbeatEnabled=false，但仍為 true")
	}
	if loaded.Mail.IsEnabled() {
		t.Fatal("預期 Mail.IsEnabled()=false，但仍為 true")
	}
	if loaded.Filecheck.Enabled {
		t.Fatal("預期 Filecheck.Enabled=false，但仍為 true")
	}
	if loaded.Filecheck.Mail.IsEnabled() {
		t.Fatal("預期 Filecheck.Mail.IsEnabled()=false，但仍為 true")
	}
}

// TestDisableAllFeaturesOnRemove_LogsSteps 確認停用各功能時都有寫入 ops-log。
func TestDisableAllFeaturesOnRemove_LogsSteps(t *testing.T) {
	setupRemoveTestConfig(t)

	ml := &mockLogger{}
	app := &cliApp{opsLogger: ml}

	if err := app.disableAllFeaturesOnRemove(); err != nil {
		t.Fatalf("disableAllFeaturesOnRemove failed: %v", err)
	}

	if !ml.anyArgContains("心跳已停用") {
		t.Fatal("期望 ops-log 包含「心跳已停用」，但未找到")
	}
	if !ml.anyArgContains("郵件排程已停用") {
		t.Fatal("期望 ops-log 包含「郵件排程已停用」，但未找到")
	}
	if !ml.anyArgContains("filecheck 排程已停用") {
		t.Fatal("期望 ops-log 包含「filecheck 排程已停用」，但未找到")
	}
}

// TestDisableAllFeaturesOnRemove_NoConfigNoError 確認 config 檔不存在時
// disableAllFeaturesOnRemove 不報錯（新安裝未初始化的情境）。
func TestDisableAllFeaturesOnRemove_NoConfigNoError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	// 預建資料目錄確保 TempDir 清理不受 ACL 影響，但不建立 config.json
	if err := os.MkdirAll(filepath.Join(tmp, "go-xwatch"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	ml := &mockLogger{}
	app := &cliApp{opsLogger: ml}

	if err := app.disableAllFeaturesOnRemove(); err != nil {
		t.Fatalf("應容錯 config 不存在，但回傳：%v", err)
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

// TestFilecheckDisableOnRemove_PreviouslyEnabled 確認移除前 filecheck 為啟用狀態，
// 執行 disableAllFeaturesOnRemove 後確實停用。
func TestFilecheckDisableOnRemove_PreviouslyEnabled(t *testing.T) {
	setupRemoveTestConfig(t) // 設定中 Filecheck.Enabled=true

	// 先確認設定中 filecheck 確實是啟用的
	before, err := config.Load()
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}
	if !before.Filecheck.Enabled {
		t.Skip("setupRemoveTestConfig 沒有啟用 filecheck，跳過此測試")
	}

	ml := &mockLogger{}
	app := &cliApp{opsLogger: ml}
	if err := app.disableAllFeaturesOnRemove(); err != nil {
		t.Fatalf("disableAllFeaturesOnRemove: %v", err)
	}

	after, err := config.Load()
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if after.Filecheck.Enabled {
		t.Error("disableAllFeaturesOnRemove 後 Filecheck.Enabled 應為 false")
	}
	if after.Filecheck.Mail.IsEnabled() {
		t.Error("disableAllFeaturesOnRemove 後 Filecheck.Mail.IsEnabled() 應為 false")
	}
}
