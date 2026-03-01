package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-xwatch/internal/journal"
)

func TestDailyFileSinkCSV(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewDailyFileSink(dir, NewCSVRecorder)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	day1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 3, 2, 9, 0, 0, 0, time.Local)

	entries := []journal.Entry{
		{TS: day1, Op: "create", Path: "a.txt", Size: 1},
		{TS: day1.Add(time.Second), Op: "write", Path: "a.txt", Size: 2},
		{TS: day2, Op: "remove", Path: "b.txt", Size: 0},
	}

	if err := sink.Handle(context.Background(), entries); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// day1 file
	b1, err := os.ReadFile(filepath.Join(dir, "2026-03-01.csv"))
	if err != nil {
		t.Fatalf("read day1: %v", err)
	}
	content1 := string(b1)
	if !containsAll(content1, []string{"ts,op,path,size,is_dir", "create", "write", "a.txt"}) {
		t.Fatalf("unexpected content day1: %s", content1)
	}

	// day2 file
	b2, err := os.ReadFile(filepath.Join(dir, "2026-03-02.csv"))
	if err != nil {
		t.Fatalf("read day2: %v", err)
	}
	content2 := string(b2)
	if !containsAll(content2, []string{"remove", "b.txt"}) {
		t.Fatalf("unexpected content day2: %s", content2)
	}
}

func containsAll(haystack string, needles []string) bool {
	for _, n := range needles {
		if !stringContains(haystack, n) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && contains(s, sub))
}

// simple contains to avoid strings import
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
