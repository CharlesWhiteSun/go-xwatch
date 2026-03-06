//go:build windows

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFindServiceForRoot_MatchesExistingConfig 建立假的 ProgramData 目錄結構，
// 驗證 FindServiceForRoot 能正確偵測已被監控的根目錄。
func TestFindServiceForRoot_MatchesExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	// 覆寫 ProgramData 讓 paths.DataDir() 指向暫存目錄
	t.Setenv("ProgramData", tmp)

	watchedRoot := filepath.Join(tmp, "watched-root")
	if err := os.MkdirAll(watchedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// 建立 %ProgramData%\go-xwatch\plant-A\config.json
	svcDataDir := filepath.Join(tmp, "go-xwatch", "plant-A")
	if err := os.MkdirAll(svcDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgData, _ := json.Marshal(map[string]any{
		"rootDir":     watchedRoot,
		"serviceName": "GoXWatch-plant-A",
	})
	if err := os.WriteFile(filepath.Join(svcDataDir, "config.json"), cfgData, 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := FindServiceForRoot(watchedRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != "GoXWatch-plant-A" {
		t.Errorf("FindServiceForRoot = %q, want \"GoXWatch-plant-A\"", found)
	}
}

// TestFindServiceForRoot_NoMatch 確認不重疊時回傳空字串、無錯誤。
func TestFindServiceForRoot_NoMatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)

	// 建立另一個服務的設定（指向不同目錄）
	otherRoot := filepath.Join(tmp, "other-root")
	svcDataDir := filepath.Join(tmp, "go-xwatch", "other")
	if err := os.MkdirAll(svcDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgData, _ := json.Marshal(map[string]any{
		"rootDir":     otherRoot,
		"serviceName": "GoXWatch-other",
	})
	if err := os.WriteFile(filepath.Join(svcDataDir, "config.json"), cfgData, 0o644); err != nil {
		t.Fatal(err)
	}

	// 搜尋一個完全不同的目錄
	newRoot := filepath.Join(tmp, "new-plant")
	found, err := FindServiceForRoot(newRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != "" {
		t.Errorf("FindServiceForRoot should return empty string for unmonitored root, got %q", found)
	}
}

// TestFindServiceForRoot_EmptyDataDir 驗證 go-xwatch 目錄不存在時正常回傳空字串。
func TestFindServiceForRoot_EmptyDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	// go-xwatch 目錄完全不存在

	found, err := FindServiceForRoot(filepath.Join(tmp, "some-root"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != "" {
		t.Errorf("expected empty string, got %q", found)
	}
}

// TestFindServiceForRoot_FallsBackToSuffixName 確認設定中無 serviceName 時，
// 能從目錄後綴重建服務名稱。
func TestFindServiceForRoot_FallsBackToSuffixName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)

	watchedRoot := filepath.Join(tmp, "factory-B")
	if err := os.MkdirAll(watchedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	svcDataDir := filepath.Join(tmp, "go-xwatch", "factory-B")
	if err := os.MkdirAll(svcDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 故意不寫 serviceName 欄位（舊版設定格式）
	cfgData, _ := json.Marshal(map[string]any{
		"rootDir": watchedRoot,
	})
	if err := os.WriteFile(filepath.Join(svcDataDir, "config.json"), cfgData, 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := FindServiceForRoot(watchedRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != "GoXWatch-factory-B" {
		t.Errorf("FindServiceForRoot fallback = %q, want \"GoXWatch-factory-B\"", found)
	}
}
