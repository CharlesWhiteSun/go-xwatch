package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DataDir returns the base data directory (ProgramData/go-xwatch) without creating it.
func DataDir() (string, error) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return "", fmt.Errorf("ProgramData is empty")
	}
	return filepath.Join(programData, "go-xwatch"), nil
}

// EnsureDataDir ensures the data directory exists (and on Windows applies tightened ACL).
func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := ensureDirWithACL(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// DataDirForSuffix 回傳特定服務的資料目錄路徑：
//   - suffix 非空 → %ProgramData%\go-xwatch\{suffix}
//   - suffix 為空 → %ProgramData%\go-xwatch（向後相容，傳統單服務模式）
//
// 此函式不建立目錄，僅計算路徑。
func DataDirForSuffix(suffix string) (string, error) {
	base, err := DataDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(suffix) == "" {
		return base, nil
	}
	return filepath.Join(base, suffix), nil
}

// EnsureDataDirForSuffix 建立並回傳特定服務的資料目錄。
// suffix 為空時退化為 EnsureDataDir（向後相容）。
func EnsureDataDirForSuffix(suffix string) (string, error) {
	dir, err := DataDirForSuffix(suffix)
	if err != nil {
		return "", err
	}
	if err := ensureDirWithACL(dir); err != nil {
		return "", err
	}
	return dir, nil
}
