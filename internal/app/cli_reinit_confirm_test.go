package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
	svc "go-xwatch/internal/service"
)

// ── askYesNoDefaultNo 單元測試 ────────────────────────────────────────

// TestAskYesNoDefaultNo_NoPause_ReturnsFalse 確認 XWATCH_NO_PAUSE=1 時自動回傳 false。
func TestAskYesNoDefaultNo_NoPause_ReturnsFalse(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	if askYesNoDefaultNo("任何提示 (N/y): ") {
		t.Error("XWATCH_NO_PAUSE=1 時 askYesNoDefaultNo 應回傳 false（預設不覆蓋）")
	}
}

// TestAskYesNoDefaultNo_vs_AskYesNo_DifferentDefault 確認預設值與 askYesNo 相反。
// askYesNo 在 XWATCH_NO_PAUSE=1 時回傳 true；askYesNoDefaultNo 應回傳 false。
func TestAskYesNoDefaultNo_vs_AskYesNo_DifferentDefault(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	if !askYesNo("提示 (Y/n): ") {
		t.Error("askYesNo 在 XWATCH_NO_PAUSE=1 應回傳 true")
	}
	if askYesNoDefaultNo("提示 (N/y): ") {
		t.Error("askYesNoDefaultNo 在 XWATCH_NO_PAUSE=1 應回傳 false")
	}
}

// ── confirmReinstall 單元測試 ─────────────────────────────────────────

// TestConfirmReinstall_UserConfirms_ReturnsTrue 確認使用者選 Y 時回傳 (true, nil)。
func TestConfirmReinstall_UserConfirms_ReturnsTrue(t *testing.T) {
	app := &cliApp{
		confirmOverwriteFn:  func(_ string) bool { return true },
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("not found") },
	}

	proceed, err := app.confirmReinstall("GoXWatch-test")
	if err != nil {
		t.Fatalf("confirmReinstall unexpected error: %v", err)
	}
	if !proceed {
		t.Error("使用者選 Y 時 proceed 應為 true")
	}
}

// TestConfirmReinstall_UserDeclines_ReturnsFalse 確認使用者選 N 時回傳 (false, nil)。
func TestConfirmReinstall_UserDeclines_ReturnsFalse(t *testing.T) {
	app := &cliApp{
		confirmOverwriteFn:  func(_ string) bool { return false },
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("not found") },
	}

	proceed, err := app.confirmReinstall("GoXWatch-test")
	if err != nil {
		t.Fatalf("confirmReinstall unexpected error: %v", err)
	}
	if proceed {
		t.Error("使用者選 N 時 proceed 應為 false")
	}
}

// TestConfirmReinstall_ExePathMismatch_PrintsWarning 確認執行檔路徑不同時，
// 會向 stderr 輸出包含「警告」與原始路徑的提示文字。
func TestConfirmReinstall_ExePathMismatch_PrintsWarning(t *testing.T) {
	// 使用一個確定與測試執行檔路徑不同的假路徑
	fakeRegisteredExe := `C:\totally\different\xwatch.exe`

	app := &cliApp{
		confirmOverwriteFn:  func(_ string) bool { return false },
		registeredExePathFn: func(_ string) (string, error) { return fakeRegisteredExe, nil },
	}

	// 攔截 stderr 輸出
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, _ = app.confirmReinstall("GoXWatch-test")

	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "警告") {
		t.Errorf("路徑不同時 stderr 應包含「警告」，實際輸出：%q", output)
	}
	if !strings.Contains(output, fakeRegisteredExe) {
		t.Errorf("警告應包含原登錄路徑 %q，實際輸出：%q", fakeRegisteredExe, output)
	}
}

// TestConfirmReinstall_SameExePath_NoWarning 確認執行檔路徑相同時，不顯示警告。
func TestConfirmReinstall_SameExePath_NoWarning(t *testing.T) {
	// 取得測試二進位本身的路徑
	currentExe, err := os.Executable()
	if err != nil {
		t.Skip("無法取得 os.Executable，跳過此測試")
	}
	currentExe, _ = filepath.Abs(currentExe)

	app := &cliApp{
		confirmOverwriteFn:  func(_ string) bool { return false },
		registeredExePathFn: func(_ string) (string, error) { return currentExe, nil },
	}

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, _ = app.confirmReinstall("GoXWatch-test")

	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if strings.Contains(output, "警告") {
		t.Errorf("相同路徑不應出現警告，實際輸出：%q", output)
	}
}

// TestConfirmReinstall_PromptContainsServiceName 確認提示文字包含服務名稱。
func TestConfirmReinstall_PromptContainsServiceName(t *testing.T) {
	var capturedPrompt string
	app := &cliApp{
		confirmOverwriteFn: func(p string) bool {
			capturedPrompt = p
			return false
		},
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("not found") },
	}

	_, _ = app.confirmReinstall("GoXWatch-plant-X")

	if !strings.Contains(capturedPrompt, "GoXWatch-plant-X") {
		t.Errorf("確認提示應包含服務名稱，實際：%q", capturedPrompt)
	}
}

// ── initAndExit 重複服務＋確認流程 整合測試 ───────────────────────────

// setupPreExistingService 在暫存 ProgramData 建立一個已存在的服務設定檔，
// 使 FindServiceForRoot 能夠偵測到它。
func setupPreExistingService(t *testing.T, tmp, rootDir string) {
	t.Helper()
	suffix := svc.ServiceSuffixFromRoot(rootDir)
	svcName := svc.ServiceNameFromRoot(rootDir)
	dataDir := filepath.Join(tmp, "go-xwatch", suffix)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgData, _ := json.Marshal(map[string]any{
		"rootDir":     rootDir,
		"serviceName": svcName,
	})
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), cfgData, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestInitAndExit_Reinstall_UserDeclines_GracefulExit 確認使用者拒絕覆蓋時，
// initAndExit 回傳 nil（優雅退出），且不重寫設定檔。
func TestInitAndExit_Reinstall_UserDeclines_GracefulExit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	root := filepath.Join(tmp, "factory-X")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// 建立「已存在的服務」設定（供 FindServiceForRoot 偵測）
	setupPreExistingService(t, tmp, root)

	app := &cliApp{
		serviceName:         "GoXWatch",
		confirmOverwriteFn:  func(_ string) bool { return false },
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("SCM skipped") },
	}

	err := app.initAndExit(root, true)
	if err != nil {
		t.Errorf("使用者拒絕覆蓋時應回傳 nil，實際：%v", err)
	}
}

// TestInitAndExit_Reinstall_UserDeclines_PrintsFriendlyMessage 確認拒絕時，
// stdout 包含友善的「不覆蓋」說明文字。
func TestInitAndExit_Reinstall_UserDeclines_PrintsFriendlyMessage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	root := filepath.Join(tmp, "factory-Y")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	setupPreExistingService(t, tmp, root)

	// 攔截 stdout 確認友善訊息
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cliApp{
		serviceName:         "GoXWatch",
		confirmOverwriteFn:  func(_ string) bool { return false },
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("SCM skipped") },
	}
	_ = app.initAndExit(root, true)

	w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "不覆蓋") {
		t.Errorf("拒絕時應輸出「不覆蓋」說明，實際：%q", output)
	}
}

// TestInitAndExit_Reinstall_UserConfirms_WriteConfig 確認使用者同意覆蓋時，
// 設定檔會被正確更新（InstallOrUpdate 透過 XWATCH_SKIP_SERVICE_OPS 跳過）。
// 因 installService=false 不觸發服務安裝，可安全驗證設定寫入邏輯。
func TestInitAndExit_Reinstall_UserConfirms_WriteConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	root := filepath.Join(tmp, "factory-Z")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// 使用者確認覆蓋，但不安裝服務（installService=false），純粹測試設定寫入
	app := &cliApp{
		serviceName:         "GoXWatch",
		confirmOverwriteFn:  func(_ string) bool { return true },
		registeredExePathFn: func(_ string) (string, error) { return "", errors.New("SCM skipped") },
	}

	if err := app.initAndExit(root, false); err != nil {
		t.Fatalf("initAndExit: %v", err)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.RootDir != root {
		t.Errorf("RootDir = %q, want %q", saved.RootDir, root)
	}
	if saved.ServiceName != svc.ServiceNameFromRoot(root) {
		t.Errorf("ServiceName = %q, want %q", saved.ServiceName, svc.ServiceNameFromRoot(root))
	}
}

// TestInitAndExit_Reinstall_ExePathConflict_UserDeclines 完整情境：
// 同資料夾內 XWatch-B.exe 嘗試覆蓋 GoXWatch-A 設定，使用者拒絕。
func TestInitAndExit_Reinstall_ExePathConflict_UserDeclines(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	root := filepath.Join(tmp, "plant-A")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	setupPreExistingService(t, tmp, root)

	// 模擬「登錄的是 XWatch.exe，但現在執行的是 XWatch-B.exe」
	fakeOldExe := filepath.Join(root, "XWatch.exe")

	var capturedStderr string
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	app := &cliApp{
		serviceName: "GoXWatch",
		// 注入：登錄路徑 = 舊的 XWatch.exe（與 os.Executable() 不同）
		registeredExePathFn: func(_ string) (string, error) { return fakeOldExe, nil },
		confirmOverwriteFn:  func(_ string) bool { return false },
	}
	err := app.initAndExit(root, true)

	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	capturedStderr = buf.String()

	// 無論使用者選 N，皆應優雅退出
	if err != nil {
		t.Errorf("優雅退出時應回傳 nil，實際：%v", err)
	}
	// 確認有印出警告（登錄路徑 ≠ 當前路徑）
	if !strings.Contains(capturedStderr, "警告") {
		t.Errorf("執行檔路徑不同時應輸出警告，stderr：%q", capturedStderr)
	}
	if !strings.Contains(capturedStderr, fakeOldExe) {
		t.Errorf("警告應包含登錄的執行檔路徑，stderr：%q", capturedStderr)
	}
}
