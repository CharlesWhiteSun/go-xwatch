package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
)

// ── checkVersionConsistency 單元測試 ─────────────────────────────────────

// TestCheckVersionConsistency_ServiceNotInstalled_ReturnsMatch
// 確認服務未安裝時跳過檢查（回傳 VersionMatch）。
func TestCheckVersionConsistency_ServiceNotInstalled_ReturnsMatch(t *testing.T) {
	app := &cliApp{
		version:            "v1.0",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMatch {
		t.Errorf("服務未安裝時應回傳 VersionMatch，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_NoConfig_ReturnsMatch
// 確認服務已安裝但設定檔不存在時跳過檢查。
func TestCheckVersionConsistency_NoConfig_ReturnsMatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.0",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMatch {
		t.Errorf("設定不存在時應回傳 VersionMatch，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_NoInstalledVersion_ReturnsMatch
// 確認設定有但未記錄 InstalledVersion 時跳過檢查（向後相容舊安裝）。
func TestCheckVersionConsistency_NoInstalledVersion_ReturnsMatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v2.0",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMatch {
		t.Errorf("InstalledVersion 為空時應回傳 VersionMatch，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_VersionMatch
// 確認版本一致時正常通過（回傳 VersionMatch）。
func TestCheckVersionConsistency_VersionMatch(t *testing.T) {
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
	result := app.checkVersionConsistency()
	if result.Kind != VersionMatch {
		t.Errorf("版本一致時應回傳 VersionMatch，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_VersionMismatch_ReturnsNonMatch
// 確認版本不一致時回傳 Kind != VersionMatch。
func TestCheckVersionConsistency_VersionMismatch_ReturnsNonMatch(t *testing.T) {
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
	result := app.checkVersionConsistency()
	if result.Kind == VersionMatch {
		t.Fatal("版本不一致時應回傳 non-VersionMatch，但實際為 VersionMatch")
	}
}

// TestCheckVersionConsistency_MismatchPopulatesVersionFields
// 確認版本不一致時，Result.Current 和 Result.Installed 正確填充。
func TestCheckVersionConsistency_MismatchPopulatesVersionFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	const installed = "v1.4"
	const current = "v2.0"
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: installed}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            current,
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind == VersionMatch {
		t.Fatal("版本不一致時應回傳 non-VersionMatch")
	}
	if result.Current != current {
		t.Errorf("Result.Current 應為 %q，實際：%q", current, result.Current)
	}
	if result.Installed != installed {
		t.Errorf("Result.Installed 應為 %q，實際：%q", installed, result.Installed)
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
	result := app.checkVersionConsistency()
	if result.Kind != VersionMatch {
		t.Errorf("含空白的相同版本應回傳 VersionMatch，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_CurrentNewer_ReturnsNewerKind
// 確認目前執行檔版本高於已安裝版本時，回傳 VersionMismatchCurrentNewer。
func TestCheckVersionConsistency_CurrentNewer_ReturnsNewerKind(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	// 安裝版本是舊版，目前執行檔是新版
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: "v1.4-46-gd57936b"}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.4-48-g519be99",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMismatchCurrentNewer {
		t.Errorf("目前版本較高時應回傳 VersionMismatchCurrentNewer，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_CurrentOlder_ReturnsOlderKind
// 確認目前執行檔版本低於已安裝版本時，回傳 VersionMismatchCurrentOlder。
func TestCheckVersionConsistency_CurrentOlder_ReturnsOlderKind(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	// 安裝版本是新版，目前執行檔是舊版
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: "v1.4-48-g519be99"}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.4-46-gd57936b",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMismatchCurrentOlder {
		t.Errorf("目前版本較舊時應回傳 VersionMismatchCurrentOlder，實際 Kind=%d", result.Kind)
	}
}

// TestCheckVersionConsistency_GitDescribeRealScenario
// 確認使用者報告的實際場景：v1.4-48 啟動 v1.4-46 安裝的服務 → VersionMismatchCurrentNewer。
func TestCheckVersionConsistency_GitDescribeRealScenario(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "factory")
	if err := config.Save(config.Settings{RootDir: root, InstalledVersion: "v1.4-46-gd57936b"}); err != nil {
		t.Fatal(err)
	}
	app := &cliApp{
		serviceName:        "GoXWatch",
		version:            "v1.4-48-g519be99",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	result := app.checkVersionConsistency()
	if result.Kind != VersionMismatchCurrentNewer {
		t.Errorf("實際場景應為 VersionMismatchCurrentNewer (commits 48>46)，實際 Kind=%d", result.Kind)
	}
	if result.Current != "v1.4-48-g519be99" {
		t.Errorf("Current 應為 v1.4-48-g519be99，實際：%q", result.Current)
	}
	if result.Installed != "v1.4-46-gd57936b" {
		t.Errorf("Installed 應為 v1.4-46-gd57936b，實際：%q", result.Installed)
	}
	if result.RootDir == "" {
		t.Error("RootDir 不應為空（應從設定讀取）")
	}
}

// ── handleVersionMismatch 測試 ────────────────────────────────────────────

// captureStderr 攔截 os.Stderr，執行 fn，回傳截取到的輸出。
func captureStderr(fn func()) string {
	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestHandleVersionMismatch_OlderVersion_ReturnsError
// 確認目前版本較舊時 handleVersionMismatch 回傳 error（不自動退出，等待使用者）。
func TestHandleVersionMismatch_OlderVersion_ReturnsError(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	app := &cliApp{}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentOlder,
		Current:   "v1.0",
		Installed: "v2.0",
	}
	if err := app.handleVersionMismatch(result); err == nil {
		t.Error("舊版執行檔應回傳 error，但得到 nil")
	}
}

// TestHandleVersionMismatch_OlderVersion_PrintsWarning
// 確認舊版執行檔時，stderr 含對應的警告訊息與版本號。
func TestHandleVersionMismatch_OlderVersion_PrintsWarning(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	app := &cliApp{}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentOlder,
		Current:   "v1.0",
		Installed: "v2.0",
	}
	output := captureStderr(func() {
		_ = app.handleVersionMismatch(result)
	})
	if !strings.Contains(output, "v1.0") {
		t.Errorf("警告內容應含目前版本 v1.0，實際：%q", output)
	}
	if !strings.Contains(output, "v2.0") {
		t.Errorf("警告內容應含安裝版本 v2.0，實際：%q", output)
	}
	if !strings.Contains(output, "降版") || !strings.Contains(output, "警告") {
		t.Errorf("警告內容應含「降版」與「警告」字樣，實際：%q", output)
	}
}

// TestHandleVersionMismatch_NewerVersion_DeclineUpgrade_ReturnsError
// 確認高版執行檔且使用者拒絕升級時，回傳 error。
func TestHandleVersionMismatch_NewerVersion_DeclineUpgrade_ReturnsError(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	app := &cliApp{
		confirmUpgradeFn: func(_ string) bool { return false },
	}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
		RootDir:   "/some/root",
	}
	if err := app.handleVersionMismatch(result); err == nil {
		t.Error("使用者拒絕升級時應回傳 error，但得到 nil")
	}
}

// TestHandleVersionMismatch_NewerVersion_DeclineUpgrade_PrintsHint
// 確認拒絕升級時，stderr 含版本不一致警告標頭及版本號。
// （取消升級後改以互動式「退出/返回」替代舊版靜態 remove 文字提示）
func TestHandleVersionMismatch_NewerVersion_DeclineUpgrade_PrintsHint(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	app := &cliApp{
		confirmUpgradeFn: func(_ string) bool { return false },
	}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
	}
	output := captureStderr(func() {
		_ = app.handleVersionMismatch(result)
	})
	if !strings.Contains(output, "版本不一致") {
		t.Errorf("拒絕升級內容應含「版本不一致」字樣，實際：%q", output)
	}
	if !strings.Contains(output, "v2.0") {
		t.Errorf("拒絕升級內容應含目前版本 v2.0，實際：%q", output)
	}
	if !strings.Contains(output, "v1.0") {
		t.Errorf("拒絕升級內容應含安裝版本 v1.0，實際：%q", output)
	}
}

// ── handleVersionMismatch 退出/返回迴圈測試 ─────────────────────────────

// TestHandleVersionMismatch_AfterDecline_PromptContainsExitOrRetry
// 確認取消升級後，afterDeclineFn 收到的提示文字包含「退出」與「返回」關鍵字。
func TestHandleVersionMismatch_AfterDecline_PromptContainsExitOrRetry(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	var capturedPrompt string
	app := &cliApp{
		confirmUpgradeFn: func(_ string) bool { return false },
		afterDeclineFn: func(prompt string) bool {
			capturedPrompt = prompt
			return false // 選擇退出
		},
	}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
	}
	_ = app.handleVersionMismatch(result)
	if !strings.Contains(capturedPrompt, "退出") {
		t.Errorf("取消升級後的提示應含「退出」字樣，實際：%q", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "返回") {
		t.Errorf("取消升級後的提示應含「返回」字樣，實際：%q", capturedPrompt)
	}
}

// TestHandleVersionMismatch_NewerVersion_DeclineRetryThenExit
// 確認使用者拒絕升級 → 選擇返回 → 再次拒絕 → 選擇退出，回傳 error，且兩輪皆被呼叫。
func TestHandleVersionMismatch_NewerVersion_DeclineRetryThenExit(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	upgradeCallCount := 0
	retryCallCount := 0
	app := &cliApp{
		confirmUpgradeFn: func(_ string) bool {
			upgradeCallCount++
			return false // 每次都拒絕
		},
		afterDeclineFn: func(_ string) bool {
			retryCallCount++
			return retryCallCount < 2 // 第一次返回重試，第二次選擇退出
		},
	}
	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
	}
	if err := app.handleVersionMismatch(result); err == nil {
		t.Error("最終選擇退出時應回傳 error，但得到 nil")
	}
	if upgradeCallCount != 2 {
		t.Errorf("confirmUpgradeFn 應被呼叫 2 次（兩輪拒絕），實際：%d", upgradeCallCount)
	}
	if retryCallCount != 2 {
		t.Errorf("afterDeclineFn 應被呼叫 2 次（一返回一退出），實際：%d", retryCallCount)
	}
}

// TestHandleVersionMismatch_NewerVersion_DeclineRetryThenConfirm
// 確認使用者拒絕升級 → 選擇返回 → 第二次確認升級，流程成功。
// 使用 XWATCH_SKIP_SERVICE_OPS=1 略過 Windows SCM 呼叫。
func TestHandleVersionMismatch_NewerVersion_DeclineRetryThenConfirm(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")
	t.Setenv("XWATCH_NO_PAUSE", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "plant-R")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config.SetServiceSuffix("plant-R")
	if err := config.Save(config.Settings{
		RootDir:          rootDir,
		InstalledVersion: "v1.0",
		ServiceName:      "GoXWatch-plant-R",
	}); err != nil {
		t.Fatal(err)
	}

	upgradeCallCount := 0
	app := &cliApp{
		serviceName: "GoXWatch-plant-R",
		version:     "v2.0",
		confirmUpgradeFn: func(_ string) bool {
			upgradeCallCount++
			return upgradeCallCount >= 2 // 第一次拒絕，第二次確認
		},
		afterDeclineFn:     func(_ string) bool { return true }, // 選擇返回
		serviceInstalledFn: func(_ string) bool { return false },
		serviceStatusFn:    func(_ string) (string, error) { return "", nil },
	}

	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
		RootDir:   rootDir,
	}
	if err := app.handleVersionMismatch(result); err != nil {
		t.Fatalf("重試後確認升級應成功，但得到錯誤：%v", err)
	}
	if upgradeCallCount != 2 {
		t.Errorf("confirmUpgradeFn 應被呼叫 2 次（一拒絕一確認），實際：%d", upgradeCallCount)
	}

	// 驗證升級後版本已更新至設定檔
	s, err := config.Load()
	if err != nil {
		t.Fatalf("升級後 Load 失敗：%v", err)
	}
	if s.InstalledVersion != "v2.0" {
		t.Errorf("升級後 InstalledVersion 應為 v2.0，實際：%q", s.InstalledVersion)
	}
}

// TestAskExitOrRetry_NoPause_ReturnsFalse
// 確認 XWATCH_NO_PAUSE=1 時 askExitOrRetry 直接回傳 false（預設退出）。
func TestAskExitOrRetry_NoPause_ReturnsFalse(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	if askExitOrRetry("任何提示 (E/r): ") {
		t.Error("XWATCH_NO_PAUSE=1 時 askExitOrRetry 應回傳 false（預設退出）")
	}
}

// TestHandleVersionMismatch_NewerVersion_ConfirmUpgrade_Success
// 確認高版執行檔且使用者確認升級時，完整執行 remove → init --install-service 流程並成功。
func TestHandleVersionMismatch_NewerVersion_ConfirmUpgrade_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")
	t.Setenv("XWATCH_NO_PAUSE", "1")
	defer config.ResetServiceSuffix()

	// 建立模擬「舊版服務」的目錄和設定
	rootDir := filepath.Join(tmp, "plant-U")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config.SetServiceSuffix("plant-U")
	if err := config.Save(config.Settings{
		RootDir:          rootDir,
		InstalledVersion: "v1.0",
		ServiceName:      "GoXWatch-plant-U",
	}); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch-plant-U",
		version:            "v2.0",
		confirmUpgradeFn:   func(_ string) bool { return true },
		serviceInstalledFn: func(_ string) bool { return false }, // 移除後服務已不存在
		serviceStatusFn:    func(_ string) (string, error) { return "", nil },
	}

	result := VersionCheckResult{
		Kind:      VersionMismatchCurrentNewer,
		Current:   "v2.0",
		Installed: "v1.0",
		RootDir:   rootDir,
	}
	if err := app.handleVersionMismatch(result); err != nil {
		t.Fatalf("升級流程應成功，但得到錯誤：%v", err)
	}

	// 驗證新版本已寫入設定
	s, err := config.Load()
	if err != nil {
		t.Fatalf("升級後 Load 失敗：%v", err)
	}
	if s.InstalledVersion != "v2.0" {
		t.Errorf("升級後 InstalledVersion 應為 v2.0，實際：%q", s.InstalledVersion)
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
