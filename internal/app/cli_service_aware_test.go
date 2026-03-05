package app

import (
	"errors"
	"strings"
	"testing"

	"go-xwatch/internal/cli"
)

// mockSARunner 是用於測試 registerServiceAware 的模擬 cli.ServiceAwareRunner。
type mockSARunner struct {
	feature  string
	subcmds  []string
	runErr   error
	lastArgs []string
}

func (m *mockSARunner) Run(args []string) error {
	m.lastArgs = args
	return m.runErr
}

func (m *mockSARunner) ServiceRequiredFor() (string, []string) {
	return m.feature, m.subcmds
}

// 確保 mockSARunner 編譯期即符合 cli.ServiceAwareRunner 介面。
var _ cli.ServiceAwareRunner = &mockSARunner{}

// ── registerServiceAware 單元測試 ─────────────────────────────────────

// TestRegisterServiceAware_ServiceNotInstalled_BlocksRequiredSubcmd
// 確認服務未安裝時，聲明需要服務的子指令被正確阻擋並回傳友善錯誤。
func TestRegisterServiceAware_ServiceNotInstalled_BlocksRequiredSubcmd(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{feature: "測試功能", subcmds: []string{"enable"}}

	app.registerServiceAware(reg, "test", runner)

	cmd, ok := reg.Get("test")
	if !ok {
		t.Fatal("指令應已註冊")
	}
	err := cmd.Run([]string{"enable"})
	if err == nil {
		t.Fatal("服務未安裝時 enable 應回傳錯誤，但得到 nil")
	}
	if !strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("錯誤訊息應包含 'init --install-service'，實際：%v", err)
	}
	if !strings.Contains(err.Error(), "測試功能") {
		t.Errorf("錯誤訊息應包含功能名稱，實際：%v", err)
	}
}

// TestRegisterServiceAware_ServiceInstalled_AllowsRequiredSubcmd
// 確認服務已安裝時，聲明需要服務的子指令可正常執行（不會被阻擋）。
func TestRegisterServiceAware_ServiceInstalled_AllowsRequiredSubcmd(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true },
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{feature: "測試功能", subcmds: []string{"enable"}}

	app.registerServiceAware(reg, "test", runner)

	cmd, _ := reg.Get("test")
	err := cmd.Run([]string{"enable", "--to", "a@b.com"})
	// 服務已安裝，只要 runner 不報錯就應成功
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("服務已安裝時不應出現服務未安裝錯誤，實際：%v", err)
	}
}

// TestRegisterServiceAware_FreeSubcmd_NotBlocked
// 確認沒有列在 subcmds 清單的子指令不受服務安裝檢查阻擋。
func TestRegisterServiceAware_FreeSubcmd_NotBlocked(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false }, // 服務未安裝
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{
		feature: "測試功能",
		subcmds: []string{"enable"}, // 只有 enable 需要服務
	}

	app.registerServiceAware(reg, "test", runner)
	cmd, _ := reg.Get("test")

	// status / disable / set 均不需要服務
	for _, sub := range []string{"status", "disable", "set"} {
		err := cmd.Run([]string{sub})
		if err != nil && strings.Contains(err.Error(), "init --install-service") {
			t.Errorf("%q 不應受服務安裝檢查阻擋，實際：%v", sub, err)
		}
	}
}

// TestRegisterServiceAware_MultipleServiceSubcmds_AllBlocked
// 確認宣告多個需服務子指令時，每一個均被正確阻擋。
func TestRegisterServiceAware_MultipleServiceSubcmds_AllBlocked(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{
		feature: "複合功能",
		subcmds: []string{"enable", "start"},
	}

	app.registerServiceAware(reg, "test", runner)
	cmd, _ := reg.Get("test")

	for _, sub := range []string{"enable", "start"} {
		err := cmd.Run([]string{sub})
		if err == nil {
			t.Errorf("%q 服務未安裝時應回傳錯誤，但得到 nil", sub)
		}
		if err != nil && !strings.Contains(err.Error(), "init --install-service") {
			t.Errorf("%q 錯誤訊息應含 'init --install-service'，實際：%v", sub, err)
		}
	}
}

// TestRegisterServiceAware_RunnerError_Propagates
// 確認 runner.Run 回傳的非服務安裝錯誤可正確傳遞給呼叫端。
func TestRegisterServiceAware_RunnerError_Propagates(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return true }, // 服務已安裝
	}
	reg := cli.NewRegistry()
	wantErr := errors.New("runner 內部錯誤")
	runner := &mockSARunner{
		feature: "測試功能",
		subcmds: []string{"enable"},
		runErr:  wantErr,
	}

	app.registerServiceAware(reg, "test", runner)
	cmd, _ := reg.Get("test")

	err := cmd.Run([]string{"enable"})
	if !errors.Is(err, wantErr) {
		t.Errorf("期望錯誤 %v，實際：%v", wantErr, err)
	}
}

// TestRegisterServiceAware_NoArgs_NotBlocked
// 確認無參數時不會觸發服務安裝檢查（交由 runner 本身處理）。
func TestRegisterServiceAware_NoArgs_NotBlocked(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{feature: "測試功能", subcmds: []string{"enable"}}

	app.registerServiceAware(reg, "test", runner)
	cmd, _ := reg.Get("test")

	err := cmd.Run([]string{})
	if err != nil && strings.Contains(err.Error(), "init --install-service") {
		t.Errorf("無參數不應觸發服務安裝檢查，實際：%v", err)
	}
}

// TestRegisterServiceAware_CaseInsensitive_Subcmd
// 確認子指令比對不區分大小寫（"ENABLE" 與 "enable" 效果相同）。
func TestRegisterServiceAware_CaseInsensitive_Subcmd(t *testing.T) {
	app := &cliApp{
		serviceName:        "GoXWatch",
		serviceInstalledFn: func(_ string) bool { return false },
	}
	reg := cli.NewRegistry()
	runner := &mockSARunner{feature: "測試功能", subcmds: []string{"enable"}}

	app.registerServiceAware(reg, "test", runner)
	cmd, _ := reg.Get("test")

	for _, variant := range []string{"ENABLE", "Enable", "eNaBlE"} {
		err := cmd.Run([]string{variant})
		if err == nil {
			t.Errorf("大小寫變體 %q 應被阻擋，但得到 nil", variant)
		}
	}
}
