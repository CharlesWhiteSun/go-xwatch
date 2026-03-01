# go-xwatch

Windows 檔案/資料夾異動監控服務（單一 exe，內建 Windows 服務、自動安裝、事件日誌）。

## 功能概要
- 首次執行自動初始化：寫設定、註冊並啟動 Windows 服務，完成後退出；服務啟動類型為 Automatic (Delayed Start)。
- 監控遞迴目錄變動，建立新資料夾時自動補註冊 watcher。
- 事件日誌：寫入 `%ProgramData%\go-xwatch\journal.db`（SQLite WAL，payload 以 AES-GCM 加密，金鑰用 DPAPI 封存），並有去重/節流與寫入失敗退避。
- 匯出：支援 `export` 子命令，格式 `json|jsonl|text`，可時間篩選；支援 `--bom`（UTF-8 BOM）與 `--out`（自訂輸出路徑，預設寫入 `%ProgramData%\go-xwatch\export_YYYYMMDD_HHMMSS.xxx`）。
- 清理工具：`cleanup/remove` 一鍵停止並移除服務；`clear/purge/wipe` 清空事件資料庫並重建空庫。
- 互動啟動體驗：在非服務模式偵測未以系統管理員執行時，提示是否以 UAC 重新啟動（可設 `XWATCH_NO_ELEVATE=1` 關閉）；每次指令後提供 Y/n 退出確認，輸入 n 可回到 CLI 簡易主畫面再輸入新指令。

## Build

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
| `run [-root PATH]` | 前景模式執行，不作為服務 |
| `help` | 顯示指令列表 |

啟動時若非系統管理員，會詢問是否以 UAC 重新啟動；Enter 預設同意。每次指令完成後會詢問下一步，預設 Enter 回到 CLI 輸入新指令，選 1 才退出，選 3 顯示 help。

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
