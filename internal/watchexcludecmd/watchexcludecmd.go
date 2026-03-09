// Package watchexcludecmd 實作監控目錄排除清單的隱藏管理指令。
// 此功能不對外公開，不顯示於任何 CLI 說明或主畫面中，僅供後台管理使用。
// 所有修改類子指令均需透過 --pw 旗標或互動式隱藏輸入進行授權。
package watchexcludecmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"go-xwatch/internal/config"
)

// Run 是隱藏指令 watchexclude 的入口函式。
func Run(args []string) error {
	if len(args) == 0 {
		return errors.New("watchexclude: 請指定子指令（status/enable/disable/add-to/set/passwd/help）")
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "help":
		return authorized(rest, func(_ []string) error {
			printHelp()
			return nil
		})
	case "status":
		return authorized(rest, func(_ []string) error {
			return runStatus()
		})
	case "enable":
		return authorized(rest, func(_ []string) error {
			return setEnabled(true)
		})
	case "disable":
		return authorized(rest, func(_ []string) error {
			return setEnabled(false)
		})
	case "add-to":
		return authorized(rest, func(remaining []string) error {
			return runAddTo(remaining)
		})
	case "set":
		return authorized(rest, func(remaining []string) error {
			return runSet(remaining)
		})
	case "passwd":
		return runPasswd(rest)
	default:
		return fmt.Errorf("watchexclude: 未知子指令 %q", sub)
	}
}

// authorized 從 args 中提取密碼旗標，驗證後以剩餘 args 呼叫 fn。
// 密碼提取優先順序：--pw（新格式）> --passwd（舊格式，向後相容）。
// 若兩者皆未提供，會呼叫 PasswordPromptFn 取得互動式隱藏輸入。
func authorized(args []string, fn func(remaining []string) error) error {
	pw, remaining := extractPasswordFlag(args)

	if pw == "" {
		var err error
		pw, err = PasswordPromptFn("密碼：")
		if err != nil {
			return fmt.Errorf("讀取密碼失敗：%w", err)
		}
	}

	s, err := config.Load()
	if err != nil {
		return err
	}
	if !config.VerifyWatchExcludePassword(pw, s.WatchExclude.PasswordHash) {
		return errors.New("密碼錯誤")
	}
	return fn(remaining)
}

// extractPasswordFlag 支援 --pw（新格式）與 --passwd（舊格式，向後相容）。
// 優先採用 --pw；若不存在則嘗試 --passwd，使既有腳本不受旗標更名影響。
func extractPasswordFlag(args []string) (pw string, remaining []string) {
	if pw, remaining = extractFlag(args, "pw"); pw != "" {
		return
	}
	return extractFlag(args, "passwd")
}

// extractFlag 從 args 中線性搜尋指定名稱的 flag（--name 或 --name=value），
// 回傳其值與去除該 flag 後的剩餘 args。不匹配時回傳空字串與原 args。
func extractFlag(args []string, name string) (value string, remaining []string) {
	prefixEq := "--" + name + "="
	prefixSpace := "--" + name
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, prefixEq) {
			value = a[len(prefixEq):]
			result = append(result, args[i+1:]...)
			return value, result
		}
		if a == prefixSpace && i+1 < len(args) {
			value = args[i+1]
			result = append(result, args[i+2:]...)
			return value, result
		}
		result = append(result, a)
	}
	return "", result
}

// runStatus 顯示目前排除功能的啟用狀態與目錄清單。
func runStatus() error {
	s, err := config.Load()
	if err != nil {
		return err
	}
	we := s.WatchExclude
	fmt.Printf("功能狀態：%s\n", boolToStatus(we.IsEnabled()))
	fmt.Printf("排除目錄清單（共 %d 項）：\n", len(we.Dirs))
	if len(we.Dirs) == 0 {
		fmt.Println("  （清單為空）")
	} else {
		for _, d := range we.Dirs {
			fmt.Printf("  %s\n", d)
		}
	}
	return nil
}

// setEnabled 將功能啟用/停用狀態寫入設定檔。
func setEnabled(enabled bool) error {
	s, err := config.Load()
	if err != nil {
		return err
	}
	s.WatchExclude.Enabled = config.BoolPtr(enabled)
	if err := config.Save(s); err != nil {
		return err
	}
	fmt.Printf("排除功能已%s。服務重啟後生效。\n", boolToVerb(enabled))
	return nil
}

// runAddTo 將單一目錄名稱追加到排除清單（自動去重）。
// 可透過 --to <dir> 旗標或直接使用第一個位置參數指定目錄名稱。
func runAddTo(args []string) error {
	fs := flag.NewFlagSet("watchexclude add-to", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	toFlag := fs.String("to", "", "目錄名稱（相對 rootDir 或絕對路徑）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := strings.TrimSpace(*toFlag)
	if dir == "" && len(fs.Args()) > 0 {
		dir = strings.TrimSpace(fs.Args()[0])
	}
	if dir == "" {
		return errors.New("請指定目錄名稱，用法：add-to <dir> --pw <password>")
	}

	s, err := config.Load()
	if err != nil {
		return err
	}

	for _, existing := range s.WatchExclude.Dirs {
		if strings.EqualFold(existing, dir) {
			fmt.Printf("%q 已在排除清單中，略過。\n", dir)
			return nil
		}
	}
	s.WatchExclude.Dirs = append(s.WatchExclude.Dirs, dir)
	if err := config.Save(s); err != nil {
		return err
	}
	fmt.Printf("已將 %q 加入排除清單（共 %d 項）。服務重啟後生效。\n", dir, len(s.WatchExclude.Dirs))
	return nil
}

// runSet 以逗號分隔的清單完整覆寫排除目錄清單。
func runSet(args []string) error {
	fs := flag.NewFlagSet("watchexclude set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirsFlag := fs.String("dirs", "", "以逗號分隔的目錄名稱清單（覆寫現有清單）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw := strings.TrimSpace(*dirsFlag)
	if raw == "" {
		return errors.New("請提供 --dirs 旗標，用法：set --dirs <dir1,dir2,...> --pw <password>")
	}

	var dirs []string
	for _, d := range strings.Split(raw, ",") {
		t := strings.TrimSpace(d)
		if t != "" {
			dirs = append(dirs, t)
		}
	}
	if len(dirs) == 0 {
		return errors.New("--dirs 清單不得為空")
	}

	s, err := config.Load()
	if err != nil {
		return err
	}
	s.WatchExclude.Dirs = dirs
	if err := config.Save(s); err != nil {
		return err
	}
	fmt.Printf("排除清單已更新（共 %d 項）。服務重啟後生效。\n", len(dirs))
	return nil
}

// runPasswd 驗證目前密碼後將新密碼的雜湊值寫入設定檔。
// 若未提供 --pw 或 --new 旗標，會呼叫 PasswordPromptFn 進行互動式隱藏輸入。
func runPasswd(args []string) error {
	pw, rest := extractPasswordFlag(args)
	newPw, _ := extractFlag(rest, "new")

	var err error
	if pw == "" {
		pw, err = PasswordPromptFn("目前密碼：")
		if err != nil {
			return fmt.Errorf("讀取目前密碼失敗：%w", err)
		}
	}
	if strings.TrimSpace(pw) == "" {
		return errors.New("目前密碼不得為空白")
	}
	if newPw == "" {
		newPw, err = PasswordPromptFn("新密碼：")
		if err != nil {
			return fmt.Errorf("讀取新密碼失敗：%w", err)
		}
	}
	if strings.TrimSpace(newPw) == "" {
		return errors.New("新密碼不得為空白")
	}

	s, err := config.Load()
	if err != nil {
		return err
	}
	if !config.VerifyWatchExcludePassword(pw, s.WatchExclude.PasswordHash) {
		return errors.New("目前密碼錯誤")
	}
	s.WatchExclude.PasswordHash = config.HashWatchExcludePassword(newPw)
	if err := config.Save(s); err != nil {
		return err
	}
	fmt.Println("密碼已更新。")
	return nil
}

// printHelp 顯示此隱藏功能的操作說明。
func printHelp() {
	fmt.Println()
	fmt.Println("監控目錄排除清單管理")
	fmt.Println()
	fmt.Println("用法：watchexclude <subcommand> --pw <password> [flags]")
	fmt.Println("      若省略 --pw，將以互動方式提示輸入密碼（不顯示輸入內容）")
	fmt.Println()
	fmt.Println("子指令：")
	fmt.Println("  status                          顯示目前啟用狀態與排除目錄清單")
	fmt.Println("  enable                          啟用排除功能（服務重啟後生效）")
	fmt.Println("  disable                         停用排除功能（服務重啟後生效）")
	fmt.Println("  add-to <dir>                    追加目錄至排除清單（不覆蓋現有）")
	fmt.Println("  set --dirs <dir1,dir2,...>       完整覆寫排除目錄清單")
	fmt.Println("  passwd [--pw <old>] [--new <new>]  更新管理密碼（省略旗標則互動輸入）")
	fmt.Println("  help                            顯示本說明")
	fmt.Println()
	fmt.Println("授權旗標（passwd 子指令亦適用）：")
	fmt.Println("  --pw <password>   管理密碼（簡短格式，推薦）")
	fmt.Println("  --passwd <password>  管理密碼（舊格式，向後相容）")
	fmt.Println()
	fmt.Println("注意：排除清單變更需重啟服務後才會生效。")
}

func boolToStatus(b bool) string {
	if b {
		return "已啟用"
	}
	return "已停用"
}

func boolToVerb(b bool) string {
	if b {
		return "啟用"
	}
	return "停用"
}
