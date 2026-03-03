package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	root := filepath.Join(tmp, "root")
	if err := Save(Settings{RootDir: root, DailyCSVEnabled: true}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	absRoot, _ := filepath.Abs(root)
	if loaded.RootDir != absRoot {
		t.Fatalf("unexpected rootDir, got %q want %q", loaded.RootDir, absRoot)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be set")
	}
	if loaded.DailyCSVDir != "daily" {
		t.Fatalf("expected DailyCSVDir default 'daily', got %q", loaded.DailyCSVDir)
	}

	stat, err := os.Stat(filepath.Join(tmp, "go-xwatch", "config.json"))
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatalf("config file is empty")
	}
}

func TestValidateAndFillDefaults(t *testing.T) {
	root := "./foo"
	s, err := ValidateAndFillDefaults(Settings{RootDir: root, DailyCSVEnabled: true})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !filepath.IsAbs(s.RootDir) {
		t.Fatalf("root should be absolute, got %q", s.RootDir)
	}
	if s.DailyCSVDir != "daily" {
		t.Fatalf("expected default daily dir, got %q", s.DailyCSVDir)
	}
}

func TestValidateAndFillDefaultsEmptyRoot(t *testing.T) {
	if _, err := ValidateAndFillDefaults(Settings{RootDir: "   "}); err == nil {
		t.Fatalf("expected error for empty root")
	}
}
