package watchexcludecmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// PasswordPromptFn 是讀取密碼的函式變數，預設使用終端機隱藏輸入（不回顯任何字元）。
// 設計為套件層級可替換函式（DIP），測試時可注入 mock 實作，無需真實終端機互動。
//
// 若 stdin 不是終端機（如管線或非互動環境），會回傳錯誤並提示使用者改用 --pw 旗標。
var PasswordPromptFn func(prompt string) (string, error) = defaultPasswordPrompt

func defaultPasswordPrompt(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("非互動式終端機，請透過 --pw 旗標直接提供密碼")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // 隱藏輸入後補換行，保持終端機排版整齊
	if err != nil {
		return "", fmt.Errorf("讀取密碼失敗：%w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
