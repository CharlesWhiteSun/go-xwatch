//go:build windows

package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"go-xwatch/internal/paths"
)

// FindServiceForRoot 掃描所有已安裝的 GoXWatch 服務設定，
// 尋找是否已有服務監控了相同的根目錄。
//
// 原理：讀取 %ProgramData%\go-xwatch\*\config.json，比對 rootDir 欄位。
// 找到吻合時回傳該服務的服務名稱；未找到則回傳空字串（非錯誤）。
func FindServiceForRoot(rootDir string) (string, error) {
	baseDir, err := paths.DataDir()
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	absTarget, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cfgPath := filepath.Join(baseDir, entry.Name(), "config.json")
		cfgBytes, readErr := os.ReadFile(cfgPath)
		if readErr != nil {
			continue
		}
		// 僅解析需要的欄位，避免 import cycle（不使用 config.Settings）
		var partial struct {
			RootDir     string `json:"rootDir"`
			ServiceName string `json:"serviceName"`
		}
		if jsonErr := json.Unmarshal(cfgBytes, &partial); jsonErr != nil {
			continue
		}
		absExisting := filepath.Clean(partial.RootDir)
		if strings.EqualFold(absExisting, absTarget) {
			// 找到吻合的根目錄：優先使用設定中記錄的服務名稱，
			// 若未記錄則由資料目錄名稱（後綴）重建服務名稱。
			svcName := strings.TrimSpace(partial.ServiceName)
			if svcName == "" {
				svcName = ServicePrefix + "-" + entry.Name()
			}
			return svcName, nil
		}
	}
	return "", nil
}
