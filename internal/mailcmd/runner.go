package mailcmd

// mailCmdRunner 實作 cli.ServiceAwareRunner，
// 讓 CLI 層可透過抽象介面取得「哪些子指令需要服務已安裝」，
// 而無需在 cli.go 中硬編碼 mailcmd 的細節。
type mailCmdRunner struct{}

// Run 委派至套件層級的 Run 函式。
func (r mailCmdRunner) Run(args []string) error {
	return Run(args)
}

// ServiceRequiredFor 宣告 "enable" 子指令需要 Windows 服務已安裝。
func (r mailCmdRunner) ServiceRequiredFor() (string, []string) {
	return "郵件", []string{"enable"}
}

// Runner 是 mailcmd 套件的預設指令執行者，實作 cli.ServiceAwareRunner。
// CLI 層透過此實例取得服務安裝需求聲明。
var Runner mailCmdRunner
