package exporter

import (
	"bufio"
	"context"
	"io"
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
	prevSkipACL := os.Getenv("XWATCH_SKIP_ACL")
	os.Setenv("XWATCH_SKIP_ACL", "1")
	t.Cleanup(func() {
		os.Setenv("ProgramData", prevProgramData)
		os.Setenv("XWATCH_SKIP_ACL", prevSkipACL)
	})

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

// TestExportDefaultPath 確認省略 --out 時，輸出檔案會建立在 xwatch-export-files 子目錄下。
func TestExportDefaultPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		t.Fatalf("ensure data dir: %v", err)
	}

	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer j.Close()

	fixedTime := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	var capturedPath string

	err = Export("", "", 10, "json", false, false, "",
		WithNow(func() time.Time { return fixedTime }),
		WithCreateFile(func(p string) (io.WriteCloser, error) {
			capturedPath = p
			return os.Create(p)
		}),
	)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	wantDir := filepath.Join(dataDir, "xwatch-export-files")
	if filepath.Dir(capturedPath) != wantDir {
		t.Errorf("預設輸出目錄應為 %s，實際為 %s", wantDir, filepath.Dir(capturedPath))
	}
	if filepath.Base(capturedPath) != "export_20260304_120000.json" {
		t.Errorf("預設輸出檔名應為 export_20260304_120000.json，實際為 %s", filepath.Base(capturedPath))
	}
}

// TestExportWithDataDirFn 確認 WithDataDirFn 讓 Export 從指定後綴子目錄讀取資料，
// 而非退化到基底目錄（修正 multi-instance 環境下 export 找不到 key.bin / journal.db 的 bug）。
func TestExportWithDataDirFn(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	// 建立後綴子目錄，模擬 multi-instance 服務資料（e.g. --suffix plant-A）
	suffixDir := filepath.Join(tmp, "go-xwatch", "plant-A")
	if err := os.MkdirAll(suffixDir, 0o755); err != nil {
		t.Fatal(err)
	}

	key, err := crypto.LoadOrCreateKey(filepath.Join(suffixDir, "key.bin"), 32)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	j, err := journal.Open(filepath.Join(suffixDir, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	entries := []journal.Entry{
		{TS: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), Op: "CREATE", Path: "report.csv"},
	}
	if err := j.Append(context.Background(), entries); err != nil {
		j.Close()
		t.Fatal(err)
	}
	j.Close()

	outFile := filepath.Join(tmp, "out.json")
	err = Export("", "", 10, "json", true, false, outFile,
		WithDataDirFn(func() (string, error) { return suffixDir, nil }),
	)
	if err != nil {
		t.Fatalf("Export with WithDataDirFn: %v", err)
	}

	// 基底目錄不應有 key.bin（確認 WithDataDirFn 沒有 fallback 到基底目錄）
	baseDir := filepath.Join(tmp, "go-xwatch")
	if _, statErr := os.Stat(filepath.Join(baseDir, "key.bin")); !os.IsNotExist(statErr) {
		t.Error("key.bin 不應出現在基底目錄（WithDataDirFn 已指定後綴目錄）")
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "report.csv") {
		t.Errorf("輸出應包含 report.csv 記錄：%s", string(data))
	}
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
