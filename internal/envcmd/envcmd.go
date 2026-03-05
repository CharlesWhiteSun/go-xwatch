// Package envcmd 實作 env CLI 指令，用於切換 XWatch 執行環境（dev/prod）。
// 切換環境僅儲存 environment 欄位，不會自動改寫已設定的收件人清單。
// 若要修改收件人，請另行執行 mail 或 filecheck 指令。
package envcmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"go-xwatch/internal/config"
)

// Run 是 env 指令的入口。
func Run(args []string) error {
	if len(args) == 0 {
		return printCurrent()
	}
	switch strings.ToLower(args[0]) {
	case "help":
		printHelp()
		return nil
	case "set":
		if len(args) < 2 {
			return fmt.Errorf("env set: 請指定環境名稱（dev 或 prod）")
		}
		return setEnv(strings.ToLower(strings.TrimSpace(args[1])))
	default:
		return fmt.Errorf("env: 未知子指令 %q，請執行 'env help' 查看說明", args[0])
	}
}

// printCurrent 讀取設定並顯示目前環境。
func printCurrent() error {
	s, err := config.Load()
	if err != nil {
		return fmt.Errorf("讀取設定失敗: %w", err)
	}
	env := s.Environment
	if env == "" {
		env = config.EnvProd
	}
	fmt.Printf("目前環境：%s\n", env)
	fmt.Println()
	fmt.Println("預設收件人清單：")
	for _, addr := range config.DefaultMailToListForEnv(env) {
		fmt.Printf("  %s\n", addr)
	}
	fmt.Println()
	fmt.Println("（使用 'env set dev' 或 'env set prod' 切換環境）")
	return nil
}

// setEnv 將環境寫入設定檔。
func setEnv(env string) error {
	if env != config.EnvDev && env != config.EnvProd {
		return fmt.Errorf("env set: 不支援的環境 %q，請使用 dev 或 prod", env)
	}
	s, err := config.Load()
	if err != nil {
		return fmt.Errorf("讀取設定失敗: %w", err)
	}
	if s.Environment == env {
		fmt.Printf("目前已是 %s 環境，無需切換。\n", env)
		return nil
	}
	s.Environment = env
	if err := config.Save(s); err != nil {
		return fmt.Errorf("儲存設定失敗: %w", err)
	}
	fmt.Printf("環境已切換為：%s\n", env)
	fmt.Println()
	fmt.Println("注意：此操作僅儲存環境標記。")
	fmt.Println("  - 若要更新收件人，請另行執行 'mail set' 或 'filecheck mail enable'。")
	fmt.Printf("  - %s 環境預設收件人清單：\n", env)
	for _, addr := range config.DefaultMailToListForEnv(env) {
		fmt.Printf("      %s\n", addr)
	}
	return nil
}

// printHelp 顯示 env 指令的詳細說明。
func printHelp() {
	fmt.Println()
	fmt.Println("env — 切換 XWatch 執行環境（dev / prod）")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  env                  顯示目前環境與對應預設收件人")
	fmt.Println("  env set <env>        切換環境（dev 或 prod）")
	fmt.Println("  env help             顯示本說明")
	fmt.Println()
	fmt.Println("環境說明：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  dev\t開發環境，預設收件人不含外部單位")
	fmt.Fprintln(w, "  prod\t正式環境（預設），預設收件人包含外部單位 589497@cpc.com.tw")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("各環境預設收件人清單（僅在收件人為空時自動填入）：")
	fmt.Printf("  dev:  %s\n", strings.Join(config.DefaultMailToListDev, ", "))
	fmt.Printf("  prod: %s\n", strings.Join(config.DefaultMailToListProd, ", "))
	fmt.Println()
	fmt.Println("注意：切換環境不會自動改寫已設定的收件人。")
	fmt.Println("      若需更新收件人，請另行執行 'mail set --to' 或 'filecheck mail enable --to'。")
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  env                  # 顯示目前環境")
	fmt.Println("  env set dev          # 切換為開發環境")
	fmt.Println("  env set prod         # 切換為正式環境")
}
