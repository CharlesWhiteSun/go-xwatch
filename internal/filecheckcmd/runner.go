package filecheckcmd

import (
	"fmt"
	"strings"
)

// filecheckCmdRunner 實作 cli.ServiceAwareRunner。
// sender 欄位以 TextMailSender 介面取代函式型注入（ISP），
// 讓依賴更清晰可文件化，測試可直接以 struct mock 替換。
type filecheckCmdRunner struct {
	sender TextMailSender
}

// Run 是主要派發器。
// 以 r.sender 取代舊有的 mailSend(args, sendFn) 函式型注入，
// 統一透過 TextMailSender 介面執行純文字郵件寄送。
func (r filecheckCmdRunner) Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "help":
		printHelp()
		return nil
	case "status":
		return mailStatus()
	case "enable":
		return mailEnable(rest)
	case "disable":
		return mailDisable()
	case "add-to":
		return mailAddTo(rest)
	case "send":
		return mailSendWithSender(rest, r.sender)
	default:
		return fmt.Errorf("filecheck: 未知子指令 %q，請執行 'filecheck help' 查看說明", sub)
	}
}

// ServiceRequiredFor 宣告 "enable" 子指令需要 Windows 服務已安裝。
func (r filecheckCmdRunner) ServiceRequiredFor() (string, []string) {
	return "目錄檔案檢查", []string{"enable"}
}

// Runner 是 filecheckcmd 套件的預設指令執行者，實作 cli.ServiceAwareRunner。
// sender 預設使用 realTextMailSender（委派 mailer.SendTextMail）；
// 整合測試可建立 filecheckCmdRunner{sender: mockSender} 進行注入。
var Runner = filecheckCmdRunner{sender: realTextMailSender{}}
