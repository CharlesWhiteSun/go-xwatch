package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/filecheck"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/mailutil"
	"go-xwatch/internal/paths"
)

// filecheckKey 記錄影響 filecheck 行為的關鍵欄位（含排程郵件），用於偵測設定變更。
type filecheckKey struct {
	enabled     bool
	scanDir     string
	mailEnabled bool
	mailSched   string
	mailTZ      string
	mailTo      string
	smtpHost    string
	smtpPort    int
	smtpUser    string
	smtpPass    string
}

func filecheckKeyFromSettings(s config.Settings) filecheckKey {
	return filecheckKey{
		enabled:     s.Filecheck.Enabled,
		scanDir:     s.Filecheck.ScanDir,
		mailEnabled: s.Filecheck.Mail.IsEnabled(),
		mailSched:   s.Filecheck.Mail.Schedule,
		mailTZ:      s.Filecheck.Mail.Timezone,
		mailTo:      strings.Join(s.Filecheck.Mail.To, ","),
		smtpHost:    s.Mail.SMTPHost,
		smtpPort:    s.Mail.SMTPPort,
		smtpUser:    s.Mail.SMTPUser,
		smtpPass:    s.Mail.SMTPPass,
	}
}

// runFilecheckManager 在背景 goroutine 中管理目錄檔案存在性檢查，支援熱重載設定。
// 服務啟動後若 filecheck 設定變更，不需重啟服務即可自動套用。
func (r *Runner) runFilecheckManager(ctx context.Context, logger *slog.Logger) {
	var mailCancel context.CancelFunc
	nowFn := r.nowFn()

	stopMail := func() {
		if mailCancel != nil {
			mailCancel()
			mailCancel = nil
		}
	}

	startFromSettings := func(s config.Settings) {
		stopMail()

		if !s.Filecheck.Enabled {
			return
		}

		// 若 filecheck mail 已啟用，啟動每日郵件排程
		if s.Filecheck.Mail.IsEnabled() {
			mailCtx, cancelMail := context.WithCancel(ctx)
			mailCancel = cancelMail
			go runFilecheckMailScheduler(mailCtx, logger, s, nowFn, r.dataDirFn())
			logger.Info(fmt.Sprintf("已啟用 filecheck 郵件排程器，排程時間：%s", s.Filecheck.Mail.Schedule))
		}
	}

	// 依啟動時的設定決定是否立即啟動
	startFromSettings(r.Settings)

	curKey := filecheckKeyFromSettings(r.Settings)
	cfgFn := r.configLoadFn()

	reloadTicker := time.NewTicker(r.filecheckReloadInterval())
	defer reloadTicker.Stop()

	for {
		select {
		case <-reloadTicker.C:
			newSettings, err := cfgFn()
			if err != nil {
				continue
			}
			newKey := filecheckKeyFromSettings(newSettings)
			if newKey != curKey {
				startFromSettings(newSettings)
				curKey = newKey
			}
		case <-ctx.Done():
			stopMail()
			return
		}
	}
}

// runFilecheckMailScheduler 依設定的 HH:MM 時間每日寄送 filecheck 報告。
// dataDirFn 提供服務對應的資料目錄（含後綴），用於決定 filecheck log 的寫入位置。
func runFilecheckMailScheduler(ctx context.Context, logger *slog.Logger, s config.Settings, now func() time.Time, dataDirFn func() (string, error)) {
	if now == nil {
		now = time.Now
	}

	loc := mailutil.LoadLocation(s.Filecheck.Mail.Timezone)

	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(fmt.Sprintf("filecheck 每日寄信排程異常 (panic)：%v", rec))
		}
	}()

	for {
		schedule := strings.TrimSpace(s.Filecheck.Mail.Schedule)
		if schedule == "" {
			schedule = config.DefaultFilecheckMailSchedule
		}
		next, err := nextSendTime(now(), schedule, loc)
		if err != nil {
			logger.Error(fmt.Sprintf("filecheck 每日寄信時間設定無效：%v", err))
			return
		}

		delay := time.Until(next)
		if delay <= 0 {
			logger.Warn("filecheck 排程時間計算異常（delay <= 0），重新計算下次時間")
			continue
		}

		nextStr := next.In(loc).Format("2006-01-02 15:04")
		logger.Info(fmt.Sprintf("等待 filecheck 每日寄信：%s (%s)，距今 %s", nextStr, loc.String(), delay.Round(time.Second)))

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := sendFilecheckMail(ctx, logger, s, loc, now(), nil, dataDirFn); err != nil {
				logger.Error(fmt.Sprintf("filecheck 每日寄信失敗：%v", err))
			}
		}
	}
}

// filecheckSendFn 是可注入的文字郵件寄送函式型別（與 mailer.SendTextMail 同簽章），
// 供測試時替換為 fakeSend 以攔截主旨與內文。
type filecheckSendFn func(ctx context.Context, cfg mailer.SMTPConfig, subject, body string, sendFn mailer.SendMailFunc) error

// sendFilecheckMail 主動對前一日各路徑執行檔案存在性檢查，
// 並將結果以純文字郵件寄出。無論任何路徑是否有檔案，皆會寄送報告。
// sendFn 供測試注入，nil 時使用 mailer.SendTextMail。
// dataDirFn 決定 filecheck log 寫入的資料目錄（應含服務後綴）；nil 時以全域 suffix 推導。
func sendFilecheckMail(ctx context.Context, logger *slog.Logger, s config.Settings, loc *time.Location, now time.Time, sendFn filecheckSendFn, dataDirFn func() (string, error)) error {
	if sendFn == nil {
		sendFn = mailer.SendTextMail
	}
	fc := s.Filecheck
	mail := s.Mail

	recipients := mailutil.NormalizeList(fc.Mail.To)
	if len(recipients) == 0 {
		return fmt.Errorf("沒有有效的收件人")
	}

	targetDay := now.In(loc).AddDate(0, 0, -1)
	dayStr := targetDay.Format("2006-01-02")

	// 主動掃描前一日符合 YYYY-MM-DD 格式的檔案（確保時間到必寄，有無均送）
	scanDir := filecheck.ResolveScanDir(s.RootDir, fc.ScanDir)
	files, scanErr := filecheck.ScanForDate(scanDir, targetDay)
	subject, body := filecheck.BuildMailReport(scanDir, files, targetDay, scanErr)

	// 寫入 filecheck log（不影響寄信流程）
	resolvedDataDirFn := dataDirFn
	if resolvedDataDirFn == nil {
		resolvedDataDirFn = func() (string, error) {
			return paths.EnsureDataDirForSuffix(config.GetServiceSuffix())
		}
	}
	if dataDir, err := resolvedDataDirFn(); err != nil {
		logger.Warn(fmt.Sprintf("取得資料目錄失敗（filecheck log 略過）：%v", err))
	} else {
		if err := filecheck.WriteLog(filecheck.DefaultLogDir(dataDir), scanDir, files, targetDay, scanErr, now); err != nil {
			logger.Warn(fmt.Sprintf("寫入 filecheck log 失敗：%v", err))
		}
	}

	cfg := mailutil.ResolveSMTPConfig(mail, recipients)

	logger.Info(fmt.Sprintf("開始寄送 filecheck 報告：day=%s recipients=%s host=%s:%d", dayStr, strings.Join(recipients, ","), cfg.Host, cfg.Port))

	if err := sendFn(ctx, cfg, subject, body, nil); err != nil {
		logger.Error(fmt.Sprintf("filecheck 寄信失敗：%v", err))
		return err
	}

	logger.Info(fmt.Sprintf("filecheck 報告寄送完成：day=%s recipients=%s", dayStr, strings.Join(recipients, ",")))
	return nil
}
