// Package watchexcludecmd 實作監控目錄排除清單的隱藏管理指令。
// 此功能不對外公開，不顯示於任何 CLI 說明或主畫面中，僅供後台管理使用。
// 所有修改類子指令均需提供 --passwd 旗標進行授權。
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

// authorized 從 args 中提取 --passwd 旗標，驗證密碼後以剩餘 args 呼叫 fn。
// 密碼不符時回傳錯誤，不執行 fn。
func authorized(args []string, fn func(remaining []string) error) error {
	passwd, remaining := extractFlag(args, "passwd")

	s, err := config.Load()
	if err != nil {
		return err
	}
	if !config.VerifyWatchExcludePassword(passwd, s.WatchExclude.PasswordHash) {
		return errors.New("密碼錯誤")
	}
	return fn(remaining)
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
		return errors.New("請指定目錄名稱，用法：add-to <dir> --passwd <password>")
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
		return errors.New("請提供 --dirs 旗標，用法：set --dirs <dir1,dir2,...> --passwd <password>")
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
func runPasswd(args []string) error {
	passwd, rest := extractFlag(args, "passwd")
	newPasswd, _ := extractFlag(rest, "new")

	if passwd == "" || newPasswd == "" {
		return errors.New("用法：passwd --passwd <目前密碼> --new <新密碼>")
	}
	if strings.TrimSpace(newPasswd) == "" {
		return errors.New("新密碼不得為空白")
	}

	s, err := config.Load()
	if err != nil {
		return err
	}
	if !config.VerifyWatchExcludePassword(passwd, s.WatchExclude.PasswordHash) {
		return errors.New("目前密碼錯誤")
	}
	s.WatchExclude.PasswordHash = config.HashWatchExcludePassword(newPasswd)
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
	fmt.Println("用法：watchexclude <subcommand> --passwd <password> [flags]")
	fmt.Println()
	fmt.Println("子指令：")
	fmt.Println("  status                         顯示目前啟用狀態與排除目錄清單")
	fmt.Println("  enable                         啟用排除功能（服務重啟後生效）")
	fmt.Println("  disable                        停用排除功能（服務重啟後生效）")
	fmt.Println("  add-to <dir>                   追加目錄至排除清單（不覆蓋現有）")
	fmt.Println("  set --dirs <dir1,dir2,...>      完整覆寫排除目錄清單")
	fmt.Println("  passwd --passwd <old> --new <new>  更新管理密碼")
	fmt.Println("  help                           顯示本說明")
	fmt.Println()
	fmt.Println("必要旗標（passwd 子指令除外）：")
	fmt.Println("  --passwd <password>  管理密碼")
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
