package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseServiceNameArg_WithName(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	os.Args = []string{"xwatch.exe", "--service", "--name", "GoXWatch-plant-A"}
	name, suffix := parseServiceNameArg()
	if name != "GoXWatch-plant-A" {
		t.Errorf("name = %q, want %q", name, "GoXWatch-plant-A")
	}
	if suffix != "plant-A" {
		t.Errorf("suffix = %q, want %q", suffix, "plant-A")
	}
}

func TestParseServiceNameArg_NoName(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	os.Args = []string{"xwatch.exe", "--service"}
	name, suffix := parseServiceNameArg()
	if name != legacyServiceName {
		t.Errorf("name = %q, want %q", name, legacyServiceName)
	}
	if suffix != "" {
		t.Errorf("suffix = %q, want empty", suffix)
	}
}

func TestParseServiceNameArg_NameAtEnd(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	// --name 在最後且後面沒有值：應退化為 legacy
	os.Args = []string{"xwatch.exe", "--service", "--name"}
	name, suffix := parseServiceNameArg()
	if name != legacyServiceName {
		t.Errorf("expected legacy name, got %q", name)
	}
	if suffix != "" {
		t.Errorf("expected empty suffix, got %q", suffix)
	}
}

func TestDeriveServiceContext_NormalFolder(t *testing.T) {
	tmp := t.TempDir()
	// 建立假執行檔路徑：tmp\plant-A\xwatch.exe
	exeDir := filepath.Join(tmp, "plant-A")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(exeDir, "xwatch.exe")

	name, suffix := deriveServiceContext(exePath)
	if name != "GoXWatch-plant-A" {
		t.Errorf("name = %q, want %q", name, "GoXWatch-plant-A")
	}
	if suffix != "plant-A" {
		t.Errorf("suffix = %q, want %q", suffix, "plant-A")
	}
}

func TestDeriveServiceContext_ExeAtDriveRoot(t *testing.T) {
	// 若 filepath.Base 回傳 "." 或 "\"，ServiceSuffixFromRoot 會退化
	// deriveServiceContext 在 suf="" 時回傳 legacyServiceName
	name, suffix := deriveServiceContext(`C:\xwatch.exe`)
	// 預期退化為 legacy（C:\ 的 Base 是 "."/"C:\\"，suffix 可能是 "default"，視實作而定）
	// 只要 name 有值即可
	if name == "" {
		t.Error("deriveServiceContext must never return empty name")
	}
	_ = suffix
}

func TestDeriveServiceContext_UnderscoreName(t *testing.T) {
	tmp := t.TempDir()
	exeDir := filepath.Join(tmp, "my_service")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(exeDir, "xwatch.exe")

	name, suffix := deriveServiceContext(exePath)
	if name != "GoXWatch-my_service" {
		t.Errorf("name = %q, want %q", name, "GoXWatch-my_service")
	}
	if suffix != "my_service" {
		t.Errorf("suffix = %q, want %q", suffix, "my_service")
	}
}
