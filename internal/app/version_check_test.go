package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
)

// ── checkVersionConsistency 單元測試 ─────────────────────────────────────

// TestCheckVersionConsistency_ServiceNotInstalled_ReturnsNil
// 確認服務未安裝時跳過檢查（靜默通過）。
func TestCheckVersionConsistency_ServiceNotInstalled_ReturnsNil(t *testing.T) {
	app := &cliApp{
		version:            "v1.0",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	if err := app.checkVersionConsistency(); err != nil {
		t.Errorf("服務未安裝時應回傳 nil，實際：%v", err)
	}
}

// TestCheckVersionConsistency_NoConfig_ReturnsNil
// 確認服務已安裝但設定檔不存在時跳過檢查（靜默通過）。
func TestCheckVersionConsistency_NoConfig_ReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	// 不建立 config 檔案
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.0",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	if err := app.checkVersionConsistency(); err != nil {
		t.Errorf("設定不存在時應回傳 nil，實際：%v", err)
	}
}

// TestCheckVersionConsistency_NoInstalledVersion_ReturnsNil
// 確認設定有但未記錄 InstalledVersion 時跳過檢查（向後相容舊安裝）。
func TestCheckVersionConsistency_NoInstalledVersion_ReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	// InstalledVersion 為空字串
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v2.0",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	if err := app.checkVersionConsistency(); err != nil {
		t.Errorf("InstalledVersion 為空時應回傳 nil，實際：%v", err)
	}
}

// TestCheckVersionConsistency_VersionMatch_ReturnsNil
// 確認版本一致時正常通過（回傳 nil）。
func TestCheckVersionConsistency_VersionMatch_ReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: "v1.4"}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.4",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	if err := app.checkVersionConsistency(); err != nil {
		t.Errorf("版本一致時應回傳 nil，實際：%v", err)
	}
}

// TestCheckVersionConsistency_VersionMismatch_ReturnsError
// 確認版本不一致時回傳非 nil 錯誤。
func TestCheckVersionConsistency_VersionMismatch_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: "v1.4"}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.5",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	err := app.checkVersionConsistency()
	if err == nil {
		t.Fatal("版本不一致時應回傳錯誤，但得到 nil")
	}
}

// TestCheckVersionConsistency_MismatchErrorContainsVersions
// 確認錯誤訊息同時包含目前版本與安裝版本，方便使用者判讀。
func TestCheckVersionConsistency_MismatchErrorContainsVersions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	const installed = "v1.4"
	const current = "v2.0-dev"
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: installed}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            current,
		serviceInstalledFn: func(_ string) bool { return true },
	}
	err := app.checkVersionConsistency()
	if err == nil {
		t.Fatal("版本不一致時應回傳錯誤，但得到 nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, installed) {
		t.Errorf("錯誤訊息應包含安裝版本 %q，實際：%q", installed, msg)
	}
	if !strings.Contains(msg, current) {
		t.Errorf("錯誤訊息應包含目前版本 %q，實際：%q", current, msg)
	}
}

// TestCheckVersionConsistency_WhitespaceVersionMatch
// 確認版本字串含空白時仍可正確比對（trim 行為驗證）。
func TestCheckVersionConsistency_WhitespaceVersionMatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: " v1.4 "}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            " v1.4 ",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	if err := app.checkVersionConsistency(); err != nil {
		t.Errorf("含空白的相同版本應回傳 nil，實際：%v", err)
	}
}

// ── initAndExit 寫入 InstalledVersion 測試 ───────────────────────────────

// TestInitAndExit_NoInstallService_PreservesExistingInstalledVersion
// 確認 initAndExit（不安裝服務）重新 init 時，不會清除既有的 InstalledVersion。
// 確保使用者調整 rootDir 後版本一致性檢查仍然有效。
func TestInitAndExit_NoInstallService_PreservesExistingInstalledVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "plant-V")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v2.0",
		serviceInstalledFn: func(_ string) bool { return true },
		serviceStatusFn:    func(_ string) (string, error) { return "running", nil },
	}

	// 第一次 init：建立設定檔於正確路徑（suffix 由 initAndExit 推導）。
	if err := app.initAndExit(rootDir, false); err != nil {
		t.Fatalf("initAndExit（首次）failed: %v", err)
	}

	// 手動在已建立的設定中寫入 InstalledVersion（模擬之前 --install-service 的痕跡）。
	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	s.InstalledVersion = "v1.4"
	if err := config.Save(s); err != nil {
		t.Fatalf("Save InstalledVersion failed: %v", err)
	}

	// 第二次 init（不安裝服務）：應保留 InstalledVersion。
	if err := app.initAndExit(rootDir, false); err != nil {
		t.Fatalf("initAndExit（第二次）failed: %v", err)
	}
	after, err := config.Load()
	if err != nil {
		t.Fatalf("Load after second init failed: %v", err)
	}
	if after.InstalledVersion != "v1.4" {
		t.Errorf("init（不安裝）不應修改 InstalledVersion，應為 v1.4，實際：%q", after.InstalledVersion)
	}
}

// TestInitAndExit_NoInstallService_DoesNotWriteInstalledVersion
// 確認 init（不安裝服務）不會寫入或修改 InstalledVersion。
func TestInitAndExit_NoInstallService_DoesNotWriteInstalledVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "factory-W")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v9.9",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	if err := app.initAndExit(rootDir, false); err != nil {
		t.Fatalf("initAndExit failed: %v", err)
	}
	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// init（不安裝）時 InstalledVersion 應為空
	if s.InstalledVersion != "" {
		t.Errorf("init（不安裝）InstalledVersion 應為空，實際：%q", s.InstalledVersion)
	}
}
