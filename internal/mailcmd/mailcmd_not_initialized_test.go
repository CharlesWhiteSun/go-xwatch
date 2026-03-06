package mailcmd

import (
	"errors"
	"testing"

	"go-xwatch/internal/config"
)

// setupNoConfig 建立空暫存目錄（無 config.json），模擬 remove 後未重新初始化的狀態。
func setupNoConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	config.ResetServiceSuffix()
}

// TestMailStatus_NotInitialized_ReturnsErrNotInitialized
// 確認 remove 後（設定檔不存在）執行 mail status 回傳的是
// config.ErrNotInitialized，而非原始 os 檔案錯誤。
func TestMailStatus_NotInitialized_ReturnsErrNotInitialized(t *testing.T) {
	setupNoConfig(t)

	err := Run([]string{"status"})
	if err == nil {
		t.Fatal("設定檔不存在時 mail status 應回傳錯誤，但回傳 nil")
	}
	if !errors.Is(err, config.ErrNotInitialized) {
		t.Fatalf("預期 config.ErrNotInitialized，實際：%v", err)
	}
}

// TestMailStatus_Initialized_NoError 確認初始化後 mail status 可正常執行。
func TestMailStatus_Initialized_NoError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	config.ResetServiceSuffix()

	setupTestMailConfig(t, tmp, config.MailSettings{})

	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("初始化後 mail status 不應回傳錯誤，got：%v", err)
	}
}

// TestMailEnable_NotInitialized_ReturnsErrNotInitialized
// 確認未初始化時 mail enable 也回傳 ErrNotInitialized。
func TestMailEnable_NotInitialized_ReturnsErrNotInitialized(t *testing.T) {
	setupNoConfig(t)
	t.Setenv("XWATCH_SKIP_SERVICE_OPS", "1")

	// enable 子指令在 registerServiceAware 的前置檢查可能先攔截（服務未安裝）；
	// 直接呼叫套件層級函式以繞過服務檢查，驗證 config 層的 ErrNotInitialized。
	err := enable(nil)
	if err == nil {
		t.Fatal("設定檔不存在時 enable 應回傳錯誤")
	}
	if !errors.Is(err, config.ErrNotInitialized) {
		t.Fatalf("預期 config.ErrNotInitialized，實際：%v", err)
	}
}
