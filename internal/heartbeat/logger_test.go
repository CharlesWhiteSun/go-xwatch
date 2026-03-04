package heartbeat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 3, 3, 10, 0, 0, 123000000, time.UTC)

func TestDefaultLogDir_MissingProgramData(t *testing.T) {
	t.Setenv("ProgramData", "")
	if _, err := DefaultLogDir(); err == nil {
		t.Fatal("expected error when ProgramData is empty")
	}
}

func TestDefaultLogDir_ReturnsCorrectPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	got, err := DefaultLogDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmp, "go-xwatch", "xwatch-heartbeat-logs")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteEntry_CreatesFileAndContent(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "hb-logs")

	if err := WriteEntry(logDir, testTime, 1, 60*time.Second); err != nil {
		t.Fatalf("WriteEntry failed: %v", err)
	}

	expectedFile := filepath.Join(logDir, "heartbeat_2026-03-03.log")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("log file not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "2026-03-03 10:00:00.123") {
		t.Errorf("expected timestamp in log, got:\n%s", content)
	}
	if !strings.Contains(content, "心跳 #1 正常") {
		t.Errorf("expected sequence marker, got:\n%s", content)
	}
	if !strings.Contains(content, "間隔: 60s") {
		t.Errorf("expected interval label, got:\n%s", content)
	}
}

func TestWriteEntry_AppendsMultipleEntries(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "hb-logs")

	for i := int64(1); i <= 3; i++ {
		if err := WriteEntry(logDir, testTime, i, 30*time.Second); err != nil {
			t.Fatalf("WriteEntry #%d failed: %v", i, err)
		}
	}

	expectedFile := filepath.Join(logDir, "heartbeat_2026-03-03.log")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("log file not found: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), string(data))
	}
}

func TestWriteEntry_DifferentDatesCreateSeparateFiles(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "hb-logs")

	t1 := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	if err := WriteEntry(logDir, t1, 1, time.Minute); err != nil {
		t.Fatalf("WriteEntry day1 failed: %v", err)
	}
	if err := WriteEntry(logDir, t2, 2, time.Minute); err != nil {
		t.Fatalf("WriteEntry day2 failed: %v", err)
	}

	f1 := filepath.Join(logDir, "heartbeat_2026-03-03.log")
	f2 := filepath.Join(logDir, "heartbeat_2026-03-04.log")
	for _, f := range []string{f1, f2} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("expected file %q to exist: %v", f, err)
		}
	}
}

func TestWriteEntry_CreatesDirectoryIfNotExist(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "nested", "deep", "hb")

	if err := WriteEntry(logDir, testTime, 1, time.Minute); err != nil {
		t.Fatalf("WriteEntry failed: %v", err)
	}
	if _, err := os.Stat(logDir); err != nil {
		t.Fatalf("expected logDir to be created: %v", err)
	}
}

func TestIntervalLabel(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-1 * time.Second, "0s"},
		{time.Second, "1s"},
		{60 * time.Second, "60s"},
		{120 * time.Second, "120s"},
	}
	for _, c := range cases {
		got := intervalLabel(c.d)
		if got != c.want {
			t.Errorf("intervalLabel(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestNewFileLogFunc_WritesEntries(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "hb-logs")

	fn := NewFileLogFunc(logDir, 60*time.Second)
	fn(testTime)
	fn(testTime)
	fn(testTime)

	expectedFile := filepath.Join(logDir, "heartbeat_2026-03-03.log")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("log file not found: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	// 確認序號遞增
	if !strings.Contains(lines[0], "#1") {
		t.Errorf("line 0 expected #1, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "#2") {
		t.Errorf("line 1 expected #2, got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "#3") {
		t.Errorf("line 2 expected #3, got: %s", lines[2])
	}
}

func TestNewFileLogFunc_SequenceIsIndependent(t *testing.T) {
	tmp := t.TempDir()

	fn1 := NewFileLogFunc(filepath.Join(tmp, "a"), time.Minute)
	fn2 := NewFileLogFunc(filepath.Join(tmp, "b"), time.Minute)

	fn1(testTime)
	fn1(testTime)
	fn2(testTime)

	data1, _ := os.ReadFile(filepath.Join(tmp, "a", "heartbeat_2026-03-03.log"))
	data2, _ := os.ReadFile(filepath.Join(tmp, "b", "heartbeat_2026-03-03.log"))

	if !strings.Contains(string(data1), "#2") {
		t.Errorf("fn1 should have seq #2, data: %s", string(data1))
	}
	if !strings.Contains(string(data2), "#1") {
		t.Errorf("fn2 should have seq #1, data: %s", string(data2))
	}
	if strings.Contains(string(data2), "#2") {
		t.Errorf("fn2 should NOT contain #2, data: %s", string(data2))
	}
}
