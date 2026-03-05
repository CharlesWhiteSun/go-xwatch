package app

import (
	"testing"
)

// ── Help 函式不崩潰測試 ────────────────────────────────────────────────

// TestPrintUsage_NoPanic 確認 printUsage 可正常執行不 panic。
func TestPrintUsage_NoPanic(t *testing.T) {
	app := &cliApp{version: "1.2.3"}
	app.printUsage()
}

// TestPrintInitHelp_NoPanic 確認 printInitHelp 可正常執行不 panic。
func TestPrintInitHelp_NoPanic(t *testing.T) {
	printInitHelp()
}

// TestPrintDBHelp_NoPanic 確認 printDBHelp 可正常執行不 panic。
func TestPrintDBHelp_NoPanic(t *testing.T) {
	printDBHelp()
}

// TestPrintExportHelp_NoPanic 確認 printExportHelp 可正常執行不 panic。
func TestPrintExportHelp_NoPanic(t *testing.T) {
	printExportHelp()
}
