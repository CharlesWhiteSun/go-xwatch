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

// TestPrintUsage_NotContainsClear
// 確認 printUsage 的輸出中不列出 "clear" 指令說明（隱藏指令，不展示於主畫面）。
// 注意：clear 指令本身仍可正常執行，只是不出現於使用說明中。
func TestPrintUsage_NotContainsClear(t *testing.T) {
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

	if strings.Contains(out, "  clear") {
		t.Errorf("printUsage 輸出不應包含 'clear' 指令說明，實際輸出：\n%s", out)
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
