# Build script with auto version injection from git tags.
# Usage: ./build.ps1
#
# 功能變更記錄（Phase 5 - 多服務支援）：
#   - 服務名稱由 "GoXWatch" 改為 "GoXWatch-{資料夾名稱}"
#   - 每個服務的資料路徑獨立於 %ProgramData%\go-xwatch\{後綴}\
#   - 同一根目錄重複安裝時自動偵測並警告
#   - 重複 init --install-service 時以預設 N 提示使用者確認是否覆蓋
#   - 偵測 SCM 登錄的執行檔路徑是否與當前執行的一致（改名執行檔防呆）

$ErrorActionPreference = "Stop"

# Get version from git describe; fallback to "dev"
$ver = git describe --tags --always --dirty 2>$null
if (-not $ver) { $ver = "dev" }

Write-Host "Running tests..."
go test -count=1 -timeout 120s ./...
if ($LASTEXITCODE -ne 0) {
	Write-Error "Tests failed with exit code $LASTEXITCODE"
	exit $LASTEXITCODE
}
Write-Host "All tests passed."

Write-Host "Building xwatch.exe (version $ver)..."

go build -ldflags "-X main.version=$ver" -o xwatch.exe ./cmd/xwatch
if ($LASTEXITCODE -ne 0) {
	Write-Error "Build failed with exit code $LASTEXITCODE"
	exit $LASTEXITCODE
}

Write-Host "Build succeeded. Output: xwatch.exe"
