package watchexcludecmd

import (
	"os"
	"path/filepath"
	"testing"

	"go-xwatch/internal/config"
)

func setupConfig(t *testing.T, we config.WatchExcludeSettings) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root failed: %v", err)
	}

	if err := config.Save(config.Settings{RootDir: root, WatchExclude: we}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
}

func loadSettings(t *testing.T) config.Settings {
	t.Helper()
	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return s
}

// --- extractFlag ---

func TestExtractFlag_LongForm(t *testing.T) {
	val, rest := extractFlag([]string{"--passwd", "secret", "extra"}, "passwd")
	if val != "secret" {
		t.Fatalf("expected 'secret', got %q", val)
	}
	if len(rest) != 1 || rest[0] != "extra" {
		t.Fatalf("unexpected rest: %v", rest)
	}
}

func TestExtractFlag_EqualForm(t *testing.T) {
	val, rest := extractFlag([]string{"--passwd=abc", "other"}, "passwd")
	if val != "abc" {
		t.Fatalf("expected 'abc', got %q", val)
	}
	if len(rest) != 1 || rest[0] != "other" {
		t.Fatalf("unexpected rest: %v", rest)
	}
}

func TestExtractFlag_NotFound(t *testing.T) {
	val, rest := extractFlag([]string{"foo", "bar"}, "passwd")
	if val != "" {
		t.Fatalf("expected empty value, got %q", val)
	}
	if len(rest) != 2 {
		t.Fatalf("expected rest to be unchanged: %v", rest)
	}
}

func TestExtractFlag_MultipleFlags(t *testing.T) {
	val1, r1 := extractFlag([]string{"--passwd", "p1", "--new", "p2"}, "passwd")
	val2, _ := extractFlag(r1, "new")
	if val1 != "p1" || val2 != "p2" {
		t.Fatalf("got passwd=%q new=%q", val1, val2)
	}
}

// --- authorized ---

func TestAuthorized_WrongPassword(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := authorized([]string{"--passwd", "wrongpassword"}, func(_ []string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestAuthorized_CorrectPassword_DefaultPassword(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	called := false
	err := authorized([]string{"--passwd", config.DefaultWatchExcludeRawPassword}, func(_ []string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

func TestAuthorized_EmptyPassword_Rejected(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := authorized([]string{}, func(_ []string) error { return nil })
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

// --- Run / subcommands ---

func TestRun_UnknownSubcommand(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	if err := Run([]string{"unknown", "--passwd", config.DefaultWatchExcludeRawPassword}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRun_NoSubcommand(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	if err := Run([]string{}); err == nil {
		t.Fatal("expected error for missing subcommand")
	}
}

// --- setEnabled via Run ---

func TestRun_Enable(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{Enabled: config.BoolPtr(false)})
	if err := Run([]string{"enable", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := loadSettings(t)
	if s.WatchExclude.Enabled == nil || !*s.WatchExclude.Enabled {
		t.Fatalf("expected enabled=true, got %v", s.WatchExclude.Enabled)
	}
}

func TestRun_Disable(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	if err := Run([]string{"disable", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := loadSettings(t)
	if s.WatchExclude.Enabled == nil || *s.WatchExclude.Enabled {
		t.Fatalf("expected enabled=false, got %v", s.WatchExclude.Enabled)
	}
}

func TestRun_Enable_WrongPassword(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"enable", "--passwd", "badpassword"})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

// --- runStatus via Run ---

func TestRun_Status(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	// status outputs to stdout; just verify no error
	if err := Run([]string{"status", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- runAddTo via Run ---

func TestRun_AddTo_PositionalArg(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{Dirs: []string{"app"}})
	if err := Run([]string{"add-to", "storage", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := loadSettings(t)
	found := false
	for _, d := range s.WatchExclude.Dirs {
		if d == "storage" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'storage' in dirs, got %v", s.WatchExclude.Dirs)
	}
}

func TestRun_AddTo_Flag(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{Dirs: []string{"app"}})
	if err := Run([]string{"add-to", "--to", "routes", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := loadSettings(t)
	found := false
	for _, d := range s.WatchExclude.Dirs {
		if d == "routes" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'routes' in dirs, got %v", s.WatchExclude.Dirs)
	}
}

func TestRun_AddTo_NoDuplicates(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{Dirs: []string{"app"}})
	// Add same dir twice
	_ = Run([]string{"add-to", "app", "--passwd", config.DefaultWatchExcludeRawPassword})
	s := loadSettings(t)
	count := 0
	for _, d := range s.WatchExclude.Dirs {
		if d == "app" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 'app' to appear exactly once, got %d times in %v", count, s.WatchExclude.Dirs)
	}
}

func TestRun_AddTo_NoDir_ReturnsError(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"add-to", "--passwd", config.DefaultWatchExcludeRawPassword})
	if err == nil {
		t.Fatal("expected error when no dir specified")
	}
}

// --- runSet via Run ---

func TestRun_Set_OverwritesDirs(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{Dirs: []string{"app", "config"}})
	if err := Run([]string{"set", "--dirs", "routes,storage", "--passwd", config.DefaultWatchExcludeRawPassword}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := loadSettings(t)
	if len(s.WatchExclude.Dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d: %v", len(s.WatchExclude.Dirs), s.WatchExclude.Dirs)
	}
	if s.WatchExclude.Dirs[0] != "routes" || s.WatchExclude.Dirs[1] != "storage" {
		t.Fatalf("unexpected dirs: %v", s.WatchExclude.Dirs)
	}
}

func TestRun_Set_NoDirsFlag_ReturnsError(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"set", "--passwd", config.DefaultWatchExcludeRawPassword})
	if err == nil {
		t.Fatal("expected error when --dirs not provided")
	}
}

func TestRun_Set_EmptyDirs_ReturnsError(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"set", "--dirs", "  ,  ", "--passwd", config.DefaultWatchExcludeRawPassword})
	if err == nil {
		t.Fatal("expected error for blank dirs list")
	}
}

// --- runPasswd via Run ---

func TestRun_Passwd_ChangesPassword(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	newPwd := "mynewpassword99"
	if err := Run([]string{"passwd", "--passwd", config.DefaultWatchExcludeRawPassword, "--new", newPwd}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// New password should work, old should not
	s := loadSettings(t)
	if !config.VerifyWatchExcludePassword(newPwd, s.WatchExclude.PasswordHash) {
		t.Fatal("new password should verify")
	}
	if config.VerifyWatchExcludePassword(config.DefaultWatchExcludeRawPassword, s.WatchExclude.PasswordHash) {
		t.Fatal("old password should no longer verify")
	}
}

func TestRun_Passwd_WrongCurrentPassword(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"passwd", "--passwd", "wrongpassword", "--new", "newpwd"})
	if err == nil {
		t.Fatal("expected error for wrong current password")
	}
}

func TestRun_Passwd_MissingNewFlag(t *testing.T) {
	setupConfig(t, config.WatchExcludeSettings{})
	err := Run([]string{"passwd", "--passwd", config.DefaultWatchExcludeRawPassword})
	if err == nil {
		t.Fatal("expected error when --new flag missing")
	}
}
