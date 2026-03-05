// Package filecheck 提供目錄內檔案存在性排程掃描功能。
// 在每日排程時間掃描指定目錄，確認是否存在包含前一日日期（YYYY-MM-DD）的檔案，
// 無論結果如何均寄送郵件通知相關人員。
package filecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileDateFormat 是在檔案名稱中搜尋日期時使用的 Go 時間格式，對應 YYYY-MM-DD。
// 例如 2026-03-04 會格式化為 "2026-03-04"（標準年-月-日格式）。
const FileDateFormat = "2006-01-02"

// ScanForDate 掃描 dir 目錄，找出名稱中包含 date（YYYY-MM-DD 格式）字串的檔案。
// 若目錄不存在或無法讀取，回傳 nil slice 與 error；
// 目錄存在但無符合檔案時回傳空 slice 與 nil error。
func ScanForDate(dir string, date time.Time) ([]string, error) {
	dateStr := date.Format(FileDateFormat)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var matched []string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), dateStr) {
			matched = append(matched, e.Name())
		}
	}
	return matched, nil
}

// DefaultScanDir 回傳預設掃描目錄：{rootDir}\storage\logs。
func DefaultScanDir(rootDir string) string {
	return filepath.Join(rootDir, "storage", "logs")
}

// ResolveScanDir 解析實際要掃描的目錄路徑。
// 若 configured 為空則使用預設路徑；相對路徑以 rootDir 為基底解析。
func ResolveScanDir(rootDir, configured string) string {
	trimmed := strings.TrimSpace(configured)
	if trimmed == "" {
		return DefaultScanDir(rootDir)
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	return filepath.Join(rootDir, trimmed)
}

// BuildMailReport 依掃描結果建立郵件主旨與內文。
// 無論 files 是否為空（或 scanErr 不為 nil），皆回傳非空的主旨與內文，
// 確保「有無皆寄」的行為得以實現。
//
// date 為被掃描的前一日日期；datePattern 為在目錄中搜尋的格式（YYYY-MM-DD）。
func BuildMailReport(scanDir string, files []string, date time.Time, scanErr error) (subject, body string) {
	dayStr := date.Format("2006-01-02")
	datePattern := date.Format(FileDateFormat) // YYYY-MM-DD

	var statusLabel string
	switch {
	case scanErr != nil:
		statusLabel = "掃描異常"
	case len(files) == 0:
		statusLabel = "無符合檔案"
	default:
		statusLabel = fmt.Sprintf("找到 %d 個", len(files))
	}
	subject = fmt.Sprintf("XWatch 目錄檔案存在性報告：%s（%s）", dayStr, statusLabel)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("前一日（%s）目錄檔案存在性報告\n", dayStr))
	sb.WriteString(strings.Repeat("=", 56) + "\n\n")
	sb.WriteString(fmt.Sprintf("掃描目錄：%s\n", scanDir))
	sb.WriteString(fmt.Sprintf("搜尋格式：YYYY-MM-DD（本次搜尋：%s）\n\n", datePattern))

	if scanErr != nil {
		sb.WriteString(fmt.Sprintf("結果：[ERROR] 無法讀取目錄\n  %v\n", scanErr))
	} else if len(files) == 0 {
		sb.WriteString("結果：[NOT FOUND] 目錄中未找到包含指定日期格式的檔案\n")
	} else {
		sb.WriteString(fmt.Sprintf("結果：[FOUND] 找到 %d 個符合的檔案：\n", len(files)))
		const maxShow = 20
		for i, f := range files {
			if i >= maxShow {
				sb.WriteString(fmt.Sprintf("   以及另外 %d 個檔案\n", len(files)-maxShow))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	sb.WriteString("\n" + strings.Repeat("=", 56) + "\n")
	sb.WriteString("（此報告由 XWatch filecheck mail 排程自動寄出）\n")
	body = sb.String()
	return
}

// WriteLog 將掃描結果以追加方式寫入 logDir/filecheck_YYYY-MM-DD.log（以 now 決定檔名）。
func WriteLog(logDir string, scanDir string, files []string, date time.Time, scanErr error, now time.Time) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("建立 filecheck log 目錄失敗: %w", err)
	}
	logFile := filepath.Join(logDir, "filecheck_"+now.Format("2006-01-02")+".log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("開啟 filecheck log 失敗: %w", err)
	}
	defer f.Close()

	ts := now.Format("2006-01-02 15:04:05")
	dateStr := date.Format(FileDateFormat)
	var line string
	if scanErr != nil {
		line = fmt.Sprintf("%s\t%s\t[ERROR] %v\n", ts, scanDir, scanErr)
	} else if len(files) == 0 {
		line = fmt.Sprintf("%s\t%s\t[NOT FOUND] 無包含 %s 的檔案\n", ts, scanDir, dateStr)
	} else {
		line = fmt.Sprintf("%s\t%s\t[FOUND] %d 個：%s\n", ts, scanDir, len(files), joinFiles(files))
	}
	_, err = f.WriteString(line)
	return err
}

// DefaultLogDir 回傳 filecheck log 目錄的預設路徑。
func DefaultLogDir(dataDir string) string {
	return filepath.Join(dataDir, "xwatch-filecheck-logs")
}

func joinFiles(files []string) string {
	const maxShow = 5
	if len(files) > maxShow {
		return strings.Join(files[:maxShow], ", ") + fmt.Sprintf("  共 %d 個", len(files))
	}
	return strings.Join(files, ", ")
}
