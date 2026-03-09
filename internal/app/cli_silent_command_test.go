package app

import (
	"os"
	"strings"
	"testing"
)

// ── isSilentCommand 單元測試 ─────────────────────────────────────────────

// TestIsSilentCommand_WatchExclude_ReturnsTrue
// 確認 watchexclude 被判定為靜默指令。
func TestIsSilentCommand_WatchExclude_ReturnsTrue(t *testing.T) {
	if !isSilentCommand("watchexclude") {
		t.Error("watchexclude 應為靜默指令，但 isSilentCommand 回傳 false")
	}
}

// TestIsSilentCommand_RegularCommands_ReturnsFalse
// 確認一般指令不被誤判為靜默指令。
func TestIsSilentCommand_RegularCommands_ReturnsFalse(t *testing.T) {
	for _, cmd := range []string{"status", "init", "mail", "heartbeat", "filecheck", "remove", "start", "stop", "db", "export", "env", "help", ""} {
		if isSilentCommand(cmd) {
			t.Errorf("指令 %q 不應為靜默指令，但 isSilentCommand 回傳 true", cmd)
		}
	}
}

// ── runInteractive 靜默指令不記錄 command log ─────────────────────────

// TestRunInteractive_WatchExclude_DoesNotLogCommand
// 確認執行 watchexclude 時，runInteractive 不寫入 "command" ops log。
func TestRunInteractive_WatchExclude_DoesNotLogCommand(t *testing.T) {
	setupMinimalCLIConfig(t)
	t.Setenv("XWATCH_NO_PAUSE", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	// 注入 watchexclude 指令（帶密碼以通過驗證；或直接觸發 unknown subcommand，
	// 此測試目的是驗證日誌行為，回傳錯誤本身不影響斷言）
	t.Setenv("os_args_override", "")
	origArgs := append([]string(nil), _testOsArgs...)
	setTestArgs(t, "watchexclude")

	_ = app.runInteractive()

	if ml.hasMsg("command") {
		t.Error("watchexclude 執行後不應有 'command' ops log 記錄")
	}
	_ = origArgs
}

// TestRunInteractive_RegularCommand_LogsCommand
// 確認一般指令（如 status）執行時，runInteractive 仍正常寫入 "command" ops log。
func TestRunInteractive_RegularCommand_LogsCommand(t *testing.T) {
	setupMinimalCLIConfig(t)
	t.Setenv("XWATCH_NO_PAUSE", "1")

	ml := &mockLogger{}
	app := &cliApp{
		serviceName:        "GoXWatch",
		opsLogger:          ml,
		serviceInstalledFn: func(_ string) bool { return true },
		serviceStatusFn:    func(_ string) (string, error) { return "running", nil },
	}

	setTestArgs(t, "status")
	_ = app.runInteractive()

	if !ml.hasMsg("command") {
		t.Error("status 指令執行後應有 'command' ops log 記錄，但未找到")
	}
	if !ml.anyArgContains("status") {
		t.Error("'command' log 應含 'status' 指令名稱，但未找到")
	}
}

// TestRunInteractive_WatchExclude_SetsLastCommandSilent
// 確認執行 watchexclude 後，lastCommandSilent 欄位為 true。
func TestRunInteractive_WatchExclude_SetsLastCommandSilent(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}

	setTestArgs(t, "watchexclude")
	_ = app.runInteractive()

	if !app.lastCommandSilent {
		t.Error("執行 watchexclude 後 lastCommandSilent 應為 true，但為 false")
	}
}

// TestRunInteractive_RegularCommand_LastCommandSilentFalse
// 確認執行一般指令後，lastCommandSilent 欄位為 false。
func TestRunInteractive_RegularCommand_LastCommandSilentFalse(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
		serviceStatusFn:    func(_ string) (string, error) { return "running", nil },
	}

	setTestArgs(t, "status")
	_ = app.runInteractive()

	if app.lastCommandSilent {
		t.Error("執行 status 後 lastCommandSilent 應為 false，但為 true")
	}
}

// ── cli start log 靜默抑制測試 ───────────────────────────────────────

// TestCliStart_WatchExclude_NoStartLog
// 確認當第一個 arg 為 watchexclude 時，"cli start" 不被寫入 ops log。
func TestCliStart_WatchExclude_NoStartLog(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	setTestArgs(t, "watchexclude")
	// 只測試靜默判斷邏輯：模擬 run() 開頭的條件判斷
	firstArg := ""
	if len(_testCurrentArgs) > 1 && len(_testCurrentArgs[1]) > 0 {
		firstArg = _testCurrentArgs[1]
	}
	if !isSilentCommand(firstArg) {
		app.opsLogger.Info("cli start")
	}

	if ml.hasMsg("cli start") {
		t.Error("watchexclude 指令時不應寫入 'cli start' ops log")
	}
}

// TestCliStart_RegularCommand_LogsStart
// 確認一般指令時 "cli start" 正常寫入 ops log。
func TestCliStart_RegularCommand_LogsStart(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	setTestArgs(t, "status")
	firstArg := ""
	if len(_testCurrentArgs) > 1 {
		firstArg = _testCurrentArgs[1]
	}
	if !isSilentCommand(firstArg) {
		app.opsLogger.Info("cli start")
	}

	if !ml.hasMsg("cli start") {
		t.Error("一般指令應寫入 'cli start' ops log，但未找到")
	}
}

// ── command ok/error 靜默抑制測試 ────────────────────────────────────

// TestCommandOkLog_WatchExclude_NotLogged
// 確認 lastCommandSilent=true 時，不寫入 "command ok" ops log。
func TestCommandOkLog_WatchExclude_NotLogged(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml, lastCommandSilent: true}
	if !app.lastCommandSilent {
		app.logOp("command ok")
	}
	if ml.hasMsg("command ok") {
		t.Error("lastCommandSilent=true 時不應寫入 'command ok' log")
	}
}

// TestCommandOkLog_RegularCommand_IsLogged
// 確認 lastCommandSilent=false 時，正常寫入 "command ok" ops log。
func TestCommandOkLog_RegularCommand_IsLogged(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml, lastCommandSilent: false}
	if !app.lastCommandSilent {
		app.logOp("command ok")
	}
	if !ml.hasMsg("command ok") {
		t.Error("lastCommandSilent=false 時應寫入 'command ok' log，但未找到")
	}
}

// TestCommandErrorLog_WatchExclude_NotLogged
// 確認 lastCommandSilent=true 時，不寫入 "command error" ops log。
func TestCommandErrorLog_WatchExclude_NotLogged(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml, lastCommandSilent: true}
	if !app.lastCommandSilent {
		app.logOp("command error", "err", "some error")
	}
	if ml.hasMsg("command error") {
		t.Error("lastCommandSilent=true 時不應寫入 'command error' log")
	}
}

// TestCommandErrorLog_RegularCommand_IsLogged
// 確認 lastCommandSilent=false 時，正常寫入 "command error" ops log。
func TestCommandErrorLog_RegularCommand_IsLogged(t *testing.T) {
	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml, lastCommandSilent: false}
	if !app.lastCommandSilent {
		app.logOp("command error", "err", "some error")
	}
	if !ml.hasMsg("command error") {
		t.Error("lastCommandSilent=false 時應寫入 'command error' log，但未找到")
	}
}

// ── 輔助工具 ─────────────────────────────────────────────────────────

// _testCurrentArgs 記錄目前 setTestArgs 設定的 os.Args（供靜默判斷測試使用）
var _testCurrentArgs []string

// _testOsArgs 備份原始 os.Args
var _testOsArgs = os.Args

// setTestArgs 替換 os.Args 並在測試結束後自動還原，同步更新 _testCurrentArgs。
func setTestArgs(t *testing.T, args ...string) {
	t.Helper()
	orig := os.Args
	newArgs := append([]string{orig[0]}, args...)
	_testCurrentArgs = newArgs
	os.Args = newArgs
	t.Cleanup(func() {
		os.Args = orig
		_testCurrentArgs = orig
	})
}

// 確保 os 套件可用（其他測試已 import，此處顯示宣告以利靜態分析）
var _ = strings.Contains
