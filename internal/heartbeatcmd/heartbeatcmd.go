// Package heartbeatcmd 實作 heartbeat CLI 子指令。
package heartbeatcmd

import (
	"flag"
	"fmt"
	"strings"

	"go-xwatch/internal/config"
	"go-xwatch/internal/heartbeat"
)

// Run 處理 heartbeat 子指令。
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "help":
		printUsage()
		return nil
	case "status":
		return status()
	case "start":
		return start()
	case "stop":
		return stop()
	case "set":
		return set(rest)
	default:
		return fmt.Errorf("未知子指令: %s", sub)
	}
}

func status() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	interval := settings.HeartbeatInterval
	if interval <= 0 {
		interval = config.DefaultHeartbeatInterval
	}
	logDir, _ := heartbeat.DefaultLogDir()
	fmt.Println("心跳已啟用:", settings.HeartbeatEnabled)
	fmt.Printf("心跳間隔: %d 秒\n", interval)
	if logDir != "" {
		fmt.Println("心跳 log 目錄:", logDir)
	}
	return nil
}

func start() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.HeartbeatEnabled = true
	if settings.HeartbeatInterval <= 0 {
		settings.HeartbeatInterval = config.DefaultHeartbeatInterval
	}
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("心跳已啟用。")
	return nil
}

func stop() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.HeartbeatEnabled = false
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Println("心跳已停止。")
	return nil
}

func set(args []string) error {
	fs := flag.NewFlagSet("heartbeat set", flag.ContinueOnError)
	intervalFlag := fs.Int("interval", 0, "心跳間隔秒數（最小 1）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *intervalFlag <= 0 {
		return fmt.Errorf("--interval 必須大於 0")
	}
	settings, err := config.Load()
	if err != nil {
		return err
	}
	settings.HeartbeatInterval = *intervalFlag
	if err := config.Save(settings); err != nil {
		return err
	}
	fmt.Printf("心跳間隔已設定為 %d 秒。\n", *intervalFlag)
	return nil
}

func printUsage() {
	fmt.Println("heartbeat 子指令:")
	fmt.Println("  status              顯示心跳狀態、間隔與 log 目錄")
	fmt.Println("  start               啟用心跳")
	fmt.Println("  stop                停止心跳")
	fmt.Println("  set --interval N    設定心跳間隔秒數（最小 1）")
	fmt.Println()
	fmt.Println("  log 目錄: %ProgramData%\\go-xwatch\\xwatch-heartbeat-logs\\heartbeat_YYYY-MM-DD.log")
}
