package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/mailutil"
)

// sendModeImmediate / sendModeScheduled 分別代表「即時寄送」與「排程寄送」的模式標籤，
// 用於動態組成郵件主旨，避免重複硬編碼。
const (
	sendModeImmediate = "(即時寄送)"
	sendModeScheduled = "(排程寄送)"
)

// runMailScheduler 依設定的 HH:MM 時間每日寄信。
// 流程：計算下次寄信時間 → 等待 → 寄信 → 重複。
// 不寫任何「排程中」或「心跳」日誌；mail log 只記錄實際寄信結果（ok / fail）。
func runMailScheduler(ctx context.Context, logger *slog.Logger, mail config.MailSettings, rootDir string, now func() time.Time) {
	if now == nil {
		now = time.Now
	}

	loc := mailutil.LoadLocation(mail.Timezone)

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

		delay := next.Sub(now())
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

	logDir := mailutil.ResolveLogDir(mail.LogDir)
	mailLogDir := mailutil.ResolveLogDir(mail.MailLogDir)
	if mailLogDir == "" {
		mailLogDir = logDir
	}

	// 取監控主目錄名稱作為郵件主旨與內文的識別前綴
	rootDirName := filepath.Base(strings.TrimSpace(rootDir))
	if rootDirName == "" || rootDirName == "." {
		rootDirName = "XWatch"
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("watch_%s.log", dayStr))
	subject, body, attachmentMissing := mailutil.BuildMailContent(rootDirName, dayStr, logPath, sendModeScheduled, false)

	recipients := mailutil.NormalizeList(mail.To)
	if len(recipients) == 0 {
		return errors.New("沒有有效的收件人")
	}

	cfg := mailutil.ResolveSMTPConfig(mail, recipients)
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
		if logErr := mailutil.WriteMailLog(mailLogDir, now, "fail", dayStr, recipients, subject, attachmentStatus, err.Error()); logErr != nil {
			logger.Error(fmt.Sprintf("寫入 mail log 失敗：%v", logErr))
		}
		logger.Error(fmt.Sprintf("寄信錯誤：%v", err))
		return err
	}
	if logErr := mailutil.WriteMailLog(mailLogDir, now, "ok", dayStr, recipients, subject, attachmentStatus, ""); logErr != nil {
		logger.Error(fmt.Sprintf("寫入 mail log 失敗：%v", logErr))
	}
	logger.Info(fmt.Sprintf("寄信完成：day=%s recipients=%s 附件=%s", dayStr, strings.Join(recipients, ","), attachmentStatus))
	return nil
}
