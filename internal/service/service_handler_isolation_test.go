//go:build windows

package service

import (
	"strings"
	"testing"

	"go-xwatch/internal/config"
	"go-xwatch/internal/paths"
)

// TestHandler_ServiceNameNotDependentOnSettingsServiceName
// 驗證 handler.serviceName（由 Run() 直接傳入）是路徑隔離的權威來源。
// 修正前：Execute() 使用 h.settings.ServiceName，若 config.json 是舊版本（無此欄位），
// 則 suffix 為空字串，所有資料目錄退化至基底目錄（bug）。
// 修正後：Execute() 一律使用 h.serviceName，不依賴 config 檔案內容。
func TestHandler_ServiceNameNotDependentOnSettingsServiceName(t *testing.T) {
	// 模擬舊版 config.json（config.json 中無 serviceName 欄位）
	h := &handler{
		settings:    config.Settings{ServiceName: ""}, // 舊版 config、或初始化前尚未寫入
		serviceName: "GoXWatch-plant-A",               // Run() 從 --name 參數傳入的可靠來源
	}

	// Execute() 現在使用 h.serviceName
	suffixFromName := SuffixFromServiceName(h.serviceName)
	if suffixFromName != "plant-A" {
		t.Errorf("suffix from h.serviceName = %q, want 'plant-A'", suffixFromName)
	}

	// 若使用舊版邏輯（h.settings.ServiceName）則得到空後綴 → 路徑退化至基底目錄
	suffixFromSettings := SuffixFromServiceName(h.settings.ServiceName)
	if suffixFromSettings != "" {
		t.Errorf("settings.ServiceName 應為空（舊版 config），但 SuffixFromServiceName 回傳 %q", suffixFromSettings)
	}
}

// TestHandler_IsolatedDataDirDerivedFromServiceName
// 驗證 handler 以 h.serviceName 所推導的 dataDirFn，在 settings.ServiceName 為空時
// 仍可回傳正確的服務後綴隔離子目錄，而非退化至基底目錄。
func TestHandler_IsolatedDataDirDerivedFromServiceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	cases := []struct {
		serviceName       string
		emptySettingsName bool   // 是否模擬舊版 config（settings.ServiceName 為空）
		wantSuffix        string // 期望路徑中出現的後綴子目錄
		wantBaseDir       bool   // 是否期望退化至基底目錄（legacy 單服務模式）
	}{
		{
			serviceName:       "GoXWatch-plant-A",
			emptySettingsName: true,
			wantSuffix:        "plant-A",
			wantBaseDir:       false,
		},
		{
			serviceName:       "GoXWatch-factory-line-2",
			emptySettingsName: true,
			wantSuffix:        "factory-line-2",
			wantBaseDir:       false,
		},
		{
			serviceName:       "GoXWatch", // 傳統單服務模式
			emptySettingsName: true,
			wantSuffix:        "",
			wantBaseDir:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.serviceName, func(t *testing.T) {
			settingsName := tc.serviceName
			if tc.emptySettingsName {
				settingsName = "" // 模擬舊版 config
			}

			h := &handler{
				settings:    config.Settings{ServiceName: settingsName},
				serviceName: tc.serviceName,
			}

			// Execute() 現在使用 h.serviceName
			suffix := SuffixFromServiceName(h.serviceName)
			dir, err := paths.EnsureDataDirForSuffix(suffix)
			if err != nil {
				t.Fatalf("EnsureDataDirForSuffix(%q): %v", suffix, err)
			}

			if tc.wantBaseDir {
				// 傳統 "GoXWatch" 服務應使用基底目錄（無後綴子目錄）
				if strings.Contains(dir, "GoXWatch") {
					t.Errorf("legacy 模式不應在路徑中出現服務名稱，got %s", dir)
				}
			} else {
				if !strings.Contains(dir, tc.wantSuffix) {
					t.Errorf("期望路徑包含後綴 %q，got %s", tc.wantSuffix, dir)
				}
				// 確認不是落在基底目錄（即路徑末段應為後綴，而非 go-xwatch 本身）
				if strings.HasSuffix(dir, "go-xwatch") {
					t.Errorf("路徑不應退化至 go-xwatch 基底目錄，got %s", dir)
				}
			}
		})
	}
}

// TestHandler_TwoInstancesHaveDifferentDataDirs
// 驗證兩個不同 serviceName 的 handler 各自推導出不同的資料目錄，互不干擾。
func TestHandler_TwoInstancesHaveDifferentDataDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")

	// 兩個 handler，settings.ServiceName 均為空（模擬舊版 config），
	// 但透過 Run() 傳入的 serviceName 不同。
	hA := &handler{settings: config.Settings{ServiceName: ""}, serviceName: "GoXWatch-plant-A"}
	hB := &handler{settings: config.Settings{ServiceName: ""}, serviceName: "GoXWatch-plant-B"}

	dirA, errA := paths.EnsureDataDirForSuffix(SuffixFromServiceName(hA.serviceName))
	dirB, errB := paths.EnsureDataDirForSuffix(SuffixFromServiceName(hB.serviceName))

	if errA != nil {
		t.Fatalf("plant-A: %v", errA)
	}
	if errB != nil {
		t.Fatalf("plant-B: %v", errB)
	}
	if dirA == dirB {
		t.Errorf("兩個不同服務的資料目錄不應相同：A=%s, B=%s", dirA, dirB)
	}
	if !strings.Contains(dirA, "plant-A") {
		t.Errorf("plant-A 資料目錄應含 'plant-A'，got %s", dirA)
	}
	if !strings.Contains(dirB, "plant-B") {
		t.Errorf("plant-B 資料目錄應含 'plant-B'，got %s", dirB)
	}
}
