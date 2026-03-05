package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unsafe"

	"go-xwatch/internal/cli"
	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/exporter"
	"go-xwatch/internal/filecheckcmd"
	"go-xwatch/internal/heartbeatcmd"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/mailcmd"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/service"

	"golang.org/x/sys/windows"
)

const defaultElevationPrompt = "偵測到目前非系統管理員，是否重新以系統管理員執行？(Y/n): "

type infoLogger interface {
	Info(string, ...any)
}

// RunCLI 執行互動式 CLI 主流程，回傳退出碼。
func RunCLI(version, serviceName string, opsLogger infoLogger) int {
	app := &cliApp{version: version, serviceName: serviceName, opsLogger: opsLogger}
	return app.run()
}

type cliApp struct {
	version     string
	serviceName string
	opsLogger   infoLogger

	suppressEmptyHelp bool

	// serviceInstalledFn 用於檢查服務是否已安裝，便於測試注入。nil 時使用 service.IsInstalled。
	serviceInstalledFn func(name string) bool
}

func (c *cliApp) run() int {
	if c.opsLogger != nil {
		c.opsLogger.Info("cli start", "version", c.version, "pid", os.Getpid(), "args", os.Args[1:])
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		c.logOp("cli signal", "signal", sig.String())
		os.Exit(130)
	}()

	decision, elevErr := evaluateElevation(os.Getenv("XWATCH_NO_ELEVATE") == "1", isInteractiveConsole(), isElevated(), askYesNo, relaunchElevated, os.Args[1:])
	if elevErr != nil {
		fmt.Fprintln(os.Stderr, "無法自動提升權限，請改以系統管理員執行：", elevErr)
	}
	switch decision {
	case "exit":
		fmt.Println("已取消提升權限，3 秒後自動退出...")
		c.logOp("cli exit", "code", 1, "reason", "user_decline_elevate")
		time.Sleep(3 * time.Second)
		return 1
	case "relaunch":
		return 0
	}

	exitCode := 0
	for {
		if err := c.runInteractive(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			c.logOp("command error", "err", err)
			if isAccessDenied(err) {
				fmt.Fprintln(os.Stderr, "請以系統管理員身分執行，或用管理員權限的 PowerShell 開啟此程式。")
			}
			exitCode = 1
		} else {
			c.logOp("command ok")
		}

		action, cmdLine := promptNextAction()
		switch action {
		case "exit":
			c.logOp("cli exit", "code", exitCode)
			return exitCode
		case "help":
			fmt.Println()
			c.printUsage()
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
			c.suppressEmptyHelp = true
			os.Args = []string{os.Args[0]}
			continue
		}
		lower := strings.ToLower(line)
		if lower == "e" || lower == "exit" {
			c.logOp("cli exit", "code", exitCode)
			return exitCode
		} else if lower == "h" || lower == "help" {
			fmt.Println()
			c.printUsage()
			os.Args = []string{os.Args[0]}
			exitCode = 0
			continue
		} else {
			os.Args = append([]string{os.Args[0]}, strings.Fields(line)...)
		}
		exitCode = 0
	}
}

func (c *cliApp) logOp(msg string, args ...any) {
	if c.opsLogger == nil {
		return
	}
	c.opsLogger.Info(msg, args...)
}

// isServiceInstalled 回傳服務是否已安裝，支援測試注入自訂實作。
func (c *cliApp) isServiceInstalled() bool {
	fn := c.serviceInstalledFn
	if fn == nil {
		fn = service.IsInstalled
	}
	return fn(c.serviceName)
}

// requireServiceInstalled 如果服務尚未安裝，回傳含操作提示的錯誤。
func (c *cliApp) requireServiceInstalled(feature string) error {
	if !c.isServiceInstalled() {
		return fmt.Errorf("服務尚未安裝，無法啟用%s功能，請先執行『init --install-service』以安裝 Windows 服務", feature)
	}
	return nil
}

func (c *cliApp) runInteractive() error {
	command := ""
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = strings.ToLower(args[0])
		args = args[1:]
	}
	c.logOp("command", "cmd", command, "args", args)

	if len(os.Args) <= 1 || command == "" {
		if c.suppressEmptyHelp {
			c.suppressEmptyHelp = false
			return nil
		}
		c.printUsage()
		return nil
	}

	reg := c.buildCommandRegistry()
	cmd, ok := reg.Get(command)
	if !ok {
		return fmt.Errorf("unknown command: %s", command)
	}
	return cmd.Run(args)
}

func (c *cliApp) buildCommandRegistry() *cli.Registry {
	reg := cli.NewRegistry()

	reg.Register(cli.CommandFunc{CommandName: "help", Fn: func(_ []string) error {
		c.printUsage()
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "init", Fn: func(args []string) error {
		if len(args) > 0 && strings.ToLower(args[0]) == "help" {
			printInitHelp()
			return nil
		}
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		rootFlag := fs.String("root", "", "watch root directory (default: exe directory)")
		installFlag := fs.Bool("install-service", false, "install and start Windows service after init")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return c.initAndExit(*rootFlag, *installFlag)
	}})

	reg.Register(cli.CommandFunc{CommandName: "status", Fn: func(_ []string) error {
		return c.printStatus()
	}})

	reg.Register(cli.CommandFunc{CommandName: "start", Fn: func(_ []string) error {
		if err := service.Start(c.serviceName); err != nil {
			return err
		}
		fmt.Println("服務已啟動。")
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "stop", Fn: func(_ []string) error {
		if err := service.Stop(c.serviceName); err != nil {
			return err
		}
		fmt.Println("服務已停止。")
		return nil
	}})

	reg.Register(cli.CommandFunc{CommandName: "remove", Fn: func(_ []string) error {
		return c.stopAndUninstall()
	}})

	reg.Register(cli.CommandFunc{CommandName: "db", Fn: func(args []string) error {
		if len(args) == 0 || strings.ToLower(args[0]) == "help" {
			printDBHelp()
			return nil
		}
		switch strings.ToLower(args[0]) {
		case "clear":
			return c.clearJournal()
		default:
			return fmt.Errorf("db: 未知子指令 %q，請執行 'db help' 查看說明", args[0])
		}
	}})

	reg.Register(cli.CommandFunc{CommandName: "export", Fn: func(args []string) error {
		if len(args) > 0 && strings.ToLower(args[0]) == "help" {
			printExportHelp()
			return nil
		}
		fs := flag.NewFlagSet("export", flag.ContinueOnError)
		sinceFlag := fs.String("since", "", "RFC3339 timestamp filter for export")
		untilFlag := fs.String("until", "", "optional RFC3339 upper bound for export")
		limitFlag := fs.Int("limit", 1000, "max rows for export")
		formatFlag := fs.String("format", "json", "export format: json|jsonl|text")
		allFlag := fs.Bool("all", false, "export all entries ignoring time filters")
		bomFlag := fs.Bool("bom", false, "prepend UTF-8 BOM for Windows editors")
		outFlag := fs.String("out", "", "output file path (use '-' for stdout; default: %ProgramData%/go-xwatch/xwatch-export-files)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return exporter.Export(*sinceFlag, *untilFlag, *limitFlag, *formatFlag, *allFlag, *bomFlag, *outFlag)
	}})

	reg.Register(cli.CommandFunc{CommandName: "mail", Fn: func(args []string) error {
		if len(args) > 0 && strings.ToLower(args[0]) == "enable" {
			if err := c.requireServiceInstalled("郵件"); err != nil {
				return err
			}
		}
		return mailcmd.Run(args)
	}})

	reg.Register(cli.CommandFunc{CommandName: "heartbeat", Fn: func(args []string) error {
		if len(args) > 0 && strings.ToLower(args[0]) == "start" {
			if err := c.requireServiceInstalled("心跳"); err != nil {
				return err
			}
		}
		return heartbeatcmd.Run(args)
	}})

	reg.Register(cli.CommandFunc{CommandName: "filecheck", Fn: func(args []string) error {
		// 只有 enable 子指令需要服務已安裝
		if len(args) > 0 && strings.ToLower(args[0]) == "enable" {
			if err := c.requireServiceInstalled("目錄檔案檢查"); err != nil {
				return err
			}
		}
		return filecheckcmd.Run(args)
	}})

	return reg
}

func (c *cliApp) initAndExit(rootArg string, installService bool) error {
	fmt.Println("[1/3] 準備初始化...")
	root, err := c.resolveRootForInit(rootArg)
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

		if err := service.InstallOrUpdate(c.serviceName, exePath, "--service"); err != nil {
			return fmt.Errorf("無法註冊服務: %w", err)
		}

		if err := service.Start(c.serviceName); err != nil && !errors.Is(err, service.ErrAlreadyRunning) {
			return fmt.Errorf("無法啟動服務: %w", err)
		}
	} else {
		fmt.Println("[3/3] 已完成設定，未註冊/啟動服務。需註冊請改用 --install-service。")
	}

	fmt.Println("完成。")
	return nil
}

func (c *cliApp) printStatus() error {
	status, err := service.Status(c.serviceName)
	if err != nil {
		if isServiceMissing(err) {
			fmt.Fprintln(os.Stderr, "提示：服務尚未安裝。請先執行『init --install-service』安裝 Windows 服務後，再使用 status 指令查看完整狀態。")
		}
		return err
	}
	fmt.Println("service:", c.serviceName)
	fmt.Println("status:", status)

	// 顯示目前 CLI 執行的 Windows 權限等級
	if isElevated() {
		fmt.Println("privilege: administrator（系統管理員）")
	} else {
		fmt.Println("privilege: standard user（一般使用者）")
	}

	// 顯示服務所使用的 Windows 帳戶
	if account, aerr := service.ServiceAccount(c.serviceName); aerr == nil {
		fmt.Println("service account:", account)
	}

	settings, err := config.Load()
	if err == nil {
		fmt.Println("root:", settings.RootDir)
		fmt.Println("heartbeat:", settings.HeartbeatEnabled)
		if settings.HeartbeatEnabled {
			fmt.Printf("heartbeat interval: %d 秒\n", settings.HeartbeatInterval)
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

func (c *cliApp) stopAndUninstall() error {
	if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("無法停止服務: %w", err)
	}
	c.logOp("remove step", "step", "XWatch 註冊之 Windows 服務已主動停止")
	fmt.Println("[1/5] XWatch 註冊之 Windows 服務已主動停止。")

	// 停用所有功能並寫入設定
	if err := c.disableAllFeaturesOnRemove(); err != nil {
		// 停用失敗不中斷移除，記錄後繼續
		c.logOp("remove step", "step", fmt.Sprintf("停用功能失敗（繼續移除）：%v", err))
	}

	if err := service.Uninstall(c.serviceName); err != nil && !isServiceMissing(err) {
		return fmt.Errorf("無法移除服務: %w", err)
	}
	c.logOp("remove step", "step", "已移除 XWatch 註冊之 Windows 服務")
	fmt.Println("[5/5] XWatch 註冊之 Windows 服務已移除。")

	fmt.Println("所有服務、排程已停止並移除。")
	return nil
}

// disableAllFeaturesOnRemove 停用心跳與郵件排程，並將結果寫入 ops-log。
// 若設定檔無法讀取（如首次安裝未完成），直接回傳 nil 不報錯。
func (c *cliApp) disableAllFeaturesOnRemove() error {
	settings, err := config.Load()
	if err != nil {
		// 設定檔不存在時不視為錯誤
		return nil
	}

	// 停用心跳
	settings.HeartbeatEnabled = false
	c.logOp("remove step", "step", "心跳已停用")
	fmt.Println("[2/5] 心跳已停用。")

	// 停用郵件排程
	settings.Mail.Enabled = config.BoolPtr(false)
	c.logOp("remove step", "step", "郵件排程已停用")
	fmt.Println("[3/5] mail 已停用。")

	// 停用 filecheck 排程
	settings.Filecheck.Enabled = false
	settings.Filecheck.Mail.Enabled = config.BoolPtr(false)
	c.logOp("remove step", "step", "filecheck 排程已停用")
	fmt.Println("[4/5] filecheck 已停用。")

	return config.Save(settings)
}

func (c *cliApp) clearJournal() error {
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
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

func (c *cliApp) printUsage() {
	fmt.Println()
	fmt.Println("  XWATCH")
	fmt.Printf("   - version: %s\n", c.version)
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("指令列表：")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  init [help] <subcommand>\t初始化監控設定")
	fmt.Fprintln(w, "  status\t顯示服務狀態、版本與設定摘要")
	fmt.Fprintln(w, "  start\t啟動服務")
	fmt.Fprintln(w, "  stop\t停止服務")
	fmt.Fprintln(w, "  remove\t停止並移除服務及所有排程")
	fmt.Fprintln(w, "  db [help] <subcommand>\t管理事件資料庫")
	fmt.Fprintln(w, "  export [help] <subcommand>\t匯出監控事件記錄")
	fmt.Fprintln(w, "  mail [help] <subcommand>\t郵件排程管理")
	fmt.Fprintln(w, "  heartbeat [help] <subcommand>\t管理心跳測試")
	fmt.Fprintln(w, "  filecheck [help] <subcommand>\t監控指定目錄內的檔案存在性")
	_ = w.Flush()
	fmt.Println("============================================================")
}

// printInitHelp 顯示 init 指令的詳細說明。
func printInitHelp() {
	fmt.Println()
	fmt.Println("init — 初始化 XWatch 監控設定")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  init [--root PATH] [--install-service]")
	fmt.Println()
	fmt.Println("參數說明：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  --root PATH\t指定監控根目錄（絕對或相對路徑）。")
	fmt.Fprintln(w, "  \t省略時以執行檔所在目錄作為根目錄。")
	fmt.Fprintln(w, "  --install-service\t初始化完成後同時將 XWatch 註冊為 Windows 服務並立即啟動。")
	fmt.Fprintln(w, "  \t若服務已存在則更新設定後重新啟動。")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("執行步驟：")
	fmt.Println("  [1/3] 確認根目錄存在（不存在時詢問是否自動建立）")
	fmt.Println("  [2/3] 將設定寫入 %ProgramData%\\go-xwatch\\config.json")
	fmt.Println("  [3/3] 若加入 --install-service：向 Windows SCM 註冊服務並啟動")
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 使用執行檔目錄作為根目錄（僅寫入設定，不安裝服務）")
	fmt.Println("  xwatch init")
	fmt.Println()
	fmt.Println("  # 指定目錄並安裝服務（需以系統管理員執行）")
	fmt.Println("  xwatch init --root D:\\data\\watch --install-service")
	fmt.Println()
	fmt.Println("  # 已有服務時重新指定根目錄")
	fmt.Println("  xwatch init --root D:\\new-data --install-service")
	fmt.Println()
	fmt.Println("注意事項：")
	fmt.Println("  - 安裝 Windows 服務需要系統管理員權限")
	fmt.Println("  - 根目錄不存在時程式會詢問是否自動建立")
	fmt.Println("  - 重複執行 init 不會清除已有的事件資料庫")
}

// printDBHelp 顯示 db 指令的詳細說明。
func printDBHelp() {
	fmt.Println()
	fmt.Println("db — 管理 XWatch 事件資料庫")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  db <subcommand>")
	fmt.Println()
	fmt.Println("子指令：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  clear\t清除所有事件記錄（操作不可逆）")
	fmt.Fprintln(w, "  help\t顯示本說明")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 清除所有事件記錄")
	fmt.Println("  xwatch db clear")
	fmt.Println()
	fmt.Println("注意事項：")
	fmt.Println("  - db clear 執行前會先停止 Windows 服務，完成後服務不會自動重啟")
	fmt.Println("  - 清除操作不可復原，請確認後再執行")
	fmt.Println("  - 需要系統管理員權限")
}

// printExportHelp 顯示 export 指令的詳細說明。
func printExportHelp() {
	fmt.Println()
	fmt.Println("export — 匯出 XWatch 監控事件記錄")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  export [--since TIME] [--until TIME] [--limit N] [--format TYPE] [--all] [--bom] [--out PATH]")
	fmt.Println()
	fmt.Println("參數說明：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  --since RFC3339\t篩選起始時間，採 RFC3339 格式（如 2026-03-01T00:00:00+08:00）。")
	fmt.Fprintln(w, "  \t省略時不限制起始時間。")
	fmt.Fprintln(w, "  --until RFC3339\t篩選截止時間，採 RFC3339 格式。省略時不限制截止時間。")
	fmt.Fprintln(w, "  --limit N\t最大匯出筆數（預設：1000）。指定 --all 時此參數無效。")
	fmt.Fprintln(w, "  --all\t匯出所有事件，忽略 --since / --until / --limit 限制。")
	fmt.Fprintln(w, "  --format TYPE\t匯出格式（預設：json）：")
	fmt.Fprintln(w, "  \t  json   完整 JSON 陣列")
	fmt.Fprintln(w, "  \t  jsonl  每行一筆 JSON（JSON Lines）")
	fmt.Fprintln(w, "  \t  text   人類可讀的文字格式")
	fmt.Fprintln(w, "  --bom\t在輸出開頭加入 UTF-8 BOM，供 Windows 記事本正確顯示中文。")
	fmt.Fprintln(w, "  --out PATH\t輸出檔案路徑。使用 '-' 輸出至 stdout。")
	fmt.Fprintln(w, "  \t省略時自動命名並存入 %ProgramData%\\go-xwatch\\xwatch-export-files\\。")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  # 匯出最近 1000 筆事件為 JSON（預設模式）")
	fmt.Println("  xwatch export")
	fmt.Println()
	fmt.Println("  # 匯出所有事件輸出至 stdout")
	fmt.Println("  xwatch export --all --out -")
	fmt.Println()
	fmt.Println("  # 篩選特定日期範圍，以 text 格式輸出並加 BOM（供記事本閱讀）")
	fmt.Println("  xwatch export --since 2026-03-01T00:00:00+08:00 --until 2026-03-02T00:00:00+08:00 --format text --bom")
	fmt.Println()
	fmt.Println("  # 匯出最新 50 筆，存入指定檔案")
	fmt.Println("  xwatch export --limit 50 --out D:\\logs\\events.json")
	fmt.Println()
	fmt.Println("  # 匯出全部事件為 JSON Lines 格式")
	fmt.Println("  xwatch export --all --format jsonl --out D:\\logs\\events.jsonl")
}

func (c *cliApp) resolveRoot(rootArg string) (string, error) {
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
func (c *cliApp) resolveRootForInit(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveAndEnsureDir(filepath.Dir(exePath), "根目錄")
}

func evaluateElevation(skipEnv, interactive, elevated bool, ask func(string) bool, relaunch func([]string) error, args []string) (string, error) {
	if skipEnv || !interactive || elevated {
		return "continue", nil
	}

	if ask(defaultElevationPrompt) {
		if err := relaunch(args); err != nil {
			return "continue", err
		}
		return "relaunch", nil
	}

	return "exit", nil
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
