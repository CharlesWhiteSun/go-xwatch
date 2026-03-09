// Package mailutil 提供 mail 相關的共用工具函式，
// 供 mailcmd 與 service 套件共同使用，消除重複定義並確保兩處行為一致。
package mailutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/paths"
)

// BuildMailContent 依日誌是否存在與寄送模式，自動產生郵件主旨與內文。
// mode 為顯示用標籤字串（如「(即時)」或「(排程)」），由呼叫端決定文字。
// isImmediate 為 true 時（即時寄送），有日誌的主旨以空格銜接日期；
// 為 false 時（排程寄送），以冒號銜接。
// 回傳 attachmentMissing=true 表示無日誌，呼叫端應省略附件。
func BuildMailContent(rootDirName, dayStr, logPath, mode string, isImmediate bool) (subject, body string, attachmentMissing bool) {
	info, err := os.Stat(logPath)
	logMissing := err != nil || info.Size() == 0

	if logMissing {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s: %s 無資料夾異動紀錄", rootDirName, mode, dayStr)
		body = fmt.Sprintf("您好，%s %s 無資料夾異動之紀錄，特此通知。", rootDirName, dayStr)
		return subject, body, true
	}

	// 有日誌：即時模式以空格連接，排程模式以冒號連接
	if isImmediate {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	} else {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s: %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	}
	body = fmt.Sprintf("您好，附件為 %s %s 之資料夾監控日誌壓縮檔，請卓參。", rootDirName, dayStr)
	return subject, body, false
}

// LoadLocation 依時區字串載入 *time.Location，失敗時回退到 UTC+8 固定時區。
func LoadLocation(tz string) *time.Location {
	trimmed := strings.TrimSpace(tz)
	if trimmed == "" {
		trimmed = config.DefaultMailTimezone
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return time.FixedZone(trimmed, 8*60*60)
	}
	return loc
}

// RenderWithDay 將模板字串中的 {day} 或 %s 替換為實際日期字串。
// 若模板為空，回傳 fallback；若模板不含替換符號，補充空格加日期。
func RenderWithDay(template string, day string, fallback string) string {
	trimmed := strings.TrimSpace(template)
	if trimmed == "" {
		return fallback
	}
	if strings.Contains(trimmed, "{day}") {
		return strings.ReplaceAll(trimmed, "{day}", day)
	}
	if strings.Contains(trimmed, "%s") {
		return fmt.Sprintf(trimmed, day)
	}
	return fmt.Sprintf("%s %s", trimmed, day)
}

// ResolveLogDir 將 path 轉為絕對路徑；若 path 為空，回傳預設 xwatch-watch-logs 目錄。
// 注意：空路徑時會隱式呼叫 paths.DataDir()（無後綴基底目錄）。
// 已知正確 dataDir 時，建議改用 ResolveLogDirForDataDir 以明確語意。
func ResolveLogDir(path string) string {
	if strings.TrimSpace(path) == "" {
		dataDir, err := paths.DataDir()
		if err != nil {
			return ""
		}
		return filepath.Join(dataDir, "xwatch-watch-logs")
	}
	return MakeAbsPath(path)
}

// ResolveLogDirForDataDir 將 configPath 轉為絕對路徑；
// 若 configPath 為空，以 dataDir 作為基底回傳預設 xwatch-watch-logs 目錄。
// 適用於呼叫端已持有正確後綴子目錄的情境，避免隱式使用全域 paths.DataDir()。
func ResolveLogDirForDataDir(configPath, dataDir string) string {
	if strings.TrimSpace(configPath) == "" {
		if dataDir == "" {
			return ""
		}
		return filepath.Join(dataDir, "xwatch-watch-logs")
	}
	return MakeAbsPath(configPath)
}

// MakeAbsPath 將相對路徑轉為絕對路徑，失敗時回傳原路徑。
func MakeAbsPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	return abs
}

// NormalizeList 過濾掉空白字串，回傳去除空值的字串切片。
func NormalizeList(values []string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// SplitValidInvalidEmails 將收件人清單拆分為格式有效與無效兩組。
// 有效條件：
//   - 恰好包含一個 @ 符號
//   - @ 左側與右側均非空
//   - 不含空格、角括號、方括號、圓括號等特殊字元
//
// 輸入會先去除前後空白；輸入為空白的項目會直接略過（不列入 invalid）。
func SplitValidInvalidEmails(addrs []string) (valid []string, invalid []string) {
	for _, a := range addrs {
		trimmed := strings.TrimSpace(a)
		if trimmed == "" {
			continue
		}
		at := strings.Index(trimmed, "@")
		if at > 0 && at < len(trimmed)-1 &&
			strings.Count(trimmed, "@") == 1 &&
			!strings.ContainsAny(trimmed, " []()<>") {
			valid = append(valid, trimmed)
		} else {
			invalid = append(invalid, trimmed)
		}
	}
	return
}

// PrepareBody 依日誌是否存在產生郵件內文，回傳 (body, attachmentMissing)。
// 若日誌不存在且模板為空，回傳 missingBody；模板非空則在末尾加注「未附檔」說明。
func PrepareBody(logPath string, day string, template string, defaultBody string, missingBody string) (string, bool) {
	info, err := os.Stat(logPath)
	missing := err != nil || info.Size() == 0
	base := RenderWithDay(template, day, defaultBody)
	if missing {
		if strings.TrimSpace(template) == "" {
			return missingBody, true
		}
		return fmt.Sprintf("%s\n\n(未附檔，無可用日誌)", base), true
	}
	return base, false
}

// WriteMailLog 將寄信結果以固定格式追加至當日 mail log 檔。
// status: "ok" / "fail"；attachmentStatus: "attached" / "missing" / "error"。
func WriteMailLog(dir string, now time.Time, status string, day string, recipients []string, subject string, attachmentStatus string, errMsg string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	loc := time.FixedZone("CST", 8*60*60)
	localTime := now.In(loc)
	file := filepath.Join(dir, fmt.Sprintf("mail_%s.log", localTime.Format("2006-01-02")))

	statusText := map[string]string{"ok": "成功", "fail": "失敗"}[status]
	if statusText == "" {
		statusText = status
	}
	attachmentText := map[string]string{"attached": "已附檔", "missing": "未附檔", "error": "失敗"}[attachmentStatus]
	if attachmentText == "" {
		attachmentText = attachmentStatus
	}

	line := fmt.Sprintf("%s | 狀態=%s | 日期=%s | 收件人=%s | 主旨=%s | 附件=%s",
		localTime.Format("2006-01-02 15:04:05.000"), statusText, day, strings.Join(recipients, ","), subject, attachmentText)
	if strings.TrimSpace(errMsg) != "" {
		line += " | 錯誤=" + errMsg
	}
	line += "\n"
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// FirstNonEmpty 回傳第一個非空白字串，若全為空則回傳空字串。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ResolveSMTPConfig 從 MailSettings 建立 mailer.SMTPConfig，
// 以 config 預設值填補未設定的欄位（host、port、user、pass、from）。
// recipients 為已正規化的收件人清單。
func ResolveSMTPConfig(mail config.MailSettings, recipients []string) mailer.SMTPConfig {
	host := strings.TrimSpace(mail.SMTPHost)
	if host == "" {
		host = config.DefaultSMTPHost
	}
	port := mail.SMTPPort
	if port == 0 {
		port = config.DefaultSMTPPort
	}
	user := strings.TrimSpace(mail.SMTPUser)
	if user == "" {
		user = config.DefaultSMTPUser
	}
	pass := strings.TrimSpace(mail.SMTPPass)
	if pass == "" {
		pass = config.DefaultSMTPPass
	}
	from := strings.TrimSpace(FirstNonEmpty(mail.SMTPFrom, user))
	if from == "" {
		from = user
	}
	return mailer.SMTPConfig{
		Host:        host,
		Port:        port,
		Username:    user,
		Password:    pass,
		From:        from,
		To:          recipients,
		DialTimeout: time.Duration(mail.SMTPDialTimeout) * time.Second,
	}
}
