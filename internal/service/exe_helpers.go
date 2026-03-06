package service

import "strings"

// parseExeFromBinaryPath 從 Windows 服務 BinaryPathName 字串中解析出執行檔路徑。
//
// Windows SCM 儲存的 BinaryPathName 有兩種常見格式：
//   - 有引號（路徑含空格）："C:\path\xwatch.exe" --service --name GoXWatch-A
//   - 無引號（路徑無空格）：C:\path\xwatch.exe --service --name GoXWatch-A
//
// 本函式只擷取可執行檔的路徑部分，去除所有命令列參數。
func parseExeFromBinaryPath(binaryPath string) string {
	s := strings.TrimSpace(binaryPath)
	if strings.HasPrefix(s, `"`) {
		// 有引號格式：取第一個結束引號之前的內容
		end := strings.Index(s[1:], `"`)
		if end >= 0 {
			return s[1 : end+1]
		}
	}
	// 無引號格式：取第一個空白字元之前的部分
	if idx := strings.IndexByte(s, ' '); idx > 0 {
		return s[:idx]
	}
	return s
}
