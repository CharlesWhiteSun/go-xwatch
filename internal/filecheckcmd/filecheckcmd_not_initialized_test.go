package filecheckcmd

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

// TestFilecheckStatus_NotInitialized_ReturnsErrNotInitialized
// 確認 remove 後（設定檔不存在）執行 filecheck status 回傳的是
// config.ErrNotInitialized，而非原始 os 檔案錯誤。
func TestFilecheckStatus_NotInitialized_ReturnsErrNotInitialized(t *testing.T) {
	setupNoConfig(t)

	err := Run([]string{"status"})
	if err == nil {
		t.Fatal("設定檔不存在時 filecheck status 應回傳錯誤，但回傳 nil")
	}
	if !errors.Is(err, config.ErrNotInitialized) {
		t.Fatalf("預期 config.ErrNotInitialized，實際：%v", err)
	}
}

// TestFilecheckStatus_Initialized_NoError 確認初始化後 filecheck status 正常執行。
func TestFilecheckStatus_Initialized_NoError(t *testing.T) {
	setupConfig(t)

	if err := Run([]string{"status"}); err != nil {
		t.Fatalf("初始化後 filecheck status 不應回傳錯誤，got：%v", err)
	}
}

// TestFilecheckEnable_NotInitialized_ReturnsErrNotInitialized
// 確認未初始化時 filecheck enable 也回傳 ErrNotInitialized。
func TestFilecheckEnable_NotInitialized_ReturnsErrNotInitialized(t *testing.T) {
	setupNoConfig(t)

	err := mailEnable(nil)
	if err == nil {
		t.Fatal("設定檔不存在時 enable 應回傳錯誤")
	}
	if !errors.Is(err, config.ErrNotInitialized) {
		t.Fatalf("預期 config.ErrNotInitialized，實際：%v", err)
	}
}
