package opslog

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatOpsMessage(t *testing.T) {
	cases := []struct {
		msg  string
		args []any
		want string
	}{
		{"cli start", []any{"version", "1.0", "pid", 123, "args", []string{"a", "b"}}, "CLI 啟動；版本=1.0；PID=123；參數=[a b]"},
		{"command", []any{"cmd", "status", "args", []string{}}, "收到指令：status；參數=[]"},
		{"command ok", nil, "指令已完成"},
		{"command error", []any{"err", "boom"}, "指令失敗：boom"},
		{"cli exit", []any{"code", 0}, "CLI 結束；代碼=0"},
		{"cli exit", []any{"code", 1, "reason", "test"}, "CLI 結束；代碼=1；原因=test"},
		{"cli signal", []any{"signal", "INT"}, "收到訊號：INT；即將結束"},
		{"service error", []any{"err", "oops"}, "服務錯誤：oops"},
		{"other", []any{"k", "v"}, "other；內容=map[k:v]"},
	}

	for _, c := range cases {
		got := FormatOpsMessage(c.msg, c.args...)
		if got != c.want {
			t.Fatalf("msg=%s got=%q want=%q", c.msg, got, c.want)
		}
	}
}

func TestLoggerWritesDailyFileAndRotates(t *testing.T) {
	tmp := t.TempDir()
	l := New(func() (string, error) { return tmp, nil })

	day1 := time.Date(2024, 1, 2, 15, 4, 5, 0, time.Local)
	day2 := day1.Add(24 * time.Hour)

	l.log(day1, "command ok")
	l.log(day2, "cli exit", "code", 0)
	_ = l.Close()

	path1 := filepath.Join(tmp, "xwatch-ops-logs", "operations_2024-01-02.log")
	path2 := filepath.Join(tmp, "xwatch-ops-logs", "operations_2024-01-03.log")

	checkFileContains(t, path1, "指令已完成")
	checkFileContains(t, path1, "時間=")
	checkFileContains(t, path1, "層級=")
	checkFileContains(t, path1, "訊息=")
	checkFileContains(t, path1, "|")
	checkFileContains(t, path2, "CLI 結束；代碼=0")
}

func checkFileContains(t *testing.T, path string, wantSub string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.Contains(s.Text(), wantSub) {
			return
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	t.Fatalf("substring %q not found in %s", wantSub, path)
}

// TestFormatOpsMessage_RemoveStep 確認 "remove step" 訊息格式化正確。
func TestFormatOpsMessage_RemoveStep(t *testing.T) {
cases := []struct {
step string
want string
}{
{"服務已停止", "移除服務：服務已停止"},
{"心跳已停用", "移除服務：心跳已停用"},
{"郵件排程已停用", "移除服務：郵件排程已停用"},
{"服務已移除", "移除服務：服務已移除"},
}
for _, tc := range cases {
got := FormatOpsMessage("remove step", "step", tc.step)
if got != tc.want {
t.Fatalf("step=%q got=%q want=%q", tc.step, got, tc.want)
}
}
}
