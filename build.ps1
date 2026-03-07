# Build script with auto version injection from git tags.
# Usage: ./build.ps1
#
# 功能變更記錄（Phase 5 - 多服務支援）：
#   - 服務名稱由 "GoXWatch" 改為 "GoXWatch-{資料夾名稱}"
#   - 每個服務的資料路徑獨立於 %ProgramData%\go-xwatch\{後綴}\
#   - 同一根目錄重複安裝時自動偵測並警告
#   - 重複 init --install-service 時以預設 N 提示使用者確認是否覆蓋
#   - 偵測 SCM 登錄的執行檔路徑是否與當前執行的一致（改名執行檔防呆）
#
# 功能變更記錄（Phase 6 - remove 防呆 + status 未初始化友善提示）：
#   - config.Load() 在設定檔不存在時回傳 ErrNotInitialized（取代原始 os 錯誤）
#   - 新增 config.IsInitialized() 供呼叫端判斷是否已初始化
#   - mail/filecheck/heartbeat status 在未初始化時顯示友善錯誤訊息
#   - stopAndUninstall 的 config.DeleteConfig 失敗時改為畫面顯示警告
#   - 確認正常重啟流程（無 remove）設定檔可正確讀取

$ErrorActionPreference = "Stop"

# Get version from git describe; fallback to "dev"
$ver = git describe --tags --always --dirty 2>$null
if (-not $ver) { $ver = "dev" }

$outFile = "XWatch-$ver.exe"
Write-Host "Building $outFile..."

go build -ldflags "-X main.version=$ver" -o $outFile ./cmd/xwatch
if ($LASTEXITCODE -ne 0) {
	Write-Error "Build failed with exit code $LASTEXITCODE"
	exit $LASTEXITCODE
}

Write-Host "Build succeeded. Output: $outFile"
