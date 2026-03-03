package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
	"unsafe"

	"go-xwatch/internal/cli"
	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/service"
	"go-xwatch/internal/watcher"

	"golang.org/x/sys/windows"
)

const serviceName = "GoXWatch"

var version = "dev"

const elevationPrompt = "偵測到目前非系統管理員，是否重新以系統管理員執行？(Y/n): "

var opsLog struct {
	mu     sync.Mutex
	logger *slog.Logger
	file   *os.File
	date   string
	err    error
}

var suppressEmptyHelp bool

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "this program currently supports Windows service mode only")
		os.Exit(1)
	}

	if service.IsWindowsServiceProcess() {
		if err := runAsService(); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			logOp("service error", "err", err)
			os.Exit(1)
		}
		return
	}

	logOp("cli start", "version", version, "pid", os.Getpid(), "args", os.Args[1:])

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logOp("cli signal", "signal", sig.String())
		os.Exit(130)
	}()

	// 嘗試在互動模式下提示並自動提升權限，以便順利註冊/控制服務。
	decision, elevErr := evaluateElevation(os.Getenv("XWATCH_NO_ELEVATE") == "1", isInteractiveConsole(), isElevated(), askYesNo, relaunchElevated, os.Args[1:])
	if elevErr != nil {
		fmt.Fprintln(os.Stderr, "無法自動提升權限，請改以系統管理員執行：", elevErr)
	}
	switch decision {
	case "exit":
		fmt.Println("已取消提升權限，3 秒後自動退出...")
		logOp("cli exit", "code", 1, "reason", "user_decline_elevate")
		time.Sleep(3 * time.Second)
		os.Exit(1)
	case "relaunch":
		return
	}

	exitCode := 0
	for {
		if err := runInteractive(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			logOp("command error", "err", err)
			if isAccessDenied(err) {
				fmt.Fprintln(os.Stderr, "請以系統管理員身分執行，或用管理員權限的 PowerShell 開啟此程式。")
			}
			exitCode = 1
		} else {
			logOp("command ok")
		}

		action, cmdLine := promptNextAction()
		switch action {
		case "exit":
			logOp("cli exit", "code", exitCode)
			os.Exit(exitCode)
		case "help":
			fmt.Println()
			printUsage()
			os.Args = []string{os.Args[0]}
			continue
		case "command":
			if cmdLine == "" {
				os.Args = []string{os.Args[0]}
			} else {
				os.Args = append([]string{os.Args[0]}, strings.Fields(cmdLine)...)
			}
			exitCode = 0
			continue
		case "continue":
			os.Args = []string{os.Args[0]}
			exitCode = 0
			continue
		}

		// 回到簡易 CLI 主畫面，讓使用者輸入下一個指令。
		fmt.Println()
		fmt.Print("> ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			// 空白輸入時留在選單，下一輪不顯示 help，並清空命令狀態避免重跑上一個指令
			suppressEmptyHelp = true
			os.Args = []string{os.Args[0]}
			continue
		}
		lower := strings.ToLower(line)
		if lower == "e" || lower == "exit" {
			logOp("cli exit", "code", exitCode)
			os.Exit(exitCode)
		} else if lower == "h" || lower == "help" {
			fmt.Println()
			printUsage()
			os.Args = []string{os.Args[0]}
			exitCode = 0
			continue
		} else {
			os.Args = append([]string{os.Args[0]}, strings.Fields(line)...)
		}
		// 重置 exitCode 為下一輪；若下一輪成功會保持 0。
		exitCode = 0
	}
}

func getOpsLogger(now time.Time) (*slog.Logger, error) {
	opsLog.mu.Lock()
	defer opsLog.mu.Unlock()

	if opsLog.logger != nil && opsLog.date == now.In(time.Local).Format("2006-01-02") && opsLog.err == nil {
		return opsLog.logger, nil
	}

	if opsLog.file != nil {
		_ = opsLog.file.Close()
		opsLog.file = nil
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		opsLog.err = err
		return nil, err
	}
	logDir := filepath.Join(dataDir, "xwatch-ops-logs")
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		opsLog.err = mkErr
		return nil, mkErr
	}
	day := now.In(time.Local).Format("2006-01-02")
	logPath := filepath.Join(logDir, fmt.Sprintf("operations_%s.log", day))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		opsLog.err = err
		return nil, err
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Value = slog.StringValue(a.Value.Time().In(time.Local).Format("2006-01-02 15:04:05"))
			case slog.LevelKey:
				a.Value = slog.StringValue(strings.ToUpper(a.Value.String()))
			}
			return a
		},
	})
	opsLog.logger = slog.New(handler)
	opsLog.file = f
	opsLog.date = day
	opsLog.err = nil
	return opsLog.logger, nil
}

func logOp(msg string, args ...any) {
	logger, err := getOpsLogger(time.Now())
	if err != nil || logger == nil {
		return
	}
	logger.Info(formatOpsMessage(msg, args...))
}

// formatOpsMessage converts structured args into a readable Traditional Chinese line.
func formatOpsMessage(msg string, args ...any) string {
	kv := make(map[string]any)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		kv[key] = args[i+1]
	}

	switch msg {
	case "cli start":
		return fmt.Sprintf("CLI 啟動；版本=%v；PID=%v；參數=%v", kv["version"], kv["pid"], kv["args"])
	case "command":
		cmd := kv["cmd"]
		return fmt.Sprintf("收到指令：%v；參數=%v", cmd, kv["args"])
	case "command ok":
		return "指令已完成"
	case "command error":
		return fmt.Sprintf("指令失敗：%v", kv["err"])
	case "cli exit":
		if reason, ok := kv["reason"]; ok {
			return fmt.Sprintf("CLI 結束；代碼=%v；原因=%v", kv["code"], reason)
		}
		return fmt.Sprintf("CLI 結束；代碼=%v", kv["code"])
	case "service error":
		return fmt.Sprintf("服務錯誤：%v", kv["err"])
	case "cli signal":
		return fmt.Sprintf("收到訊號：%v；即將結束", kv["signal"])
	default:
		if len(args) == 0 {
			return msg
		}
		return fmt.Sprintf("%s；內容=%v", msg, kv)
	}
}

func runInteractive() error {
	command := ""
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = strings.ToLower(args[0])
		args = args[1:]
	}
	logOp("command", "cmd", command, "args", args)

	if len(os.Args) <= 1 || command == "" {
		if suppressEmptyHelp {
			suppressEmptyHelp = false
			return nil
		}
		printUsage()
		return nil
	}

	reg := buildCommandRegistry()
	cmd, ok := reg.Get(command)
	if !ok {
		return fmt.Errorf("unknown command: %s", command)
	}
	return cmd.Run(args)
}

func buildCommandRegistry() *cli.Registry {
	reg := cli.NewRegistry()

	reg.Register(cli.CommandFunc{NameStr: "help", Fn: func(_ []string) error {
		printUsage()
		return nil
	}})

	reg.Register(cli.CommandFunc{NameStr: "init", Fn: func(args []string) error {
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
		installFlag := fs.Bool("install-service", false, "install and start Windows service after init")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return initAndExit(*rootFlag, *installFlag)
	}})

	reg.Register(cli.CommandFunc{NameStr: "status", Fn: func(_ []string) error {
		return printStatus()
	}})

	reg.Register(cli.CommandFunc{NameStr: "start", Fn: func(_ []string) error {
		if err := service.Start(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已啟動。")
		return nil
	}})

	reg.Register(cli.CommandFunc{NameStr: "stop", Fn: func(_ []string) error {
		if err := service.Stop(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已停止。")
		return nil
	}})

	reg.Register(cli.CommandFunc{NameStr: "uninstall", Fn: func(_ []string) error {
		if err := service.Uninstall(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已移除。")
		return nil
	}})

	cleanupFn := func([]string) error { return stopAndUninstall() }
	reg.Register(cli.CommandFunc{NameStr: "cleanup", Fn: cleanupFn})
	reg.Register(cli.CommandFunc{NameStr: "remove", Fn: cleanupFn})

	clearFn := func([]string) error { return clearJournal() }
	reg.Register(cli.CommandFunc{NameStr: "clear", Fn: clearFn})
	reg.Register(cli.CommandFunc{NameStr: "purge", Fn: clearFn})
	reg.Register(cli.CommandFunc{NameStr: "wipe", Fn: clearFn})

	reg.Register(cli.CommandFunc{NameStr: "export", Fn: func(args []string) error {
		fs := flag.NewFlagSet("export", flag.ContinueOnError)
		sinceFlag := fs.String("since", "", "RFC3339 timestamp filter for export")
		untilFlag := fs.String("until", "", "optional RFC3339 upper bound for export")
		limitFlag := fs.Int("limit", 1000, "max rows for export")
		formatFlag := fs.String("format", "json", "export format: json|jsonl|text")
		allFlag := fs.Bool("all", false, "export all entries ignoring time filters")
		bomFlag := fs.Bool("bom", false, "prepend UTF-8 BOM for Windows editors")
		outFlag := fs.String("out", "", "output file path (use '-' for stdout; default: %ProgramData%/go-xwatch)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return exportJournal(*sinceFlag, *untilFlag, *limitFlag, *formatFlag, *allFlag, *bomFlag, *outFlag)
	}})

	reg.Register(cli.CommandFunc{NameStr: "daily", Fn: func(args []string) error {
		return runDaily(args)
	}})

	reg.Register(cli.CommandFunc{NameStr: "run", Fn: func(args []string) error {
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		root, err := resolveRoot(*rootFlag)
		if err != nil {
			return err
		}
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		logger.Info("running in console mode", "root", root)
		return watcher.Run(nil, root, logger, nil)
	}})

	return reg
}

func initAndExit(rootArg string, installService bool) error {
	fmt.Println("[1/3] 準備初始化...")
	root, err := resolveRoot(rootArg)
	if err != nil {
		return err
	}
	fmt.Println("[2/3] 寫入設定檔...")
	if err := config.Save(config.Settings{RootDir: root}); err != nil {
		return err
	}

	if installService {
		fmt.Println("[3/3] 註冊或更新 Windows 服務並啟動...")
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return err
		}

		if err := service.InstallOrUpdate(serviceName, exePath, "--service"); err != nil {
			return fmt.Errorf("無法註冊服務: %w", err)
		}

		if err := service.Start(serviceName); err != nil && !errors.Is(err, service.ErrAlreadyRunning) {
			return fmt.Errorf("無法啟動服務: %w", err)
		}
	} else {
		fmt.Println("[3/3] 已完成設定，未註冊/啟動服務。需註冊請改用 --install-service。")
	}

	fmt.Println("完成。")
	return nil
}

func printStatus() error {
	status, err := service.Status(serviceName)
	if err != nil {
		return err
	}
	fmt.Println("service:", serviceName)
	fmt.Println("status:", status)

	settings, err := config.Load()
	if err == nil {
		fmt.Println("root:", settings.RootDir)
		fmt.Println("daily csv:", settings.DailyCSVEnabled)
		if settings.DailyCSVEnabled {
			dir := settings.DailyCSVDir
			if dir == "" {
				dir = filepath.Join(os.Getenv("ProgramData"), "go-xwatch", "daily")
			}
			fmt.Println("daily dir:", dir)
		}
	} else {
		fmt.Println("root: (讀取設定失敗)")
	}

	dataDir, derr := paths.EnsureDataDir()
	if derr == nil {
		fmt.Println("data dir:", dataDir)
		journalPath := filepath.Join(dataDir, "journal.db")
		fmt.Println("journal:", journalPath)
		if key, kerr := crypto.LoadOrCreateKey(filepath.Join(dataDir, "key.bin"), 32); kerr == nil {
			if j, jerr := journal.Open(journalPath, key); jerr == nil {
				if n, cerr := j.Count(context.Background()); cerr == nil {
					fmt.Println("journal entries:", n)
				}
				_ = j.Close()
			}
		}
	} else {
		fmt.Println("data dir: (無法取得)")
	}
	return nil
}

func stopAndUninstall() error {
	// 先嘗試停止服務；若已停止或不存在則繼續。
	if err := service.Stop(serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("無法停止服務: %w", err)
	}

	if err := service.Uninstall(serviceName); err != nil && !isServiceMissing(err) {
		return fmt.Errorf("無法移除服務: %w", err)
	}

	fmt.Println("服務已停止並移除。")
	return nil
}

func clearJournal() error {
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		// 嘗試先停止服務避免檔案被占用。
		if err := service.Stop(serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		return err
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	for _, p := range []string{journalPath, journalPath + "-wal", journalPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("無法刪除 %s: %w", filepath.Base(p), err)
		}
	}

	// 重新建立空白資料庫，確保格式正確。
	j, err := journal.Open(journalPath, key)
	if err != nil {
		return fmt.Errorf("重建日誌資料庫失敗: %w", err)
	}
	_ = j.Close()

	fmt.Println("資料庫事件紀錄已清除。")
	return nil
}

func printUsage() {
	fmt.Println()
	fmt.Println("  XWATCH")
	fmt.Printf("   - version: %s\n", version)
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("help 指令列表:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  init [-root PATH] [--install-service]\t初始化設定；加上 --install-service 會註冊並啟動服務")
	fmt.Fprintln(w, "  status\t顯示服務狀態、路徑與事件筆數")
	fmt.Fprintln(w, "  start | stop\t啟動或停止服務")
	fmt.Fprintln(w, "  uninstall\t移除服務")
	fmt.Fprintln(w, "  cleanup | remove\t停止並移除服務")
	fmt.Fprintln(w, "  clear | purge | wipe\t清空事件資料庫 (需權限)")
	fmt.Fprintln(w, "  export [flags]\t匯出事件，flags:")
	fmt.Fprintln(w, "    --since RFC3339\t起始時間 (預設不限)")
	fmt.Fprintln(w, "    --until RFC3339\t結束時間 (預設不限)")
	fmt.Fprintln(w, "    --limit N\t最大筆數 (預設 1000)")
	fmt.Fprintln(w, "    --all\t匯出所有事件，忽略時間條件")
	fmt.Fprintln(w, "    --format json|jsonl|text\t匯出格式 (預設 json)")
	fmt.Fprintln(w, "    --bom\t輸出 UTF-8 BOM 以供記事本辨識中文")
	fmt.Fprintln(w, "    --out PATH\t輸出檔路徑，'-' 為 stdout，預設 %ProgramData%/go-xwatch")
	fmt.Fprintln(w, "  daily <subcommand> [flags]\t管理每日輸出 (csv/json/email 等)")
	fmt.Fprintln(w, "  run [-root PATH]\t前景模式執行，不作為服務")
	_ = w.Flush()
	fmt.Println("============================================================")
}

func resolveRoot(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
	}

	settings, err := config.Load()
	if err == nil && settings.RootDir != "" {
		return resolveAndEnsureDir(settings.RootDir, "根目錄")
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveAndEnsureDir(filepath.Dir(exePath), "根目錄")
}

func runAsService() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	if settings.RootDir == "" {
		return errors.New("empty root dir in config")
	}
	return service.Run(serviceName, settings)
}

func runDaily(args []string) error {
	if len(args) == 0 {
		printDailyUsage()
		return nil
	}

	sub := strings.ToLower(args[0])
	args = args[1:]

	settings, err := config.Load()
	if err != nil {
		return err
	}

	defaultDir := filepath.Join(os.Getenv("ProgramData"), "go-xwatch", "daily")

	parseFormat := func(fs *flag.FlagSet) *string {
		return fs.String("format", "csv", "daily output format: csv|json|email (目前僅支援 csv)")
	}

	switch sub {
	case "status":
		dir := settings.DailyCSVDir
		if dir == "" {
			dir = defaultDir
		}
		fmt.Println("每日輸出格式: csv")
		fmt.Println("csv 已啟用:", settings.DailyCSVEnabled)
		fmt.Println("csv 目錄:", dir)
		return nil

	case "enable":
		fs := flag.NewFlagSet("daily enable", flag.ContinueOnError)
		dirFlag := fs.String("dir", "", "輸出目錄 (預設 %ProgramData%/go-xwatch/daily)")
		formatFlag := parseFormat(fs)
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *formatFlag != "csv" {
			return fmt.Errorf("目前僅支援 csv")
		}
		dir := settings.DailyCSVDir
		if *dirFlag != "" {
			dir = *dirFlag
		}
		if dir == "" {
			dir = defaultDir
		}
		resolvedDir, err := resolveAndEnsureDir(dir, "每日 CSV 目錄")
		if err != nil {
			return err
		}
		settings.DailyCSVEnabled = true
		settings.DailyCSVDir = resolvedDir
		if err := config.Save(settings); err != nil {
			return err
		}
		fmt.Println("已啟用每日 CSV 輸出。")
		return nil

	case "disable":
		fs := flag.NewFlagSet("daily disable", flag.ContinueOnError)
		formatFlag := parseFormat(fs)
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *formatFlag != "csv" {
			return fmt.Errorf("目前僅支援 csv")
		}
		settings.DailyCSVEnabled = false
		if err := config.Save(settings); err != nil {
			return err
		}
		fmt.Println("已停用每日 CSV 輸出。")
		return nil

	case "set":
		fs := flag.NewFlagSet("daily set", flag.ContinueOnError)
		dirFlag := fs.String("dir", "", "輸出目錄 (預設 %ProgramData%/go-xwatch/daily)")
		formatFlag := parseFormat(fs)
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *formatFlag != "csv" {
			return fmt.Errorf("目前僅支援 csv")
		}
		if *dirFlag != "" {
			resolvedDir, err := resolveAndEnsureDir(*dirFlag, "每日 CSV 目錄")
			if err != nil {
				return err
			}
			settings.DailyCSVDir = resolvedDir
		}
		if err := config.Save(settings); err != nil {
			return err
		}
		fmt.Println("每日 CSV 設定已更新。")
		return nil

	case "test":
		fs := flag.NewFlagSet("daily test", flag.ContinueOnError)
		dirFlag := fs.String("dir", "", "輸出目錄 (預設 %ProgramData%/go-xwatch/daily)")
		formatFlag := parseFormat(fs)
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *formatFlag != "csv" {
			return fmt.Errorf("目前僅支援 csv")
		}
		dir := settings.DailyCSVDir
		if *dirFlag != "" {
			dir = *dirFlag
		}
		if dir == "" {
			dir = defaultDir
		}
		resolvedDir, err := resolveAndEnsureDir(dir, "每日 CSV 目錄")
		if err != nil {
			return err
		}
		sink, err := pipeline.NewDailyFileSink(resolvedDir, pipeline.NewCSVRecorder)
		if err != nil {
			return fmt.Errorf("建立測試 sink 失敗: %w", err)
		}
		defer sink.Close()
		now := time.Now()
		entry := journal.Entry{TS: now, Op: "TEST", Path: "\u003ctest\u003e", IsDir: false, Size: 0}
		if err := sink.Handle(context.Background(), []journal.Entry{entry}); err != nil {
			return fmt.Errorf("寫入測試事件失敗: %w", err)
		}
		day := now.In(time.Local).Format("2006-01-02")
		filePath := filepath.Join(resolvedDir, day+".csv")
		fmt.Println("測試事件已寫入:", filePath)
		return nil

	default:
		return fmt.Errorf("未知子指令: %s", sub)
	}
}

func printDailyUsage() {
	fmt.Println("daily 指令用法：")
	fmt.Println("  daily status")
	fmt.Println("  daily enable [--dir PATH] [--format csv]")
	fmt.Println("  daily disable [--format csv]")
	fmt.Println("  daily set [--dir PATH] [--format csv]")
	fmt.Println("  daily test [--dir PATH] [--format csv]")
	fmt.Println("(目前僅支援 csv，其他格式預留)")
}

func exportJournal(sinceStr, untilStr string, limit int, format string, all, bom bool, outPath string) error {
	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		return err
	}
	j, err := journal.Open(filepath.Join(dataDir, "journal.db"), key)
	if err != nil {
		return err
	}
	defer j.Close()

	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339, s)
	}
	since, err := parse(sinceStr)
	if err != nil {
		return fmt.Errorf("invalid since: %w", err)
	}
	until, err := parse(untilStr)
	if err != nil {
		return fmt.Errorf("invalid until: %w", err)
	}
	if all {
		since = time.Time{}
		until = time.Time{}
	}

	entries, err := j.Query(context.Background(), since, until, limit)
	if err != nil {
		return err
	}

	ext := "json"
	switch strings.ToLower(format) {
	case "jsonl":
		ext = "jsonl"
	case "json":
		ext = "json"
	case "text":
		ext = "txt"
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	var out io.Writer = os.Stdout
	var closeFn func() error
	if outPath == "" {
		outPath = filepath.Join(dataDir, fmt.Sprintf("export_%s.%s", time.Now().Format("20060102_150405"), ext))
	}
	if outPath != "-" {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		out = f
		closeFn = f.Close
	}
	if bom {
		// Prepend BOM for editors（如記事本）正確辨識 UTF-8。
		if _, err := out.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			if closeFn != nil {
				_ = closeFn()
			}
			return err
		}
	}

	switch strings.ToLower(format) {
	case "jsonl", "json":
		enc := json.NewEncoder(out)
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				if closeFn != nil {
					_ = closeFn()
				}
				return err
			}
		}
	case "text":
		for _, e := range entries {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%d\t%t\n", e.TS.Format(time.RFC3339Nano), e.Op, e.Path, e.Size, e.IsDir); err != nil {
				if closeFn != nil {
					_ = closeFn()
				}
				return err
			}
		}
	}
	if closeFn != nil {
		_ = closeFn()
		fmt.Fprintf(os.Stderr, "已匯出 %d 筆事件到 %s。\n", len(entries), outPath)
	} else {
		fmt.Fprintf(os.Stderr, "已匯出 %d 筆事件。\n", len(entries))
	}
	return nil
}

func isElevated() bool {
	token := windows.Token(0)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	var elevation uint32
	var outLen uint32
	// windows.TokenElevation returns 1 when elevated.
	if err := windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &outLen); err != nil {
		return false
	}
	return elevation != 0
}

func relaunchElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	params := strings.Join(args, " ")
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"), windows.StringToUTF16Ptr(exe), windows.StringToUTF16Ptr(params), nil, windows.SW_SHOW)
}

func askYesNo(prompt string) bool {
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return true
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			return true
		}
		if strings.EqualFold(line, "n") || strings.EqualFold(line, "no") {
			return false
		}
	}
}

// evaluateElevation 決定在互動模式下是否嘗試提升權限、退出或繼續。
// decision 可能為：continue（照常執行）、relaunch（已觸發提升並應結束當前行程）、exit（使用者拒絕並退出）。
func evaluateElevation(skipEnv, interactive, elevated bool, ask func(string) bool, relaunch func([]string) error, args []string) (string, error) {
	if skipEnv || !interactive || elevated {
		return "continue", nil
	}

	if ask(elevationPrompt) {
		if err := relaunch(args); err != nil {
			return "continue", err
		}
		return "relaunch", nil
	}

	return "exit", nil
}

func promptNextAction() (string, string) {
	// 非互動或要求略過時，直接退出。
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return "exit", ""
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Println("請輸入指令 或 以下快捷選項")
		fmt.Println("  h) 顯示 help")
		fmt.Println("  e) 退出程式")
		fmt.Fprint(os.Stderr, "> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return "continue", ""
		}
		if strings.EqualFold(line, "e") {
			return "exit", ""
		}
		if strings.EqualFold(line, "h") {
			return "help", ""
		}
		// 視為直接輸入下一個指令。
		return "command", line
	}
}

// resolveAndEnsureDir 會去除前後空白，轉為絕對路徑，檢查是否存在，不存在時提示是否建立。
func resolveAndEnsureDir(path string, purpose string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("%s 不可為空", purpose)
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s 不是資料夾: %s", purpose, absPath)
		}
		return absPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	prompt := fmt.Sprintf("%s 不存在，是否建立？(Y/n): ", absPath)
	if !askYesNo(prompt) {
		return "", fmt.Errorf("已取消建立 %s", absPath)
	}
	if mkErr := os.MkdirAll(absPath, 0o755); mkErr != nil {
		return "", mkErr
	}
	return absPath, nil
}

func isInteractiveConsole() bool {
	// 在測試或管線內常無 TTY，此時不需暫停。
	file, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (file.Mode() & os.ModeCharDevice) != 0
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "access is denied")
}

func isServiceMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "service does not exist") || strings.Contains(msg, "does not exist")
}
