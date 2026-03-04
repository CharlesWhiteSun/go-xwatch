package app

import (
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
)

// setupMinimalCLIConfig 在暫存目錄建立最小可用設定檔供 CLI 測試使用。
func setupMinimalCLIConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("setupMinimalCLIConfig: Save failed: %v", err)
	}
}

// ── mail enable 服務安裝檢查 ──────────────────────────────────────────

// TestMailEnable_ServiceNotInstalled_ReturnsError 確認服務未安裝時，
// mail enable 返回含「init --install-service」的友善錯誤訊息。
func TestMailEnable_ServiceNotInstalled_ReturnsError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("mail")
	if !ok {
		t.Fatal("mail command not registered")
	}

	err := cmd.Run([]string{"enable"})
	if err == nil {
		t.Fatal("服務未安裝時期望 mail enable 回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("錯誤訊息應包含 'init --install-service'，實際：%v", err)
	}
	if !strings.Contains(err.Error(), "郵件") {
		t.Errorf("錯誤訊息應提及功能名稱「郵件」，實際：%v", err)
	}
}

// TestMailEnable_ServiceInstalled_NoServiceError 確認服務已安裝時，
// mail enable 不會因服務未安裝而報錯（可能有 config 相關錯誤，但不是服務錯誤）。
func TestMailEnable_ServiceInstalled_NoServiceError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("mail")
	if !ok {
		t.Fatal("mail command not registered")
	}

	err := cmd.Run([]string{"enable"})
	// 可能因其他原因（如設定未完整）回傳錯誤，但不應是服務未安裝的錯誤
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("服務已安裝時不應出現服務未安裝錯誤，實際：%v", err)
	}
}

// TestMailSet_ServiceNotInstalled_NotBlocked 確認服務未安裝時，
// mail set 仍可執行（只有 enable 才需要服務）。
func TestMailSet_ServiceNotInstalled_NotBlocked(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("mail")
	if !ok {
		t.Fatal("mail command not registered")
	}

	err := cmd.Run([]string{"set", "--schedule", "10:00"})
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("mail set 不應受服務安裝檢查阻擋，實際：%v", err)
	}
}

// ── heartbeat start 服務安裝檢查 ─────────────────────────────────────

// TestHeartbeatStart_ServiceNotInstalled_ReturnsError 確認服務未安裝時，
// heartbeat start 返回含「init --install-service」的友善錯誤訊息。
func TestHeartbeatStart_ServiceNotInstalled_ReturnsError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("heartbeat")
	if !ok {
		t.Fatal("heartbeat command not registered")
	}

	err := cmd.Run([]string{"start"})
	if err == nil {
		t.Fatal("服務未安裝時期望 heartbeat start 回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("錯誤訊息應包含 'init --install-service'，實際：%v", err)
	}
	if !strings.Contains(err.Error(), "心跳") {
		t.Errorf("錯誤訊息應提及功能名稱「心跳」，實際：%v", err)
	}
}

// TestHeartbeatStop_ServiceNotInstalled_NotBlocked 確認服務未安裝時，
// heartbeat stop 仍可執行（只有 start 才需要服務）。
func TestHeartbeatStop_ServiceNotInstalled_NotBlocked(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("heartbeat")
	if !ok {
		t.Fatal("heartbeat command not registered")
	}

	err := cmd.Run([]string{"stop"})
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("heartbeat stop 不應受服務安裝檢查阻擋，實際：%v", err)
	}
}

// ── requireServiceInstalled helper ───────────────────────────────────

// TestRequireServiceInstalled_Installed_NoError 確認服務已安裝時 requireServiceInstalled 回傳 nil。
func TestRequireServiceInstalled_Installed_NoError(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	if err := app.requireServiceInstalled("測試功能"); err != nil {
		t.Errorf("服務已安裝時不應回傳錯誤，實際：%v", err)
	}
}

// TestRequireServiceInstalled_NotInstalled_ErrorContainsHint 確認服務未安裝時
// requireServiceInstalled 回傳包含安裝提示的錯誤。
func TestRequireServiceInstalled_NotInstalled_ErrorContainsHint(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	err := app.requireServiceInstalled("測試功能")
	if err == nil {
		t.Fatal("服務未安裝時應回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("錯誤訊息應包含 'init --install-service'，實際：%v", err)
	}
	if !strings.Contains(err.Error(), "測試功能") {
		t.Errorf("錯誤訊息應包含功能名稱，實際：%v", err)
	}
}
