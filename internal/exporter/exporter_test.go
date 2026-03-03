package exporter

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
)

func TestExportJSONL(t *testing.T) {
	tmp := t.TempDir()
	prevProgramData := os.Getenv("ProgramData")
	os.Setenv("ProgramData", tmp)
	t.Cleanup(func() { os.Setenv("ProgramData", prevProgramData) })

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		t.Fatalf("ensure data dir: %v", err)
	}

	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	j, err := journal.Open(journalPath, key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer j.Close()

	entries := []journal.Entry{
		{TS: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Op: "CREATE", Path: "a.txt", IsDir: false, Size: 10},
		{TS: time.Date(2024, 1, 3, 4, 5, 6, 0, time.UTC), Op: "DELETE", Path: "b.txt", IsDir: false, Size: 0},
	}
	if err := j.Append(context.Background(), entries); err != nil {
		t.Fatalf("append: %v", err)
	}

	outFile := filepath.Join(tmp, "out.jsonl")
	if err := Export("", "", 10, "jsonl", false, false, outFile); err != nil {
		t.Fatalf("export: %v", err)
	}

	assertFileHasLines(t, outFile, 2, []string{"CREATE", "DELETE"})
}

func assertFileHasLines(t *testing.T, path string, wantLines int, wantSubs []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	count := 0
	found := make(map[string]bool)
	for s.Scan() {
		line := s.Text()
		count++
		for _, sub := range wantSubs {
			if strings.Contains(line, sub) {
				found[sub] = true
			}
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if count != wantLines {
		t.Fatalf("want %d lines, got %d", wantLines, count)
	}
	for _, sub := range wantSubs {
		if !found[sub] {
			t.Fatalf("substring %q not found in %s", sub, path)
		}
	}
}
