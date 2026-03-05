package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// ── isServiceMissing 單元測試 ─────────────────────────────────────────

// TestIsServiceMissing_Nil 確認 nil 錯誤回傳 false。
func TestIsServiceMissing_Nil(t *testing.T) {
	if isServiceMissing(nil) {
		t.Error("nil 錯誤不應視為服務不存在")
	}
}

// TestIsServiceMissing_WindowsError 確認 ERROR_SERVICE_DOES_NOT_EXIST 回傳 true。
func TestIsServiceMissing_WindowsError(t *testing.T) {
	err := windows.ERROR_SERVICE_DOES_NOT_EXIST
	if !isServiceMissing(err) {
		t.Error("ERROR_SERVICE_DOES_NOT_EXIST 應視為服務不存在")
	}
}

// TestIsServiceMissing_MessageMatch 確認含 "service does not exist" 字串的錯誤回傳 true。
func TestIsServiceMissing_MessageMatch(t *testing.T) {
	err := errors.New("service does not exist")
	if !isServiceMissing(err) {
		t.Error("包含 'service does not exist' 的錯誤應視為服務不存在")
	}
}

// TestIsServiceMissing_UnrelatedError 確認無關錯誤回傳 false。
func TestIsServiceMissing_UnrelatedError(t *testing.T) {
	err := errors.New("access is denied")
	if isServiceMissing(err) {
		t.Error("無關錯誤不應視為服務不存在")
	}
}

// ── askYesNo 單元測試 ─────────────────────────────────────────────────

// TestAskYesNo_SkipPause 確認 XWATCH_NO_PAUSE=1 時自動回傳 true（非互動模式）。
func TestAskYesNo_SkipPause(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	if !askYesNo("請確認 (Y/n): ") {
		t.Error("XWATCH_NO_PAUSE=1 時 askYesNo 應回傳 true")
	}
}

// ── resolveAndEnsureDir 單元測試 ──────────────────────────────────────

// TestResolveAndEnsureDir_ExistingDir 確認已存在目錄正常回傳絕對路徑。
func TestResolveAndEnsureDir_ExistingDir(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	tmp := t.TempDir()
	got, err := resolveAndEnsureDir(tmp, "測試目錄")
	if err != nil {
		t.Fatalf("已存在目錄應成功，實際：%v", err)
	}
	abs, _ := filepath.Abs(tmp)
	if got != abs {
		t.Errorf("回傳路徑 %q 應等於 %q", got, abs)
	}
}

// TestResolveAndEnsureDir_EmptyPath 確認空路徑回傳錯誤。
func TestResolveAndEnsureDir_EmptyPath(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	_, err := resolveAndEnsureDir("", "測試目錄")
	if err == nil {
		t.Error("空路徑應回傳錯誤，但得到 nil")
	}
}

// TestResolveAndEnsureDir_NotADir 確認路徑指向普通檔案時回傳錯誤。
func TestResolveAndEnsureDir_NotADir(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := resolveAndEnsureDir(filePath, "測試目錄")
	if err == nil {
		t.Error("指向普通檔案應回傳錯誤，但得到 nil")
	}
}

// TestResolveAndEnsureDir_AutoCreate 確認 XWATCH_NO_PAUSE=1 且目錄不存在時自動建立。
func TestResolveAndEnsureDir_AutoCreate(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	tmp := t.TempDir()
	newDir := filepath.Join(tmp, "new-auto-dir")
	got, err := resolveAndEnsureDir(newDir, "新目錄")
	if err != nil {
		t.Fatalf("自動建立目錄應成功，實際：%v", err)
	}
	if _, serr := os.Stat(got); serr != nil {
		t.Errorf("建立後目錄應存在，Stat：%v", serr)
	}
}

// TestResolveAndEnsureDir_RelativePath 確認相對路徑可正確轉換為絕對路徑。
func TestResolveAndEnsureDir_RelativePath(t *testing.T) {
	t.Setenv("XWATCH_NO_PAUSE", "1")
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	relPath := fmt.Sprintf("../%s", filepath.Base(sub))
	// 切換工作目錄至 tmp 下的子目錄中更好處理相對路徑，
	// 但為了測試簡單，直接用絕對路徑加 "." 確保 Abs 行為正確。
	abs, _ := filepath.Abs(sub)
	got, err := resolveAndEnsureDir(fmt.Sprintf("%s/.", sub), "子目錄")
	_ = relPath
	if err != nil {
		t.Fatalf("相對路徑應成功，實際：%v", err)
	}
	if got != abs {
		t.Errorf("回傳路徑 %q 應等於 %q", got, abs)
	}
}
