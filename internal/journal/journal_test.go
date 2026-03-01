package journal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go-xwatch/internal/crypto"
)

func TestJournalAppendAndList(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	dbPath := filepath.Join(tmp, "journal.db")
	j, err := Open(dbPath, key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	entries := []Entry{
		{TS: time.Unix(1, 0), Op: "create", Path: "C:/a.txt", IsDir: false, Size: 5},
		{TS: time.Unix(2, 0), Op: "write", Path: "C:/b.txt", IsDir: false, Size: 7},
	}
	if err := j.Append(context.Background(), entries); err != nil {
		t.Fatalf("append: %v", err)
	}

	out, err := j.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != len(entries) {
		t.Fatalf("unexpected count: %d", len(out))
	}
	for i := range entries {
		if out[i].Op != entries[i].Op || out[i].Path != entries[i].Path || out[i].IsDir != entries[i].IsDir || out[i].Size != entries[i].Size {
			t.Fatalf("mismatch at %d: got %+v want %+v", i, out[i], entries[i])
		}
	}
}

func TestJournalQuerySince(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	j, err := Open(filepath.Join(tmp, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	base := time.Unix(100, 0)
	entries := []Entry{
		{TS: base.Add(-time.Minute), Op: "create", Path: "old"},
		{TS: base, Op: "write", Path: "now"},
		{TS: base.Add(time.Minute), Op: "write", Path: "future"},
	}
	if err := j.Append(context.Background(), entries); err != nil {
		t.Fatalf("append: %v", err)
	}

	out, err := j.Query(context.Background(), base, time.Time{}, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	if out[0].Path != "now" || out[1].Path != "future" {
		t.Fatalf("unexpected order: %+v", out)
	}
}

func TestJournalCount(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	j, err := Open(filepath.Join(tmp, "journal.db"), key)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	if err := j.Append(context.Background(), []Entry{{Path: "a"}, {Path: "b"}}); err != nil {
		t.Fatalf("append: %v", err)
	}

	n, err := j.Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 got %d", n)
	}
}
