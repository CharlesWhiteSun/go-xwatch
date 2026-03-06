package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go-xwatch/internal/app"
	"go-xwatch/internal/config"
	"go-xwatch/internal/opslog"
	"go-xwatch/internal/service"
)

// legacyServiceName 為無後綴時使用的傳統服務名稱（向後相容）。
const legacyServiceName = "GoXWatch"

// serviceName 儲存目前程序採用的服務名稱，由 main() 初始化後即不再變更。
var serviceName = legacyServiceName

var version = "dev"

var opsLogger = opslog.New(nil)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "this program currently supports Windows service mode only")
		os.Exit(1)
	}

	if service.IsWindowsServiceProcess() {
		// 服務模式：從啟動參數解析 --name，並據此設定 suffix
		name, suffix := parseServiceNameArg()
		serviceName = name
		config.SetServiceSuffix(suffix)
		if err := runAsService(); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			logOp("service error", "err", err)
			os.Exit(1)
		}
		return
	}

	// CLI 互動模式：從執行檔所在目錄推導服務名稱（與 initAndExit 一致）
	if exePath, err := os.Executable(); err == nil {
		exePath, _ = filepath.Abs(exePath)
		derived, suffix := deriveServiceContext(exePath)
		serviceName = derived
		config.SetServiceSuffix(suffix)
	}

	exitCode := app.RunCLI(version, serviceName, opsLogger)
	os.Exit(exitCode)
}

// parseServiceNameArg 從 os.Args 中解析 --name 的值，回傳 (serviceName, suffix)。
// 若未指定 --name，回傳傳統預設值。
func parseServiceNameArg() (name, suffix string) {
	args := os.Args[1:]
	for i, a := range args {
		if a == "--name" && i+1 < len(args) {
			n := args[i+1]
			if n != "" {
				return n, service.SuffixFromServiceName(n)
			}
		}
	}
	return legacyServiceName, ""
}

// deriveServiceContext 依執行檔路徑推導服務名稱與後綴。
// 邏輯：以執行檔父目錄名稱為後綴（與 initAndExit 中的 ServiceSuffixFromRoot 一致），
// 但若執行檔直接位於磁碟根目錄，則退化為傳統服務名稱。
func deriveServiceContext(exePath string) (name, suffix string) {
	parentDir := filepath.Dir(exePath)
	suf := service.ServiceSuffixFromRoot(parentDir)
	if suf == "" {
		return legacyServiceName, ""
	}
	return service.ServiceNameFromRoot(parentDir), suf
}

func logOp(msg string, args ...any) {
	if opsLogger == nil {
		return
	}
	opsLogger.Info(msg, args...)
}

func runAsService() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	if settings.RootDir == "" {
		return errors.New("empty root dir in config")
	}
	return service.Run(serviceName, settings)
}
