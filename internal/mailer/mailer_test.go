package mailer

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
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
