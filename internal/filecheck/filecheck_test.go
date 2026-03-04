package filecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedDate = time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)

//  FileDateFormat

func TestFileDateFormat_IsYYYYDDMM(t *testing.T) {
	// 2026-03-04 格式化後應為 2026-04-03（YYYY-DD-MM）
	got := fixedDate.Format(FileDateFormat)
	want := "2026-04-03"
	if got != want {
		t.Errorf("FileDateFormat = %q, want %q", got, want)
	}
}

//  DefaultScanDir

func TestDefaultScanDir(t *testing.T) {
	got := DefaultScanDir(`D:\root`)
	want := filepath.Join(`D:\root`, "storage", "logs")
	if got != want {
		t.Errorf("DefaultScanDir = %q, want %q", got, want)
	}
}

//  ResolveScanDir

func TestResolveScanDir_Empty_UsesDefault(t *testing.T) {
	root := `D:\root`
	got := ResolveScanDir(root, "")
	want := DefaultScanDir(root)
	if got != want {
		t.Errorf("ResolveScanDir(empty) = %q, want %q", got, want)
	}
}

func TestResolveScanDir_Absolute_ReturnsAsIs(t *testing.T) {
	abs := `C:\custom\logs`
	got := ResolveScanDir(`D:\root`, abs)
	if got != abs {
		t.Errorf("ResolveScanDir(absolute) = %q, want %q", got, abs)
	}
}

func TestResolveScanDir_Relative_JoinsWithRoot(t *testing.T) {
	root := `D:\root`
	got := ResolveScanDir(root, `backup\logs`)
	want := filepath.Join(root, `backup\logs`)
	if got != want {
		t.Errorf("ResolveScanDir(relative) = %q, want %q", got, want)
	}
}

//  ScanForDate

func TestScanForDate_DirNotExist_ReturnsError(t *testing.T) {
	files, err := ScanForDate(`Z:\nonexistent\path`, fixedDate)
	if err == nil {
		t.Error("不存在的目錄應回傳 error")
	}
	if len(files) != 0 {
		t.Errorf("不存在的目錄不應有結果，got %v", files)
	}
}

func TestScanForDate_EmptyDir_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	files, err := ScanForDate(tmp, fixedDate)
	if err != nil {
		t.Fatalf("空目錄不應回傳 error，got %v", err)
	}
	if len(files) != 0 {
		t.Errorf("空目錄應回傳空清單，got %v", files)
	}
}

func TestScanForDate_NoMatchingFiles(t *testing.T) {
	tmp := t.TempDir()
	// 放一個不含目標日期的檔案
	_ = os.WriteFile(filepath.Join(tmp, "other-2025-12-31.log"), []byte("x"), 0o644)

	files, err := ScanForDate(tmp, fixedDate)
	if err != nil {
		t.Fatalf("不應回傳 error，got %v", err)
	}
	if len(files) != 0 {
		t.Errorf("應回傳空清單（無符合檔案），got %v", files)
	}
}

func TestScanForDate_MatchesDateInYYYYDDMM(t *testing.T) {
	tmp := t.TempDir()
	// fixedDate = 2026-03-04，格式化為 YYYY-DD-MM = "2026-04-03"
	dateStr := fixedDate.Format(FileDateFormat) // "2026-04-03"

	// 建立一個含有 YYYY-DD-MM 格式日期的檔案
	matchFile := "report_" + dateStr + ".csv"
	_ = os.WriteFile(filepath.Join(tmp, matchFile), []byte("data"), 0o644)
	// 不應被比對到的檔案
	_ = os.WriteFile(filepath.Join(tmp, "report_2026-03-04.csv"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "other.log"), []byte("x"), 0o644)

	files, err := ScanForDate(tmp, fixedDate)
	if err != nil {
		t.Fatalf("不應回傳 error，got %v", err)
	}
	if len(files) != 1 || files[0] != matchFile {
		t.Errorf("應只找到 %q，got %v", matchFile, files)
	}
}

func TestScanForDate_MultipleMatches(t *testing.T) {
	tmp := t.TempDir()
	dateStr := fixedDate.Format(FileDateFormat)

	names := []string{
		"a_" + dateStr + ".log",
		"b_" + dateStr + ".csv",
		"c_" + dateStr + ".txt",
	}
	for _, n := range names {
		_ = os.WriteFile(filepath.Join(tmp, n), []byte("x"), 0o644)
	}

	files, err := ScanForDate(tmp, fixedDate)
	if err != nil {
		t.Fatalf("不應回傳 error，got %v", err)
	}
	if len(files) != 3 {
		t.Errorf("應找到 3 個檔案，got %d: %v", len(files), files)
	}
}

func TestScanForDate_SkipsSubdirectories(t *testing.T) {
	tmp := t.TempDir()
	dateStr := fixedDate.Format(FileDateFormat)

	// 在子目錄放符合的「目錄名稱」（不應被計入）
	subDir := filepath.Join(tmp, "sub_"+dateStr)
	_ = os.Mkdir(subDir, 0o755)

	files, err := ScanForDate(tmp, fixedDate)
	if err != nil {
		t.Fatalf("不應回傳 error，got %v", err)
	}
	if len(files) != 0 {
		t.Errorf("子目錄不應被計入，got %v", files)
	}
}

//  BuildMailReport

var reportDate = time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
var reportDir = `D:\root\storage\logs`

func TestBuildMailReport_Found(t *testing.T) {
	files := []string{"report_2026-03-03.csv", "report_2026-04-03.csv"}
	subject, body := BuildMailReport(reportDir, files, reportDate, nil)

	// 主旨含顯示日期（YYYY-MM-DD 形式）與狀態
	if !strings.Contains(subject, "2026-03-03") {
		t.Errorf("主旨應含 2026-03-03，got %q", subject)
	}
	if !strings.Contains(subject, "找到 2 個") {
		t.Errorf("主旨應含「找到 2 個」，got %q", subject)
	}
	if !strings.Contains(body, "[FOUND]") {
		t.Errorf("內文應含 [FOUND]，got:\n%s", body)
	}
	// 內文應列出檔案名稱
	if !strings.Contains(body, "report_2026-04-03.csv") {
		t.Errorf("內文應含搜尋到的檔案名稱，got:\n%s", body)
	}
	// 搜尋格式應含 YYYY-DD-MM 格式的日期字串
	datePattern := reportDate.Format(FileDateFormat)
	if !strings.Contains(body, datePattern) {
		t.Errorf("內文應含搜尋日期格式 %q，got:\n%s", datePattern, body)
	}
}

func TestBuildMailReport_NotFound(t *testing.T) {
	subject, body := BuildMailReport(reportDir, nil, reportDate, nil)

	if !strings.Contains(subject, "無符合檔案") {
		t.Errorf("無檔案時主旨應含「無符合檔案」，got %q", subject)
	}
	if !strings.Contains(body, "[NOT FOUND]") {
		t.Errorf("內文應含 [NOT FOUND]，got:\n%s", body)
	}
}

func TestBuildMailReport_ScanError(t *testing.T) {
	subject, body := BuildMailReport(reportDir, nil, reportDate, os.ErrNotExist)

	if !strings.Contains(subject, "掃描異常") {
		t.Errorf("掃描錯誤時主旨應含「掃描異常」，got %q", subject)
	}
	if !strings.Contains(body, "[ERROR]") {
		t.Errorf("內文應含 [ERROR]，got:\n%s", body)
	}
}

func TestBuildMailReport_AlwaysNonEmpty(t *testing.T) {
	// 確保有無皆寄：任何情況下 subject 和 body 都不為空
	cases := []struct {
		name    string
		files   []string
		scanErr error
	}{
		{"有檔案", []string{"f.log"}, nil},
		{"無檔案", nil, nil},
		{"掃描錯誤", nil, os.ErrNotExist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subject, body := BuildMailReport(reportDir, tc.files, reportDate, tc.scanErr)
			if subject == "" {
				t.Error("主旨不應為空")
			}
			if body == "" {
				t.Error("內文不應為空")
			}
		})
	}
}

func TestBuildMailReport_ContainsScanDir(t *testing.T) {
	_, body := BuildMailReport(reportDir, nil, reportDate, nil)
	if !strings.Contains(body, reportDir) {
		t.Errorf("內文應含掃描目錄路徑 %q，got:\n%s", reportDir, body)
	}
}

func TestBuildMailReport_ManyFilesShowEllipsis(t *testing.T) {
	var files []string
	dateStr := reportDate.Format(FileDateFormat)
	for i := 0; i < 25; i++ {
		files = append(files, fmt.Sprintf("file_%s_%02d.log", dateStr, i))
	}
	_, body := BuildMailReport(reportDir, files, reportDate, nil)
	if !strings.Contains(body, "另外") {
		t.Errorf("超過 20 個檔案時應顯示省略，got:\n%s", body)
	}
}

//  WriteLog

func TestWriteLog_CreatesFile(t *testing.T) {
	tmp := t.TempDir()
	scanDir := `D:\root\storage\logs`
	files := []string{"data_2026-04-03.csv"}

	if err := WriteLog(tmp, scanDir, files, fixedDate, nil, fixedDate); err != nil {
		t.Fatalf("WriteLog 不應回傳錯誤，got %v", err)
	}

	logFile := filepath.Join(tmp, "filecheck_"+fixedDate.Format("2006-01-02")+".log")
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("log 檔應已建立，got %v", err)
	}
	if !strings.Contains(string(content), "[FOUND]") {
		t.Errorf("log 內容應含 [FOUND]，got %q", string(content))
	}
}

func TestWriteLog_NotFound(t *testing.T) {
	tmp := t.TempDir()
	if err := WriteLog(tmp, `D:\root\storage\logs`, nil, fixedDate, nil, fixedDate); err != nil {
		t.Fatalf("寫入空結果不應回傳錯誤，got %v", err)
	}
	logFile := filepath.Join(tmp, "filecheck_"+fixedDate.Format("2006-01-02")+".log")
	content, _ := os.ReadFile(logFile)
	if !strings.Contains(string(content), "[NOT FOUND]") {
		t.Errorf("無符合檔案應寫入 [NOT FOUND]，got %q", string(content))
	}
}

func TestWriteLog_ErrorCase(t *testing.T) {
	tmp := t.TempDir()
	if err := WriteLog(tmp, `D:\root\storage\logs`, nil, fixedDate, os.ErrNotExist, fixedDate); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}
	logFile := filepath.Join(tmp, "filecheck_"+fixedDate.Format("2006-01-02")+".log")
	content, _ := os.ReadFile(logFile)
	if !strings.Contains(string(content), "[ERROR]") {
		t.Errorf("錯誤案例應寫入 [ERROR]，got %q", string(content))
	}
}

//  DefaultLogDir

func TestDefaultLogDir(t *testing.T) {
	got := DefaultLogDir(`C:\ProgramData\go-xwatch`)
	want := filepath.Join(`C:\ProgramData\go-xwatch`, "xwatch-filecheck-logs")
	if got != want {
		t.Errorf("DefaultLogDir = %q, want %q", got, want)
	}
}
