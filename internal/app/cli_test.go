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

// ── daily 指令已移除 相關測試 ────────────────────────────────────────────

// TestDailyCommand_NotRegistered 檢役 daily 指令已從指令表中移除。
func TestDailyCommand_NotRegistered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("daily"); ok {
		t.Error("daily 指令應已移除，但註冊表中仍存在")
	}
}

// TestDailyCommand_KnownCommandsDoNotIncludeDaily 檢查所有已知指令中不包含 daily。
func TestDailyCommand_KnownCommandsDoNotIncludeDaily(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	// 確保主要指令仍存在
	for _, name := range []string{"mail", "heartbeat", "export"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("指令 %q 應存在但未找到", name)
		}
	}
	// daily 不應存在
	if _, ok := reg.Get("daily"); ok {
		t.Error("daily 指令應已移除")
	}
}

// ── 已移除指令確認 ────────────────────────────────────────────────────

// TestUninstallCommand_NotRegistered 確認 uninstall 指令已從指令表移除。
func TestUninstallCommand_NotRegistered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("uninstall"); ok {
		t.Error("uninstall 指令應已移除，但指令表中仍存在")
	}
}

// TestCleanupCommand_NotRegistered 確認 cleanup 指令已從指令表移除。
func TestCleanupCommand_NotRegistered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("cleanup"); ok {
		t.Error("cleanup 指令應已移除，但指令表中仍存在")
	}
}

// TestRunCommand_NotRegistered 確認 run 指令已從指令表移除。
func TestRunCommand_NotRegistered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("run"); ok {
		t.Error("run 指令應已移除，但指令表中仍存在")
	}
}

// TestClearPurgeWipe_NotRegistered 確認 clear / purge / wipe 頂層指令已移除。
func TestClearPurgeWipe_NotRegistered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	for _, name := range []string{"clear", "purge", "wipe"} {
		if _, ok := reg.Get(name); ok {
			t.Errorf("指令 %q 應已移除，但指令表中仍存在", name)
		}
	}
}

// ── remove 與 db 指令確認 ─────────────────────────────────────────────

// TestRemoveCommand_Registered 確認 remove 指令仍存在。
func TestRemoveCommand_Registered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("remove"); !ok {
		t.Error("remove 指令應存在，但指令表中找不到")
	}
}

// TestDBCommand_Registered 確認 db 指令已成功註冊。
func TestDBCommand_Registered(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("db"); !ok {
		t.Error("db 指令應存在，但指令表中找不到")
	}
}

// TestDBCommand_HelpSubcommand 確認 db help 不回傳錯誤。
func TestDBCommand_HelpSubcommand(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("db")
	if !ok {
		t.Fatal("db 指令未註冊")
	}
	if err := cmd.Run([]string{"help"}); err != nil {
		t.Errorf("db help 不應回傳錯誤，實際：%v", err)
	}
}

// TestDBCommand_NoArgs_ShowsHelp 確認 db（無參數）不回傳錯誤（顯示說明）。
func TestDBCommand_NoArgs_ShowsHelp(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("db")
	if !ok {
		t.Fatal("db 指令未註冊")
	}
	if err := cmd.Run([]string{}); err != nil {
		t.Errorf("db（無參數）不應回傳錯誤，實際：%v", err)
	}
}

// TestDBCommand_UnknownSubcommand_ReturnsError 確認未知子指令回傳包含提示的錯誤。
func TestDBCommand_UnknownSubcommand_ReturnsError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("db")
	if !ok {
		t.Fatal("db 指令未註冊")
	}
	err := cmd.Run([]string{"nonexistent"})
	if err == nil {
		t.Fatal("db <未知子指令> 應回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "db help") {
		t.Errorf("錯誤訊息應包含 'db help'，實際：%v", err)
	}
}

// ── help 子指令測試 ───────────────────────────────────────────────────

// TestInitCommand_HelpSubcommand 確認 init help 不回傳錯誤。
func TestInitCommand_HelpSubcommand(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("init")
	if !ok {
		t.Fatal("init 指令未註冊")
	}
	if err := cmd.Run([]string{"help"}); err != nil {
		t.Errorf("init help 不應回傳錯誤，實際：%v", err)
	}
}

// TestExportCommand_HelpSubcommand 確認 export help 不回傳錯誤。
func TestExportCommand_HelpSubcommand(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("export")
	if !ok {
		t.Fatal("export 指令未註冊")
	}
	if err := cmd.Run([]string{"help"}); err != nil {
		t.Errorf("export help 不應回傳錯誤，實際：%v", err)
	}
}

// ── filecheck enable 服務安裝檢查 ─────────────────────────────────────

// TestFilecheckEnable_ServiceNotInstalled_ReturnsError 確認服務未安裝時，
// filecheck enable 返回包含「init --install-service」的友善錯誤訊息。
func TestFilecheckEnable_ServiceNotInstalled_ReturnsError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("filecheck")
	if !ok {
		t.Fatal("filecheck command not registered")
	}

	err := cmd.Run([]string{"enable", "--to", "test@example.com"})
	if err == nil {
		t.Fatal("服務未安裝時期望 filecheck enable 回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("錯誤訊息應包含 'init --install-service'，實際：%v", err)
	}
	if !strings.Contains(err.Error(), "目錄檔案檢查") {
		t.Errorf("錯誤訊息應提及功能名稱「目錄檔案檢查」，實際：%v", err)
	}
}

// TestFilecheckEnable_ServiceInstalled_NoServiceError 確認服務已安裝時，
// filecheck enable 不會因服務未安裝而報錯。
func TestFilecheckEnable_ServiceInstalled_NoServiceError(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("filecheck")
	if !ok {
		t.Fatal("filecheck command not registered")
	}

	err := cmd.Run([]string{"enable", "--to", "test@example.com"})
	// 可能因其他原因回傳錯誤，但不應是服務未安裝的錯誤
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("服務已安裝時不應出現服務未安裝錯誤，實際：%v", err)
	}
}

// TestFilecheckDisable_ServiceNotInstalled_NotBlocked 確認服務未安裝時，
// filecheck disable 仍可執行（只有 enable 才需要服務）。
func TestFilecheckDisable_ServiceNotInstalled_NotBlocked(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("filecheck")
	if !ok {
		t.Fatal("filecheck command not registered")
	}

	err := cmd.Run([]string{"disable"})
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("filecheck disable 不應受服務安裝檢查阻擋，實際：%v", err)
	}
}

// TestFilecheckStatus_ServiceNotInstalled_NotBlocked 確認服務未安裝時，
// filecheck status 仍可執行（唯讀操作不需要服務）。
func TestFilecheckStatus_ServiceNotInstalled_NotBlocked(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}

	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("filecheck")
	if !ok {
		t.Fatal("filecheck command not registered")
	}

	err := cmd.Run([]string{"status"})
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("filecheck status 不應受服務安裝檢查阻擋，實際：%v", err)
	}
}

// TestFilecheckEnable_ErrorMessage_MatchesMailStyleHint 確認錯誤格式與 mail enable
// 的提示一致，均為「服務尚未安裝，無法啟用…功能，請先執行『init --install-service』」。
func TestFilecheckEnable_ErrorMessage_MatchesMailStyleHint(t *testing.T) {
	setupMinimalCLIConfig(t)

	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	reg := app.buildCommandRegistry()

	filecheckCmd, _ := reg.Get("filecheck")
	mailCmd, _ := reg.Get("mail")

	fcErr := filecheckCmd.Run([]string{"enable"})
	mailErr := mailCmd.Run([]string{"enable"})

	for _, e := range []error{fcErr, mailErr} {
		if e == nil {
			t.Fatal("服務未安裝時 enable 應回傳錯誤")
		}
		if !strings.Contains(e.Error(), "服務尚未安裝") {
			t.Errorf("錯誤應包含「服務尚未安裝」，實際：%v", e)
		}
		if !strings.Contains(e.Error(), "init --install-service") {
			t.Errorf("錯誤應包含 'init --install-service'，實際：%v", e)
		}
	}
}
