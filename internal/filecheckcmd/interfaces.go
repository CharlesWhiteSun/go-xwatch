package filecheckcmd

import (
	"context"

	"go-xwatch/internal/mailer"
)

// TextMailSender 抽象純文字郵件寄送行為，以介面取代函式型注入（ISP）。
// 與 mailer.SendTextMail 函式簽名完全對應，讓呼叫端只依賴介面，
// 而非具體函式值，使 mock 可直接以 struct 實作，文件更清晰。
type TextMailSender interface {
	SendTextMail(
		ctx context.Context,
		cfg mailer.SMTPConfig,
		subject, body string,
		fn mailer.SendMailFunc,
	) error
}

// realTextMailSender 是 TextMailSender 的正式實作，直接委派 mailer.SendTextMail。
// Runner 預設使用此實作；測試時以實作 TextMailSender 的 mock struct 替換。
type realTextMailSender struct{}

// 確保 realTextMailSender 編譯期即符合 TextMailSender 介面。
var _ TextMailSender = realTextMailSender{}

func (realTextMailSender) SendTextMail(
	ctx context.Context,
	cfg mailer.SMTPConfig,
	subject, body string,
	fn mailer.SendMailFunc,
) error {
	return mailer.SendTextMail(ctx, cfg, subject, body, fn)
}
