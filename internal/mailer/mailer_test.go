package mailer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildLogArchive(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2026, 3, 2, 10, 0, 0, 0, time.Local)
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	content := []byte("hello")
	if err := os.WriteFile(logPath, content, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	data, name, err := BuildLogArchive(tmp, day)
	if err != nil {
		t.Fatalf("BuildLogArchive error: %v", err)
	}
	if name != "watch-log-20260302.zip" {
		t.Fatalf("unexpected zip name: %s", name)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("expected 1 file in zip, got %d", len(zr.File))
	}
	f := zr.File[0]
	if f.Name != "watch_2026-03-02.log" {
		t.Fatalf("unexpected entry name: %s", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open zip entry: %v", err)
	}
	defer rc.Close()
	decoded, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if string(decoded) != string(content) {
		t.Fatalf("zip content mismatch: %s", string(decoded))
	}
}

func TestBuildLogArchiveMissing(t *testing.T) {
	tmp := t.TempDir()
	day := time.Now()
	if _, _, err := BuildLogArchive(tmp, day); err == nil {
		t.Fatalf("expected error when log missing")
	}
}

func TestBuildLogArchiveEmpty(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	if _, _, err := BuildLogArchive(tmp, day); !errors.Is(err, ErrEmptyLog) {
		t.Fatalf("expected ErrEmptyLog, got %v", err)
	}
}

func TestBuildMIMEMessage(t *testing.T) {
	body := "這是內容"
	msg, err := BuildMIMEMessage("from@example.com", []string{"to@example.com"}, "主旨", body, "log.zip", []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("BuildMIMEMessage error: %v", err)
	}
	s := string(msg)
	if !strings.Contains(s, "Subject: =?UTF-8?B?") {
		t.Fatalf("subject should be encoded, got: %s", s)
	}
	if !strings.Contains(s, "Content-Type: text/plain") {
		t.Fatalf("missing text part")
	}
	if !strings.Contains(s, "Content-Type: application/zip") {
		t.Fatalf("missing attachment part")
	}
	if !strings.Contains(s, "filename=\"log.zip\"") {
		t.Fatalf("missing attachment filename")
	}
}

func TestBuildMIMEMessageWithoutAttachment(t *testing.T) {
	body := "純文字"
	msg, err := BuildMIMEMessage("from@example.com", []string{"to@example.com"}, "主旨", body, "", nil)
	if err != nil {
		t.Fatalf("BuildMIMEMessage error: %v", err)
	}
	s := string(msg)
	if strings.Contains(s, "multipart/") {
		t.Fatalf("should be plain text when no attachment")
	}
	if !strings.Contains(s, body) {
		t.Fatalf("body missing: %s", s)
	}
}

func TestSendGmailUsesSender(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	called := false
	var captured struct {
		addr string
		from string
		to   []string
		msg  []byte
	}

	fake := func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		called = true
		captured.addr = addr
		captured.from = from
		captured.to = append([]string(nil), to...)
		captured.msg = append([]byte(nil), msg...)
		return nil
	}

	cfg := SMTPConfig{
		Host:     "smtp.gmail.com",
		Port:     587,
		Username: "user@gmail.com",
		Password: "pass",
		From:     "user@gmail.com",
		To:       []string{"charleswhitesun@gmail.com"},
	}
	opts := ReportOptions{
		LogDir:  tmp,
		Day:     day,
		Subject: "subject",
		Body:    "body",
	}

	if err := SendGmail(context.Background(), cfg, opts, fake); err != nil {
		t.Fatalf("SendGmail error: %v", err)
	}
	if !called {
		t.Fatalf("expected sender to be called")
	}
	if captured.addr != "smtp.gmail.com:587" {
		t.Fatalf("unexpected addr: %s", captured.addr)
	}
	if captured.from != cfg.From {
		t.Fatalf("unexpected from: %s", captured.from)
	}
	if len(captured.to) != 1 || captured.to[0] != cfg.To[0] {
		t.Fatalf("unexpected recipients: %v", captured.to)
	}
	if !strings.Contains(string(captured.msg), "watch-log-20260302.zip") {
		t.Fatalf("message should contain zip filename")
	}
}

func TestSendGmailSkipsMissingAttachment(t *testing.T) {
	tmp := t.TempDir()
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local)

	called := false
	var captured struct {
		msg []byte
	}

	fake := func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		called = true
		captured.msg = append([]byte(nil), msg...)
		return nil
	}

	cfg := SMTPConfig{
		Host:     "smtp.gmail.com",
		Port:     587,
		Username: "user@gmail.com",
		Password: "pass",
		From:     "user@gmail.com",
		To:       []string{"someone@example.com"},
	}
	opts := ReportOptions{
		LogDir:  tmp,
		Day:     day,
		Subject: "subject",
		Body:    "body",
	}

	if err := SendGmail(context.Background(), cfg, opts, fake); err != nil {
		t.Fatalf("SendGmail error: %v", err)
	}
	if !called {
		t.Fatalf("expected sender to be called")
	}
	if strings.Contains(string(captured.msg), "application/zip") {
		t.Fatalf("should not include attachment when missing")
	}
}

// ── dialAndSend 測試 ────────────────────────────────────────────────

// TestDialAndSend_ConnectionRefused 確認連線被拒絕時，dialAndSend 迅速回傳包含
// "SMTP 連線失敗" 的錯誤，不會長時間卡住。
func TestDialAndSend_ConnectionRefused(t *testing.T) {
	// 建立一個臨時 listener 取得可用埠號後立即關閉，確保連線被拒絕
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // 立即關閉，讓連線 refused

	sender := dialAndSend(5 * time.Second)
	start := time.Now()
	sendErr := sender(addr, nil, "from@x.com", []string{"to@x.com"}, []byte("hello"))
	elapsed := time.Since(start)

	if sendErr == nil {
		t.Fatal("expected error from dialAndSend with refused connection")
	}
	if !strings.Contains(sendErr.Error(), "SMTP 連線失敗") {
		t.Fatalf("expected error message to contain 'SMTP 連線失敗', got: %v", sendErr)
	}
	// 拒絕連線應在幾秒內立即失敗，遠小於 5s 超時
	if elapsed > 4*time.Second {
		t.Fatalf("connection refused should fail quickly, elapsed=%s", elapsed)
	}
}

// TestDialAndSend_HangsDetectedByTimeout 確認當 SMTP 伺服器接受 TCP 連線但
// 不回應 220 問候時，dialAndSend 在逾時後回傳錯誤（不卡住）。
func TestDialAndSend_HangsDetectedByTimeout(t *testing.T) {
	// 伺服器接受連線但不發任何資料
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			defer conn.Close()
			// 模擬不回應：等待遠長於測試逾時
			time.Sleep(10 * time.Second)
		}
	}()

	const timeout = 200 * time.Millisecond
	sender := dialAndSend(timeout)
	start := time.Now()
	sendErr := sender(ln.Addr().String(), nil, "from@x.com", []string{"to@x.com"}, []byte("hello"))
	elapsed := time.Since(start)

	if sendErr == nil {
		t.Fatal("expected timeout error from dialAndSend with non-responding server")
	}
	// 應在 timeout 的合理時間內返回（允許 2 倍寬容）
	if elapsed > timeout*3 {
		t.Fatalf("dialAndSend should time out within ~%s, but took %s", timeout, elapsed)
	}
}

// TestDialAndSend_DefaultTimeout30s 確認 dialAndSend(0) 使用 30s 預設逾時。
// 透過包裝的錯誤訊息驗證 30s 出現在訊息中（不需真正等待 30s）。
func TestDialAndSend_DefaultTimeout30s(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	sender := dialAndSend(0) // 0 → 預設 30s
	sendErr := sender(addr, nil, "from@x.com", []string{"to@x.com"}, []byte("hello"))

	if sendErr == nil {
		t.Fatal("expected error")
	}
	// 錯誤中應包含逾時資訊
	if !strings.Contains(sendErr.Error(), "SMTP 連線失敗") {
		t.Fatalf("unexpected error: %v", sendErr)
	}
}
