package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	sendModeImmediate = "(即時寄送)"
	sendModeScheduled = "(排程寄送)"
)

// buildMailContent 依日誌是否存在與寄送模式，自動產生郵件主旨與內文。
// 若日誌存在，即時模式主旨用空格分隔，排程模式用冒號分隔；兩種模式的內文格式相同。
// 回傳 attachmentMissing=true 表示無日誌，呼叫端應省略附件。
func buildMailContent(rootDirName, dayStr, logPath, mode string) (subject, body string, attachmentMissing bool) {
	info, err := os.Stat(logPath)
	logMissing := err != nil || info.Size() == 0

	if logMissing {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s: %s 無資料夾異動紀錄", rootDirName, mode, dayStr)
		body = fmt.Sprintf("您好，%s %s 無資料夾異動之紀錄，特此通知。", rootDirName, dayStr)
		return subject, body, true
	}
	// 有日誌：即時模式以空格連接，排程模式以冒號連接
	if mode == sendModeImmediate {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	} else {
		subject = fmt.Sprintf("XWatch %s 資料夾監控日誌%s: %s 已撈出資料，詳如內文", rootDirName, mode, dayStr)
	}
	body = fmt.Sprintf("您好，附件為 %s %s 之資料夾監控日誌壓縮檔，請卓參。", rootDirName, dayStr)
	return subject, body, false
}

// runMailScheduler 依設定的 HH:MM 時間每日寄信。
// 流程：計算下次寄信時間 → 等待 → 寄信 → 重複。
// 不寫任何「排程中」或「心跳」日誌；mail log 只記錄實際寄信結果（ok / fail）。
func runMailScheduler(ctx context.Context, logger *slog.Logger, mail config.MailSettings, rootDir string, now func() time.Time) {
	if now == nil {
		now = time.Now
	}

	loc := loadLocation(mail.Timezone)

	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("每日寄信排程異常 (panic)：%v", r))
		}
	}()

	for {
		next, err := nextSendTime(now(), mail.Schedule, loc)
		if err != nil {
			logger.Error(fmt.Sprintf("每日寄信時間設定無效：%v", err))
			return
		}

		delay := time.Until(next)
		if delay <= 0 {
			// 正常情況下 nextSendTime 保證 next > now()；
			// 若因極短的時間競爭導致 delay <= 0，跳過本次改排下一日。
			logger.Warn("排程時間計算異常（delay <= 0），重新計算下次時間")
			continue
		}

		nextStr := next.In(loc).Format("2006-01-02 15:04")
		logger.Info(fmt.Sprintf("等待每日寄信：%s (%s)，距今 %s", nextStr, loc.String(), delay.Round(time.Second)))

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := sendDailyMail(ctx, logger, mail, rootDir, loc, now(), nil); err != nil {
				logger.Error(fmt.Sprintf("每日寄信失敗：%v", err))
			}
		}
	}
}

func nextSendTime(now time.Time, schedule string, loc *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(schedule)
	if trimmed == "" {
		trimmed = config.DefaultMailSchedule
	}
	parsed, err := time.ParseInLocation("15:04", trimmed, loc)
	if err != nil {
		return time.Time{}, err
	}

	nowLoc := now.In(loc)
	target := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
	if !target.After(nowLoc) {
		target = target.AddDate(0, 0, 1)
	}
	return target, nil
}

func sendDailyMail(ctx context.Context, logger *slog.Logger, mail config.MailSettings, rootDir string, loc *time.Location, now time.Time, sendFn mailer.SendMailFunc) error {
	if len(mail.To) == 0 {
		return errors.New("未設定收件人，無法寄送")
	}

	targetDay := now.In(loc).AddDate(0, 0, -1)
	dayStr := targetDay.Format("2006-01-02")

	logDir := resolveLogDir(mail.LogDir)
	mailLogDir := resolveLogDir(mail.MailLogDir)
	if mailLogDir == "" {
		mailLogDir = logDir
	}

	// 取監控主目錄名稱作為郵件主旨與內文的識別前綴
	rootDirName := filepath.Base(strings.TrimSpace(rootDir))
	if rootDirName == "" || rootDirName == "." {
		rootDirName = "XWatch"
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("watch_%s.log", dayStr))
	subject, body, attachmentMissing := buildMailContent(rootDirName, dayStr, logPath, sendModeScheduled)

	recipients := normalizeList(mail.To)
	if len(recipients) == 0 {
		return errors.New("沒有有效的收件人")
	}

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
	from := strings.TrimSpace(firstNonEmpty(mail.SMTPFrom, user))
	if from == "" {
		from = user
	}

	cfg := mailer.SMTPConfig{
		Host:        host,
		Port:        port,
		Username:    user,
		Password:    pass,
		From:        from,
		To:          recipients,
		DialTimeout: time.Duration(mail.SMTPDialTimeout) * time.Second,
	}
	opts := mailer.ReportOptions{
		LogDir:  logDir,
		Day:     targetDay,
		Subject: subject,
		Body:    body,
	}

	// 重試參數
	maxRetries := mail.SMTPRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryDelay := time.Duration(mail.SMTPRetryDelay) * time.Second
	if retryDelay <= 0 {
		retryDelay = 120 * time.Second
	}

	logger.Info(fmt.Sprintf("開始寄信：day=%s recipients=%s host=%s:%d 最多重試=%d", dayStr, strings.Join(recipients, ","), cfg.Host, cfg.Port, maxRetries))

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			logger.Warn(fmt.Sprintf("寄信重試第 %d/%d 次，等待 %s...", attempt, maxRetries, retryDelay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
			}
		}
		lastErr = mailer.SendGmail(ctx, cfg, opts, sendFn)
		if lastErr == nil {
			break
		}
		logger.Warn(fmt.Sprintf("寄信失敗 (第%d/%d次)：%v", attempt+1, maxRetries+1, lastErr))
	}

	err := lastErr
	attachmentStatus := "attached"
	if attachmentMissing {
		attachmentStatus = "missing"
	}
	if err != nil {
		// 寄信失敗時仍記錄真實附件狀況（已附檔/未附檔），
		// 錯誤原因由「錯誤=」欄位描述，避免附件欄位顯示誤導性的「失敗」
		if logErr := writeMailLog(mailLogDir, now, "fail", dayStr, recipients, subject, attachmentStatus, err.Error()); logErr != nil {
			logger.Error(fmt.Sprintf("寫入 mail log 失敗：%v", logErr))
		}
		logger.Error(fmt.Sprintf("寄信錯誤：%v", err))
		return err
	}
	if logErr := writeMailLog(mailLogDir, now, "ok", dayStr, recipients, subject, attachmentStatus, ""); logErr != nil {
		logger.Error(fmt.Sprintf("寫入 mail log 失敗：%v", logErr))
	}
	logger.Info(fmt.Sprintf("寄信完成：day=%s recipients=%s 附件=%s", dayStr, strings.Join(recipients, ","), attachmentStatus))
	return nil
}

func loadLocation(tz string) *time.Location {
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
	attachmentText := map[string]string{"attached": "已附檔", "missing": "未附檔"}[attachmentStatus]
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
