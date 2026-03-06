package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetGetResetServiceSuffix 驗證 suffix 狀態管理函式正確運作。
func TestSetGetResetServiceSuffix(t *testing.T) {
	defer ResetServiceSuffix()

	if GetServiceSuffix() != "" {
		t.Fatalf("initial suffix should be empty, got %q", GetServiceSuffix())
	}

	SetServiceSuffix("plant-A")
	if got := GetServiceSuffix(); got != "plant-A" {
		t.Errorf("GetServiceSuffix() = %q, want %q", got, "plant-A")
	}

	ResetServiceSuffix()
	if got := GetServiceSuffix(); got != "" {
		t.Errorf("after ResetServiceSuffix() = %q, want empty", got)
	}
}

// TestConfigPath_PerSuffix 確認設定 suffix 後，configPath 指向正確子目錄。
func TestConfigPath_PerSuffix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer ResetServiceSuffix()

	// 無後綴 → 傳統路徑
	SetServiceSuffix("")
	p, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	wantLegacy := filepath.Join(tmp, "go-xwatch", "config.json")
	if p != wantLegacy {
		t.Errorf("legacy path = %q, want %q", p, wantLegacy)
	}

	// 有後綴 → 子目錄路徑
	SetServiceSuffix("plant-A")
	p, err = configPath()
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join(tmp, "go-xwatch", "plant-A", "config.json")
	if p != wantSuffix {
		t.Errorf("per-suffix path = %q, want %q", p, wantSuffix)
	}
}

// TestSaveLoad_PerSuffix 驗證不同後綴的 Save/Load 不會互相干擾。
func TestSaveLoad_PerSuffix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	t.Setenv("XWATCH_SKIP_ACL", "1")
	defer ResetServiceSuffix()

	rootA := filepath.Join(tmp, "rootA")
	rootB := filepath.Join(tmp, "rootB")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatal(err)
	}

	// 儲存 plant-A 的設定
	SetServiceSuffix("plant-A")
	settingsA := Settings{RootDir: rootA, ServiceName: "GoXWatch-plant-A"}
	if err := Save(settingsA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// 儲存 plant-B 的設定
	SetServiceSuffix("plant-B")
	settingsB := Settings{RootDir: rootB, ServiceName: "GoXWatch-plant-B"}
	if err := Save(settingsB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// 讀取 plant-A
	SetServiceSuffix("plant-A")
	gotA, err := Load()
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	if gotA.RootDir != rootA {
		t.Errorf("A.RootDir = %q, want %q", gotA.RootDir, rootA)
	}
	if gotA.ServiceName != "GoXWatch-plant-A" {
		t.Errorf("A.ServiceName = %q, want %q", gotA.ServiceName, "GoXWatch-plant-A")
	}

	// 讀取 plant-B，確認不受 A 干擾
	SetServiceSuffix("plant-B")
	gotB, err := Load()
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	if gotB.RootDir != rootB {
		t.Errorf("B.RootDir = %q, want %q", gotB.RootDir, rootB)
	}
	if gotB.ServiceName != "GoXWatch-plant-B" {
		t.Errorf("B.ServiceName = %q, want %q", gotB.ServiceName, "GoXWatch-plant-B")
	}
}

// TestValidateAndFillDefaults_PreservesServiceName 確認 ServiceName 在 ValidateAndFillDefaults 後保留。
func TestValidateAndFillDefaults_PreservesServiceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)
	defer ResetServiceSuffix()
	SetServiceSuffix("plant-A")

	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	s := Settings{RootDir: root, ServiceName: "GoXWatch-plant-A"}
	got, err := ValidateAndFillDefaults(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceName != "GoXWatch-plant-A" {
		t.Errorf("ServiceName lost after ValidateAndFillDefaults: got %q", got.ServiceName)
	}
}
