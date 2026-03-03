package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/paths"
)

// runMailScheduler fires daily mail at configured HH:MM in the configured time zone.
func runMailScheduler(ctx context.Context, logger *slog.Logger, mail config.MailSettings, now func() time.Time) {
	if now == nil {
		now = time.Now
	}

	loc := loadLocation(mail.Timezone)
	logDir := resolveLogDir(mail.LogDir)
	mailLogDir := resolveLogDir(mail.MailLogDir)
	if mailLogDir == "" {
		mailLogDir = logDir
	}

	if hb := mailHeartbeatInterval(); hb > 0 {
		go func() {
			ticker := time.NewTicker(hb)
			defer ticker.Stop()
			logger.Info(fmt.Sprintf("啟動排程心跳：間隔=%s", hb))
			_ = writeMailLog(mailLogDir, time.Now(), "heartbeat", "heartbeat", nil, "mail-scheduler", "none", "")
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					logger.Info(fmt.Sprintf("排程心跳：%s", t.In(loc).Format("2006-01-02 15:04:05")))
					_ = writeMailLog(mailLogDir, t, "heartbeat", "heartbeat", nil, "mail-scheduler", "none", "")
				}
			}
		}()
	}
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
		if delay < 0 {
			delay = 0
		}

		timer := time.NewTimer(delay)
		logger.Info(fmt.Sprintf("已排程每日寄信：%s (%s)", next.In(loc).Format("2006-01-02 15:04"), loc.String()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := sendDailyMail(ctx, logger, mail, loc, now()); err != nil {
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

func sendDailyMail(ctx context.Context, logger *slog.Logger, mail config.MailSettings, loc *time.Location, now time.Time) error {
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

	subject := renderWithDay(mail.Subject, dayStr, fmt.Sprintf("XWatch 前一日監控日誌 %s", dayStr))
	logPath := filepath.Join(logDir, fmt.Sprintf("watch_%s.log", dayStr))
	defaultBody := fmt.Sprintf("附件為 %s 的監控日誌。", dayStr)
	missingBody := fmt.Sprintf("沒有可用的監控日誌（%s），未附檔。", dayStr)
	body, attachmentMissing := prepareBody(logPath, dayStr, mail.Body, defaultBody, missingBody)

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
		Host:     host,
		Port:     port,
		Username: user,
		Password: pass,
		From:     from,
		To:       recipients,
	}
	opts := mailer.ReportOptions{
		LogDir:  logDir,
		Day:     targetDay,
		Subject: subject,
		Body:    body,
	}

	logger.Info(fmt.Sprintf("開始寄信：day=%s recipients=%s host=%s:%d", dayStr, strings.Join(recipients, ","), cfg.Host, cfg.Port))
	err := mailer.SendGmail(ctx, cfg, opts, nil)
	attachmentStatus := "attached"
	if attachmentMissing {
		attachmentStatus = "missing"
	}
	if err != nil {
		if logErr := writeMailLog(mailLogDir, now, "fail", dayStr, recipients, subject, "error", err.Error()); logErr != nil {
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

func mailHeartbeatInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("XWATCH_MAIL_HEARTBEAT_SEC"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
