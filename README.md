# go-xwatch

Windows 檔案/資料夾異動監控服務（單一 exe，內建 Windows 服務、自動安裝、事件日誌與每日輸出）。

## 功能概要
- 首次執行自動初始化：寫設定（預設 `dev` 環境）、註冊並啟動 Windows 服務，完成後退出；服務啟動類型為 Automatic (Delayed Start)。
- 重新執行 `init` 只更新根目錄，**保留現有設定**；僅在 `remove` 移除服務後再次安裝，才重置全部設定。
- 監控遞迴目錄變動，建立新資料夾時自動補註冊 watcher。
- 事件日誌：寫入 `%ProgramData%\go-xwatch\journal.db`（SQLite WAL，payload 以 AES-GCM 加密，金鑰用 DPAPI 封存），並有去重/節流與寫入失敗退避。
- 每日輸出：可啟用每日 CSV 檔（預設 `%ProgramData%/go-xwatch/daily`），透過緩衝批次寫入；未來可擴充 json/email 等格式。
- 匯出：支援 `export` 子命令，格式 `json|jsonl|text`，可時間篩選；支援 `--bom`（UTF-8 BOM）與 `--out`（自訂輸出路徑）。
- 清理工具：`cleanup/remove` 一鍵停止並移除服務，並**自動刪除設定檔**；`clear/purge/wipe` 清空事件資料庫並重建空庫。
- 環境管理：`env set dev|prod` 切換執行環境，並**自動同步** mail 與 filecheck 的收件人清單。
- 互動啟動體驗：在非服務模式偵測未以系統管理員執行時，提示是否以 UAC 重新啟動（可設 `XWATCH_NO_ELEVATE=1` 關閉）；每次指令後提供快捷選項，輸入 `h` 查看 help、`e` 退出。

## 近期更新（主要功能項目）
- **環境切換同步收件人**：`env set dev|prod` 切換時，自動將 `mail.to` 與 `filecheck.mail.to` 更新為目標環境預設清單；不再僅儲存標記。
- **`env status` 子指令**：新增明確查詢目前環境、mail 與 filecheck 實際收件人清單的指令。
- **`init` 保留設定**：再次執行 `init` 只更新 `rootDir`，不覆蓋已設定的環境與收件人；移除服務後重裝則重置為全新預設。
- **預設環境改為 `dev`**：首次安裝預設環境為 `dev`（收件人不含外部單位），切換 `prod` 後才納入。
- **郵件主旨加前綴**：所有郵件主旨自動加上 `[XWatch]` 前綴，便於信箱過濾。
- 設定檔驗證與預設：`rootDir` 必填且自動正規化為絕對路徑；每日輸出啟用時，未指定路徑預設寫入 `daily`。
- 服務/前景模式共用的 rotating logger，避免單一檔案無限成長。
- pipeline 新增完整 lifecycle：writer/各 sink 支援 `Close()` 並在關閉時做最後 flush，避免遺失尾端事件。

## Build

### 推薦：自動注入版本（使用 git tag）

```powershell
./build.ps1
```
腳本會以 `git describe --tags --always --dirty` 取得版本（無標籤則為 `dev`），並用 `-ldflags -X main.version=<ver>` 注入到可執行檔，輸出 `xwatch.exe`。

### 簡單編譯（不注入版本）

```powershell
go build -o xwatch.exe ./cmd/xwatch
```

## 使用方式

### 指令總覽

| 指令 | 說明 |
| --- | --- |
| `init [-root PATH]` | 初始化設定（預設 `dev` 環境）；重新執行只更新根目錄，保留現有設定 |
| `status` | 顯示服務狀態、根目錄、資料目錄、journal 路徑與事件筆數 |
| `start` / `stop` | 啟動或停止服務 |
| `uninstall` | 移除服務 |
| `cleanup` / `remove` | 停止並移除服務，同時刪除設定檔（下次 init 重置為預設值） |
| `clear` / `purge` / `wipe` | 清空事件資料庫並重建空庫（預設先嘗試停服務，可設 `XWATCH_SKIP_SERVICE_OPS=1` 跳過） |
| `export [flags]` | 匯出事件。旗標：`--since/--until` RFC3339 時間、`--limit`、`--all`、`--format json|jsonl|text`、`--bom`、`--out PATH` |
| `daily <subcommand>` | 管理每日 CSV 輸出；子指令：`status|enable|disable|set|test`，可指定 `--dir` |
| `env [subcommand]` | 查詢或切換執行環境；子指令：`status`（查詢）、`set dev|prod`（切換並同步收件人）、`help` |
| `mail <subcommand>` | 管理前一日 watch log 郵件通知；子指令：`status|enable|disable|set|send` |
| `filecheck <subcommand>` | 管理目錄存在性檢查與郵件通知；子指令：`status|scan|mail enable|mail disable|mail set|mail send` |
| `run [-root PATH]` | 前景模式執行，不作為服務 |
| `help` | 顯示指令列表 |

### 初始化 / 設定

```powershell
# 首次初始化（預設 dev 環境）
./xwatch.exe init --root "D:\target-root"

# 重新執行只更新根目錄，現有設定（環境/收件人等）維持不變
./xwatch.exe init --root "D:\new-root"

# 完整重置設定（移除後重裝）
./xwatch.exe remove
./xwatch.exe init --root "D:\target-root"
```

### 環境管理

```powershell
# 查詢目前環境與收件人設定
./xwatch.exe env
./xwatch.exe env status

# 切換環境，並自動同步 mail 與 filecheck 收件人
./xwatch.exe env set prod
./xwatch.exe env set dev
```

**環境預設收件人：**

| 環境 | 收件人清單 |
| --- | --- |
| `dev` | e003, ken, e032, e024 @httc.com.tw（不含外部單位） |
| `prod` | 589497@cpc.com.tw + 以上 4 位 |

> 切換環境後若需自訂收件人，再執行 `mail set --to` 或 `filecheck mail enable --to`。

### 郵件通知（watch log）

```powershell
# 查詢郵件設定狀態
./xwatch.exe mail status

# 啟用郵件通知（使用目前環境預設收件人）
./xwatch.exe mail enable

# 啟用並指定收件人
./xwatch.exe mail enable --to alice@example.com,bob@example.com

# 更新排程（HH:MM，預設 10:00）
./xwatch.exe mail set --schedule 08:30

# 停用郵件通知
./xwatch.exe mail disable

# 手動觸發寄送
./xwatch.exe mail send
```

郵件主旨格式：`[XWatch] XWatch 前一日監控日誌`（主旨自動加上 `[XWatch]` 前綴）。

### 目錄存在性檢查 (filecheck)

```powershell
# 查詢設定狀態
./xwatch.exe filecheck status

# 手動執行掃描
./xwatch.exe filecheck scan

# 啟用郵件通知（使用目前環境預設收件人）
./xwatch.exe filecheck mail enable

# 指定收件人
./xwatch.exe filecheck mail enable --to ops@example.com

# 停用郵件通知
./xwatch.exe filecheck mail disable

# 手動觸發寄送
./xwatch.exe filecheck mail send
```

### 每日輸出 (csv)

```powershell
./xwatch.exe daily status
./xwatch.exe daily enable --dir D:\logs\xwatch
./xwatch.exe daily set --dir D:\logs\xwatch-daily
./xwatch.exe daily disable
./xwatch.exe daily test --dir D:\logs\xwatch
```

### 匯出日誌

```powershell
# 匯出全部到預設路徑（%ProgramData%\go-xwatch\export_*.json）
./xwatch.exe export --all

# 自訂輸出檔，並加 BOM 供記事本辨識中文
./xwatch.exe export --all --format json --bom --out D:\logs\xwatch.json

# 依時間篩選，輸出到 stdout
./xwatch.exe export --since "2026-03-01T00:00:00Z" --until "2026-03-02T00:00:00Z" --format json --out -
```

### 常用維運

```powershell
./xwatch.exe status
./xwatch.exe start
./xwatch.exe stop
./xwatch.exe uninstall
./xwatch.exe cleanup   # 停止並移除服務（保留設定檔）
./xwatch.exe remove    # 停止並移除服務，同時刪除設定檔
./xwatch.exe clear     # 清空事件資料庫
```

### 前景除錯模式

```powershell
./xwatch.exe run --root "D:\target-root"
```

## 檔案路徑
- 設定檔：`%ProgramData%\go-xwatch\config.json`
- 服務日誌：`%ProgramData%\go-xwatch\xwatch.log`
- 事件日誌 (AES-GCM / DPAPI 金鑰保護)：`%ProgramData%\go-xwatch\journal.db`
- DPAPI 封存金鑰：`%ProgramData%\go-xwatch\key.bin`

## 測試

```powershell
go test ./...
```

測試環境會設 `XWATCH_SKIP_ACL=1` 以略過資料夾 ACL 設定（避免在受限 temp 目錄失敗）。

## 注意事項
- 安裝/移除服務需要系統管理員權限。
- `remove` 指令會刪除設定檔，下次 `init` 將重置為全新預設值。
- 事件日誌位置不在監控根目錄，避免自觸發；即便放在其他磁碟也可於設定檔指定根目錄。
- 若需更嚴格存取控制，可在部署時另行設定 `%ProgramData%\go-xwatch` ACL 或搭配 EFS。
