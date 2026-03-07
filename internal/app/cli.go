package app

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go-xwatch/internal/cli"
	"go-xwatch/internal/envcmd"
	"go-xwatch/internal/exporter"
	"go-xwatch/internal/filecheckcmd"
	"go-xwatch/internal/heartbeatcmd"
	"go-xwatch/internal/mailcmd"
	"go-xwatch/internal/service"
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

	// serviceStatusFn 用於查詢服務狀態，便於測試注入。nil 時使用 service.Status。
	serviceStatusFn func(name string) (string, error)

	// confirmOverwriteFn 用於服務重新部署時的使用者確認，便於測試注入。
	// nil 時使用 askYesNoDefaultNo（預設 No）。
	confirmOverwriteFn func(prompt string) bool

	// registeredExePathFn 用於查詢服務已登錄的執行檔路徑，便於測試注入。
	// nil 時使用 service.RegisteredExePath。
	registeredExePathFn func(name string) (string, error)
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

// getServiceStatus 查詢服務狀態字串，支援測試注入自訂實作。
func (c *cliApp) getServiceStatus(name string) (string, error) {
	fn := c.serviceStatusFn
	if fn == nil {
		fn = service.Status
	}
	return fn(name)
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
		return c.startService()
	}})

	reg.Register(cli.CommandFunc{CommandName: "stop", Fn: func(_ []string) error {
		return c.stopService()
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

	// 以 ServiceAwareRunner 介面統一處理「特定子指令需服務已安裝」的前置檢查。
	// 各 *cmd 套件自行聲明需求，cli.go 不再硬編碼個別套件細節（OCP + DIP）。
	c.registerServiceAware(reg, "mail", mailcmd.Runner)
	c.registerServiceAware(reg, "heartbeat", heartbeatcmd.Runner)
	c.registerServiceAware(reg, "filecheck", filecheckcmd.Runner)

	reg.Register(cli.CommandFunc{CommandName: "env", Fn: func(args []string) error {
		return envcmd.Run(args)
	}})

	return reg
}

// registerServiceAware 將實作 cli.ServiceAwareRunner 的指令執行者包裝後
// 自動插入服務安裝前置檢查，再註冊至 Registry。
// cli.go 只依賴 cli.ServiceAwareRunner 抽象（DIP），
// 各 *cmd 套件自行宣告哪些子指令需要服務，新增需求無需修改此方法（OCP）。
func (c *cliApp) registerServiceAware(reg *cli.Registry, name string, runner cli.ServiceAwareRunner) {
	feature, subcmds := runner.ServiceRequiredFor()
	subSet := make(map[string]struct{}, len(subcmds))
	for _, s := range subcmds {
		subSet[s] = struct{}{}
	}
	reg.Register(cli.CommandFunc{CommandName: name, Fn: func(args []string) error {
		if len(args) > 0 {
			if _, needsService := subSet[strings.ToLower(args[0])]; needsService {
				if err := c.requireServiceInstalled(feature); err != nil {
					return err
				}
			}
		}
		return runner.Run(args)
	}})
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

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "access is denied")
}
