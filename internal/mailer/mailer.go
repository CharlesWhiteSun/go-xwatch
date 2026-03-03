package mailer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrEmptyLog = errors.New("log file is empty")

// SendMailFunc 抽象 smtp.SendMail 便於測試。
type SendMailFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// SMTPConfig 設定 SMTP 連線與收件資訊。
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
}

// ReportOptions 控制報表檔與郵件內容。
type ReportOptions struct {
	LogDir  string
	Day     time.Time
	Subject string
	Body    string
}

// SendGmail 依設定將指定日期的監控日誌打包並寄出。
// 若 sendFn 為 nil，預設使用 smtp.SendMail。
func SendGmail(ctx context.Context, cfg SMTPConfig, opts ReportOptions, sendFn SendMailFunc) error {
	if sendFn == nil {
		sendFn = smtp.SendMail
	}

	cleanedBody := strings.TrimSpace(opts.Body)
	opts.Body = cleanedBody
	if err := validate(cfg, opts); err != nil {
		return err
	}

	archive, zipName, err := BuildLogArchive(opts.LogDir, opts.Day)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrEmptyLog) {
			archive = nil
			zipName = ""
		} else {
			return err
		}
	}

	msg, err := BuildMIMEMessage(cfg.From, cfg.To, opts.Subject, opts.Body, zipName, archive)
	if err != nil {
		return err
	}

	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	return sendFn(addr, auth, cfg.From, cfg.To, msg)
}

func validate(cfg SMTPConfig, opts ReportOptions) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return errors.New("SMTP host is required")
	}
	if cfg.Port <= 0 {
		return errors.New("SMTP port is required")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return errors.New("SMTP username is required")
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return errors.New("SMTP password is required")
	}
	if strings.TrimSpace(cfg.From) == "" {
		return errors.New("from is required")
	}
	if len(cfg.To) == 0 {
		return errors.New("at least one recipient is required")
	}
	for _, r := range cfg.To {
		if strings.TrimSpace(r) == "" {
			return errors.New("recipient is required")
		}
	}

	if strings.TrimSpace(opts.LogDir) == "" {
		return errors.New("logDir is required")
	}
	if opts.Day.IsZero() {
		return errors.New("day is required")
	}
	if strings.TrimSpace(opts.Subject) == "" {
		return errors.New("subject is required")
	}
	if strings.TrimSpace(opts.Body) == "" {
		return errors.New("body is required")
	}
	return nil
}

// BuildLogArchive 將指定日期的 watch log 打包成 zip，回傳檔名與內容。
func BuildLogArchive(logDir string, day time.Time) ([]byte, string, error) {
	trimmed := strings.TrimSpace(logDir)
	if trimmed == "" {
		return nil, "", errors.New("logDir is required")
	}

	targetDay := day.In(time.Local)
	dayStr := targetDay.Format("2006-01-02")
	logName := fmt.Sprintf("watch_%s.log", dayStr)
	logPath := filepath.Join(trimmed, logName)

	info, err := filepath.Abs(logPath)
	if err == nil {
		logPath = info
	}

	stat, err := fileStat(logPath)
	if err != nil {
		return nil, "", err
	}
	if stat.IsDir() {
		return nil, "", fmt.Errorf("log path is a directory: %s", logPath)
	}
	if stat.Size() == 0 {
		return nil, "", ErrEmptyLog
	}

	data, err := osReadFile(logPath)
	if err != nil {
		return nil, "", err
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.Create(filepath.Base(logPath))
	if err != nil {
		return nil, "", err
	}
	if _, err := w.Write(data); err != nil {
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}

	zipName := fmt.Sprintf("watch-log-%s.zip", targetDay.Format("20060102"))
	return buf.Bytes(), zipName, nil
}

// BuildMIMEMessage 建立含附件的 MIME 郵件內容。
func BuildMIMEMessage(from string, to []string, subject string, body string, attachmentName string, attachment []byte) ([]byte, error) {
	if len(to) == 0 {
		return nil, errors.New("missing recipients")
	}
	if strings.TrimSpace(from) == "" {
		return nil, errors.New("missing from")
	}
	if strings.TrimSpace(subject) == "" {
		return nil, errors.New("missing subject")
	}
	cleanBody := body

	if len(attachment) == 0 {
		var sb strings.Builder
		headers := map[string]string{
			"From":                      from,
			"To":                        strings.Join(to, ", "),
			"Subject":                   encodeSubject(subject),
			"MIME-Version":              "1.0",
			"Content-Type":              "text/plain; charset=UTF-8",
			"Content-Transfer-Encoding": "7bit",
		}
		for k, v := range headers {
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v)
			sb.WriteString("\r\n")
		}
		sb.WriteString("\r\n")
		sb.WriteString(cleanBody)
		sb.WriteString("\r\n")
		return []byte(sb.String()), nil
	}

	boundary := randomBoundary()
	encodedSubject := encodeSubject(subject)
	var sb strings.Builder

	headers := map[string]string{
		"From":         from,
		"To":           strings.Join(to, ", "),
		"Subject":      encodedSubject,
		"MIME-Version": "1.0",
		"Content-Type": fmt.Sprintf("multipart/mixed; boundary=%s", boundary),
	}
	for k, v := range headers {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")

	sb.WriteString("--")
	sb.WriteString(boundary)
	sb.WriteString("\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	sb.WriteString(cleanBody)
	sb.WriteString("\r\n\r\n")

	sb.WriteString("--")
	sb.WriteString(boundary)
	sb.WriteString("\r\n")
	sb.WriteString("Content-Type: application/zip\r\n")
	sb.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", attachmentName))
	sb.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	sb.WriteString(encodeBase64(attachment))
	sb.WriteString("\r\n--")
	sb.WriteString(boundary)
	sb.WriteString("--\r\n")

	return []byte(sb.String()), nil
}

func encodeSubject(subject string) string {
	if isASCII(subject) {
		return subject
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(subject))
	return fmt.Sprintf("=?UTF-8?B?%s?=", b64)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

func encodeBase64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var sb strings.Builder
	const lineLen = 76
	for i := 0; i < len(data); i += 57 { // 57 bytes -> 76 chars after base64
		end := i + 57
		if end > len(data) {
			end = len(data)
		}
		chunk := base64.StdEncoding.EncodeToString(data[i:end])
		sb.WriteString(chunk)
		sb.WriteString("\r\n")
	}
	return sb.String()
}

func randomBoundary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("xw-%x", b)
}

// 便於測試的薄封裝。
var osReadFile = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}

var fileStat = func(path string) (fileInfo, error) {
	info, err := os.Stat(path)
	return info, err
}

// fileInfo 提供最小介面，便於替換。
type fileInfo interface {
	IsDir() bool
	Size() int64
}
