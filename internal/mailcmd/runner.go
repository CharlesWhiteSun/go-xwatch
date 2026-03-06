package mailcmd

import (
	"fmt"
	"strings"
	"time"
)

// mailCmdRunner 實作 cli.ServiceAwareRunner。
// sender 欄位以 GmailSender 介面取代函式型注入（ISP），
// 讓依賴更清晰可文件化，測試可直接以 struct mock 替換，
// 無需傳遞 function literal。
type mailCmdRunner struct {
	sender GmailSender
}

// Run 是主要派發器。
// 以 r.sender 取代舊有的 sendWithGmailFn(args, nil) 函式型注入，
// 統一透過 GmailSender 介面執行寄信。
func (r mailCmdRunner) Run(args []string) error {
	if len(args) == 0 {
		printMailUsage()
		return nil
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "help":
		printMailHelp(time.Now())
		return nil
	case "status":
		return status()
	case "enable":
		return enable(rest)
	case "disable":
		return disable()
	case "set":
		return set(rest)
	case "add-to":
		return addTo(rest)
	case "send":
		return sendWithSender(rest, r.sender)
	default:
		return fmt.Errorf("未知子指令: %s", sub)
	}
}

// ServiceRequiredFor 宣告 "enable" 子指令需要 Windows 服務已安裝。
func (r mailCmdRunner) ServiceRequiredFor() (string, []string) {
	return "郵件", []string{"enable"}
}

// Runner 是 mailcmd 套件的預設指令執行者，實作 cli.ServiceAwareRunner。
// sender 預設使用 realGmailSender（委派 mailer.SendGmail）；
// 整合測試可建立 mailCmdRunner{sender: mockSender} 進行注入。
var Runner = mailCmdRunner{sender: realGmailSender{}}
