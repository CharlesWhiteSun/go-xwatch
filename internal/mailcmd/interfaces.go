package mailcmd

import (
	"context"

	"go-xwatch/internal/mailer"
)

// GmailSender 抽象 Gmail SMTP 寄信行為，以介面取代函式型注入（ISP）。
// 與 mailer.SendGmail 函式簽名完全對應，讓呼叫端只依賴介面，
// 而非具體函式值，使 mock 可直接以 struct 實作，文件更清晰。
type GmailSender interface {
	SendGmail(
		ctx context.Context,
		cfg mailer.SMTPConfig,
		opts mailer.ReportOptions,
		sendFn mailer.SendMailFunc,
	) error
}

// realGmailSender 是 GmailSender 的正式實作，直接委派 mailer.SendGmail。
// Runner 預設使用此實作；測試時以實作 GmailSender 的 mock struct 替換。
type realGmailSender struct{}

// 確保 realGmailSender 編譯期即符合 GmailSender 介面。
var _ GmailSender = realGmailSender{}

func (realGmailSender) SendGmail(
	ctx context.Context,
	cfg mailer.SMTPConfig,
	opts mailer.ReportOptions,
	sendFn mailer.SendMailFunc,
) error {
	return mailer.SendGmail(ctx, cfg, opts, sendFn)
}
