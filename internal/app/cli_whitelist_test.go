package app

import (
	"strings"
	"testing"

	"go-xwatch/internal/config"
	"go-xwatch/internal/watchexcludecmd"
)

// ── whitelist 指令名稱變更整合測試 ──────────────────────────────────────

// TestBuildCommandRegistry_WhitelistRegistered
// 確認 buildCommandRegistry 已將 "whitelist" 作為有效指令名稱註冊。
func TestBuildCommandRegistry_WhitelistRegistered(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("whitelist"); !ok {
		t.Error("buildCommandRegistry 應包含 'whitelist' 指令，但未找到")
	}
}

// TestBuildCommandRegistry_WatchExcludeNotRegistered
// 確認 "watchexclude" 舊指令名稱已從 Registry 中移除，避免使用舊名稱仍可執行。
func TestBuildCommandRegistry_WatchExcludeNotRegistered(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("watchexclude"); ok {
		t.Error("buildCommandRegistry 不應再包含 'watchexclude' 舊指令名稱")
	}
}

// TestWhitelistCommand_Status_Integration
// 整合測試：透過 Registry 執行 whitelist status，驗證端到端流程正出現任何 panic 或內部錯誤。
// 此測試確認 cli.go 的 whitelist 路由正確連接至 watchexcludecmd.Run。
func TestWhitelistCommand_Status_Integration(t *testing.T) {
	setupMinimalCLIConfig(t)

	// mock PasswordPromptFn 以避免互動式輸入
	orig := watchexcludecmd.PasswordPromptFn
	watchexcludecmd.PasswordPromptFn = func(string) (string, error) {
		return config.DefaultWatchExcludeRawPassword, nil
	}
	t.Cleanup(func() { watchexcludecmd.PasswordPromptFn = orig })

	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("whitelist")
	if !ok {
		t.Fatal("whitelist 指令未注冊")
	}

	// status 子指令不需要密碼確認（仍需通過 authorized，mock prompt 回傳正確密碼）
	if err := cmd.Run([]string{"status", "--pw", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Errorf("whitelist status 應成功執行，但回傳錯誤：%v", err)
	}
}

// TestWhitelistCommand_UnknownSub_ErrorContainsWhitelist
// 確認 whitelist 子指令錯誤訊息改為顯示 "whitelist" 而非 "watchexclude"。
func TestWhitelistCommand_UnknownSub_ErrorContainsWhitelist(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("whitelist")
	if !ok {
		t.Fatal("whitelist 指令未注冊")
	}

	err := cmd.Run([]string{"unknown-subcommand", "--pw", config.DefaultWatchExcludeRawPassword})
	if err == nil {
		t.Fatal("執行不存在子指令應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "whitelist") {
		t.Errorf("錯誤訊息應包含 'whitelist'，實際：%q", err.Error())
	}
	if strings.Contains(err.Error(), "watchexclude") {
		t.Errorf("錯誤訊息不應包含舊名稱 'watchexclude'，實際：%q", err.Error())
	}
}

// TestWhitelistCommand_NoSubcommand_ErrorContainsWhitelist
// 確認未提供子指令時的錯誤訊息顯示 "whitelist" 而非 "watchexclude"。
func TestWhitelistCommand_NoSubcommand_ErrorContainsWhitelist(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	cmd, ok := reg.Get("whitelist")
	if !ok {
		t.Fatal("whitelist 指令未注冊")
	}

	err := cmd.Run([]string{})
	if err == nil {
		t.Fatal("未提供子指令應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "whitelist") {
		t.Errorf("錯誤訊息應包含 'whitelist'，實際：%q", err.Error())
	}
}

// TestIsSilentCommand_Whitelist_RegisteredAndSilent
// 整合確認：whitelist 同時滿足「已在 Registry 中」且「被 isSilentCommand 判定為靜默」。
func TestIsSilentCommand_Whitelist_RegisteredAndSilent(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()

	if _, ok := reg.Get("whitelist"); !ok {
		t.Error("whitelist 應已在 Registry 中")
	}
	if !isSilentCommand("whitelist") {
		t.Error("whitelist 應被 isSilentCommand 判定為靜默指令")
	}
}
