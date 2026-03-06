package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setupEmptyDir 建立暫存目錄並設定 ProgramData 但不建立 config.json。
func setupEmptyDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	ResetServiceSuffix()
}

// ── ErrNotInitialized 與 Load() 相關測試 ──────────────────────────────────

// TestLoad_NoConfig_ReturnsErrNotInitialized 確認設定檔不存在時，
// Load() 回傳 ErrNotInitialized（而非原始 os.ErrNotExist）。
func TestLoad_NoConfig_ReturnsErrNotInitialized(t *testing.T) {
	setupEmptyDir(t)

	_, err := Load()
	if err == nil {
		t.Fatal("設定檔不存在時 Load() 應回傳錯誤，但回傳 nil")
	}
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("預期 ErrNotInitialized，實際得到：%v", err)
	}
}

// TestLoad_ExistingConfig_Succeeds 確認設定檔存在時 Load() 可正常讀取，
// 不會誤回傳 ErrNotInitialized。
func TestLoad_ExistingConfig_Succeeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗：%v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("設定檔存在時 Load() 不應回傳錯誤，got：%v", err)
	}
	if loaded.RootDir == "" {
		t.Fatal("Load() 回傳的 RootDir 不可為空")
	}
}

// TestLoad_NoConfig_NotWrappedRawOsError 確認 ErrNotInitialized
// 與 os.ErrNotExist 無法以 errors.Is 相互識別（兩者是不同的 sentinel）。
func TestLoad_NoConfig_NotWrappedRawOsError(t *testing.T) {
	setupEmptyDir(t)

	_, err := Load()
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("Load() 不應直接暴露 os.ErrNotExist，應以 ErrNotInitialized 替代")
	}
}

// ── IsInitialized() 相關測試 ─────────────────────────────────────────────

// TestIsInitialized_WhenNoConfig_ReturnsFalse 確認尚未初始化時回傳 false。
func TestIsInitialized_WhenNoConfig_ReturnsFalse(t *testing.T) {
	setupEmptyDir(t)

	if IsInitialized() {
		t.Fatal("尚無 config.json 時 IsInitialized() 應回傳 false")
	}
}

// TestIsInitialized_AfterSave_ReturnsTrue 確認初始化（Save）後回傳 true。
func TestIsInitialized_AfterSave_ReturnsTrue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗：%v", err)
	}

	if !IsInitialized() {
		t.Fatal("Save 後 IsInitialized() 應回傳 true")
	}
}

// TestIsInitialized_AfterDeleteConfig_ReturnsFalse 確認 DeleteConfig() 後回傳 false。
func TestIsInitialized_AfterDeleteConfig_ReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗：%v", err)
	}
	if err := DeleteConfig(); err != nil {
		t.Fatalf("DeleteConfig 失敗：%v", err)
	}

	if IsInitialized() {
		t.Fatal("DeleteConfig 後 IsInitialized() 應回傳 false")
	}
}

// TestIsInitialized_AfterDeleteConfig_LoadReturnsErrNotInitialized
// 確認 DeleteConfig 後 Load() 也回傳 ErrNotInitialized。
func TestIsInitialized_AfterDeleteConfig_LoadReturnsErrNotInitialized(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗：%v", err)
	}
	if err := DeleteConfig(); err != nil {
		t.Fatalf("DeleteConfig 失敗：%v", err)
	}

	_, err := Load()
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("DeleteConfig 後 Load() 應回傳 ErrNotInitialized，實際：%v", err)
	}
}

// ── 正常重啟流程（無 remove）確認測試 ─────────────────────────────────────

// TestNormalRestart_ConfigReadable 模擬「初始化後關閉、再重新開啟」的流程：
// 以相同的 suffix 儲存並重新載入設定，確認資料完整保留（不受 ErrNotInitialized 影響）。
func TestNormalRestart_ConfigReadable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	// 模擬 suffix 衍生自執行檔父目錄，例如 "plant-A"
	SetServiceSuffix("plant-A")
	defer ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	originalSettings := Settings{
		RootDir:          root,
		HeartbeatEnabled: true,
		ServiceName:      "GoXWatch-plant-A",
	}
	if err := Save(originalSettings); err != nil {
		t.Fatalf("儲存設定失敗：%v", err)
	}

	// 模擬程式關閉後重新設定 suffix（main.go deriveServiceContext 會重新呼叫）
	ResetServiceSuffix()
	SetServiceSuffix("plant-A")

	// 重新開啟後讀取設定
	loaded, err := Load()
	if err != nil {
		t.Fatalf("重啟後 Load() 失敗（不應回傳 ErrNotInitialized）：%v", err)
	}
	absRoot, _ := filepath.Abs(root)
	if loaded.RootDir != absRoot {
		t.Fatalf("RootDir 不符，got %q want %q", loaded.RootDir, absRoot)
	}
	if !loaded.HeartbeatEnabled {
		t.Fatal("HeartbeatEnabled 應保留為 true")
	}
	if loaded.ServiceName != "GoXWatch-plant-A" {
		t.Fatalf("ServiceName 不符，got %q", loaded.ServiceName)
	}
}

// TestNormalRestart_IsInitializedTrue 確認設定存在時，重啟後 IsInitialized() = true。
func TestNormalRestart_IsInitializedTrue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	SetServiceSuffix("site-B")
	defer ResetServiceSuffix()

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root}); err != nil {
		t.Fatalf("Save 失敗：%v", err)
	}

	// 模擬重啟（suffix 重新設定為相同值）
	ResetServiceSuffix()
	SetServiceSuffix("site-B")

	if !IsInitialized() {
		t.Fatal("設定存在的情況下，重啟後 IsInitialized() 應為 true")
	}
}
