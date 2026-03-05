package app

import (
	"errors"
	"testing"
)

// ── evaluateElevation 單元測試 ────────────────────────────────────────

// TestEvaluateElevation_SkipEnv 確認 XWATCH_NO_ELEVATE=1 時直接回傳 continue。
func TestEvaluateElevation_SkipEnv(t *testing.T) {
	called := false
	action, err := evaluateElevation(
		true,  // skipEnv=true
		true,  // interactive
		false, // not elevated
		func(_ string) bool { called = true; return true },
		func(_ []string) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("不應有錯誤，實際：%v", err)
	}
	if action != "continue" {
		t.Errorf("skipEnv=true 應回傳 continue，實際：%q", action)
	}
	if called {
		t.Error("skipEnv=true 時不應呼叫 ask 函式")
	}
}

// TestEvaluateElevation_NonInteractive 確認非互動模式時直接回傳 continue。
func TestEvaluateElevation_NonInteractive(t *testing.T) {
	action, err := evaluateElevation(
		false, // skipEnv
		false, // interactive=false → non-interactive
		false, // not elevated
		func(_ string) bool { t.Error("不應呼叫 ask"); return true },
		func(_ []string) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("不應有錯誤，實際：%v", err)
	}
	if action != "continue" {
		t.Errorf("non-interactive 應回傳 continue，實際：%q", action)
	}
}

// TestEvaluateElevation_AlreadyElevated 確認已提升權限時直接回傳 continue。
func TestEvaluateElevation_AlreadyElevated(t *testing.T) {
	action, err := evaluateElevation(
		false, // skipEnv
		true,  // interactive
		true,  // elevated=true
		func(_ string) bool { t.Error("不應呼叫 ask"); return true },
		func(_ []string) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("不應有錯誤，實際：%v", err)
	}
	if action != "continue" {
		t.Errorf("已提升應回傳 continue，實際：%q", action)
	}
}

// TestEvaluateElevation_UserAccepts 確認互動+未提升且使用者確認時回傳 relaunch。
func TestEvaluateElevation_UserAccepts(t *testing.T) {
	relaunchCalled := false
	action, err := evaluateElevation(
		false,                               // skipEnv
		true,                                // interactive
		false,                               // not elevated
		func(_ string) bool { return true }, // user says yes
		func(_ []string) error { relaunchCalled = true; return nil },
		[]string{"status"},
	)
	if err != nil {
		t.Fatalf("不應有錯誤，實際：%v", err)
	}
	if action != "relaunch" {
		t.Errorf("使用者接受提升應回傳 relaunch，實際：%q", action)
	}
	if !relaunchCalled {
		t.Error("應呼叫 relaunch 函式")
	}
}

// TestEvaluateElevation_UserDeclines 確認使用者拒絕提升時回傳 exit。
func TestEvaluateElevation_UserDeclines(t *testing.T) {
	action, err := evaluateElevation(
		false,                                // skipEnv
		true,                                 // interactive
		false,                                // not elevated
		func(_ string) bool { return false }, // user says no
		func(_ []string) error { t.Error("拒絕提升時不應呼叫 relaunch"); return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("不應有錯誤，實際：%v", err)
	}
	if action != "exit" {
		t.Errorf("使用者拒絕提升應回傳 exit，實際：%q", action)
	}
}

// TestEvaluateElevation_RelaunchError 確認 relaunch 失敗時回傳 continue 並附帶錯誤。
func TestEvaluateElevation_RelaunchError(t *testing.T) {
	wantErr := errors.New("UAC 啟動失敗")
	action, err := evaluateElevation(
		false,
		true,
		false,
		func(_ string) bool { return true },
		func(_ []string) error { return wantErr },
		nil,
	)
	if err == nil {
		t.Fatal("relaunch 失敗應回傳錯誤，但得到 nil")
	}
	if action != "continue" {
		t.Errorf("relaunch 失敗應回傳 continue，實際：%q", action)
	}
}
