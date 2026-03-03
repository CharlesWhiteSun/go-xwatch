package heartbeatcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"go-xwatch/internal/config"
	"go-xwatch/internal/heartbeat"
)

// setupConfig 在暫存目錄建立測試用設定檔
func setupConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		t.Fatalf("failed to save initial config: %v", err)
	}
	return tmp
}

func TestRunNoArgs_PrintsUsage(t *testing.T) {
	if err := Run(nil); err != nil {
		t.Fatalf("Run with no args should not error, got: %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	if err := Run([]string{"help"}); err != nil {
		t.Fatalf("Run help should not error: %v", err)
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"unknowncmd"}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunStatus(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("heartbeat status should not error: %v", err)
	}
}

func TestRunStart_EnablesHeartbeat(t *testing.T) {
	setupConfig(t)

	if err := Run([]string{"start"}); err != nil {
		t.Fatalf("heartbeat start failed: %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if !s.HeartbeatEnabled {
		t.Fatal("expected HeartbeatEnabled=true after start")
	}
	if s.HeartbeatInterval <= 0 {
		t.Fatalf("expected HeartbeatInterval > 0, got %d", s.HeartbeatInterval)
	}
}

func TestRunStop_DisablesHeartbeat(t *testing.T) {
	setupConfig(t)

	// 先啟用
	if err := Run([]string{"start"}); err != nil {
		t.Fatalf("heartbeat start failed: %v", err)
	}
	// 再停止
	if err := Run([]string{"stop"}); err != nil {
		t.Fatalf("heartbeat stop failed: %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if s.HeartbeatEnabled {
		t.Fatal("expected HeartbeatEnabled=false after stop")
	}
}

func TestRunSet_UpdatesInterval(t *testing.T) {
	setupConfig(t)

	if err := Run([]string{"set", "--interval", "30"}); err != nil {
		t.Fatalf("heartbeat set failed: %v", err)
	}

	s, err := config.Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if s.HeartbeatInterval != 30 {
		t.Fatalf("expected HeartbeatInterval=30, got %d", s.HeartbeatInterval)
	}
}

func TestRunSet_ZeroIntervalError(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"set", "--interval", "0"}); err == nil {
		t.Fatal("expected error for interval=0")
	}
}

func TestRunSet_NegativeIntervalError(t *testing.T) {
	setupConfig(t)
	if err := Run([]string{"set", "--interval", "-5"}); err == nil {
		t.Fatal("expected error for negative interval")
	}
}

func TestRunSet_MissingIntervalFlag(t *testing.T) {
	setupConfig(t)
	// --interval 未傳入（預設為 0），應回傳錯誤
	if err := Run([]string{"set"}); err == nil {
		t.Fatal("expected error when --interval not provided")
	}
}

func TestRunStart_SetsDefaultIntervalIfZero(t *testing.T) {
	setupConfig(t)
	// 確保 HeartbeatInterval 初始為 0（預設值）
	s, _ := config.Load()
	s.HeartbeatInterval = 0
	_ = config.Save(s)

	if err := Run([]string{"start"}); err != nil {
		t.Fatalf("heartbeat start failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if loaded.HeartbeatInterval != config.DefaultHeartbeatInterval {
		t.Fatalf("expected default interval %d, got %d", config.DefaultHeartbeatInterval, loaded.HeartbeatInterval)
	}
}

func TestRunStartStopStart_ToggleWorks(t *testing.T) {
	setupConfig(t)

	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"start", true},
		{"stop", false},
		{"start", true},
		{"stop", false},
	} {
		if err := Run([]string{tc.cmd}); err != nil {
			t.Fatalf("heartbeat %s failed: %v", tc.cmd, err)
		}
		s, err := config.Load()
		if err != nil {
			t.Fatalf("load config failed: %v", err)
		}
		if s.HeartbeatEnabled != tc.want {
			t.Fatalf("after %q: expected HeartbeatEnabled=%v, got %v", tc.cmd, tc.want, s.HeartbeatEnabled)
		}
	}
}

// TestRunStatus_WithProgramData 確認在 ProgramData 已設定時，status 不報錯
// 且會輸出 log 目錄路徑（驗證不 panic 即可）。
func TestRunStatus_WithProgramData(t *testing.T) {
	tmp := setupConfig(t)
	_ = tmp // ProgramData 已被 setupConfig 設定為 tmp
	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("heartbeat status with ProgramData should not error: %v", err)
	}
}

// TestRunStatus_EmptyProgramData_ReturnsError 確認 ProgramData 未設定時
// status 因 config.Load 失敗而回傳錯誤（預期行為，非 panic）。
func TestRunStatus_EmptyProgramData_ReturnsError(t *testing.T) {
	// 不呼叫 setupConfig，直接清空 ProgramData
	t.Setenv("ProgramData", "")
	if err := Run([]string{"status"}); err == nil {
		t.Fatal("expected error for status when ProgramData is empty (config path unavailable)")
	}
}

// TestDefaultLogDir_ContainsXwatchHeartbeat 確認 DefaultLogDir 回傳的路徑
// 包含 xwatch-heartbeat 子目錄名稱。
func TestDefaultLogDir_ContainsXwatchHeartbeat(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)

	dir, err := heartbeat.DefaultLogDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty log dir")
	}
	if !strings.Contains(filepath.ToSlash(dir), "xwatch-heartbeat") {
		t.Fatalf("expected path to contain xwatch-heartbeat, got: %s", dir)
	}
}
