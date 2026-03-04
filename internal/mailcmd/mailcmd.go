package mailcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/paths"
)

// sendModeImmediate / sendModeScheduled 分別代表「即時寄送」與「排程寄送」的模式標籤，
// 用於動態組成郵件主旨，避免重複硬編碼。
const (
	sendModeImmediate = "(即時)"
	sendModeScheduled = "(排程)"
)

// buildMailContent 依日誌是否存在與寄送模式，自動產生郵件主旨與內文。
// 若日誌存在，即時模式主旨用空格分隔，排程模式用冒號分隔；兩種模式的內文格式相同。
// 回傳 attachmentMissing=true 表示無日誌，呼叫端應省略附件。
func buildMailContent(rootDirName, dayStr, logPath, mode string) (subject, body string, attachmentMissing bool) {
	info, err := os.Stat(logPath)
	logMissing := err != nil || info.Size() == 0

	if logMissing {
		subject = fmt.Sprintf("%s 資料夾監控日誌%s: %s 無資料夾異動紀錄", rootDirName, mode, dayStr)
		body = fmt.Sprintf("您好，%s %s 無資料夾異動之紀錄，特此通知。", rootDirName, dayStr)
		return subject, body, true
	}
	// 有日誌：即時模式以空格連接，排程模式以冒號連接
	if mode == sendModeImmediate {
		subject = fmt.Sprintf("%s 資料夾監控日誌%s %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	} else {
		subject = fmt.Sprintf("%s 資料夾監控日誌%s: %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	}
	body = fmt.Sprintf("您好，附件為 %s %s 之資料夾監控日誌壓縮檔，請卓參。", rootDirName, dayStr)
	return subject, body, false
}

// Run handles mail subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		printMailUsage()
		return nil
	}

	sub := strings.ToLower(args[0])
	args = args[1:]

	switch sub {
	case "help":
		printMailHelp(time.Now())
		return nil
	case "status":
		return status()
	case "enable":
		return enable(args)
	case "disable":
		return disable()
	case "set":
		return set(args)
	case "add-to":
		return addTo(args)
	case "send":
		return send(args)
	default:
		return fmt.Errorf("未知子指令: %s", sub)
	}
}

func status() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	mail := settings.Mail
	tz := mail.Timezone
	if tz == "" {
		tz = "Asia/Taipei"
	}
	fmt.Println("郵件系統啟用狀態:", mail.IsEnabled())
	fmt.Println("寄送時間:", mail.Schedule, "(時區:", tz+")")
	fmt.Println("收件人:", strings.Join(mail.To, ", "))
	fmt.Println("主旨:", mail.Subject)
	fmt.Println("內容:", mail.Body)
	fmt.Println("SMTP:", fmt.Sprintf("%s:%d", mail.SMTPHost, mail.SMTPPort))
	fmt.Println("SMTP 使用者:", mail.SMTPUser)
	fmt.Println("watch log 目錄:", mail.LogDir)
	fmt.Println("mail log 目錄:", mail.MailLogDir)
	return nil
}

func enable(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	mail := settings.Mail
	mail.Enabled = config.BoolPtr(true)
	if err := applyFlags(&mail, args); err != nil {
		return err
	}
	settings.Mail = mail
	return config.Save(settings)
}

func disable() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.Mail.Enabled = config.BoolPtr(false)
	return config.Save(settings)
}

func set(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	mail := settings.Mail
	if err := applyFlags(&mail, args); err != nil {
		return err
	}
	settings.Mail = mail
	return config.Save(settings)
}

// addTo 追加收件人至現有清單（不覆蓋），重複地址會自動去除。
func addTo(args []string) error {
	fs := flag.NewFlagSet("mail add-to", flag.ContinueOnError)
	toFlag := fs.String("to", "", "以逗號分隔的收件人（追加，不覆蓋現有清單）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// 支援 --to 旗標或直接位置參數
	rawTo := strings.TrimSpace(*toFlag)
	if rawTo == "" && len(fs.Args()) > 0 {
		rawTo = strings.Join(fs.Args(), ",")
	}
	if rawTo == "" {
		return errors.New("請提供至少一個收件人，例如：mail add-to --to a@example.com 或 mail add-to a@example.com")
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}

	newAddrs := splitList(rawTo)
	existing := settings.Mail.To

	// 去重合併：保留現有順序，僅追加尚未存在的地址
	seen := make(map[string]struct{}, len(existing))
	for _, a := range existing {
		seen[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
	}
	added := 0
	for _, a := range newAddrs {
		key := strings.ToLower(strings.TrimSpace(a))
		if _, dup := seen[key]; !dup {
			existing = append(existing, a)
			seen[key] = struct{}{}
			added++
		}
	}

	settings.Mail.To = existing
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Printf("已追加 %d 位收件人，目前共 %d 位：%s\n", added, len(existing), strings.Join(existing, ", "))
	return nil
}

func send(args []string) error {
	return sendWithGmailFn(args, nil)
}

// sendWithGmailFn 是 send 的可測試版本，允許注入 SendGmail 函式（nil 時使用 mailer.SendGmail）。
func sendWithGmailFn(args []string, gmailFn func(ctx context.Context, cfg mailer.SMTPConfig, opts mailer.ReportOptions, sendFn mailer.SendMailFunc) error) error {
	if gmailFn == nil {
		gmailFn = mailer.SendGmail
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	mail := settings.Mail

	fs := flag.NewFlagSet("mail send", flag.ContinueOnError)
	toFlag := fs.String("to", "", "以逗號分隔的收件人 (預設 config)")
	subjectFlag := fs.String("subject", "", "自訂郵件主旨")
	bodyFlag := fs.String("body", "", "自訂郵件內容")
	dayFlag := fs.String("day", "", "寄送哪一天的日誌 (YYYY-MM-DD；預設昨天，mail 時區)")
	logDirFlag := fs.String("log-dir", "", "watch log 目錄 (預設 config)")
	mailLogDirFlag := fs.String("mail-log-dir", "", "mail log 目錄 (預設 config)")
	hostFlag := fs.String("host", "", "SMTP 主機 (預設 config)")
	portFlag := fs.Int("port", 0, "SMTP 連線埠 (預設 config)")
	userFlag := fs.String("user", "", "SMTP 使用者 (預設 config)")
	passFlag := fs.String("pass", "", "SMTP 密碼/應用程式密碼 (預設 config)")
	fromFlag := fs.String("from", "", "寄件者 (預設 SMTP 使用者)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if v := strings.TrimSpace(*toFlag); v != "" {
		mail.To = splitList(v)
	}
	// --subject / --body 旗標不直接寫入 mail，改在 buildMailContent 之後覆蓋
	if v := strings.TrimSpace(*logDirFlag); v != "" {
		mail.LogDir = makeAbsPath(v)
	}
	if v := strings.TrimSpace(*mailLogDirFlag); v != "" {
		mail.MailLogDir = makeAbsPath(v)
	}
	if v := strings.TrimSpace(*hostFlag); v != "" {
		mail.SMTPHost = v
	}
	if *portFlag != 0 {
		mail.SMTPPort = *portFlag
	}
	if v := strings.TrimSpace(*userFlag); v != "" {
		mail.SMTPUser = v
	}
	if v := strings.TrimSpace(*passFlag); v != "" {
		mail.SMTPPass = v
	}
	if v := strings.TrimSpace(*fromFlag); v != "" {
		mail.SMTPFrom = v
	}

	loc := loadLocation(mail.Timezone)
	targetDay := time.Now().In(loc).AddDate(0, 0, -1)
	if v := strings.TrimSpace(*dayFlag); v != "" {
		parsed, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return fmt.Errorf("day 需為 YYYY-MM-DD (%s): %w", mail.Timezone, err)
		}
		targetDay = parsed
	}
	dayStr := targetDay.Format("2006-01-02")

	logDir := resolveLogDir(mail.LogDir)
	mailLogDir := resolveLogDir(mail.MailLogDir)
	if mailLogDir == "" {
		mailLogDir = logDir
	}

	// 取監控主目錄名稱作為郵件主旨與內文的識別前綴
	rootDirName := filepath.Base(strings.TrimSpace(settings.RootDir))
	if rootDirName == "" || rootDirName == "." {
		rootDirName = "XWatch"
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("watch_%s.log", dayStr))
	subject, body, attachmentMissing := buildMailContent(rootDirName, dayStr, logPath, sendModeImmediate)
	// 若使用者明確提供 --subject 或 --body 旗標，以旗標值覆蓋自動產生的內容
	if v := strings.TrimSpace(*subjectFlag); v != "" {
		subject = v
	}
	if v := strings.TrimSpace(*bodyFlag); v != "" {
		body = v
	}

	recipients := normalizeList(mail.To)
	if len(recipients) == 0 {
		return errors.New("請提供收件人 (config 或 --to)")
	}

	host := normalizeHost(mail.SMTPHost)
	port := normalizePort(mail.SMTPPort)
	user := normalizeUser(mail.SMTPUser, mail.SMTPFrom)
	pass := normalizePass(mail.SMTPPass)
	from := normalizeFrom(mail.SMTPFrom, user)

	cfg := mailer.SMTPConfig{
		Host:        host,
		Port:        port,
		Username:    user,
		Password:    pass,
		From:        from,
		To:          recipients,
		DialTimeout: time.Duration(mail.SMTPDialTimeout) * time.Second, // 從 config 讀取連線逾時
	}
	opts := mailer.ReportOptions{
		LogDir:  logDir,
		Day:     targetDay,
		Subject: subject,
		Body:    body,
	}

	// 在呼叫 SendGmail 前先確立附件狀態，以便寄信失敗時也能正確記錄實際附件情況
	attachmentStatus := "attached"
	if attachmentMissing {
		attachmentStatus = "missing"
	}

	if err := gmailFn(context.Background(), cfg, opts, nil); err != nil {
		// 寄信失敗時記錄真實附件狀況，錯誤原因由「錯誤=」欄位描述
		_ = writeMailLog(mailLogDir, time.Now(), "fail", dayStr, recipients, subject, attachmentStatus, err.Error())
		return err
	}
	_ = writeMailLog(mailLogDir, time.Now(), "ok", dayStr, recipients, subject, attachmentStatus, "")
	fmt.Println("郵件已送出 (若附件缺漏會自動省略)。")
	return nil
}

func applyFlags(mail *config.MailSettings, args []string) error {
	fs := flag.NewFlagSet("mail set", flag.ContinueOnError)
	toFlag := fs.String("to", "", "以逗號分隔的收件人")
	subjectFlag := fs.String("subject", "", "郵件主旨 (可包含 {day})")
	bodyFlag := fs.String("body", "", "郵件內容 (可包含 {day})")
	logDirFlag := fs.String("log-dir", "", "watch log 目錄")
	mailLogDirFlag := fs.String("mail-log-dir", "", "mail log 目錄")
	scheduleFlag := fs.String("schedule", "", "每日寄送時間 HH:MM (預設 10:00)")
	tzFlag := fs.String("tz", "", "時區 (預設 Asia/Taipei)")
	hostFlag := fs.String("host", "", "SMTP 主機 (預設 mail.httc.com.tw)")
	portFlag := fs.Int("port", 0, "SMTP 連線埠 (預設 587)")
	userFlag := fs.String("user", "", "SMTP 使用者")
	passFlag := fs.String("pass", "", "SMTP 密碼/應用程式密碼")
	fromFlag := fs.String("from", "", "寄件者 (預設 SMTP 使用者)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if v := strings.TrimSpace(*toFlag); v != "" {
		mail.To = splitList(v)
	}
	if v := strings.TrimSpace(*subjectFlag); v != "" {
		mail.Subject = v
	}
	if v := strings.TrimSpace(*bodyFlag); v != "" {
		mail.Body = v
	}
	if v := strings.TrimSpace(*logDirFlag); v != "" {
		mail.LogDir = makeAbsPath(v)
	}
	if v := strings.TrimSpace(*mailLogDirFlag); v != "" {
		mail.MailLogDir = makeAbsPath(v)
	}
	if v := strings.TrimSpace(*scheduleFlag); v != "" {
		if _, err := time.Parse("15:04", v); err != nil {
			return fmt.Errorf("schedule 需為 HH:MM: %w", err)
		}
		mail.Schedule = v
	}
	if v := strings.TrimSpace(*tzFlag); v != "" {
		mail.Timezone = v
	}
	if v := strings.TrimSpace(*hostFlag); v != "" {
		mail.SMTPHost = v
	}
	if *portFlag != 0 {
		mail.SMTPPort = *portFlag
	}
	if v := strings.TrimSpace(*userFlag); v != "" {
		mail.SMTPUser = v
	}
	if v := strings.TrimSpace(*passFlag); v != "" {
		mail.SMTPPass = v
	}
	if v := strings.TrimSpace(*fromFlag); v != "" {
		mail.SMTPFrom = v
	}
	return nil
}

func printMailUsage() {
	fmt.Println("mail 指令用法：")
	fmt.Println("  mail help")
	fmt.Println("  mail status")
	fmt.Println("  mail enable [flags]")
	fmt.Println("  mail disable")
	fmt.Println("  mail set [flags]")
	fmt.Println("  mail add-to [--to] ADDR[,ADDR...]")
	fmt.Println("  mail send [flags]")
	fmt.Println("flags 可設定 --to --subject --body --log-dir --mail-log-dir --schedule --tz --host --port --user --pass --from")
}

func printMailHelp(now time.Time) {
	fmt.Println("mail 指令說明：")
	fmt.Println()
	fmt.Println("常用流程：")
	fmt.Println("  1) 設定並啟用： mail enable --to a@example.com --user smtp_user --pass smtp_pass")
	fmt.Println("  2) 調整設定：   mail set --schedule 10:00 --tz Asia/Taipei")
	fmt.Println("  3) 查看設定：   mail status")
	fmt.Println("  4) 立即寄送：   mail send [--day YYYY-MM-DD]")
	fmt.Println()
	fmt.Println("子指令：")
	fmt.Println("  help          顯示此說明")
	fmt.Println("  status        查看目前 mail 設定與預設值")
	fmt.Println("  enable        啟用每日寄送，接受同 set 的 flags")
	fmt.Println("  disable       停用每日寄送")
	fmt.Println("  set           調整設定，不改啟用狀態，flags 如下")
	fmt.Println("  add-to        追加收件人，不覆蓋現有清單（自動去重）")
	fmt.Println("  send          依設定立即寄送，可用 flags 暫時覆寫")
	fmt.Println()
	fmt.Println("常用 flags：")
	fmt.Println("  --to a@b,c@d       (set/enable) 覆蓋收件人清單；(add-to) 追加收件人")
	fmt.Println("  --subject TEXT      覆蓋自動產生的郵件主旨（send 指令專用）")
	fmt.Println("  --body TEXT         覆蓋自動產生的郵件內容（send 指令專用）")
	fmt.Println("  --log-dir PATH      watch log 來源目錄，預設 %ProgramData%/go-xwatch/xwatch-watch-logs")
	fmt.Println("  --mail-log-dir PATH 寄信紀錄目錄，預設 %ProgramData%/go-xwatch/xwatch-mail-logs")
	fmt.Println("  --schedule HH:MM    每日寄送時間，預設 10:00 (24 小時制)")
	fmt.Println("  --tz TZ             時區，預設 Asia/Taipei")
	fmt.Println("  --host HOST         SMTP 主機，預設 mail.httc.com.tw")
	fmt.Println("  --port N            SMTP 連線埠，預設 587")
	fmt.Println("  --user NAME         SMTP 帳號 (預設 notice@mail.httc.com.tw)")
	fmt.Println("  --pass PWD          SMTP 密碼或應用程式密碼 (預設系統內建值)")
	fmt.Println("  --from NAME         寄件者，預設同 SMTP 帳號")
	fmt.Println("  --day YYYY-MM-DD    立即發送寄送日期之日誌")
	fmt.Println()
	fmt.Println("附檔/主旨/正文規則：")
	fmt.Println("  - 會尋找 watch_YYYY-MM-DD.log；檔案不存在或為空時不附檔。")
	fmt.Println("  - 主旨與內文依日誌是否存在自動產生（含監控目錄名稱與寄送模式標籤）：")
	fmt.Println("    有日誌(即時)  主旨：{目錄} 資料夾監控日誌(即時) {日期} 已撈出資料，詳如內文")
	fmt.Println("    無日誌(即時)  主旨：{目錄} 資料夾監控日誌(即時): {日期} 無資料夾異動紀錄")
	fmt.Println("    有日誌(排程)  主旨：{目錄} 資料夾監控日誌(排程): {日期} 已撈出資料，詳如內文")
	fmt.Println("    無日誌(排程)  主旨：{目錄} 資料夾監控日誌(排程): {日期} 無資料夾異動紀錄")
	fmt.Println("  - 可用 --subject / --body 旗標覆蓋自動產生的主旨/內文。")
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  mail enable --to boss@example.com --user smtp_user --pass smtp_pass")
	fmt.Println("  mail add-to a@example.com,b@example.com")
	fmt.Println("  mail set --schedule 09:30 --tz Asia/Taipei --subject 'XWatch 日誌 {day}'")
	fmt.Println("  mail send --day " + now.AddDate(0, 0, -1).Format("2006-01-02"))
}

func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	var out []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func renderWithDay(template string, day string, fallback string) string {
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

func makeAbsPath(path string) string {
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

func resolveLogDir(path string) string {
	if strings.TrimSpace(path) == "" {
		dataDir, err := paths.DataDir()
		if err != nil {
			return ""
		}
		return filepath.Join(dataDir, "xwatch-watch-logs")
	}
	return makeAbsPath(path)
}

func loadLocation(tz string) *time.Location {
	trimmed := strings.TrimSpace(tz)
	if trimmed == "" {
		trimmed = "Asia/Taipei"
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return time.FixedZone(trimmed, 8*60*60)
	}
	return loc
}

func ensureDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.MkdirAll(path, 0o755)
}

func normalizeHost(host string) string {
	h := strings.TrimSpace(host)
	if h == "" || isGmailHost(h) {
		return config.DefaultSMTPHost
	}
	return h
}

func normalizePort(port int) int {
	if port <= 0 {
		return config.DefaultSMTPPort
	}
	return port
}

func normalizeUser(user string, from string) string {
	candidate := strings.TrimSpace(firstNonEmpty(user, from, config.DefaultSMTPUser))
	if isGmailAddress(candidate) {
		return config.DefaultSMTPUser
	}
	return candidate
}

func normalizePass(pass string) string {
	trimmed := strings.TrimSpace(pass)
	if trimmed == "" {
		return config.DefaultSMTPPass
	}
	return trimmed
}

func normalizeFrom(from string, user string) string {
	candidate := strings.TrimSpace(firstNonEmpty(from, user, config.DefaultSMTPUser))
	if isGmailAddress(candidate) {
		return user
	}
	return candidate
}

func isGmailHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "smtp.gmail.com" || h == "smtp.googlemail.com"
}

func isGmailAddress(addr string) bool {
	a := strings.ToLower(strings.TrimSpace(addr))
	return strings.HasSuffix(a, "@gmail.com") || strings.HasSuffix(a, "@googlemail.com")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func normalizeList(values []string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func prepareBody(logPath string, day string, template string, defaultBody string, missingBody string) (string, bool) {
	info, err := os.Stat(logPath)
	missing := err != nil || info.Size() == 0
	base := renderWithDay(template, day, defaultBody)
	if missing {
		if strings.TrimSpace(template) == "" {
			return missingBody, true
		}
		return fmt.Sprintf("%s\n\n(未附檔，無可用日誌)", base), true
	}
	return base, false
}

func writeMailLog(dir string, now time.Time, status string, day string, recipients []string, subject string, attachmentStatus string, errMsg string) error {
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

	line := fmt.Sprintf("%s | 狀態=%s | 日期=%s | 收件人=%s | 主旨=%s | 附件=%s", localTime.Format("2006-01-02 15:04:05.000"), statusText, day, strings.Join(recipients, ","), subject, attachmentText)
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
