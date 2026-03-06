package service

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ServicePrefix 是所有 GoXWatch Windows 服務的名稱前綴。
const ServicePrefix = "GoXWatch"

// reSanitizeService 匹配 Windows 服務名稱中不合法的字元（保留英數、連字號、底線）。
var reSanitizeService = regexp.MustCompile(`[^a-zA-Z0-9\-_]`)

// ServiceSuffixFromRoot 從監控根目錄路徑推導出服務名稱後綴。
// 取得目錄的 Base 名稱後，將不合法字元替換為連字號。
//
// 例如：
//   - D:\data\plant-A  → "plant-A"
//   - C:\監控\工廠      → "---"（非 ASCII 均被替換）
//   - D:\root          → "root"
func ServiceSuffixFromRoot(rootDir string) string {
	base := filepath.Base(strings.TrimSpace(rootDir))
	sanitized := reSanitizeService.ReplaceAllString(base, "-")
	sanitized = strings.Trim(sanitized, "-")
	if sanitized == "" {
		sanitized = "default"
	}
	return sanitized
}

// ServiceNameFromRoot 從監控根目錄路徑推導出完整的 Windows 服務名稱。
// 格式為 "GoXWatch-{後綴}"，其中後綴由 ServiceSuffixFromRoot 產生。
//
// 例如：D:\data\plant-A → "GoXWatch-plant-A"
func ServiceNameFromRoot(rootDir string) string {
	return ServicePrefix + "-" + ServiceSuffixFromRoot(rootDir)
}

// SuffixFromServiceName 從服務名稱中擷取後綴部分。
//   - "GoXWatch-plant-A" → "plant-A"
//   - "GoXWatch"         → ""（傳統單服務模式，無後綴）
func SuffixFromServiceName(name string) string {
	prefix := ServicePrefix + "-"
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return ""
}
