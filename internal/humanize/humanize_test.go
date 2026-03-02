package humanize

import (
	"testing"
	"time"

	"go-xwatch/internal/journal"
)

func TestFormatJournalEntry(t *testing.T) {
	ts := time.Date(2026, 3, 2, 10, 4, 5, 0, time.FixedZone("CST", 8*3600))
	e := journal.Entry{TS: ts, Op: "CREATE", Path: `C:\\data\\report.txt`, IsDir: false, Size: 2048}

	got := FormatJournalEntry(e, Options{Root: `C:\\data`, ShowSize: true, ShowOp: true})
	want := "2026-03-02 10:04:05 新增檔案（CREATE）：report.txt，大小 2.0 KB"

	if got != want {
		t.Fatalf("unexpected format:\n got: %q\nwant: %q", got, want)
	}
}

func TestDescribeOpFallback(t *testing.T) {
	msg := Format(Input{Op: "unknown", Path: "C:/x", TS: time.Unix(0, 0)}, Options{})
	if msg == "" {
		t.Fatalf("unexpected message: %q", msg)
	}
}
