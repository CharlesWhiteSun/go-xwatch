# Build script with auto version injection from git tags.
# Usage: ./build.ps1

$ErrorActionPreference = "Stop"

# Get version from git describe; fallback to "dev"
$ver = git describe --tags --always --dirty 2>$null
if (-not $ver) { $ver = "dev" }

Write-Host "Building xwatch.exe (version $ver)..."

go build -ldflags "-X main.version=$ver" -o xwatch.exe ./cmd/xwatch
if ($LASTEXITCODE -ne 0) {
	Write-Error "Build failed with exit code $LASTEXITCODE"
	exit $LASTEXITCODE
}

Write-Host "Build succeeded. Output: xwatch.exe"
