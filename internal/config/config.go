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
	// EnvDev 為開發環境識別字串。
	EnvDev = "dev"
	// EnvProd 為正式環境識別字串。
	EnvProd = "prod"
)

// DefaultMailToListDev 為開發環境的預設收件人清單。
var DefaultMailToListDev = []string{
	"e003@httc.com.tw",
	"ken@mail.httc.com.tw",
	"e032@httc.com.tw",
	"e024@httc.com.tw",
}

// DefaultMailToListProd 為正式環境的預設收件人清單。
var DefaultMailToListProd = []string{
	"589497@cpc.com.tw",
	"e003@httc.com.tw",
	"ken@mail.httc.com.tw",
	"e032@httc.com.tw",
	"e024@httc.com.tw",
}

// DefaultMailToListForEnv 依環境名稱回傳對應的預設收件人清單副本。
// env 為空或不認識時，回傳正式環境清單。
func DefaultMailToListForEnv(env string) []string {
	if strings.EqualFold(env, EnvDev) {
		return append([]string(nil), DefaultMailToListDev...)
	}
	return append([]string(nil), DefaultMailToListProd...)
}

// DefaultMailToList 保留為回溯相容，等同正式環境清單。
// 新程式碼請使用 DefaultMailToListForEnv。
var DefaultMailToList = DefaultMailToListProd

const (
	DefaultMailSchedule          = "10:00"
	DefaultMailTimezone          = "Asia/Taipei"
	DefaultMailTo                = "589497@cpc.com.tw" // 主要預設收件人（DefaultMailToList[0]）
	DefaultMailSubject           = "XWatch 前一日監控日誌"
	DefaultMailBody              = "附件為前一日的監控日誌。"
	DefaultSMTPUser              = "notice@mail.httc.com.tw"
	DefaultSMTPPass              = "Httc24508323"
	DefaultSMTPHost              = "mail.httc.com.tw"
	DefaultSMTPPort              = 587
	DefaultSMTPDialTimeout       = 30  // seconds
	DefaultSMTPRetries           = 3   // retry attempts after first failure
	DefaultSMTPRetryDelay        = 120 // seconds between retries
	DefaultHeartbeatInterval     = 60  // seconds
	DefaultFilecheckMailSchedule = "10:00"
)

type Settings struct {
	RootDir           string            `json:"rootDir"`
	HeartbeatEnabled  bool              `json:"heartbeatEnabled"`
	HeartbeatInterval int               `json:"heartbeatInterval"`
	Mail              MailSettings      `json:"mail"`
	Filecheck         FilecheckSettings `json:"filecheck"`
	// Environment 目前執行環境，"dev" 或 "prod"（預設 "prod"）。
	// 切換環境僅影響「收件人為空時的預設填入清單」，不自動改寫已設定的收件人。
	Environment string `json:"environment,omitempty"`
	// ServiceName 記錄此設定檔所關聯的 Windows 服務全名，例如 "GoXWatch-plant-A"。
	// 空值代表傳統單服務模式（"GoXWatch"）。
	ServiceName string    `json:"serviceName,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// activeServiceSuffix 為目前程序對應的服務後綴（例如 "plant-A"）。
// 空值表示傳統單服務模式，路徑維持向後相容。
// 啟動時透過 SetServiceSuffix 設定一次，不可在執行期間更改。
var activeServiceSuffix string

// SetServiceSuffix 設定目前程序的服務後綴。
// 必須在第一次讀寫設定檔之前呼叫。
func SetServiceSuffix(suffix string) { activeServiceSuffix = strings.TrimSpace(suffix) }

// GetServiceSuffix 回傳目前的服務後綴。
func GetServiceSuffix() string { return activeServiceSuffix }

// ResetServiceSuffix 將後綴重設為空（主要供測試使用）。
func ResetServiceSuffix() { activeServiceSuffix = "" }

// FilecheckSettings 設定目錄檔案存在性排程掃描功能。
type FilecheckSettings struct {
	// Enabled 是否啟用，預設 false，須服務安裝後才能啟動。
	Enabled bool `json:"enabled"`
	// ScanDir 要掃描的目錄路徑。相對路徑以 rootDir 為基底解析。
	// 空值代表使用預設路徑：{rootDir}\storage\logs。
	ScanDir string `json:"scanDir"`
	// Mail filecheck 結果的郵件通知設定（獨立於 watch log 郵件）。
	Mail FilecheckMailSettings `json:"mail"`
}

// FilecheckMailSettings 設定 filecheck 專屬的郵件通知。
// SMTP 連線設定繼承自 Settings.Mail（共用 SMTP 伺服器）。
type FilecheckMailSettings struct {
	// Enabled 使用指標，nil 代表從未設定預設 false，需明確 enable。
	Enabled *bool `json:"enabled,omitempty"`
	// Schedule 每日寄送時間（HH:MM），預設 10:00。
	Schedule string   `json:"schedule"`
	Timezone string   `json:"timezone"`
	To       []string `json:"to"`
}

// IsEnabled 回傳 filecheck 郵件是否啟用。
func (m FilecheckMailSettings) IsEnabled() bool {
	if m.Enabled == nil {
		return false
	}
	return *m.Enabled
}

type MailSettings struct {
	// Enabled 使用指標，nil 代表「從未設定」，預設視為 true；
	// false 指標代表使用者明確停用。
	Enabled         *bool    `json:"enabled,omitempty"`
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

// BoolPtr 回傳 b 的指標，方便建立 *bool 欄位。
func BoolPtr(b bool) *bool { return &b }

// IsEnabled 回傳郵件排程是否啟用。
// 若 Enabled 為 nil（從未明確設定），預設回傳 false；
// 必須執行 mail enable 明確設為 true 才會啟用。
func (m MailSettings) IsEnabled() bool {
	if m.Enabled == nil {
		return false
	}
	return *m.Enabled
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

	if s.HeartbeatInterval <= 0 {
		s.HeartbeatInterval = DefaultHeartbeatInterval
	}

	// 正規化環境識別字串，空値或不認識時預設為 dev
	env := strings.ToLower(strings.TrimSpace(s.Environment))
	if env != EnvDev && env != EnvProd {
		env = EnvDev
	}
	s.Environment = env

	mail, err := validateAndFillMailDefaults(s.Mail, env)
	if err != nil {
		return s, err
	}
	s.Mail = mail

	filecheck, err := validateAndFillFilecheckDefaults(s.Filecheck, env)
	if err != nil {
		return s, err
	}
	s.Filecheck = filecheck

	return s, nil
}

func validateAndFillFilecheckDefaults(fc FilecheckSettings, env string) (FilecheckSettings, error) {
	// Mail 排程預設
	trimSched := strings.TrimSpace(fc.Mail.Schedule)
	if trimSched == "" {
		trimSched = DefaultFilecheckMailSchedule
	}
	if _, err := time.Parse("15:04", trimSched); err != nil {
		return fc, fmt.Errorf("filecheck.mail.schedule must be HH:MM: %w", err)
	}
	fc.Mail.Schedule = trimSched

	if strings.TrimSpace(fc.Mail.Timezone) == "" {
		fc.Mail.Timezone = DefaultMailTimezone
	}

	// 清除格式不合法的收件人（如 ADDR[...] 或其他無 @ 的字串）
	fc.Mail.To = filterValidEmails(fc.Mail.To)

	// 若清單為空，依環境填入預設收件人
	if len(fc.Mail.To) == 0 {
		fc.Mail.To = DefaultMailToListForEnv(env)
	}

	return fc, nil
}

// filterValidEmails 過濾 email 清單，移除不包含 @ 或含括號/空格的無效項目。
func filterValidEmails(addrs []string) []string {
	var out []string
	for _, a := range addrs {
		trimmed := strings.TrimSpace(a)
		at := strings.Index(trimmed, "@")
		if at > 0 && at < len(trimmed)-1 && !strings.ContainsAny(trimmed, " []()<>") {
			out = append(out, trimmed)
		}
	}
	return out
}

func configPath() (string, error) {
	dir, err := paths.EnsureDataDirForSuffix(activeServiceSuffix)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DeleteConfig 刪除設定檔（若不存在則不報錯）。
// 由 remove 指令在移除服務後呼叫，確保下次 init 以全新預設値重新建立設定。
func DeleteConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validateAndFillMailDefaults(m MailSettings, env string) (MailSettings, error) {
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

	if len(m.To) == 0 {
		m.To = DefaultMailToListForEnv(env)
	} else {
		m.To = normalizeList(m.To)
	}

	if strings.TrimSpace(m.Subject) == "" {
		m.Subject = DefaultMailSubject
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = DefaultMailBody
	}

	dataDir, dataErr := paths.DataDirForSuffix(activeServiceSuffix)
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
