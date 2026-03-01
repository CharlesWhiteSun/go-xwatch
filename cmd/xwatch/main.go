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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unsafe"

	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/service"
	"go-xwatch/internal/watcher"

	"golang.org/x/sys/windows"
)

const serviceName = "GoXWatch"

var version = "dev"

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "this program currently supports Windows service mode only")
		os.Exit(1)
	}

	if service.IsWindowsServiceProcess() {
		if err := runAsService(); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			os.Exit(1)
		}
		return
	}

	// 嘗試在互動模式下提示並自動提升權限，以便順利註冊/控制服務。
	if os.Getenv("XWATCH_NO_ELEVATE") != "1" && isInteractiveConsole() && !isElevated() {
		if askYesNo("偵測到目前非系統管理員，是否重新以系統管理員執行？(Y/n): ") {
			if err := relaunchElevated(os.Args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "無法自動提升權限，請改以系統管理員執行：", err)
			} else {
				return
			}
		}
	}

	exitCode := 0
	for {
		if err := runInteractive(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			if isAccessDenied(err) {
				fmt.Fprintln(os.Stderr, "請以系統管理員身分執行，或用管理員權限的 PowerShell 開啟此程式。")
			}
			exitCode = 1
		}

		action, cmdLine := promptNextAction()
		switch action {
		case "exit":
			os.Exit(exitCode)
		case "help":
			fmt.Println()
			printUsage()
		case "command":
			if cmdLine == "" {
				os.Args = []string{os.Args[0]}
			} else {
				os.Args = append([]string{os.Args[0]}, strings.Fields(cmdLine)...)
			}
			exitCode = 0
			continue
		}

		// 回到簡易 CLI 主畫面，讓使用者輸入下一個指令。
		fmt.Println()
		fmt.Print("> ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			os.Args = []string{os.Args[0]}
		} else {
			os.Args = append([]string{os.Args[0]}, strings.Fields(line)...)
		}
		// 重置 exitCode 為下一輪；若下一輪成功會保持 0。
		exitCode = 0
	}
}

func runInteractive() error {
	command := ""
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = strings.ToLower(args[0])
		args = args[1:]
	}

	if len(os.Args) <= 1 || command == "" {
		printUsage()
		return nil
	}

	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
	sinceFlag := fs.String("since", "", "RFC3339 timestamp filter for export")
	untilFlag := fs.String("until", "", "optional RFC3339 upper bound for export")
	limitFlag := fs.Int("limit", 1000, "max rows for export")
	formatFlag := fs.String("format", "json", "export format: json|jsonl|text")
	allFlag := fs.Bool("all", false, "export all entries ignoring time filters")
	bomFlag := fs.Bool("bom", false, "prepend UTF-8 BOM for Windows editors")
	outFlag := fs.String("out", "", "output file path (use '-' for stdout; default: %ProgramData%/go-xwatch)")
	installFlag := fs.Bool("install-service", false, "install and start Windows service after init")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch command {
	case "init":
		if err := initAndExit(*rootFlag, *installFlag); err != nil {
			return err
		}
		return nil
	case "help":
		printUsage()
		return nil
	case "status":
		return printStatus()
	case "start":
		if err := service.Start(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已啟動。")
		return nil
	case "stop":
		if err := service.Stop(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已停止。")
		return nil
	case "uninstall":
		if err := service.Uninstall(serviceName); err != nil {
			return err
		}
		fmt.Println("服務已移除。")
		return nil
	case "cleanup", "remove":
		return stopAndUninstall()
	case "clear", "purge", "wipe":
		return clearJournal()
	case "export":
		return exportJournal(*sinceFlag, *untilFlag, *limitFlag, *formatFlag, *allFlag, *bomFlag, *outFlag)
	case "run":
		root, err := resolveRoot(*rootFlag)
		if err != nil {
			return err
		}
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		logger.Info("running in console mode", "root", root)
		return watcher.Run(nil, root, logger, nil)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
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
	fmt.Println("============================================================")
	fmt.Println("xwatch 可用指令：")
	fmt.Printf("  version: %s\n", version)
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
	fmt.Fprintln(w, "  run [-root PATH]\t前景模式執行，不作為服務")
	_ = w.Flush()
	fmt.Println("============================================================")
}

func resolveRoot(rootArg string) (string, error) {
	if rootArg != "" {
		return filepath.Abs(rootArg)
	}

	settings, err := config.Load()
	if err == nil && settings.RootDir != "" {
		return filepath.Abs(settings.RootDir)
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Dir(exePath))
}

func runAsService() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	if settings.RootDir == "" {
		return errors.New("empty root dir in config")
	}
	root, err := filepath.Abs(settings.RootDir)
	if err != nil {
		return err
	}
	return service.Run(serviceName, root)
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

func promptNextAction() (string, string) {
	// 非互動或要求略過時，直接退出。
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return "exit", ""
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Println("--------- 下一步 ----------")
		fmt.Println("請輸入以下選項或指令")
		fmt.Println("e) 退出程式")
		fmt.Println("h) 顯示 help")
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
