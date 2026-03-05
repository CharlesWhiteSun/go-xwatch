package app

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// evaluateElevation 根據目前執行環境決定是否需要提升權限。
// 回傳值：
//   - "continue"  : 繼續正常執行（已提升、非互動或設定跳過）
//   - "relaunch"  : 已啟動提升程序，呼叫端應直接結束
//   - "exit"      : 使用者拒絕提升，呼叫端應以非零退出碼結束
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

// isInteractiveConsole 回傳 stdin 是否連接到互動式終端機。
func isInteractiveConsole() bool {
	file, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (file.Mode() & os.ModeCharDevice) != 0
}

// isElevated 回傳目前程序是否以系統管理員（elevated）權限執行。
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

// relaunchElevated 以 "runas" verb 重新啟動目前執行檔，要求提升為系統管理員。
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
