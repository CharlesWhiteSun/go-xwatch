package app

import (
	"os"
	"path/filepath"
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
