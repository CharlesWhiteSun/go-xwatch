// Package envcmd 實作 env CLI 指令，用於切換 XWatch 執行環境（dev/prod）。
// 切換環境時，會自動將 mail 與 filecheck 的收件人清單更新為目標環境的預設值。
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
	case "status":
		return printCurrent()
	case "set":
		if len(args) < 2 {
			return fmt.Errorf("env set: 請指定環境名稱（dev 或 prod）")
		}
		return setEnv(strings.ToLower(strings.TrimSpace(args[1])))
	default:
		return fmt.Errorf("env: 未知子指令 %q，請執行 'env help' 查看說明", args[0])
	}
}

// printCurrent 讀取設定並顯示目前環境與實際已設定的收件人清單。
func printCurrent() error {
	s, err := config.Load()
	if err != nil {
		return fmt.Errorf("讀取設定失敗: %w", err)
	}
	env := s.Environment
	if env == "" {
		env = config.EnvDev
	}
	fmt.Printf("目前環境：%s\n", env)
	fmt.Println()

	fmt.Println("郵件收件人（mail.to）：")
	if len(s.Mail.To) == 0 {
		fmt.Println("  （未設定）")
	} else {
		for _, addr := range s.Mail.To {
			fmt.Printf("  %s\n", addr)
		}
	}

	fmt.Println()
	fmt.Println("檔案檢查郵件收件人（filecheck.mail.to）：")
	if len(s.Filecheck.Mail.To) == 0 {
		fmt.Println("  （未設定）")
	} else {
		for _, addr := range s.Filecheck.Mail.To {
			fmt.Printf("  %s\n", addr)
		}
	}

	fmt.Println()
	fmt.Println("（使用 'env set dev' 或 'env set prod' 切換環境）")
	return nil
}

// setEnv 將環境寫入設定檔，並同步將 mail 與 filecheck 收件人清單更新為目標環境的預設值。
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
	// 同步更新 mail 與 filecheck 收件人清單為新環境的預設值
	defaultTo := config.DefaultMailToListForEnv(env)
	s.Mail.To = defaultTo
	s.Filecheck.Mail.To = append([]string(nil), defaultTo...)
	if err := config.Save(s); err != nil {
		return fmt.Errorf("儲存設定失敗: %w", err)
	}
	fmt.Printf("環境已切換為：%s\n", env)
	fmt.Println()
	fmt.Println("已同步更新收件人清單（mail 與 filecheck）：")
	for _, addr := range defaultTo {
		fmt.Printf("  %s\n", addr)
	}
	return nil
}

// printHelp 顯示 env 指令的詳細說明。
func printHelp() {
	fmt.Println()
	fmt.Println("env — 切換 XWatch 執行環境（dev / prod）")
	fmt.Println()
	fmt.Println("用法：")
	fmt.Println("  env                  顯示目前環境與已設定的收件人")
	fmt.Println("  env status           同上，明確查詢目前環境設定狀態")
	fmt.Println("  env set <env>        切換環境（dev 或 prod），並同步更新收件人清單")
	fmt.Println("  env help             顯示本說明")
	fmt.Println()
	fmt.Println("環境說明：")
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  dev\t開發環境，收件人不含外部單位")
	fmt.Fprintln(w, "  prod\t正式環境，收件人包含外部單位 589497@cpc.com.tw")
	_ = w.Flush()
	fmt.Println()
	fmt.Println("各環境預設收件人清單：")
	fmt.Printf("  dev:  %s\n", strings.Join(config.DefaultMailToListDev, ", "))
	fmt.Printf("  prod: %s\n", strings.Join(config.DefaultMailToListProd, ", "))
	fmt.Println()
	fmt.Println("注意：切換環境會自動將 mail 與 filecheck 收件人更新為目標環境的預設清單。")
	fmt.Println("      若需自訂收件人，切換後再執行 'mail set --to' 或 'filecheck mail enable --to'。")
	fmt.Println()
	fmt.Println("範例：")
	fmt.Println("  env                  # 顯示目前環境與收件人")
	fmt.Println("  env status           # 同上")
	fmt.Println("  env set dev          # 切換為開發環境並更新收件人")
	fmt.Println("  env set prod         # 切換為正式環境並更新收件人")
}
