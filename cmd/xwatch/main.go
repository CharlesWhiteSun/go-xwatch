package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unsafe"

	"go-xwatch/internal/cli"
	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/daily"
	"go-xwatch/internal/exporter"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/opslog"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/service"
	"go-xwatch/internal/watcher"

	"golang.org/x/sys/windows"
)

const serviceName = "GoXWatch"

var version = "dev"

const elevationPrompt = "偵測到目前非系統管理員，是否重新以系統管理員執行？(Y/n): "

var opsLogger = opslog.New(nil)

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

		fmt.Println()
		fmt.Print("> ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
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
		exitCode = 0
	}
}

func logOp(msg string, args ...any) {
	if opsLogger == nil {
		return
	}
	opsLogger.Info(msg, args...)
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

	reg.Register(cli.CommandFunc{CommandName: "help", Fn: func(_ []string) error {
		printUsage()
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "init", Fn: func(args []string) error {
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
		installFlag := fs.Bool("install-service", false, "install and start Windows service after init")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return initAndExit(*rootFlag, *installFlag)
	}})

	reg.Register(cli.CommandFunc{CommandName: "status", Fn: func(_ []string) error {
		return printStatus()
	}})

	reg.Register(cli.CommandFunc{CommandName: "start", Fn: func(_ []string) error {
		if err := service.Start(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已啟動。")
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "stop", Fn: func(_ []string) error {
		if err := service.Stop(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已停止。")
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "uninstall", Fn: func(_ []string) error {
		if err := service.Uninstall(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已移除。")
		return nil
	}})

	cleanupFn := func([]string) error { return stopAndUninstall() }
	reg.Register(cli.CommandFunc{CommandName: "cleanup", Fn: cleanupFn})
	reg.Register(cli.CommandFunc{CommandName: "remove", Fn: cleanupFn})

	clearFn := func([]string) error { return clearJournal() }
	reg.Register(cli.CommandFunc{CommandName: "clear", Fn: clearFn})
	reg.Register(cli.CommandFunc{CommandName: "purge", Fn: clearFn})
	reg.Register(cli.CommandFunc{CommandName: "wipe", Fn: clearFn})

	reg.Register(cli.CommandFunc{CommandName: "export", Fn: func(args []string) error {
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
		return exporter.Export(*sinceFlag, *untilFlag, *limitFlag, *formatFlag, *allFlag, *bomFlag, *outFlag)
	}})

	reg.Register(cli.CommandFunc{CommandName: "daily", Fn: func(args []string) error {
		return daily.Run(args)
	}})

	reg.Register(cli.CommandFunc{CommandName: "run", Fn: func(args []string) error {
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		root, err := resolveRoot(*rootFlag)
		if err != nil {
			return err
		}
		logger := watcher.NewLogger(os.Stdout)
		logger.Info("前景模式啟動", slog.String("根目錄", root))
		return watcher.Run(nil, root, logger, nil)
	}})

	return reg
}

func initAndExit(rootArg string, installService bool) error {
	fmt.Println("[1/3] 準備初始化...")
	root, err := resolveRootForInit(rootArg)
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

// resolveRootForInit 在初始化時使用：若未指定 root，優先使用目前執行檔所在目錄，
// 以避免沿用舊設定檔中的過期根目錄。
func resolveRootForInit(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
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

func isElevated() bool {
	token := windows.Token(0)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	var elevation uint32
	var outLen uint32
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
		return "command", line
	}
}

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
