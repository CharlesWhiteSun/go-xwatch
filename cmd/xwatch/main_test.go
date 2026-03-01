package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
)

func TestResolveRootPrefersFlag(t *testing.T) {
	tmp := t.TempDir()
	got, err := resolveRoot(tmp)
	if err != nil {
		t.Fatalf("resolveRoot failed: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(tmp) {
		t.Fatalf("unexpected root: %s", got)
	}
}

func TestResolveRootFallsBackToConfig(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	cfgRoot := filepath.Join(programData, "watched")
	if err := config.Save(config.Settings{RootDir: cfgRoot}); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	got, err := resolveRoot("")
	if err != nil {
		t.Fatalf("resolveRoot failed: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(cfgRoot) {
		t.Fatalf("unexpected root: %s", got)
	}
}

func TestResolveRootFallsBackToExeDir(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	got, err := resolveRoot("")
	if err != nil {
		t.Fatalf("resolveRoot failed: %v", err)
	}
	if got == "" {
		t.Fatalf("expected non-empty root")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected exe dir to exist, got error: %v", err)
	}
}

func TestExportJournalJSONL(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	dataDir := filepath.Join(programData, "go-xwatch")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := j.Append(context.Background(), []journal.Entry{{TS: time.Unix(10, 0), Op: "create", Path: "p"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = j.Close()

	saved := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	if err := exportJournal("1970-01-01T00:00:00Z", "", 10, "json", false, false, "-"); err != nil {
		w.Close()
		t.Fatalf("export: %v", err)
	}
	w.Close()
	os.Stdout = saved

	scanner := bufio.NewScanner(r)
	lines := 0
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), "\"create\"") {
			t.Fatalf("unexpected content: %s", scanner.Text())
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lines != 1 {
		t.Fatalf("expected 1 line, got %d", lines)
	}
}

func TestExportJournalWithBOM(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	dataDir := filepath.Join(programData, "go-xwatch")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := j.Append(context.Background(), []journal.Entry{{TS: time.Unix(10, 0), Op: "create", Path: "路徑"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = j.Close()

	saved := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	if err := exportJournal("", "", 10, "json", true, true, "-"); err != nil {
		w.Close()
		t.Fatalf("export: %v", err)
	}
	w.Close()
	os.Stdout = saved

	bytesOut, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(bytesOut) < 3 || bytesOut[0] != 0xEF || bytesOut[1] != 0xBB || bytesOut[2] != 0xBF {
		t.Fatalf("missing BOM")
	}
	if !strings.Contains(string(bytesOut), "路徑") {
		t.Fatalf("unicode path not found in output: %s", string(bytesOut))
	}
}

func TestClearJournal(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")
	dataDir := filepath.Join(programData, "go-xwatch")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := j.Append(context.Background(), []journal.Entry{{TS: time.Unix(10, 0), Op: "create", Path: "p"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = j.Close()

	if err := clearJournal(); err != nil {
		t.Fatalf("clearJournal: %v", err)
	}

	j2, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	count, err := j2.Count(context.Background())
	_ = j2.Close()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty journal after clear, got %d", count)
	}
}

func TestExportDefaultPath(t *testing.T) {
	programData := t.TempDir()
	t.Setenv("ProgramData", programData)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	dataDir := filepath.Join(programData, "go-xwatch")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := j.Append(context.Background(), []journal.Entry{{TS: time.Unix(10, 0), Op: "create", Path: "p"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = j.Close()

	if err := exportJournal("", "", 10, "json", true, false, ""); err != nil {
		t.Fatalf("export default path: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dataDir, "export_*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected export file in %s", dataDir)
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(content), "\"create\"") {
		t.Fatalf("export content missing data: %s", string(content))
	}
}

func TestIsAccessDenied(t *testing.T) {
	if !isAccessDenied(fmt.Errorf("Access is denied.")) {
		t.Fatalf("expected true for message match")
	}
	if isAccessDenied(fmt.Errorf("other error")) {
		t.Fatalf("expected false for other error")
	}
}

func TestEvaluateElevation_SkipOrElevated(t *testing.T) {
	cases := []struct {
		name        string
		skipEnv     bool
		interactive bool
		elevated    bool
	}{
		{"skip env", true, true, false},
		{"non interactive", false, false, false},
		{"already elevated", false, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := evaluateElevation(tc.skipEnv, tc.interactive, tc.elevated, func(string) bool { return false }, func([]string) error { return nil }, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision != "continue" {
				t.Fatalf("decision = %s, want continue", decision)
			}
		})
	}
}

func TestEvaluateElevation_Decline(t *testing.T) {
	decision, err := evaluateElevation(false, true, false, func(prompt string) bool {
		if prompt != elevationPrompt {
			t.Fatalf("unexpected prompt: %s", prompt)
		}
		return false
	}, func([]string) error { return nil }, []string{"--foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "exit" {
		t.Fatalf("decision = %s, want exit", decision)
	}
}

func TestEvaluateElevation_Relaunch(t *testing.T) {
	called := false
	decision, err := evaluateElevation(false, true, false, func(string) bool { return true }, func(args []string) error {
		called = true
		if len(args) != 2 || args[0] != "--foo" || args[1] != "bar" {
			t.Fatalf("unexpected args: %#v", args)
		}
		return nil
	}, []string{"--foo", "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "relaunch" {
		t.Fatalf("decision = %s, want relaunch", decision)
	}
	if !called {
		t.Fatalf("relaunch not called")
	}
}

func TestEvaluateElevation_RelaunchError(t *testing.T) {
	relaunchErr := errors.New("boom")
	decision, err := evaluateElevation(false, true, false, func(string) bool { return true }, func([]string) error { return relaunchErr }, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, relaunchErr) {
		t.Fatalf("error = %v, want %v", err, relaunchErr)
	}
	if decision != "continue" {
		t.Fatalf("decision = %s, want continue", decision)
	}
}
