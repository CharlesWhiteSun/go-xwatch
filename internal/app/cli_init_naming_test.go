package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
	svc "go-xwatch/internal/service"
)

// TestInitAndExit_SetsSuffixAndServiceName 驗證 initAndExit（不安裝服務）
// 會正確設定 config suffix 並將 ServiceName 寫入設定檔。
func TestInitAndExit_SetsSuffixAndServiceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	// 不安裝服務，避免呼叫 Windows SCM
	defer config.ResetServiceSuffix()

	// 建立監控根目錄為有意義名稱
	rootDir := filepath.Join(tmp, "factory-C")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName: "GoXWatch", // 初始值，應被 initAndExit 覆寫
		opsLogger:   nil,
	}
	if err := app.initAndExit(rootDir, false); err != nil {
		t.Fatalf("initAndExit failed: %v", err)
	}

	// cliApp 的 serviceName 應已更新
	expectedName := svc.ServiceNameFromRoot(rootDir)
	if app.serviceName != expectedName {
		t.Errorf("serviceName = %q, want %q", app.serviceName, expectedName)
	}

	// config suffix 應已設定
	expectedSuffix := svc.ServiceSuffixFromRoot(rootDir)
	if got := config.GetServiceSuffix(); got != expectedSuffix {
		t.Errorf("GetServiceSuffix() = %q, want %q", got, expectedSuffix)
	}

	// 設定檔中的 ServiceName 應正確
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if saved.ServiceName != expectedName {
		t.Errorf("saved ServiceName = %q, want %q", saved.ServiceName, expectedName)
	}
}

// TestInitAndExit_SetsRootDir 確認 initAndExit 正確寫入 RootDir。
func TestInitAndExit_SetsRootDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "plant-D")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{serviceName: "GoXWatch"}
	if err := app.initAndExit(rootDir, false); err != nil {
		t.Fatalf("initAndExit: %v", err)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.RootDir != rootDir {
		t.Errorf("RootDir = %q, want %q", saved.RootDir, rootDir)
	}
}

// ── initAndExit 訊息感知測試 ─────────────────────────────────────────

// captureStdout 攔截 os.Stdout，執行 fn，回傳截取到的輸出。
func captureStdout(fn func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestInitAndExit_ServiceAlreadyInstalled_Stopped_ShowsStartHint
// 確認服務已存在但停止時，init（不安裝）顯示友善的「可執行 start 重新啟動」提示。
func TestInitAndExit_ServiceAlreadyInstalled_Stopped_ShowsStartHint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "plant-X")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
		serviceStatusFn:    func(_ string) (string, error) { return "stopped", nil },
	}

	var err error
	output := captureStdout(func() {
		err = app.initAndExit(rootDir, false)
	})
	if err != nil {
		t.Fatalf("initAndExit failed: %v", err)
	}
	if strings.Contains(output, "服務尚未安裝") {
		t.Errorf("不應顯示「服務尚未安裝」，實際：%q", output)
	}
	if !strings.Contains(output, "start") {
		t.Errorf("應包含 start 提示，實際：%q", output)
	}
}

// TestInitAndExit_ServiceAlreadyInstalled_Running_ShowsRunningMessage
// 確認服務已在執行中時，init（不安裝）顯示「執行中」訊息而非「需要安裝」。
func TestInitAndExit_ServiceAlreadyInstalled_Running_ShowsRunningMessage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "factory-Y")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
		serviceStatusFn:    func(_ string) (string, error) { return "running", nil },
	}

	var err error
	output := captureStdout(func() {
		err = app.initAndExit(rootDir, false)
	})
	if err != nil {
		t.Fatalf("initAndExit failed: %v", err)
	}
	if strings.Contains(output, "服務尚未安裝") {
		t.Errorf("不應顯示「服務尚未安裝」，實際：%q", output)
	}
	if !strings.Contains(output, "執行中") {
		t.Errorf("應顯示「執行中」訊息，實際：%q", output)
	}
}

// TestInitAndExit_ServiceNotInstalled_ShowsInstallHint
// 確認服務完全未安裝時，init 顯示需改用 --install-service 的提示。
func TestInitAndExit_ServiceNotInstalled_ShowsInstallHint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_NO_PAUSE", "1")
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer config.ResetServiceSuffix()

	rootDir := filepath.Join(tmp, "factory-Z")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	var err error
	output := captureStdout(func() {
		err = app.initAndExit(rootDir, false)
	})
	if err != nil {
		t.Fatalf("initAndExit failed: %v", err)
	}
	if !strings.Contains(output, "--install-service") {
		t.Errorf("未安裝服務時應顯示 --install-service 提示，實際：%q", output)
	}
	if !strings.Contains(output, "服務尚未安裝") {
		t.Errorf("未安裝服務時應顯示「服務尚未安裝」，實際：%q", output)
	}
}
