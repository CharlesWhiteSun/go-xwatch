# go-xwatch

 Windows 檔案/資料夾異動監控服務（單一 exe，內建 Windows 服務、自動安裝、事件日誌與每日輸出）。

## 功能概要
- 首次執行自動初始化：寫設定、註冊並啟動 Windows 服務，完成後退出；服務啟動類型為 Automatic (Delayed Start)。
- 監控遞迴目錄變動，建立新資料夾時自動補註冊 watcher。
- 事件日誌：寫入 `%ProgramData%\go-xwatch\journal.db`（SQLite WAL，payload 以 AES-GCM 加密，金鑰用 DPAPI 封存），並有去重/節流與寫入失敗退避。
- 每日輸出：可啟用每日 CSV 檔（預設 `%ProgramData%/go-xwatch/daily`），透過緩衝批次寫入；未來可擴充 json/email 等格式。
- 匯出：支援 `export` 子命令，格式 `json|jsonl|text`，可時間篩選；支援 `--bom`（UTF-8 BOM）與 `--out`（自訂輸出路徑，預設寫入 `%ProgramData%/go-xwatch\export_YYYYMMDD_HHMMSS.xxx`）。
- 清理工具：`cleanup/remove` 一鍵停止並移除服務；`clear/purge/wipe` 清空事件資料庫並重建空庫。
- 互動啟動體驗：在非服務模式偵測未以系統管理員執行時，提示是否以 UAC 重新啟動（可設 `XWATCH_NO_ELEVATE=1` 關閉）；每次指令後提供快捷選項，輸入 `h` 查看 help、`e` 退出。

## 近期更新（主要功能項目）
- 設定檔驗證與預設：`rootDir` 必填且自動正規化為絕對路徑；每日輸出啟用時，未指定路徑預設寫入 `daily`。
- 服務/前景模式共用的 rotating logger，避免單一檔案無限成長。
- pipeline 新增完整 lifecycle：writer/各 sink 支援 `Close()` 並在關閉時做最後 flush，避免遺失尾端事件。
- watcher 支援注入自訂 formatter/hook，便於在不同場景覆用。

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
| `init [-root PATH]` | 初始化設定並註冊/啟動服務，預設根目錄為 exe 所在目錄 |
| `status` | 顯示服務狀態、根目錄、資料目錄、journal 路徑與事件筆數 |
| `start` / `stop` | 啟動或停止服務 |
| `uninstall` | 移除服務 |
| `cleanup` / `remove` | 停止並移除服務（容忍已不存在/已停止狀態） |
| `clear` / `purge` / `wipe` | 清空事件資料庫並重建空庫（預設先嘗試停服務，可設 `XWATCH_SKIP_SERVICE_OPS=1` 跳過） |
| `export [flags]` | 匯出事件。旗標：`--since/--until` RFC3339 時間、`--limit`、`--all`、`--format json|jsonl|text`、`--bom`、`--out PATH`（`-` 表 stdout，預設 `%ProgramData%/go-xwatch` 產生檔名） |
| `daily <subcommand>` | 管理每日輸出（目前支援 csv；子指令 `status|enable|disable|set|test`，可指定 `--dir`） |
| `run [-root PATH]` | 前景模式執行，不作為服務 |
| `help` | 顯示指令列表 |

啟動時若非系統管理員，會詢問是否以 UAC 重新啟動；Enter 預設同意。每次指令完成後會詢問下一步，Enter 可直接輸入指令；快捷選項：`h` 顯示 help、`e` 退出。

### 初始化 / 設定

```powershell
./xwatch.exe init --root "D:\target-root"
```
僅寫入設定，不會自動註冊服務；未給 `--root` 則預設為 exe 所在資料夾。

### 初始化並註冊/啟動服務（需系統管理員）

```powershell
./xwatch.exe init --root "D:\target-root" --install-service
```

### 匯出日誌

```powershell
# 匯出全部到預設路徑（%ProgramData%\go-xwatch\export_*.json）
./xwatch.exe export --all

# 自訂輸出檔，並加 BOM 供記事本辨識中文
./xwatch.exe export --all --format json --bom --out D:\logs\xwatch.json

# 依時間篩選，輸出到 stdout
./xwatch.exe export --since "2026-03-01T00:00:00Z" --until "2026-03-02T00:00:00Z" --limit 1000 --format json --out - > events.json
```

### 每日輸出 (csv)

```powershell
# 查看狀態（是否啟用、輸出目錄）
./xwatch.exe daily status

# 啟用每日 CSV，指定輸出目錄（預設 %ProgramData%/go-xwatch/daily）
./xwatch.exe daily enable --dir D:\logs\xwatch

# 更新輸出目錄
./xwatch.exe daily set --dir D:\logs\xwatch-daily

# 停用每日 CSV
./xwatch.exe daily disable

# 寫入一筆測試事件到當日 CSV
./xwatch.exe daily test --dir D:\logs\xwatch
```

目前僅支援 CSV；未來可在 `daily` 子命令擴充 `--format json|email` 等。每日輸出預設以緩衝批次寫入，目錄預設 `%ProgramData%/go-xwatch/daily`。

### 常用維運

```powershell
./xwatch.exe status
./xwatch.exe start
./xwatch.exe stop
./xwatch.exe uninstall
./xwatch.exe cleanup   # 停止並移除服務
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
- 事件日誌位置不在監控根目錄，避免自觸發；即便放在其他磁碟也可於設定檔指定根目錄。
- 若需更嚴格存取控制，可在部署時另行設定 `%ProgramData%\go-xwatch` ACL 或搭配 EFS。 
