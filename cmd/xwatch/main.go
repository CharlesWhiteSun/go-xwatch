package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"go-xwatch/internal/app"
	"go-xwatch/internal/config"
	"go-xwatch/internal/opslog"
	"go-xwatch/internal/service"
)

const serviceName = "GoXWatch"

var version = "dev"

var opsLogger = opslog.New(nil)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "this program currently supports Windows service mode only")
		os.Exit(1)
	}

	if service.IsWindowsServiceProcess() {
		if err := runAsService(); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			logOp("service error", "err", err)
			os.Exit(1)
		}
		return
	}

	exitCode := app.RunCLI(version, serviceName, opsLogger)
	os.Exit(exitCode)
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
