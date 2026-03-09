# 2026-03-07 修正摘要（管理者視角）

> 本文件由管理者視角整理 2026-03-07 當日 GitHub 所有提交，依影響層面分類呈現，
> 供系統管理人員評估部署影響與後續維運動作。

---

## 摘要概覽

| 類別 | 數量 |
|------|------|
| 🐛 Bug 修正 | 7 筆 |
| ✨ 新功能 | 3 筆 |
| 🔧 重構 / 改善 | 1 筆 |
| **合計** | **11 筆** |

**核心問題主軸：** 多服務實例（multi-instance / `--suffix`）環境下，資料目錄隔離長期失效，導致各實例資料互相污染。本日提交圍繞此問題進行全面修正，同時補強版本一致性檢查與使用者操作體驗。

---

## 🔴 高影響 Bug 修正

> 以下問題在多服務實例部署環境中會造成**資料錯誤或資料遺失**，需優先關注。

### 1. 多實例 ProgramData 目錄隔離失效（根本原因修正）
**Commit:** `aa3e6c3f` · 2026-03-07T13:14

**問題：**
`config.json` 採舊版格式（`serviceName` 欄位不存在或為空）時，服務的 `Execute()` 從 `h.settings.ServiceName` 推導後綴會得到空字串，所有資料（`journal.db`、`key.bin`、`xwatch-watch-logs`、`xwatch-heartbeat-logs`）靜默退化至基底目錄 `%ProgramData%\go-xwatch\`，導致多服務實例資料互相混用。

**修正：**
- `service_windows.go` 的 `handler` struct 新增 `serviceName` 欄位，`Run()` 將啟動參數直接儲存於此
- `Execute()` 改用 `h.serviceName`（由 `main.go` 解析 `--name` 啟動參數所提供），不再依賴 `config.json` 中的 `serviceName` 欄位

**管理者注意：** 舊版設定檔升級後不需要手動修改，修正已向後相容。

---

### 2. 多實例 ProgramData 目錄隔離失效（整體補修）
**Commit:** `7ba03d20` · 2026-03-07T08:38

**問題：**
以不同根目錄啟動多個服務實例時，`journal.db`、`key.bin`、`xwatch-ops-logs`、`xwatch-watch-logs`、`xwatch-heartbeat-logs` 均寫入基底目錄（`%ProgramData%\go-xwatch\`），而非各自的 suffix 子目錄，導致多實例資料互相混用。

**修正：**
- `logger.go` 新增 `LogDirForDataDir(dataDir string)` 函式，依傳入的資料目錄計算心跳 log 子目錄路徑
- `service_windows.go` 移除 package-level `watchLogRotator`，改在 `Execute()` 內依 `ServiceName` 建立 per-instance `watchLogRotator`
- `runner.go` 的 `dataDirFn()` 與 `heartbeatLogDir()` fallback 改用 `paths.EnsureDataDirForSuffix(config.GetServiceSuffix())`
- `main.go` 的 `opsLogger` 改用 lazy closure，確保 `xwatch-ops-logs` 落入正確的 suffix 子目錄

---

### 3. `remove` 指令因 ops log 未關閉導致資料夾無法刪除
**Commit:** `d57936bc` · 2026-03-07T13:56

**問題：**
執行 `remove` 指令時，`opslog.Logger` 仍持有 `operations_YYYY-MM-DD.log` 的檔案控制代碼。Windows 檔案鎖定機制使 `os.RemoveAll()` 無法刪除仍開啟的檔案，導致殘留目錄並顯示「The process cannot access the file because it is being used by another process」警告。

**修正：**
- `cli.go` 新增 `closeOpsLogger()` 方法，在 `stopAndUninstall()` 呼叫 `config.DeleteConfigDir()` 前確保 log 檔案控制代碼已釋放

**管理者注意：** 修正後執行 `remove` 指令可完整清除 suffix 資料目錄，不再殘留 log 資料夾。

---

## 🟡 中影響 Bug 修正

> 以下問題會造成**資料寫入錯誤位置**，不影響核心監控功能，但會在基底目錄產生孤立的資料夾或檔案。

### 4. `status` 與 `clear-journal` 在基底目錄建立資料檔案
**Commit:** `403c4867` · 2026-03-07T14:57

**問題：**
`printStatus()` 與 `clearJournal()` 使用 `paths.EnsureDataDir()`（無後綴），執行 `status` / `clear-journal` 指令時會在 `%ProgramData%\go-xwatch\` 基底目錄建立 `key.bin` 與 `journal.db`，而非服務專屬的後綴子目錄。

**修正：**
`cli_operations.go` 兩者改用 `paths.EnsureDataDirForSuffix(service.SuffixFromServiceName(c.serviceName))`。

---

### 5. `filecheck` log 在基底目錄建立孤立資料夾
**Commit:** `519be997` · 2026-03-07T15:10

**問題：**
`sendFilecheckMail()` 呼叫 `paths.DataDir()`（固定無後綴），每次 filecheck 郵件排程觸發時都在基底目錄建立孤立的 `xwatch-filecheck-logs` 資料夾，而非服務專屬子目錄。

**修正：**
`sendFilecheckMail()` 與 `runFilecheckMailScheduler()` 新增 `dataDirFn` 參數，由呼叫端注入正確後綴路徑；設計模式與 `HeartbeatLogDirFn` 一致。

---

### 6. `export` 指令在多服務後綴環境下讀取錯誤資料目錄
**Commit:** `202c9179` · 2026-03-07T15:41

**問題：**
`export` 指令呼叫 `paths.EnsureDataDir()`（無後綴基底目錄），在多服務實例環境（如 `--suffix plant-A`）下，會試圖從基底目錄讀取 `key.bin` 與 `journal.db`，導致解密失敗或匯出空資料。

**修正：**
- `exporter/exporter.go` 新增 `WithDataDirFn` Option，使 `Export` 可注入資料目錄來源
- `internal/app/cli.go` 的 `export` 指令傳入 `WithDataDirFn`，確保讀取正確後綴子目錄

---

### 7. 服務已存在時顯示誤導性錯誤訊息
**Commit:** `3b228e9c` · 2026-03-07T00:43

**問題：**
`init --install-service` 完成後執行 `stop` 再用新 exe 執行 `init` 時，顯示「未註冊/啟動服務。需註冊請改用 `--install-service`」，但服務實際上已存在只是處於停止狀態，易誤導使用者。

**修正：**
`initAndExit` 改依服務實際狀態顯示適當訊息：
- 服務已存在且停止 → 顯示「設定已更新，可執行 `start` 重新啟動」
- 服務已存在且執行中 → 顯示「設定已更新，服務目前正在執行中」
- 服務確實未安裝 → 顯示「服務尚未安裝，需安裝請改用 `--install-service`」

`printStatus` 在服務停止時額外顯示引導提示。

---

## ✨ 新功能

### 8. 版本一致性檢查 — 偵測執行檔與服務安裝版本不符
**Commit:** `95080cca` · 2026-03-07T02:05

**功能說明：**
CLI 啟動時比對執行檔版本與服務安裝記錄的版本（儲存於 `config.json` 的 `InstalledVersion` 欄位）。版本不符時：
- 版本不一致 → 輸出警告至 `stderr`、等待 3 秒，以 exit code 1 結束

**設定影響：**
- `config.Settings` 新增 `InstalledVersion` 欄位（`omitempty`，向後相容）
- `init --install-service` 時自動將當前版本號寫入設定檔
- 舊版安裝（設定無版本記錄）自動略過版本檢查，不影響既有部署

**管理者注意：** 此功能在執行 `init --install-service` 後即自動生效，無需額外設定。

---

### 9. 版本不一致時依高低版本採取差異化處理策略
**Commit:** `5da77615` · 2026-03-07T22:31

**功能說明：**
延伸版本一致性檢查（Commit 8），加入版本高低方向判斷並採取不同互動策略：
- **低版本啟動**（執行檔舊於已安裝服務）→ 顯示降版限制警告 → 等待使用者按 Enter → 退出（視窗不再自動關閉）
- **高版本啟動**（執行檔新於已安裝服務）→ 詢問 `(N/y)` 是否升級 → 確認後自動執行 `remove` 再 `init --install-service` → 繼續主程式；拒絕時顯示手動升級說明並等待 Enter

**新增檔案：**
- `version_compare.go`：定義 `VersionMismatchKind`、`VersionCheckResult`、`compareVersions()` 純函式（支援 `v1.4` 及 `v1.4-48-g519be99` 等 `git describe` 格式）

**管理者注意：** 此功能增強了版本部署時的安全防護，可防止低版本執行檔意外覆蓋已安裝的較新服務設定。

---

### 10. `start` / `stop` 加入各功能聯動步驟顯示
**Commit:** `8b2b0a57` · 2026-03-07T00:23

**功能說明：**
執行 `stop` / `start` 指令時，逐步顯示 heartbeat、mail、filecheck 各功能的聯動停止／啟動狀態，取代原本無任何輸出的行為。

**範例輸出：**
```
[1/3] heartbeat 已隨服務停止
[2/3] mail 已隨服務停止
[3/3] filecheck 未啟用，略過
```

---

## 🔧 重構 / 改善

### 11. 調整編譯腳本：移除自動測試、輸出檔名加入版本號
**Commit:** `9900f8c5` · 2026-03-07T23:08

**變更：**
- 移除 `build.ps1` 中的 `go test` 執行區塊，改由開發者視需要手動執行，避免每次編譯等待 20+ 秒
- 編譯輸出檔名由 `xwatch.exe` 改為 `XWatch-{版本號}.exe`（如 `XWatch-v1.4-50-g5da7761.exe`），便於識別部署版本

**管理者注意：** 此改動不影響服務執行，僅影響開發/部署流程中的編譯步驟。部署時請確認執行檔版本號是否符合預期。

---

## 維運注意事項

### 多服務實例環境（含 `--suffix` 部署）
本日修正解決了多服務實例環境中資料目錄隔離的一系列問題。部署新版後：
- 若環境中存在舊版 `config.json`（`serviceName` 欄位不存在），需確認各服務使用正確的 `--name` 啟動參數，確保 suffix 能從啟動參數正確推導
- 基底目錄（`%ProgramData%\go-xwatch\`）若存在舊版遺留的孤立資料夾（如 `xwatch-filecheck-logs`、`xwatch-ops-logs` 等），可手動清除

### 版本管理
- 新安裝（`init --install-service`）會自動記錄版本號至 `config.json`
- 升級部署時，建議依標準流程執行：`remove` → `init --install-service`（或使用高版本啟動時的互動升級功能）
- 低版本執行檔啟動時將被系統阻擋並顯示警告，防止降版事故

### `remove` 指令
修正後 `remove` 可完整清除所有 suffix 資料目錄，包含 log 檔案，不再殘留孤立資料夾。

---

## 測試覆蓋摘要

本日共新增 / 更新測試如下（全套 503 個測試通過）：

| 提交 | 新增/更新測試 |
|------|--------------|
| `8b2b0a57` | `startstop_test.go` — 8 項 |
| `3b228e9c` | `cli_init_naming_test.go`、`status_test.go` — 7 項 |
| `95080cca` | `version_check_test.go` — 9 項 |
| `7ba03d20` | `logger_test.go`、`runner_test.go` — 3 項 |
| `aa3e6c3f` | `service_handler_isolation_test.go` — 3 項（Windows 限定）|
| `d57936bc` | `remove_test.go` — 2 項 |
| `403c4867` | `cli_operations_test.go` — 3 項 |
| `519be997` | `filecheck_scheduler_test.go` — 2 項 |
| `202c9179` | `exporter_test.go`、`mailutil_test.go` — 5 項 |
| `5da77615` | `version_compare_test.go`、`version_check_test.go` — 15 項 |

---

*本摘要由 Copilot Coding Agent 依據 2026-03-07 GitHub 提交記錄自動整理，供管理者審查。*
