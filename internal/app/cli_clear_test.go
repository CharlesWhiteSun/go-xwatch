package app

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestBuildCommandRegistry_ClearRegistered
// 確認 buildCommandRegistry 已將 "clear" 作為有效指令名稱註冊。
func TestBuildCommandRegistry_ClearRegistered(t *testing.T) {
	setupMinimalCLIConfig(t)
	app := &cliApp{serviceName: "GoXWatch"}
	reg := app.buildCommandRegistry()
	if _, ok := reg.Get("clear"); !ok {
		t.Error("buildCommandRegistry 應包含 'clear' 指令，但未找到")
	}
}

// TestPrintUsage_ContainsClear
// 確認 printUsage 的輸出中包含 "clear" 指令說明。
func TestPrintUsage_ContainsClear(t *testing.T) {
	// 截取 stdout
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	app := &cliApp{version: "test"}
	app.printUsage()

	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "clear") {
		t.Errorf("printUsage 輸出應包含 'clear' 指令，實際輸出：\n%s", out)
	}
}

// TestClearScreen_NoPanic
// 確認 clearScreen() 呼叫不會 panic（執行環境可能無終端，允許錯誤回傳）。
func TestClearScreen_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("clearScreen() panic: %v", r)
		}
	}()
	// 於 CI / 測試環境中 cls 可能無輸出目標而失敗，但不應 panic
	_ = clearScreen()
}
