package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-xwatch/internal/config"
	"go-xwatch/internal/crypto"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/paths"
	"go-xwatch/internal/service"

	"golang.org/x/sys/windows"
)

// initAndExit 執行初始化流程：寫入設定，並視需要安裝或更新 Windows 服務。
func (c *cliApp) initAndExit(rootArg string, installService bool) error {
	fmt.Println("[1/3] 準備初始化...")
	root, err := c.resolveRootForInit(rootArg)
	if err != nil {
		return err
	}
	fmt.Println("[2/3] 寫入設定檔...")
	// 嘗試載入既有設定，若存在則保留所有設定僅更新根目錄；
	// 設定檔不存在（首次初始化或移除後）則以預設値建立（環境預設 dev）。
	existing, loadErr := config.Load()
	var settings config.Settings
	if loadErr == nil {
		settings = existing
		settings.RootDir = root
	} else {
		settings = config.Settings{RootDir: root}
	}
	if err := config.Save(settings); err != nil {
		return err
	}

	if installService {
		fmt.Println("[3/3] 註冊或更新 Windows 服務並啟動...")
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return err
		}

		if err := service.InstallOrUpdate(c.serviceName, exePath, "--service"); err != nil {
			return fmt.Errorf("無法註冊服務: %w", err)
		}

		if err := service.Start(c.serviceName); err != nil && !errors.Is(err, service.ErrAlreadyRunning) {
			return fmt.Errorf("無法啟動服務: %w", err)
		}
	} else {
		fmt.Println("[3/3] 已完成設定，未註冊/啟動服務。需註冊請改用 --install-service。")
	}

	fmt.Println("完成。")
	return nil
}

// printStatus 輸出服務狀態、權限等級及設定摘要。
func (c *cliApp) printStatus() error {
	status, err := service.Status(c.serviceName)
	if err != nil {
		if isServiceMissing(err) {
			fmt.Fprintln(os.Stderr, "提示：服務尚未安裝。請先執行『init --install-service』安裝 Windows 服務後，再使用 status 指令查看完整狀態。")
		}
		return err
	}
	fmt.Println("service:", c.serviceName)
	fmt.Println("status:", status)

	// 顯示目前 CLI 執行的 Windows 權限等級
	if isElevated() {
		fmt.Println("privilege: administrator（系統管理員）")
	} else {
		fmt.Println("privilege: standard user（一般使用者）")
	}

	// 顯示服務所使用的 Windows 帳戶
	if account, aerr := service.ServiceAccount(c.serviceName); aerr == nil {
		fmt.Println("service account:", account)
	}

	settings, err := config.Load()
	if err == nil {
		fmt.Println("root:", settings.RootDir)
		fmt.Println("heartbeat:", settings.HeartbeatEnabled)
		if settings.HeartbeatEnabled {
			fmt.Printf("heartbeat interval: %d 秒\n", settings.HeartbeatInterval)
		}
	} else {
		fmt.Println("root: (讀取設定失敗)")
	}

	dataDir, derr := paths.EnsureDataDir()
	if derr == nil {
		fmt.Println("data dir:", dataDir)
		journalPath := filepath.Join(dataDir, "journal.db")
		fmt.Println("journal:", journalPath)
		if key, kerr := crypto.LoadOrCreateKey(filepath.Join(dataDir, "key.bin"), 32); kerr == nil {
			if j, jerr := journal.Open(journalPath, key); jerr == nil {
				if n, cerr := j.Count(context.Background()); cerr == nil {
					fmt.Println("journal entries:", n)
				}
				_ = j.Close()
			}
		}
	} else {
		fmt.Println("data dir: (無法取得)")
	}
	return nil
}

// stopAndUninstall 停止並移除 Windows 服務，同時停用所有功能並刪除設定檔。
func (c *cliApp) stopAndUninstall() error {
	if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("無法停止服務: %w", err)
	}
	c.logOp("remove step", "step", "XWatch 註冊之 Windows 服務已主動停止")
	fmt.Println("[1/5] XWatch 註冊之 Windows 服務已主動停止。")

	// 停用所有功能並寫入設定
	if err := c.disableAllFeaturesOnRemove(); err != nil {
		// 停用失敗不中斷移除，記錄後繼續
		c.logOp("remove step", "step", fmt.Sprintf("停用功能失敗（繼續移除）：%v", err))
	}

	if err := service.Uninstall(c.serviceName); err != nil && !isServiceMissing(err) {
		return fmt.Errorf("無法移除服務: %w", err)
	}
	c.logOp("remove step", "step", "已移除 XWatch 註冊之 Windows 服務")

	// 删除設定檔，確保下次 init 會以全新預設値重新初始化
	if err := config.DeleteConfig(); err != nil {
		c.logOp("remove step", "step", fmt.Sprintf("設定檔删除失敗（非致命）：%v", err))
	} else {
		c.logOp("remove step", "step", "設定檔已删除")
	}

	fmt.Println("[5/5] XWatch 註冊之 Windows 服務已移除。")

	fmt.Println("所有服務、排程已停止並移除。")
	return nil
}

// disableAllFeaturesOnRemove 停用心跳與郵件排程，並將結果寫入 ops-log。
// 若設定檔無法讀取（如首次安裝未完成），直接回傳 nil 不報錯。
func (c *cliApp) disableAllFeaturesOnRemove() error {
	settings, err := config.Load()
	if err != nil {
		// 設定檔不存在時不視為錯誤
		return nil
	}

	// 停用心跳
	settings.HeartbeatEnabled = false
	c.logOp("remove step", "step", "心跳已停用")
	fmt.Println("[2/5] 心跳已停用。")

	// 停用郵件排程
	settings.Mail.Enabled = config.BoolPtr(false)
	c.logOp("remove step", "step", "郵件排程已停用")
	fmt.Println("[3/5] mail 已停用。")

	// 停用 filecheck 排程
	settings.Filecheck.Enabled = false
	settings.Filecheck.Mail.Enabled = config.BoolPtr(false)
	c.logOp("remove step", "step", "filecheck 排程已停用")
	fmt.Println("[4/5] filecheck 已停用。")

	return config.Save(settings)
}

// clearJournal 先停止服務，再刪除並重建日誌資料庫。
func (c *cliApp) clearJournal() error {
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}

	dataDir, err := paths.EnsureDataDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, "key.bin")
	key, err := crypto.LoadOrCreateKey(keyPath, 32)
	if err != nil {
		return err
	}

	journalPath := filepath.Join(dataDir, "journal.db")
	for _, p := range []string{journalPath, journalPath + "-wal", journalPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("無法刪除 %s: %w", filepath.Base(p), err)
		}
	}

	j, err := journal.Open(journalPath, key)
	if err != nil {
		return fmt.Errorf("重建日誌資料庫失敗: %w", err)
	}
	_ = j.Close()

	fmt.Println("資料庫事件紀錄已清除。")
	return nil
}

// resolveRoot 解析監控根目錄：優先使用指定路徑，其次讀取設定，最後回落至執行檔所在目錄。
func (c *cliApp) resolveRoot(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
	}

	settings, err := config.Load()
	if err == nil && settings.RootDir != "" {
		return resolveAndEnsureDir(settings.RootDir, "根目錄")
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveAndEnsureDir(filepath.Dir(exePath), "根目錄")
}

// resolveRootForInit 在初始化時使用：若未指定 root，優先使用目前執行檔所在目錄，
// 以避免沿用舊設定檔中的過期根目錄。
func (c *cliApp) resolveRootForInit(rootArg string) (string, error) {
	if rootArg != "" {
		return resolveAndEnsureDir(rootArg, "根目錄")
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveAndEnsureDir(filepath.Dir(exePath), "根目錄")
}

// resolveAndEnsureDir 將路徑轉為絕對路徑，若目錄不存在則詢問使用者是否自動建立。
func resolveAndEnsureDir(path string, purpose string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("%s 不可為空", purpose)
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s 不是資料夾: %s", purpose, absPath)
		}
		return absPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	prompt := fmt.Sprintf("%s 不存在，是否建立？(Y/n): ", absPath)
	if !askYesNo(prompt) {
		return "", fmt.Errorf("已取消建立 %s", absPath)
	}
	if mkErr := os.MkdirAll(absPath, 0o755); mkErr != nil {
		return "", mkErr
	}
	return absPath, nil
}

// askYesNo 在互動式終端顯示提示並讀取 Y/N 回應；非互動環境預設回傳 true。
func askYesNo(prompt string) bool {
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return true
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			return true
		}
		if strings.EqualFold(line, "n") || strings.EqualFold(line, "no") {
			return false
		}
	}
}

// isServiceMissing 判斷錯誤是否代表 Windows 服務不存在。
func isServiceMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "service does not exist") || strings.Contains(msg, "does not exist")
}
