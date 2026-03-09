// Package filecheckcmd 實作 filecheck CLI 子指令。
// filecheck 在每日排程時間揃描指定目錄，確認是否存在前一日對應的 laravel-{YYYY-MM-DD}.log 檔案，
// 無論有無皆寄送郵件通知指定人員。
package filecheckcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/filecheck"
	"go-xwatch/internal/mailer"
)

// Run 展開 filecheck 子指令，委派至 Runner.Run（能後相容包裝）。
// 指令派發、sender 喊入均由 filecheckCmdRunner.Run 負責。
func Run(args []string) error {
	return Runner.Run(args)
}

//  各子指令實作

func mailStatus() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	m := settings.Filecheck.Mail
	fmt.Println("filecheck mail 啟用：", m.IsEnabled())
	sched := m.Schedule
	if sched == "" {
		sched = config.DefaultFilecheckMailSchedule + "（預設）"
	}
	fmt.Println("寄送排程：", sched, "(時區:", m.Timezone+")")
	fmt.Println("收件人：", strings.Join(m.To, ", "))
	fmt.Println()
	fmt.Println("（SMTP 設定繼承自 mail 指令設定）")
	fmt.Printf("  SMTP Host:Port: %s:%d\n", settings.Mail.SMTPHost, settings.Mail.SMTPPort)
	fmt.Println("  SMTP Account: ", settings.Mail.SMTPUser)
	return nil
}

// mailAddTo 追加收件人至 filecheck mail 通知清單（不覆蓋），重複地址自動去除。
func mailAddTo(args []string) error {
	fs := flag.NewFlagSet("filecheck mail add-to", flag.ContinueOnError)
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
		return errors.New("請提供至少一個收件人，例如：filecheck mail add-to a@example.com")
	}
	newAddrs := splitList(rawTo)
	for _, a := range newAddrs {
		if !looksLikeEmail(a) {
			return fmt.Errorf("無效的電子郵件地址：%q（應包含 @ 符號且格式正確）", a)
		}
	}
	settings, err := config.Load()
	if err != nil {
		return err
	}
	existing := settings.Filecheck.Mail.To
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
	settings.Filecheck.Mail.To = existing
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Printf("已追加 %d 位收件人，目前共 %d 位：%s\n", added, len(existing), strings.Join(existing, ", "))
	return nil
}

func mailEnable(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	m := settings.Filecheck.Mail
	m.Enabled = config.BoolPtr(true)
	if err := applyMailFlags(&m, args); err != nil {
		return err
	}
	settings.Filecheck.Mail = m
	// 同步啟用外層 Filecheck.Enabled，確保後端排程器可正常運作
	settings.Filecheck.Enabled = true
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck 郵件通知已啟用。服務將於下次設定重載（約 30 秒）後開始執行。")
	return nil
}

func mailDisable() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.Filecheck.Mail.Enabled = config.BoolPtr(false)
	// 同步停用外層 Filecheck.Enabled
	settings.Filecheck.Enabled = false
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck 郵件通知已停用。")
	return nil
}

func applyMailFlags(m *config.FilecheckMailSettings, args []string) error {
	fs := flag.NewFlagSet("filecheck enable", flag.ContinueOnError)
	toFlag := fs.String("to", "", "以逗號分隔的收件人（覆蓋現有清單；追加請用 add-to）")
	scheduleFlag := fs.String("schedule", "", "每日寄送時間 HH:MM")
	tzFlag := fs.String("tz", "", "時區（預設 Asia/Taipei）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if v := strings.TrimSpace(*toFlag); v != "" {
		addrs := splitList(v)
		for _, a := range addrs {
			if !looksLikeEmail(a) {
				return fmt.Errorf("無效的電子郵件地址：%q（應包含 @ 符號且格式正確）", a)
			}
		}
		m.To = addrs
	}
	if v := strings.TrimSpace(*scheduleFlag); v != "" {
		if _, err := time.Parse("15:04", v); err != nil {
			return fmt.Errorf("schedule 需為 HH:MM：%w", err)
		}
		m.Schedule = v
	}
	if v := strings.TrimSpace(*tzFlag); v != "" {
		m.Timezone = v
	}
	return nil
}

// looksLikeEmail 檢查字串是否包含 @ 且前後均非空，作為基本格式驗證。
func looksLikeEmail(s string) bool {
	at := strings.Index(s, "@")
	return at > 0 && at < len(s)-1 && !strings.ContainsAny(s, " []()<>")
}

// mailSendWithSender 主動揃描前一日的 laravel-{YYYY-MM-DD}.log 檔案，並立即寄送結果報告。
// 無論有無符合檔案，皮會寄送。
// 以 TextMailSender 介面取代函式型注入（ISP），sender 為 nil 時自動使用 realTextMailSender。
func mailSendWithSender(args []string, sender TextMailSender) error {
	if sender == nil {
		sender = realTextMailSender{}
	}

	fs := flag.NewFlagSet("filecheck mail send", flag.ContinueOnError)
	dayFlag := fs.String("day", "", "掃描哪一天（YYYY-MM-DD；預設昨天）")
	toFlag := fs.String("to", "", "臨時覆蓋收件人（逗號分隔）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}

	// 決定目標日期（預設昨天）
	targetDay := time.Now().AddDate(0, 0, -1)
	if d := strings.TrimSpace(*dayFlag); d != "" {
		parsed, err := time.Parse("2006-01-02", d)
		if err != nil {
			return fmt.Errorf("日期格式錯誤，請使用 YYYY-MM-DD：%w", err)
		}
		targetDay = parsed
	}

	// 決定收件人
	recipients := settings.Filecheck.Mail.To
	if v := strings.TrimSpace(*toFlag); v != "" {
		list := splitList(v)
		for _, a := range list {
			if !looksLikeEmail(a) {
				return fmt.Errorf("無效的電子郵件地址：%q（應包含 @ 符號且格式正確）", a)
			}
		}
		recipients = list
	}
	if len(recipients) == 0 {
		return errors.New("未設定收件人，請先執行 'filecheck mail enable --to <email>' 或使用 --to 指定")
	}

	// 揃描前一日的 laravel-{YYYY-MM-DD}.log 檔案（有無均寄）
	scanDir := filecheck.ResolveScanDir(settings.RootDir, settings.Filecheck.ScanDir)
	files, scanErr := filecheck.ScanForDate(scanDir, targetDay)
	subject, body := filecheck.BuildMailReport(scanDir, files, targetDay, scanErr)

	// 組裝 SMTP 設定（繼承 mail 設定）
	smtpCfg := settings.Mail
	cfg := mailer.SMTPConfig{
		Host:        smtpCfg.SMTPHost,
		Port:        smtpCfg.SMTPPort,
		Username:    smtpCfg.SMTPUser,
		Password:    smtpCfg.SMTPPass,
		From:        smtpCfg.SMTPFrom,
		To:          recipients,
		DialTimeout: time.Duration(smtpCfg.SMTPDialTimeout) * time.Second,
	}

	if err := sender.SendTextMail(context.Background(), cfg, subject, body, nil); err != nil {
		return fmt.Errorf("寄送失敗：%w", err)
	}
	fmt.Printf("filecheck 報告已寄送至 %s。\n", strings.Join(recipients, ", "))
	return nil
}

//  help 輸出

func printUsage() {
	fmt.Println("用法：filecheck <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：help | status | enable | disable | add-to | send")
	fmt.Println("執行 'filecheck help' 查看完整說明")
}

func printHelp() {
	fmt.Println()
	fmt.Println("filecheck  管理 filecheck 目錄掃描報告的郵件通知")
	fmt.Println()
	fmt.Println("說明：")
	fmt.Println("  在每日指定時間（預設 10:00）掃描目錄，確認是否存在前一日對應的")
	fmt.Printf("  laravel-{YYYY-MM-DD}.log 檔案（例：%s），無論找到或未找到皆以郵件通知指定人員。\n",
		filecheck.TargetFileName(time.Now().AddDate(0, 0, -1)))
	fmt.Println("  SMTP 連線設定繼承自 'mail' 指令的 SMTP 組態（共用 SMTP 伺服器）。")
	fmt.Println("  此郵件通知功能獨立於監控 watch log 的 mail 指令。")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  filecheck <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  help\t顯示本說明")
	fmt.Fprintln(w, "  status\t顯示目前郵件設定")
	fmt.Fprintln(w, "  enable [flags]\t啟用排程郵件並設定參數（需服務已安裝）")
	fmt.Fprintln(w, "  disable\t停用排程郵件")
	fmt.Fprintln(w, "  add-to <email>\t追加收件人（不覆蓋現有清單）")
	fmt.Fprintln(w, "  send\t立即掃描並寄送指定日期的 filecheck 報告")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("enable 參數：")
	w2 := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w2, "  --to <email[,email,...]>\t覆蓋收件人清單（逗號分隔多個地址）")
	fmt.Fprintln(w2, "  --schedule HH:MM\t每日寄送時間（預設 10:00）")
	fmt.Fprintln(w2, "  --tz TIMEZONE\t時區（預設 Asia/Taipei）")
	_ = w2.Flush()
	fmt.Println()
	fmt.Println("add-to 用法（追加收件人，不覆蓋）：")
	fmt.Println("  filecheck add-to user@example.com")
	fmt.Println("  filecheck add-to --to user@example.com")
	fmt.Println()
	fmt.Println("send 參數：")
	w3 := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w3, "  --day YYYY-MM-DD\t掃描哪一天（預設昨天）")
	fmt.Fprintln(w3, "  --to <email,...>\t臨時覆蓋收件人")
	_ = w3.Flush()
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 啟用 filecheck 郵件，設定收件人與時間")
	fmt.Println("  filecheck enable --to admin@example.com --schedule 10:00")
	fmt.Println()
	fmt.Println("  # 追加收件人")
	fmt.Println("  filecheck add-to another@example.com")
	fmt.Println()
	fmt.Println("  # 停用 filecheck 郵件")
	fmt.Println("  filecheck disable")
	fmt.Println()
	fmt.Println("  # 立即掃描昨天並寄送 filecheck 報告")
	fmt.Println("  filecheck send")
	fmt.Println()
	fmt.Println("  # 掃描並寄送指定日期的報告")
	fmt.Println("  filecheck send --day 2026-03-01")
	fmt.Println()
	fmt.Println("與 mail 指令的差異：")
	fmt.Println("  mail send       寄送 xwatch-watch-logs（檔案系統監控日誌，含 zip 附件）")
	fmt.Println("  filecheck send  掃描 laravel-{YYYY-MM-DD}.log 存在性並寄送報告（純文字）")
}

//  工具函式

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
