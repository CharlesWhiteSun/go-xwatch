package app

import (
	"testing"

	"go-xwatch/internal/config"
)

// ── buildStartStopFeatures 單元測試 ──────────────────────────────────────

// TestBuildStartStopFeatures_ContainsAllThreeFeatures 確認清單包含三個預期功能。
func TestBuildStartStopFeatures_ContainsAllThreeFeatures(t *testing.T) {
	features := buildStartStopFeatures()
	want := []string{"heartbeat", "mail", "filecheck"}
	got := make(map[string]bool, len(features))
	for _, f := range features {
		got[f.CmdName] = true
	}
	for _, cmd := range want {
		if !got[cmd] {
			t.Errorf("buildStartStopFeatures 缺少 %q", cmd)
		}
	}
}

// TestBuildStartStopFeatures_IsEnabled_Heartbeat 確認心跳功能的 IsEnabled 邏輯正確。
func TestBuildStartStopFeatures_IsEnabled_Heartbeat(t *testing.T) {
	var hb ssFeature
	for _, f := range buildStartStopFeatures() {
		if f.CmdName == "heartbeat" {
			hb = f
		}
	}
	if !hb.IsEnabled(config.Settings{HeartbeatEnabled: true}) {
		t.Error("HeartbeatEnabled=true 時 IsEnabled 應回傳 true")
	}
	if hb.IsEnabled(config.Settings{HeartbeatEnabled: false}) {
		t.Error("HeartbeatEnabled=false 時 IsEnabled 應回傳 false")
	}
}

// TestBuildStartStopFeatures_IsEnabled_Mail 確認郵件功能的 IsEnabled 邏輯正確（含 nil 指標）。
func TestBuildStartStopFeatures_IsEnabled_Mail(t *testing.T) {
	var mail ssFeature
	for _, f := range buildStartStopFeatures() {
		if f.CmdName == "mail" {
			mail = f
		}
	}
	tr, fl := true, false
	if !mail.IsEnabled(config.Settings{Mail: config.MailSettings{Enabled: &tr}}) {
		t.Error("Mail.Enabled=true 時 IsEnabled 應回傳 true")
	}
	if mail.IsEnabled(config.Settings{Mail: config.MailSettings{Enabled: &fl}}) {
		t.Error("Mail.Enabled=false 時 IsEnabled 應回傳 false")
	}
	if mail.IsEnabled(config.Settings{}) {
		t.Error("Mail.Enabled=nil 時 IsEnabled 應回傳 false")
	}
}

// TestBuildStartStopFeatures_IsEnabled_Filecheck 確認目錄排程功能的 IsEnabled 邏輯正確。
func TestBuildStartStopFeatures_IsEnabled_Filecheck(t *testing.T) {
	var fc ssFeature
	for _, f := range buildStartStopFeatures() {
		if f.CmdName == "filecheck" {
			fc = f
		}
	}
	if !fc.IsEnabled(config.Settings{Filecheck: config.FilecheckSettings{Enabled: true}}) {
		t.Error("Filecheck.Enabled=true 時 IsEnabled 應回傳 true")
	}
	if fc.IsEnabled(config.Settings{Filecheck: config.FilecheckSettings{Enabled: false}}) {
		t.Error("Filecheck.Enabled=false 時 IsEnabled 應回傳 false")
	}
}

// ── stopService 整合測試 ─────────────────────────────────────────────────

// TestStopService_WithAllEnabled_LogsFeaturesStopped 確認所有功能啟用時，
// stopService 記錄每個功能的「已隨服務停止」訊息。
func TestStopService_WithAllEnabled_LogsFeaturesStopped(t *testing.T) {
	setupRemoveTestConfig(t) // heartbeat + mail + filecheck 全部啟用
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	if err := app.stopService(); err != nil {
		t.Fatalf("stopService 失敗：%v", err)
	}
	for _, want := range []string{
		"heartbeat: 已隨服務停止",
		"mail: 已隨服務停止",
		"filecheck: 已隨服務停止",
	} {
		if !ml.anyArgContains(want) {
			t.Errorf("opsLog 缺少 %q（實際 msgs: %v）", want, ml.msgs)
		}
	}
}

// TestStopService_NoConfig_LogsAllSkipped 確認設定檔不存在時，
// stopService 記錄每個功能的「未啟用，略過」訊息且不回傳錯誤。
func TestStopService_NoConfig_LogsAllSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	if err := app.stopService(); err != nil {
		t.Fatalf("stopService 失敗：%v", err)
	}
	for _, want := range []string{
		"heartbeat: 未啟用，略過",
		"mail: 未啟用，略過",
		"filecheck: 未啟用，略過",
	} {
		if !ml.anyArgContains(want) {
			t.Errorf("opsLog 缺少 %q（實際 msgs: %v）", want, ml.msgs)
		}
	}
}

// ── startService 整合測試 ────────────────────────────────────────────────

// TestStartService_WithAllEnabled_LogsFeaturesStarted 確認所有功能啟用時，
// startService 記錄每個功能的「已隨服務啟動」訊息。
func TestStartService_WithAllEnabled_LogsFeaturesStarted(t *testing.T) {
	setupRemoveTestConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	if err := app.startService(); err != nil {
		t.Fatalf("startService 失敗：%v", err)
	}
	for _, want := range []string{
		"heartbeat: 已隨服務啟動",
		"mail: 已隨服務啟動",
		"filecheck: 已隨服務啟動",
	} {
		if !ml.anyArgContains(want) {
			t.Errorf("opsLog 缺少 %q（實際 msgs: %v）", want, ml.msgs)
		}
	}
}

// TestStartService_NoConfig_LogsAllSkipped 確認設定檔不存在時，
// startService 記錄每個功能的「未啟用，略過」訊息且不回傳錯誤。
func TestStartService_NoConfig_LogsAllSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	ml := &mockLogger{}
	app := &cliApp{serviceName: "GoXWatch", opsLogger: ml}

	if err := app.startService(); err != nil {
		t.Fatalf("startService 失敗：%v", err)
	}
	for _, want := range []string{
		"heartbeat: 未啟用，略過",
		"mail: 未啟用，略過",
		"filecheck: 未啟用，略過",
	} {
		if !ml.anyArgContains(want) {
			t.Errorf("opsLog 缺少 %q（實際 msgs: %v）", want, ml.msgs)
		}
	}
}
