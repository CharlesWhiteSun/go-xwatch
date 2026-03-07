package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
	"go-xwatch/internal/service"
)

// ── printStatus 停止狀態提示測試 ─────────────────────────────────────

// TestPrintStatus_ServiceStopped_ShowsStartHint 確認服務已停止時，
// printStatus 輸出包含可執行 start 重新啟動的友善提示訊息。
func TestPrintStatus_ServiceStopped_ShowsStartHint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	app := &cliApp{
		serviceName:     "GoXWatch-test",
		serviceStatusFn: func(_ string) (string, error) { return "stopped", nil },
	}

	var err error
	output := captureStdout(func() {
		err = app.printStatus()
	})
	if err != nil {
		t.Fatalf("printStatus failed: %v", err)
	}
	if !strings.Contains(output, "start") {
		t.Errorf("停止狀態時應顯示 start 提示，實際：%q", output)
	}
}

// TestPrintStatus_ServiceRunning_NoStartHint 確認服務執行中時，
// printStatus 不顯示 start 提示（服務運作正常，不需要額外引導）。
func TestPrintStatus_ServiceRunning_NoStartHint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	app := &cliApp{
		serviceName:     "GoXWatch-test",
		serviceStatusFn: func(_ string) (string, error) { return "running", nil },
	}

	var err error
	output := captureStdout(func() {
		err = app.printStatus()
	})
	if err != nil {
		t.Fatalf("printStatus failed: %v", err)
	}
	if strings.Contains(output, "可執行 `start`") {
		t.Errorf("執行中狀態時不應顯示 start 提示，實際：%q", output)
	}
}

// TestPrintStatus_ServiceStopped_ShowsStatusStopped 確認 status 欄位確實顯示 stopped。
func TestPrintStatus_ServiceStopped_ShowsStatusStopped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	app := &cliApp{
		serviceName:     "GoXWatch-test",
		serviceStatusFn: func(_ string) (string, error) { return "stopped", nil },
	}

	var err error
	output := captureStdout(func() {
		err = app.printStatus()
	})
	if err != nil {
		t.Fatalf("printStatus failed: %v", err)
	}
	if !strings.Contains(output, "stopped") {
		t.Errorf("應顯示 stopped 狀態，實際：%q", output)
	}
}

// TestPrintStatus_ServiceMissing_ReturnsError 確認服務不存在時回傳錯誤，
// 確保注入機制正確將錯誤傳遞回呼叫端。
func TestPrintStatus_ServiceMissing_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	app := &cliApp{
		serviceName: "GoXWatch-test",
		serviceStatusFn: func(_ string) (string, error) {
			return "", os.ErrNotExist // 模擬服務不存在
		},
	}

	origStderr := os.Stderr
	_, wErr, _ := os.Pipe()
	os.Stderr = wErr
	defer func() {
		wErr.Close()
		os.Stderr = origStderr
	}()

	err := app.printStatus()
	if err == nil {
		t.Error("服務不存在時 printStatus 應回傳錯誤，但得到 nil")
	}
}

// ── 資料目錄隔離測試 ─────────────────────────────────────────────────

// TestPrintStatus_UsesServiceSuffixDataDir 確認 printStatus 使用含後綴的資料目錄，
// 而非基底目錄（%ProgramData%\go-xwatch）。
func TestPrintStatus_UsesServiceSuffixDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	const svcName = "GoXWatch-abc123"
	expectedSuffix := service.SuffixFromServiceName(svcName) // "abc123"

	app := &cliApp{
		serviceName:     svcName,
		serviceStatusFn: func(_ string) (string, error) { return "stopped", nil },
	}

	captureStdout(func() { _ = app.printStatus() })

	// key.bin 與 journal.db 應出現在後綴子目錄，不得出現在基底目錄。
	baseDir := filepath.Join(tmp, "go-xwatch")
	suffixDir := filepath.Join(baseDir, expectedSuffix)

	if _, err := os.Stat(filepath.Join(baseDir, "key.bin")); !os.IsNotExist(err) {
		t.Error("key.bin 不應出現在基底目錄 %ProgramData%\\go-xwatch")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "journal.db")); !os.IsNotExist(err) {
		t.Error("journal.db 不應出現在基底目錄 %ProgramData%\\go-xwatch")
	}
	if _, err := os.Stat(filepath.Join(suffixDir, "key.bin")); os.IsNotExist(err) {
		t.Errorf("key.bin 應出現在後綴目錄 %s", suffixDir)
	}
}

// TestClearJournal_UsesServiceSuffixDataDir 確認 clearJournal 使用含後綴的資料目錄，
// key.bin 與 journal.db 不得建立在基底目錄。
func TestClearJournal_UsesServiceSuffixDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	const svcName = "GoXWatch-abc123"
	expectedSuffix := service.SuffixFromServiceName(svcName) // "abc123"

	app := &cliApp{serviceName: svcName}

	captureStdout(func() { _ = app.clearJournal() })

	baseDir := filepath.Join(tmp, "go-xwatch")
	suffixDir := filepath.Join(baseDir, expectedSuffix)

	if _, err := os.Stat(filepath.Join(baseDir, "key.bin")); !os.IsNotExist(err) {
		t.Error("key.bin 不應出現在基底目錄 %ProgramData%\\go-xwatch")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "journal.db")); !os.IsNotExist(err) {
		t.Error("journal.db 不應出現在基底目錄 %ProgramData%\\go-xwatch")
	}
	if _, err := os.Stat(filepath.Join(suffixDir, "key.bin")); os.IsNotExist(err) {
		t.Errorf("key.bin 應出現在後綴目錄 %s", suffixDir)
	}
	if _, err := os.Stat(filepath.Join(suffixDir, "journal.db")); os.IsNotExist(err) {
		t.Errorf("journal.db 應出現在後綴目錄 %s", suffixDir)
	}
}

// TestPrintStatus_DataDirContainsSuffix 確認 printStatus 輸出的 data dir 路徑包含服務後綴子目錄。
func TestPrintStatus_DataDirContainsSuffix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	const svcName = "GoXWatch-myinst"
	expectedSuffix := service.SuffixFromServiceName(svcName) // "myinst"

	app := &cliApp{
		serviceName:     svcName,
		serviceStatusFn: func(_ string) (string, error) { return "running", nil },
	}

	output := captureStdout(func() { _ = app.printStatus() })

	if !strings.Contains(output, expectedSuffix) {
		t.Errorf("printStatus 輸出的 data dir 應包含後綴 %q，實際輸出：\n%s", expectedSuffix, output)
	}
}
