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

		// 傳入 --service --name <serviceName> 讓服務模式能正確解析自身名稱。
		if err := service.InstallOrUpdate(newServiceName, exePath, "--service", "--name", newServiceName); err != nil {
			return fmt.Errorf("無法註冊服務: %w", err)
		}

		if err := service.Start(newServiceName); err != nil && !errors.Is(err, service.ErrAlreadyRunning) {
			return fmt.Errorf("無法啟動服務: %w", err)
		}
	} else {
		fmt.Printf("[3/3] 已完成設定（服務名稱：%s），未註冊/啟動服務。需註冊請改用 --install-service。\n", newServiceName)
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
