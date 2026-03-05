package cli

import (
	"errors"
	"testing"
)

// ── ServiceAwareRunner 介面測試 ───────────────────────────────────────

// mockServiceAwareRunner 實作 ServiceAwareRunner 供測試使用。
type mockServiceAwareRunner struct {
	feature  string
	subcmds  []string
	runError error
	lastArgs []string
}

func (m *mockServiceAwareRunner) Run(args []string) error {
	m.lastArgs = args
	return m.runError
}

func (m *mockServiceAwareRunner) ServiceRequiredFor() (string, []string) {
	return m.feature, m.subcmds
}

// TestServiceAwareRunner_InterfaceCompliance 確認 mockServiceAwareRunner
// 滿足 ServiceAwareRunner 介面約束（編譯期即驗證）。
func TestServiceAwareRunner_InterfaceCompliance(t *testing.T) {
	var _ ServiceAwareRunner = &mockServiceAwareRunner{}
}

// TestServiceAwareRunner_Run_DelegatesCorrectly 確認 Run 委派並傳遞正確參數。
func TestServiceAwareRunner_Run_DelegatesCorrectly(t *testing.T) {
	mock := &mockServiceAwareRunner{}
	args := []string{"status", "--json"}
	if err := mock.Run(args); err != nil {
		t.Fatalf("不應回傳錯誤，實際：%v", err)
	}
	if len(mock.lastArgs) != len(args) {
		t.Errorf("Run 未正確傳遞 args，期望 %v，實際 %v", args, mock.lastArgs)
	}
}

// TestServiceAwareRunner_Run_ReturnsError 確認 Run 可正確回傳錯誤。
func TestServiceAwareRunner_Run_ReturnsError(t *testing.T) {
	wantErr := errors.New("test error")
	mock := &mockServiceAwareRunner{runError: wantErr}
	if err := mock.Run(nil); err != wantErr {
		t.Errorf("期望錯誤 %v，實際：%v", wantErr, err)
	}
}

// TestServiceAwareRunner_ServiceRequiredFor_ReturnsDeclaration 確認回傳正確聲明。
func TestServiceAwareRunner_ServiceRequiredFor_ReturnsDeclaration(t *testing.T) {
	mock := &mockServiceAwareRunner{
		feature: "測試功能",
		subcmds: []string{"enable", "start"},
	}
	feature, subcmds := mock.ServiceRequiredFor()
	if feature != "測試功能" {
		t.Errorf("feature 期望 %q，實際 %q", "測試功能", feature)
	}
	if len(subcmds) != 2 {
		t.Errorf("subcmds 長度期望 2，實際 %d", len(subcmds))
	}
}
