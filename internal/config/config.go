package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/paths"
)

const (
	DefaultMailSchedule      = "10:00"
	DefaultMailTimezone      = "Asia/Taipei"
	DefaultMailSubject       = "XWatch 前一日監控日誌"
	DefaultMailBody          = "附件為前一日的監控日誌。"
	DefaultSMTPUser          = "notice@mail.httc.com.tw"
	DefaultSMTPPass          = "Httc24508323"
	DefaultSMTPHost          = "mail.httc.com.tw"
	DefaultSMTPPort          = 587
	DefaultSMTPDialTimeout   = 30  // seconds
	DefaultSMTPRetries       = 3   // retry attempts after first failure
	DefaultSMTPRetryDelay    = 120 // seconds between retries
	DefaultHeartbeatInterval = 60  // seconds
)

type Settings struct {
	RootDir           string       `json:"rootDir"`
	DailyCSVEnabled   bool         `json:"dailyCsvEnabled"`
	DailyCSVDir       string       `json:"dailyCsvDir"`
	HeartbeatEnabled  bool         `json:"heartbeatEnabled"`
	HeartbeatInterval int          `json:"heartbeatInterval"`
	Mail              MailSettings `json:"mail"`
	UpdatedAt         time.Time    `json:"updatedAt"`
}

type MailSettings struct {
	Enabled         bool     `json:"enabled"`
	Schedule        string   `json:"schedule"`
	Timezone        string   `json:"timezone"`
	To              []string `json:"to"`
	Subject         string   `json:"subject"`
	Body            string   `json:"body"`
	LogDir          string   `json:"logDir"`
	MailLogDir      string   `json:"mailLogDir"`
	SMTPHost        string   `json:"smtpHost"`
	SMTPPort        int      `json:"smtpPort"`
	SMTPUser        string   `json:"smtpUser"`
	SMTPPass        string   `json:"smtpPass"`
	SMTPFrom        string   `json:"smtpFrom"`
	SMTPDialTimeout int      `json:"smtpDialTimeout"` // 連線逾時秒數，0 = 預設 30s
	SMTPRetries     int      `json:"smtpRetries"`     // 失敗後重試次數，0 = 預設 3
	SMTPRetryDelay  int      `json:"smtpRetryDelay"`  // 重試間隔秒數，0 = 預設 120s
}

func Load() (Settings, error) {
	path, err := configPath()
	if err != nil {
		return Settings{}, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(bytes, &s); err != nil {
		return Settings{}, err
	}
	return ValidateAndFillDefaults(s)
}

func Save(s Settings) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	validated, err := ValidateAndFillDefaults(s)
	if err != nil {
		return err
	}
	validated.UpdatedAt = time.Now().UTC()
	bytes, err := json.MarshalIndent(validated, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

// ValidateAndFillDefaults trims/normalizes settings and returns filled defaults.
func ValidateAndFillDefaults(s Settings) (Settings, error) {
	trimmedRoot := strings.TrimSpace(s.RootDir)
	if trimmedRoot == "" {
		return s, errors.New("rootDir is required")
	}
	absRoot, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return s, err
	}
	s.RootDir = absRoot

	if s.DailyCSVEnabled && strings.TrimSpace(s.DailyCSVDir) == "" {
		s.DailyCSVDir = "daily"
	}

	if s.HeartbeatInterval <= 0 {
		s.HeartbeatInterval = DefaultHeartbeatInterval
	}

	mail, err := validateAndFillMailDefaults(s.Mail)
	if err != nil {
		return s, err
	}
	s.Mail = mail

	return s, nil
}

func configPath() (string, error) {
	dir, err := paths.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func validateAndFillMailDefaults(m MailSettings) (MailSettings, error) {
	trimmedSchedule := strings.TrimSpace(m.Schedule)
	if trimmedSchedule == "" {
		trimmedSchedule = DefaultMailSchedule
	}
	if _, err := time.Parse("15:04", trimmedSchedule); err != nil {
		return m, fmt.Errorf("mail.schedule must be HH:MM: %w", err)
	}
	m.Schedule = trimmedSchedule

	trimmedTZ := strings.TrimSpace(m.Timezone)
	if trimmedTZ == "" {
		trimmedTZ = DefaultMailTimezone
	}
	m.Timezone = trimmedTZ

	if len(m.To) > 0 {
		m.To = normalizeList(m.To)
	}

	if strings.TrimSpace(m.Subject) == "" {
		m.Subject = DefaultMailSubject
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = DefaultMailBody
	}

	dataDir, dataErr := paths.DataDir()
	defaultLogDir := "xwatch-watch-logs"
	defaultMailLogDir := "xwatch-mail-logs"
	if dataErr == nil {
		defaultLogDir = filepath.Join(dataDir, "xwatch-watch-logs")
		defaultMailLogDir = filepath.Join(dataDir, "xwatch-mail-logs")
	}

	m.LogDir = normalizePathOrDefault(m.LogDir, defaultLogDir)
	m.MailLogDir = normalizePathOrDefault(m.MailLogDir, defaultMailLogDir)

	trimmedHost := strings.TrimSpace(firstNonEmpty(m.SMTPHost, DefaultSMTPHost))
	if isGmailHost(trimmedHost) {
		trimmedHost = DefaultSMTPHost
	}
	m.SMTPHost = trimmedHost
	if m.SMTPPort == 0 {
		m.SMTPPort = DefaultSMTPPort
	}

	user := strings.TrimSpace(firstNonEmpty(m.SMTPUser, DefaultSMTPUser))
	if isGmailAddress(user) {
		user = DefaultSMTPUser
	}
	m.SMTPUser = user
	m.SMTPPass = strings.TrimSpace(firstNonEmpty(m.SMTPPass, DefaultSMTPPass))

	from := strings.TrimSpace(firstNonEmpty(m.SMTPFrom, m.SMTPUser, DefaultSMTPUser))
	if isGmailAddress(from) {
		from = m.SMTPUser
	}
	m.SMTPFrom = from

	if m.SMTPDialTimeout <= 0 {
		m.SMTPDialTimeout = DefaultSMTPDialTimeout
	}
	if m.SMTPRetries <= 0 {
		m.SMTPRetries = DefaultSMTPRetries
	}
	if m.SMTPRetryDelay <= 0 {
		m.SMTPRetryDelay = DefaultSMTPRetryDelay
	}

	return m, nil
}

func normalizePathOrDefault(value string, def string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return def
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	return abs
}

func normalizeList(values []string) []string {
	var out []string
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isGmailHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "smtp.gmail.com" || h == "smtp.googlemail.com"
}

func isGmailAddress(addr string) bool {
	a := strings.ToLower(strings.TrimSpace(addr))
	return strings.HasSuffix(a, "@gmail.com") || strings.HasSuffix(a, "@googlemail.com")
}
