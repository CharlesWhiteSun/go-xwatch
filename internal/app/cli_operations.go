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

	// 依根目錄推導本次服務後綴（例如 "plant-A"）與完整服務名稱（例如 "GoXWatch-plant-A"）。
	// 必須在首次存取設定檔之前設定，以確保路徑計算正確。
	newSuffix := service.ServiceSuffixFromRoot(root)
	newServiceName := service.ServiceNameFromRoot(root)
	config.SetServiceSuffix(newSuffix)
	c.serviceName = newServiceName

	// 若準備安裝服務，先偵測是否已有另一個服務監控相同根目錄。
	if installService {
		if existing, ferr := service.FindServiceForRoot(root); ferr == nil && existing != "" {
			if existing == newServiceName {
				// 相同服務名稱：向使用者確認是否覆蓋（預設 No）。
				proceed, cerr := c.confirmReinstall(newServiceName)
				if cerr != nil {
					return cerr
				}
				if !proceed {
					return nil // 使用者選擇不覆蓋，安全退出
				}
			} else {
				return fmt.Errorf("根目錄 %q 已被服務 %q 監控中，不可重複註冊", root, existing)
			}
		}
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
	// 將服務名稱寫入設定檔，供日後服務模式自我辨識。
	settings.ServiceName = newServiceName
	if err := config.Save(settings); err != nil {
		return err
	}

	if installService {
		fmt.Printf("[3/3] 註冊或更新 Windows 服務（%s）並啟動...\n", newServiceName)
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return err
		}

		// 寫入安裝版本，供日後版本一致性檢查使用。
		settings.InstalledVersion = c.version
		if err := config.Save(settings); err != nil {
			return err
		}

		// XWATCH_SKIP_SERVICE_OPS=1 略過 SCM 呼叫（供整合測試使用，與 stopAndUninstall 一致）。
		if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
			// 傳入 --service --name <serviceName> 讓服務模式能正確解析自身名稱。
			if err := service.InstallOrUpdate(newServiceName, exePath, "--service", "--name", newServiceName); err != nil {
				return fmt.Errorf("無法註冊服務: %w", err)
			}
			if err := service.Start(newServiceName); err != nil && !errors.Is(err, service.ErrAlreadyRunning) {
				return fmt.Errorf("無法啟動服務: %w", err)
			}
		}
	} else {
		if c.isServiceInstalled() {
			// 服務已存在：依狀態顯示提示，避免誤導使用者以為服務尚未註冊
			st, _ := c.getServiceStatus(c.serviceName)
			if st == "running" {
				fmt.Printf("[3/3] 設定已更新，服務（%s）目前正在執行中。\n", newServiceName)
			} else {
				fmt.Printf("[3/3] 設定已更新，服務（%s）已存在但目前停止，可執行 `start` 重新啟動。\n", newServiceName)
			}
		} else {
			fmt.Printf("[3/3] 已完成設定（服務名稱：%s），服務尚未安裝，需安裝服務請改用 --install-service。\n", newServiceName)
		}
	}

	fmt.Println("完成。")
	return nil
}

// printStatus 輸出服務狀態、權限等級及設定摘要。
func (c *cliApp) printStatus() error {
	status, err := c.getServiceStatus(c.serviceName)
	if err != nil {
		if isServiceMissing(err) {
			fmt.Fprintln(os.Stderr, "提示：服務尚未安裝。請先執行「init --install-service」安裝 Windows 服務後，再使用 status 指令查看完整狀態。")
		}
		return err
	}
	fmt.Println("service:", c.serviceName)
	fmt.Println("status:", status)
	if status == "stopped" {
		fmt.Println("提示：服務已停止，可執行 `start` 重新啟動。")
	}

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

	dataDir, derr := paths.EnsureDataDirForSuffix(service.SuffixFromServiceName(c.serviceName))
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

// removeAction 描述 remove 流程中各 CLI 功能的處理方式。
type removeAction uint8

const (
	removeActionDisable           removeAction = iota // 停用 config 對應的啟用狀態
	removeActionPreserve                              // 資料保留於磁碟（db / export）
	removeActionClearedByDeletion                     // 隨設定檔刪除一併清除（env）
)

// removeFeature 描述一個 CLI 功能在 remove 流程中的處理方式。
// OCP 資料驅動設計：新增 CLI 功能只需在 buildRemoveFeatures 加一筆資料。
type removeFeature struct {
	CmdName string                   // CLI 指令名稱，供 ops-log 記錄
	Title   string                   // 使用者友善顯示名稱
	Action  removeAction             // 處理動作類型
	Disable func(s *config.Settings) // Action==removeActionDisable 時使用
	Note    string                   // Preserve / ClearedByDeletion 時顯示的說明文字
}

// buildRemoveFeatures 回傳所有 CLI 功能的 remove 處理描述。
// 要新增功能時只需在此清單加一筆，stepAndUninstall 自動計算步驟數。
func buildRemoveFeatures() []removeFeature {
	return []removeFeature{
		{
			CmdName: "heartbeat",
			Title:   "heartbeat 心跳排程",
			Action:  removeActionDisable,
			Disable: func(s *config.Settings) { s.HeartbeatEnabled = false },
		},
		{
			CmdName: "mail",
			Title:   "mail 郵件排程",
			Action:  removeActionDisable,
			Disable: func(s *config.Settings) { s.Mail.Enabled = config.BoolPtr(false) },
		},
		{
			CmdName: "filecheck",
			Title:   "filecheck 目錄排程",
			Action:  removeActionDisable,
			Disable: func(s *config.Settings) {
				s.Filecheck.Enabled = false
				s.Filecheck.Mail.Enabled = config.BoolPtr(false)
			},
		},
		{
			CmdName: "env",
			Title:   "env 環境設定",
			Action:  removeActionClearedByDeletion,
			Note:    "環境設定隨設定檔一併清除。",
		},
		{
			CmdName: "db / export",
			Title:   "db / export 日誌資料庫",
			Action:  removeActionClearedByDeletion,
			Note:    "日誌資料庫隨資料夾一併刪除。",
		},
	}
}

// ssFeature 描述一個 CLI 功能在 start/stop 流程中的狀態查詢。
// 資料驅動設計：新增功能只需在 buildStartStopFeatures 加一筆記錄。
type ssFeature struct {
	CmdName   string                       // CLI 指令名稱，供 ops-log 記錄
	Title     string                       // 使用者友善顯示名稱
	IsEnabled func(s config.Settings) bool // 判斷此功能是否已啟用
}

// buildStartStopFeatures 回傳所有 CLI 功能的 start/stop 聯動描述。
// 與 buildRemoveFeatures 並列，共同覆蓋各功能的完整生命週期管理。
func buildStartStopFeatures() []ssFeature {
	return []ssFeature{
		{
			CmdName:   "heartbeat",
			Title:     "heartbeat 心跳排程",
			IsEnabled: func(s config.Settings) bool { return s.HeartbeatEnabled },
		},
		{
			CmdName:   "mail",
			Title:     "mail 郵件排程",
			IsEnabled: func(s config.Settings) bool { return s.Mail.Enabled != nil && *s.Mail.Enabled },
		},
		{
			CmdName:   "filecheck",
			Title:     "filecheck 目錄排程",
			IsEnabled: func(s config.Settings) bool { return s.Filecheck.Enabled },
		},
	}
}

// stopAndUninstall 停止並移除 Windows 服務，同時停用所有功能並刪除設定檔。
//
// 步驟數由 buildRemoveFeatures 自動計算（目前共 8 步）：
//
//	[1/N]         停止 Windows 服務
//	[2/N]~[N-2/N] 逐一處理各 CLI 功能（由 buildRemoveFeatures 決定）
//	[N-1/N]       解除安裝 Windows 服務
//	[N/N]         刪除設定資料夾（後綴為空時僅刪設定檔）
//
// 設定環境變數 XWATCH_SKIP_SERVICE_OPS=1 可略過 SCM 呼叫（供測試使用）。
func (c *cliApp) stopAndUninstall() error {
	features := buildRemoveFeatures()
	const fixedSteps = 3 // stop + uninstall + delete config
	total := fixedSteps + len(features)
	step := 0
	next := func() int { step++; return step }

	skipSvcOps := os.Getenv("XWATCH_SKIP_SERVICE_OPS") == "1"

	// [1/N] 停止 Windows 服務
	if !skipSvcOps {
		if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}
	c.logOp("remove step", "step", "XWatch 服務已主動停止")
	fmt.Printf("[%d/%d] XWatch 服務已主動停止。\n", next(), total)

	// 載入設定（盡力嘗試，失敗時仍繼續流程）
	settings, loadErr := config.Load()
	hasConfig := loadErr == nil

	// [2/N]~[N-2/N] 遍歷各 CLI 功能
	for _, f := range features {
		n := next()
		switch f.Action {
		case removeActionDisable:
			if hasConfig {
				f.Disable(&settings)
				c.logOp("remove step", "step", f.CmdName+": 已停用")
				fmt.Printf("[%d/%d] %s：已停用。\n", n, total, f.Title)
			} else {
				c.logOp("remove step", "step", f.CmdName+": 設定檔不存在，略過")
				fmt.Printf("[%d/%d] %s：設定檔不存在，略過。\n", n, total, f.Title)
			}
		case removeActionPreserve:
			c.logOp("remove step", "step", f.CmdName+": "+f.Note)
			fmt.Printf("[%d/%d] %s：%s\n", n, total, f.Title, f.Note)
		case removeActionClearedByDeletion:
			if hasConfig {
				c.logOp("remove step", "step", f.CmdName+": "+f.Note)
				fmt.Printf("[%d/%d] %s：%s\n", n, total, f.Title, f.Note)
			} else {
				c.logOp("remove step", "step", f.CmdName+": 設定檔不存在，略過")
				fmt.Printf("[%d/%d] %s：設定檔不存在，略過。\n", n, total, f.Title)
			}
		}
	}

	// 儲存所有停用變更（單一 I/O，僅在有設定時執行）
	if hasConfig {
		if err := config.Save(settings); err != nil {
			c.logOp("remove step", "step", fmt.Sprintf("停用設定儲存失敗：%v", err))
		}
	}

	// [N-1/N] 解除安裝 Windows 服務
	if !skipSvcOps {
		if err := service.Uninstall(c.serviceName); err != nil && !isServiceMissing(err) {
			return fmt.Errorf("無法移除服務: %w", err)
		}
	}
	c.logOp("remove step", "step", "XWatch 服務已解除安裝")
	fmt.Printf("[%d/%d] XWatch 服務已解除安裝。\n", next(), total)

	// 在刪除設定資料夾之前主動關閉 ops log 檔案控制代碼。
	// Windows 檔案鎖定機制會阻止刪除仍被開啟的檔案，
	// 若 opsLogger 為寫入同一目錄的 *opslog.Logger，不先關閉將導致
	// xwatch-ops-logs/operations_YYYY-MM-DD.log 被鎖定，RemoveAll 失敗。
	c.closeOpsLogger()

	// [N/N] 刪除設定資料夾（後綴為空時僅刪設定檔）
	// 失敗時印出畫面警告，讓使用者知道需手動清除，避免誤以為已完全還原。
	if err := config.DeleteConfigDir(); err != nil {
		c.logOp("remove step", "step", fmt.Sprintf("設定資料夾刪除失敗：%v", err))
		fmt.Fprintf(os.Stderr, "⚠  警告：設定資料夾無法自動刪除，請手動移除：%v\n", err)
	} else {
		c.logOp("remove step", "step", "設定資料夾已移除")
		fmt.Printf("[%d/%d] 設定資料夾已移除。\n", next(), total)
	}

	fmt.Println("所有服務、排程已停止並移除。")
	return nil
}

// stopService 停止 Windows 服務，並逐一顯示各 CLI 功能的聯動停止狀態。
//
// 步驟數由 buildStartStopFeatures 自動計算：
//
//	[1/N]       停止 Windows 服務
//	[2/N]~[N/N] 顯示各 CLI 功能的聯動停止狀態
//
// 設定環境變數 XWATCH_SKIP_SERVICE_OPS=1 可略過 SCM 呼叫（供測試使用）。
func (c *cliApp) stopService() error {
	features := buildStartStopFeatures()
	total := 1 + len(features)
	step := 0
	next := func() int { step++; return step }

	// 載入設定（盡力嘗試，以判斷哪些功能已啟用）
	settings, loadErr := config.Load()
	hasConfig := loadErr == nil

	// [1/N] 停止 Windows 服務
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}
	c.logOp("stop", "step", "XWatch 服務已停止")
	fmt.Printf("[%d/%d] XWatch 服務已停止。\n", next(), total)

	// [2/N]~[N/N] 顯示各 CLI 功能的聯動停止狀態
	for _, f := range features {
		n := next()
		if hasConfig && f.IsEnabled(settings) {
			c.logOp("stop", "step", f.CmdName+": 已隨服務停止")
			fmt.Printf("[%d/%d] %s：已隨服務停止。\n", n, total, f.Title)
		} else {
			c.logOp("stop", "step", f.CmdName+": 未啟用，略過")
			fmt.Printf("[%d/%d] %s：未啟用，略過。\n", n, total, f.Title)
		}
	}
	fmt.Println("服務及所有聯動功能已停止。")
	return nil
}

// startService 啟動 Windows 服務，並逐一顯示各 CLI 功能的聯動啟動狀態。
//
// 步驟數由 buildStartStopFeatures 自動計算：
//
//	[1/N]       啟動 Windows 服務
//	[2/N]~[N/N] 顯示各 CLI 功能的聯動啟動狀態
//
// 設定環境變數 XWATCH_SKIP_SERVICE_OPS=1 可略過 SCM 呼叫（供測試使用）。
func (c *cliApp) startService() error {
	features := buildStartStopFeatures()
	total := 1 + len(features)
	step := 0
	next := func() int { step++; return step }

	// [1/N] 啟動 Windows 服務
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Start(c.serviceName); err != nil {
			return err
		}
	}
	c.logOp("start", "step", "XWatch 服務已啟動")
	fmt.Printf("[%d/%d] XWatch 服務已啟動。\n", next(), total)

	// 載入設定（在服務啟動後，顯示實際將運行的功能清單）
	settings, loadErr := config.Load()
	hasConfig := loadErr == nil

	// [2/N]~[N/N] 顯示各 CLI 功能的聯動啟動狀態
	for _, f := range features {
		n := next()
		if hasConfig && f.IsEnabled(settings) {
			c.logOp("start", "step", f.CmdName+": 已隨服務啟動")
			fmt.Printf("[%d/%d] %s：已隨服務啟動。\n", n, total, f.Title)
		} else {
			c.logOp("start", "step", f.CmdName+": 未啟用，略過")
			fmt.Printf("[%d/%d] %s：未啟用，略過。\n", n, total, f.Title)
		}
	}
	fmt.Println("服務及所有聯動功能已啟動。")
	return nil
}

// clearJournal 先停止服務，再刪除並重建日誌資料庫。
func (c *cliApp) clearJournal() error {
	if os.Getenv("XWATCH_SKIP_SERVICE_OPS") != "1" {
		if err := service.Stop(c.serviceName); err != nil && !isServiceMissing(err) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("無法停止服務: %w", err)
		}
	}

	dataDir, err := paths.EnsureDataDirForSuffix(service.SuffixFromServiceName(c.serviceName))
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

// askYesNoDefaultNo 顯示提示並讀取 Y/N 回應，空白輸入預設為 false（不執行）。
// 該函式用於屬於「讓使用者明確同意才執行」的高風險操作提示。
// 非互動環境或 XWATCH_NO_PAUSE=1 時，預設回傳 false（不覆蓋）。
func askYesNoDefaultNo(prompt string) bool {
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return false
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "n") || strings.EqualFold(line, "no") {
			return false
		}
		if strings.EqualFold(line, "y") || strings.EqualFold(line, "yes") {
			return true
		}
	}
}

// resolveRegisteredExePath 回傳服務已登錄的執行檔路徑。
// 優先使用注入的 registeredExePathFn（測試用），nil 時呼叫 service.RegisteredExePath。
func (c *cliApp) resolveRegisteredExePath(name string) (string, error) {
	if c.registeredExePathFn != nil {
		return c.registeredExePathFn(name)
	}
	return service.RegisteredExePath(name)
}

// confirmReinstall 偵測執行檔路徑是否與登錄的不一致，並要求使用者確認覆蓋。
//
// 返回値：
//   - (true,  nil) ：使用者選擇覆蓋，可繼續執行
//   - (false, nil) ：使用者選擇不覆蓋，安全退出
//   - (false, err) ：發生非使用者取消的錯誤
func (c *cliApp) confirmReinstall(svcName string) (bool, error) {
	// 取得目前執行檔絕對路徑
	currentExe, exErr := os.Executable()
	if exErr == nil {
		currentExe, _ = filepath.Abs(currentExe)
	}

	// 從 SCM 讀取服務已登錄的執行檔路徑
	registeredExe, regErr := c.resolveRegisteredExePath(svcName)

	// 若讀取成功且路徑不同，展示警告（可能是改名的執行檔）
	if exErr == nil && regErr == nil && registeredExe != "" {
		absRegistered := filepath.Clean(registeredExe)
		absCurrent := filepath.Clean(currentExe)
		if !strings.EqualFold(absRegistered, absCurrent) {
			fmt.Fprintf(os.Stderr,
				"⚠  警告：服務 %q 目前登錄的執行檔為：\n  %s\n但現在執行的是：\n  %s\n兩者路徑不同，可能是改名或複製的執行檔，請確認。\n",
				svcName, absRegistered, absCurrent)
		}
	}

	// 展示確認提示（預設 No）
	confirmFn := askYesNoDefaultNo
	if c.confirmOverwriteFn != nil {
		confirmFn = c.confirmOverwriteFn
	}
	prompt := fmt.Sprintf("服務 %%q 設定已存在，是否覆蓋現有設定並重新部署？(N/y): ")
	prompt = fmt.Sprintf(prompt, svcName)
	if confirmFn(prompt) {
		fmt.Println("您選擇了覆蓋，繼續執行更新...")
		return true, nil
	}
	fmt.Println("您選擇了不覆蓋，已取消本次初始化操作。")
	return false, nil
}

// checkVersionConsistency 在 CLI 啟動時比對目前執行檔版本與服務安裝版本，回傳結構化結果。
// 若服務尚未安裝、設定檔不存在、或未記錄安裝版本，回傳 VersionMatch（靜默通過）。
// 偵測到不一致時設置 Kind 欄位為對應方向，供呼叫端決策升級或降版阻擋邏輯。
func (c *cliApp) checkVersionConsistency() VersionCheckResult {
	if !c.isServiceInstalled() {
		return VersionCheckResult{Kind: VersionMatch}
	}
	settings, err := config.Load()
	if err != nil {
		return VersionCheckResult{Kind: VersionMatch}
	}
	installed := strings.TrimSpace(settings.InstalledVersion)
	if installed == "" {
		return VersionCheckResult{Kind: VersionMatch}
	}
	current := strings.TrimSpace(c.version)
	cmp := compareVersions(current, installed)
	if cmp == 0 {
		return VersionCheckResult{Kind: VersionMatch}
	}
	kind := VersionMismatchCurrentOlder
	if cmp > 0 {
		kind = VersionMismatchCurrentNewer
	}
	return VersionCheckResult{
		Kind:      kind,
		Current:   current,
		Installed: installed,
		RootDir:   settings.RootDir,
	}
}

// handleVersionMismatch 依版本差異類型採取對應措施：
//
//   - VersionMismatchCurrentOlder：顯示警告並等待 Enter（不自動退出），回傳 error 讓呼叫端退出。
//   - VersionMismatchCurrentNewer：詢問是否升級（預設 N）；
//     確認後依序執行 remove → init --install-service，回傳 nil 繼續主程式；
//     拒絕升級則顯示提示、等待 Enter 並回傳 error。
func (c *cliApp) handleVersionMismatch(result VersionCheckResult) error {
	switch result.Kind {
	case VersionMismatchCurrentOlder:
		fmt.Fprintln(os.Stderr, "\n⚠  警告：版本不一致（降版限制）")
		fmt.Fprintf(os.Stderr, "目前執行檔版本：%s\n", result.Current)
		fmt.Fprintf(os.Stderr, "服務安裝版本：%s（較新）\n", result.Installed)
		fmt.Fprintln(os.Stderr, "此為舊版執行檔，不支援覆蓋較新版本的服務設定。")
		fmt.Fprintf(os.Stderr, "請改用版本 %s 的主程式，或以新版主程式執行 init --install-service 重新安裝服務。\n", result.Installed)
		c.logOp("cli exit", "code", 1, "reason", "version_downgrade_blocked", "current", result.Current, "installed", result.Installed)
		c.waitForEnter()
		return fmt.Errorf("降版受阻：目前 %s < 安裝 %s", result.Current, result.Installed)

	case VersionMismatchCurrentNewer:
		fmt.Fprintln(os.Stderr, "\n⚠  注意：版本不一致（可升級）")
		fmt.Fprintf(os.Stderr, "目前執行檔版本：%s（較新）\n", result.Current)
		fmt.Fprintf(os.Stderr, "服務安裝版本：%s\n", result.Installed)

		if !c.confirmUpgrade("是否自動移除舊版服務並重新安裝至目前版本？(N/y): ") {
			fmt.Fprintln(os.Stderr, "已取消升級。請手動執行 remove 後重新執行 init --install-service，或改用正確版本的主程式。")
			c.logOp("cli exit", "code", 1, "reason", "version_upgrade_declined", "current", result.Current, "installed", result.Installed)
			c.waitForEnter()
			return fmt.Errorf("使用者取消版本升級")
		}

		fmt.Println("\n--- 移除舊版服務 ---")
		if err := c.stopAndUninstall(); err != nil {
			return fmt.Errorf("移除服務失敗: %w", err)
		}

		fmt.Println("\n--- 安裝新版服務 ---")
		if err := c.initAndExit(result.RootDir, true); err != nil {
			return fmt.Errorf("安裝服務失敗: %w", err)
		}

		fmt.Println("升級完成，繼續執行主程式...")
	}
	return nil
}

// confirmUpgrade 顯示升級確認提示（預設 N，需明確輸入 y 才執行）。
// 可透過 cliApp.confirmUpgradeFn 注入自訂行為（供測試使用）；
// nil 時使用 askYesNoDefaultNo（非互動環境預設回傳 false）。
func (c *cliApp) confirmUpgrade(prompt string) bool {
	if c.confirmUpgradeFn != nil {
		return c.confirmUpgradeFn(prompt)
	}
	return askYesNoDefaultNo(prompt)
}

// waitForEnter 顯示提示並等待使用者按 Enter 鍵，供警告畫面使用（防止視窗自動關閉）。
// 非互動環境或 XWATCH_NO_PAUSE=1 時直接返回，不阻塞。
func (c *cliApp) waitForEnter() {
	if os.Getenv("XWATCH_NO_PAUSE") == "1" || !isInteractiveConsole() {
		return
	}
	fmt.Fprintln(os.Stderr, "按 Enter 鍵關閉視窗...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
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
