package filecheckcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
)

// ── Runner 介面配適測試 ───────────────────────────────────────────────

// TestRunner_ServiceRequiredFor 確認 ServiceRequiredFor 回傳正確功能名稱與子指令清單。
func TestRunner_ServiceRequiredFor(t *testing.T) {
	feature, subcmds := Runner.ServiceRequiredFor()
	if feature == "" {
		t.Error("feature 名稱不應為空")
	}
	if !strings.Contains(feature, "目錄") {
		t.Errorf("feature 應包含「目錄」，實際：%q", feature)
	}
	if len(subcmds) == 0 {
		t.Error("subcmds 不應為空")
	}
	found := false
	for _, s := range subcmds {
		if s == "enable" {
			found = true
		}
	}
	if !found {
		t.Errorf("subcmds 應包含 \"enable\"，實際：%v", subcmds)
	}
}

// TestRunner_ServiceRequiredFor_Idempotent 確認多次呼叫回傳相同結果。
func TestRunner_ServiceRequiredFor_Idempotent(t *testing.T) {
	f1, s1 := Runner.ServiceRequiredFor()
	f2, s2 := Runner.ServiceRequiredFor()
	if f1 != f2 {
		t.Errorf("多次呼叫 feature 不一致：%q vs %q", f1, f2)
	}
	if len(s1) != len(s2) {
		t.Errorf("多次呼叫 subcmds 長度不一致：%d vs %d", len(s1), len(s2))
	}
}

// TestRunner_Run_HelpSubcmd 確認 Runner.Run 可正確委派至 Run("help")。
func TestRunner_Run_HelpSubcmd(t *testing.T) {
	if err := Runner.Run([]string{"help"}); err != nil {
		t.Errorf("Runner.Run([help]) 不應回傳錯誤，實際：%v", err)
	}
}

// TestRunner_Run_StatusSubcmd 確認 Runner.Run 可正確委派至 Run("status")。
func TestRunner_Run_StatusSubcmd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	if err := config.Save(config.Settings{RootDir: filepath.Join(tmp, "root")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Runner.Run([]string{"status"}); err != nil {
		t.Errorf("Runner.Run([status]) 不應回傳錯誤，實際：%v", err)
	}
}

// TestRunner_Run_UnknownSubcmd 確認 Runner.Run 對未知子指令回傳錯誤。
func TestRunner_Run_UnknownSubcmd(t *testing.T) {
	err := Runner.Run([]string{"nonexistent-cmd-xyz"})
	if err == nil {
		t.Error("對未知子指令應回傳錯誤，但得到 nil")
	}
}
