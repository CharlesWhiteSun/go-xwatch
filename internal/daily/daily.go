package daily

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/pipeline"
)

// Run handles daily subcommands.
func Run(args []string) error {
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
		entry := journal.Entry{TS: now, Op: "TEST", Path: "<test>", IsDir: false, Size: 0}
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
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			break
		}
		if strings.EqualFold(line, "n") || strings.EqualFold(line, "no") {
			return "", fmt.Errorf("已取消建立 %s", absPath)
		}
	}
	if mkErr := os.MkdirAll(absPath, 0o755); mkErr != nil {
		return "", mkErr
	}
	return absPath, nil
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
