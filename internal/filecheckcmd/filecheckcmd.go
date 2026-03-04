// Package filecheckcmd 實作 filecheck CLI 子指令。
// filecheck 在每日排程時間掃描指定目錄，確認是否存在包含前一日日期（YYYY-DD-MM）的檔案，
// 無論有無皆寄送郵件通知指定人員。
package filecheckcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/filecheck"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/paths"
)

// Run 處理 filecheck 子指令。
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "help":
		printHelp()
		return nil
	case "status":
		return status()
	case "start":
		return start()
	case "stop":
		return stop()
	case "set":
		return set(rest)
	case "run":
		return runCheck(rest)
	case "mail":
		return runMail(rest)
	default:
		return fmt.Errorf("filecheck: 未知子指令 %q，請執行 'filecheck help' 查看說明", sub)
	}
}

//  status

func status() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	fc := settings.Filecheck

	fmt.Println("filecheck 功能啟用：", fc.Enabled)

	// 掃描目錄
	resolvedScanDir := filecheck.ResolveScanDir(settings.RootDir, fc.ScanDir)
	if fc.ScanDir == "" {
		fmt.Printf("掃描目錄：%s（預設）\n", resolvedScanDir)
	} else {
		fmt.Printf("掃描目錄：%s\n", resolvedScanDir)
	}

	// log 目錄
	dataDir, _ := paths.EnsureDataDir()
	logDir := filecheck.DefaultLogDir(dataDir)
	fmt.Println("log 目錄：", logDir)

	// Mail 狀態
	m := fc.Mail
	fmt.Println()
	fmt.Println("filecheck mail 啟用：", m.IsEnabled())
	if m.IsEnabled() {
		sched := m.Schedule
		if sched == "" {
			sched = config.DefaultFilecheckMailSchedule + "（預設）"
		}
		fmt.Println("  寄送排程：", sched, "(時區:", m.Timezone+")")
		fmt.Println("  收件人：", strings.Join(m.To, ", "))
	}
	return nil
}

//  start

func start() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	if settings.Filecheck.Enabled {
		fmt.Println("filecheck 功能已在啟用狀態。")
		return nil
	}
	settings.Filecheck.Enabled = true
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck 功能已啟用。服務將於下次設定重載（約 30 秒）後開始執行。")
	return nil
}

//  stop

func stop() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	if !settings.Filecheck.Enabled {
		fmt.Println("filecheck 功能已在停用狀態。")
		return nil
	}
	settings.Filecheck.Enabled = false
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck 功能已停用。")
	return nil
}

//  set

func set(args []string) error {
	fs := flag.NewFlagSet("filecheck set", flag.ContinueOnError)
	scanDirFlag := fs.String("scan-dir", "", "掃描目錄（空白=預設 {rootDir}\\storage\\logs；支援絕對路徑或相對 rootDir 的相對路徑）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}

	changed := false

	if v := strings.TrimSpace(*scanDirFlag); v != "" {
		settings.Filecheck.ScanDir = v
		fmt.Printf("已設定掃描目錄：%s\n", filecheck.ResolveScanDir(settings.RootDir, v))
		changed = true
	}

	if !changed {
		fmt.Println("未指定任何變更，請執行 'filecheck set --help' 查看可用參數。")
		return nil
	}

	return config.Save(settings)
}

//  run（手動立即執行一次掃描）

func runCheck(args []string) error {
	fs := flag.NewFlagSet("filecheck run", flag.ContinueOnError)
	dateFlag := fs.String("date", "", "指定要掃描的日期（YYYY-MM-DD）；省略時使用昨天")
	if err := fs.Parse(args); err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}

	// 預設昨天
	checkDate := time.Now().AddDate(0, 0, -1)
	if d := strings.TrimSpace(*dateFlag); d != "" {
		parsed, err := time.Parse("2006-01-02", d)
		if err != nil {
			return fmt.Errorf("日期格式錯誤，請使用 YYYY-MM-DD：%w", err)
		}
		checkDate = parsed
	}

	scanDir := filecheck.ResolveScanDir(settings.RootDir, settings.Filecheck.ScanDir)
	datePattern := checkDate.Format(filecheck.FileDateFormat)

	fmt.Printf(" filecheck 結果（%s）\n", checkDate.Format("2006-01-02"))
	fmt.Printf("掃描目錄：%s\n", scanDir)
	fmt.Printf("搜尋格式：%s（YYYY-DD-MM）\n\n", datePattern)

	files, scanErr := filecheck.ScanForDate(scanDir, checkDate)
	if scanErr != nil {
		fmt.Printf("[ERROR] 無法讀取目錄：%v\n", scanErr)
	} else if len(files) == 0 {
		fmt.Println("[NOT FOUND] 未找到符合指定日期格式的檔案")
	} else {
		fmt.Printf("[FOUND] 找到 %d 個符合的檔案：\n", len(files))
		for _, f := range files {
			fmt.Printf("  - %s\n", f)
		}
	}

	// 寫入 log
	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告：取得資料目錄失敗，log 略過：%v\n", err)
		return nil
	}
	logDir := filecheck.DefaultLogDir(dataDir)
	if err := filecheck.WriteLog(logDir, scanDir, files, checkDate, scanErr, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "警告：寫入 log 失敗：%v\n", err)
	} else {
		fmt.Printf("\n已寫入 log：%s\n", filepath.Join(logDir, "filecheck_"+time.Now().Format("2006-01-02")+".log"))
	}
	return nil
}

//  mail 子指令群

func runMail(args []string) error {
	if len(args) == 0 {
		printMailUsage()
		return nil
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "help":
		printMailHelp()
		return nil
	case "status":
		return mailStatus()
	case "enable":
		return mailEnable(rest)
	case "disable":
		return mailDisable()
	case "set":
		return mailSet(rest)
	case "add-to":
		return mailAddTo(rest)
	case "send":
		return mailSend(rest, nil)
	default:
		return fmt.Errorf("filecheck mail: 未知子指令 %q，請執行 'filecheck mail help' 查看說明", sub)
	}
}

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
	fmt.Printf("  SMTP 主機：%s:%d\n", settings.Mail.SMTPHost, settings.Mail.SMTPPort)
	fmt.Println("  SMTP 使用者：", settings.Mail.SMTPUser)
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
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck mail 已啟用。")
	return nil
}

func mailDisable() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.Filecheck.Mail.Enabled = config.BoolPtr(false)
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("filecheck mail 已停用。")
	return nil
}

func mailSet(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	m := settings.Filecheck.Mail
	if err := applyMailFlags(&m, args); err != nil {
		return err
	}
	settings.Filecheck.Mail = m
	return config.Save(settings)
}

func applyMailFlags(m *config.FilecheckMailSettings, args []string) error {
	fs := flag.NewFlagSet("filecheck mail set", flag.ContinueOnError)
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

// mailSend 主動掃描前一日符合 YYYY-DD-MM 格式的檔案，並立即寄送結果報告。
// 無論有無符合檔案，皆會寄送；sendFn 供測試注入，nil 時使用 mailer.SendTextMail。
func mailSend(args []string, sendFn func(ctx context.Context, cfg mailer.SMTPConfig, subject, body string, fn mailer.SendMailFunc) error) error {
	if sendFn == nil {
		sendFn = mailer.SendTextMail
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

	// 掃描前一日符合 YYYY-DD-MM 格式的檔案（有無皆寄）
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

	if err := sendFn(context.Background(), cfg, subject, body, nil); err != nil {
		return fmt.Errorf("寄送失敗：%w", err)
	}
	fmt.Printf("filecheck 報告已寄送至 %s。\n", strings.Join(recipients, ", "))
	return nil
}

//  help 輸出

func printUsage() {
	fmt.Println("用法：filecheck <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：help | status | start | stop | set | run | mail")
	fmt.Println("執行 'filecheck help' 查看完整說明")
}

func printHelp() {
	fmt.Println()
	fmt.Println("filecheck  每日排程掃描指定目錄的檔案存在性")
	fmt.Println()
	fmt.Println("說明：")
	fmt.Println("  在每日排程時間（預設早上 10:00），掃描指定目錄，")
	fmt.Println("  確認目錄內是否存在包含前一日日期（YYYY-DD-MM 格式）的檔案。")
	fmt.Println("  無論找到或未找到，皆以郵件通知指定人員。")
	fmt.Println()
	fmt.Println("  預設掃描路徑：")
	fmt.Println("    {rootDir}\\storage\\logs\\")
	fmt.Println("  搜尋格式（YYYY-DD-MM）範例：前一日 2026-03-04  搜尋 '2026-04-03'")
	fmt.Println("  可透過 set --scan-dir 指定自訂目錄。")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  filecheck <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  status\t顯示目前設定與 log 目錄")
	fmt.Fprintln(w, "  start\t啟用 filecheck（需服務已安裝）")
	fmt.Fprintln(w, "  stop\t停用 filecheck")
	fmt.Fprintln(w, "  set [flags]\t修改掃描設定")
	fmt.Fprintln(w, "  run [--date]\t立即執行一次掃描並輸出結果")
	fmt.Fprintln(w, "  mail <subcommand>\t管理 filecheck 郵件通知（獨立於 watch log 郵件）")
	fmt.Fprintln(w, "  help\t顯示本說明")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("set 參數：")
	w2 := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w2, "  --scan-dir PATH\t掃描目錄（空白=使用預設 {rootDir}\\storage\\logs）")
	_ = w2.Flush()
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 啟用 filecheck")
	fmt.Println("  filecheck start")
	fmt.Println()
	fmt.Println("  # 設定自訂掃描目錄（絕對路徑）")
	fmt.Println(`  filecheck set --scan-dir "D:\data\logs"`)
	fmt.Println()
	fmt.Println("  # 設定相對 rootDir 的掃描目錄")
	fmt.Println(`  filecheck set --scan-dir "storage\archives"`)
	fmt.Println()
	fmt.Println("  # 立即執行昨天的掃描")
	fmt.Println("  filecheck run")
	fmt.Println()
	fmt.Println("  # 立即執行指定日期的掃描")
	fmt.Println("  filecheck run --date 2026-03-01")
	fmt.Println()
	fmt.Println("注意：")
	fmt.Println("  - filecheck start 需要服務已透過 'init --install-service' 安裝")
	fmt.Println("  - filecheck run 可在服務未啟動下獨立執行")
	fmt.Println("  - filecheck mail 的郵件通知與 mail（watch log）完全獨立")
}

func printMailUsage() {
	fmt.Println("用法：filecheck mail <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：help | status | enable | disable | set | add-to | send")
	fmt.Println("執行 'filecheck mail help' 查看完整說明")
}

func printMailHelp() {
	fmt.Println()
	fmt.Println("filecheck mail  管理 filecheck 目錄掃描報告的郵件通知")
	fmt.Println()
	fmt.Println("說明：")
	fmt.Println("  此郵件通知功能獨立於監控 watch log 的 mail 指令，")
	fmt.Println("  在每日指定時間（預設 10:00）掃描目錄並寄送結果。")
	fmt.Println("  SMTP 連線設定繼承自 'mail' 指令的 SMTP 組態（共用 SMTP 伺服器）。")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  filecheck mail <subcommand> [flags]")
	fmt.Println()
	fmt.Println("子指令：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  help\t顯示本說明")
	fmt.Fprintln(w, "  status\t顯示目前郵件設定")
	fmt.Fprintln(w, "  enable [flags]\t啟用排程郵件並設定參數")
	fmt.Fprintln(w, "  disable\t停用排程郵件")
	fmt.Fprintln(w, "  set [flags]\t修改郵件設定（不改變啟用狀態）")
	fmt.Fprintln(w, "  add-to <email>\t追加收件人（不覆蓋現有清單）")
	fmt.Fprintln(w, "  send\t立即掃描並寄送指定日期的 filecheck 報告")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("enable / set 共用參數：")
	w2 := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w2, "  --to <email[,email,...]>\t覆蓋收件人清單（逗號分隔多個地址）")
	fmt.Fprintln(w2, "  --schedule HH:MM\t每日寄送時間（預設 10:00）")
	fmt.Fprintln(w2, "  --tz TIMEZONE\t時區（預設 Asia/Taipei）")
	_ = w2.Flush()
	fmt.Println()
	fmt.Println("add-to 用法（追加收件人，不覆蓋）：")
	fmt.Println("  filecheck mail add-to user@example.com")
	fmt.Println("  filecheck mail add-to --to user@example.com")
	fmt.Println()
	fmt.Println("send 參數：")
	w3 := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w3, "  --day YYYY-MM-DD\t掃描哪一天（預設昨天）")
	fmt.Fprintln(w3, "  --to <email,...>\t臨時覆蓋收件人")
	_ = w3.Flush()
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 啟用 filecheck 郵件，設定收件人與時間")
	fmt.Println("  filecheck mail enable --to admin@example.com --schedule 10:00")
	fmt.Println()
	fmt.Println("  # 追加收件人")
	fmt.Println("  filecheck mail add-to another@example.com")
	fmt.Println()
	fmt.Println("  # 停用 filecheck 郵件")
	fmt.Println("  filecheck mail disable")
	fmt.Println()
	fmt.Println("  # 立即掃描昨天並寄送 filecheck 報告")
	fmt.Println("  filecheck mail send")
	fmt.Println()
	fmt.Println("  # 掃描並寄送指定日期的報告")
	fmt.Println("  filecheck mail send --day 2026-03-01")
	fmt.Println()
	fmt.Println("與 mail 指令的差異：")
	fmt.Println("  mail send            寄送 xwatch-watch-logs（檔案系統監控日誌，含 zip 附件）")
	fmt.Println("  filecheck mail send  寄送目錄檔案存在性結果（YYYY-DD-MM 格式比對，純文字）")
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
