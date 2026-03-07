package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/filecheck"
	"go-xwatch/internal/mailer"
)

// discardLogger 回傳捨棄所有輸出的 slog.Logger，供測試使用。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

//  sendFilecheckMail

func TestSendFilecheckMail_NoRecipients_ReturnsError(t *testing.T) {
	s := config.Settings{RootDir: t.TempDir()}
	err := sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, time.Now(), nil, nil)
	if err == nil {
		t.Fatal("沒有收件人應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "收件人") {
		t.Errorf("錯誤應提及收件人，got %q", err.Error())
	}
}

func TestSendFilecheckMail_ScanDirNotExist_SendsErrorSubject(t *testing.T) {
	// 預設 scanDir（storage/logs）不存在  subject 應含「掃描異常」
	tmp := t.TempDir()
	enabled := true
	s := config.Settings{
		RootDir: tmp,
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"test@example.com"},
			},
		},
	}
	var gotSubject, gotBody string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subj, body string, _ mailer.SendMailFunc) error {
		gotSubject = subj
		gotBody = body
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	if err := sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, nil); err != nil {
		t.Fatalf("不應回傳 error，got %v", err)
	}
	// 主旨應含前一日日期 2026-03-03
	if !strings.Contains(gotSubject, "2026-03-03") {
		t.Errorf("主旨應含前一日日期，got %q", gotSubject)
	}
	// 主旨應含「掃描異常」
	if !strings.Contains(gotSubject, "掃描異常") {
		t.Errorf("scanDir 不存在時主旨應含「掃描異常」，got %q", gotSubject)
	}
	// 內文應含 [ERROR]
	if !strings.Contains(gotBody, "[ERROR]") {
		t.Errorf("內文應含 [ERROR]，got:\n%s", gotBody)
	}
}

func TestSendFilecheckMail_NoMatch_SendsNotFoundSubject(t *testing.T) {
	// scanDir 存在但無符合 YYYY-MM-DD 的檔案  「無符合檔案」
	tmp := t.TempDir()
	scanDir := filepath.Join(tmp, "storage", "logs")
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 放一個不符合前一日格式的檔案
	_ = os.WriteFile(filepath.Join(scanDir, "other-2026-03-04.log"), []byte("x"), 0o644)

	enabled := true
	s := config.Settings{
		RootDir: tmp,
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	var gotSubject string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subj, _ string, _ mailer.SendMailFunc) error {
		gotSubject = subj
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	_ = sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, nil)

	if !strings.Contains(gotSubject, "無符合檔案") {
		t.Errorf("無符合檔案時主旨應含「無符合檔案」，got %q", gotSubject)
	}
}

func TestSendFilecheckMail_MatchFound_SendsFoundSubject(t *testing.T) {
	// scanDir 存在且有符合 YYYY-MM-DD 格式的檔案  「找到 N 個」
	tmp := t.TempDir()
	scanDir := filepath.Join(tmp, "storage", "logs")
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 前一日 = 2026-03-03，YYYY-MM-DD = "2026-03-03"（2026年3月3日）
	// FileDateFormat = "2006-01-02"  time.Date(2026,3,3) .Format("2006-01-02") = "2026-03-03"
	yesterday := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	datePattern := yesterday.Format(filecheck.FileDateFormat)
	matchFile := "report_" + datePattern + ".csv"
	_ = os.WriteFile(filepath.Join(scanDir, matchFile), []byte("data"), 0o644)

	enabled := true
	s := config.Settings{
		RootDir: tmp,
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	var gotSubject, gotBody string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subj, body string, _ mailer.SendMailFunc) error {
		gotSubject = subj
		gotBody = body
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	if err := sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, nil); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}

	if !strings.Contains(gotSubject, "找到 1 個") {
		t.Errorf("主旨應含「找到 1 個」，got %q", gotSubject)
	}
	if !strings.Contains(gotBody, "[FOUND]") {
		t.Errorf("內文應含 [FOUND]，got:\n%s", gotBody)
	}
	if !strings.Contains(gotBody, matchFile) {
		t.Errorf("內文應含匹配的檔案名稱 %q，got:\n%s", matchFile, gotBody)
	}
}

func TestSendFilecheckMail_CustomScanDir(t *testing.T) {
	// 透過 ScanDir 欄位指定自訂掃描目錄（絕對路徑）
	tmp := t.TempDir()
	customScan := filepath.Join(tmp, "custom_scan")
	if err := os.MkdirAll(customScan, 0o755); err != nil {
		t.Fatal(err)
	}
	yesterday := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	datePattern := yesterday.Format(filecheck.FileDateFormat)
	_ = os.WriteFile(filepath.Join(customScan, "myfile_"+datePattern+".log"), []byte("x"), 0o644)

	enabled := true
	s := config.Settings{
		RootDir: tmp,
		Filecheck: config.FilecheckSettings{
			ScanDir: customScan,
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	var gotSubject string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, subj, _ string, _ mailer.SendMailFunc) error {
		gotSubject = subj
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	_ = sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, nil)

	if !strings.Contains(gotSubject, "找到 1 個") {
		t.Errorf("自訂 scanDir 有符合檔案時主旨應含「找到 1 個」，got %q", gotSubject)
	}
}

func TestSendFilecheckMail_BodyContainsScanDir(t *testing.T) {
	tmp := t.TempDir()
	enabled := true
	s := config.Settings{
		RootDir: tmp,
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	var gotBody string
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, _, body string, _ mailer.SendMailFunc) error {
		gotBody = body
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	_ = sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, nil)

	// 內文應含預設 scanDir 路徑（storage\logs 的前綴 rootDir）
	if !strings.Contains(gotBody, tmp) {
		t.Errorf("內文應含 rootDir %q，got:\n%s", tmp, gotBody)
	}
}

//  runFilecheckMailScheduler

func TestRunFilecheckMailScheduler_StopsOnContextCancel(t *testing.T) {
	enabled := true
	s := config.Settings{
		RootDir: t.TempDir(),
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled:  &enabled,
				Schedule: "23:59",
				To:       []string{"a@b.com"},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runFilecheckMailScheduler(ctx, discardLogger(), s, time.Now, nil)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("排程器在 context 取消後應於 3 秒內停止")
	}
}

// ── filecheck log 目錄隔離測試 ──────────────────────────────────────────

// TestSendFilecheckMail_LogWrittenToSuffixDir 確認 log 寫入 dataDirFn 所指定的目錄，
// 而非 %ProgramData%\go-xwatch\ 基底目錄。
func TestSendFilecheckMail_LogWrittenToSuffixDir(t *testing.T) {
	tmp := t.TempDir()
	suffixDir := filepath.Join(tmp, "myinst") // 模擬後綴子目錄
	baseDir := tmp

	dataDirFn := func() (string, error) { return suffixDir, nil }

	enabled := true
	s := config.Settings{
		RootDir: t.TempDir(),
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, _, _ string, _ mailer.SendMailFunc) error {
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	if err := sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, dataDirFn); err != nil {
		t.Fatalf("不應回傳錯誤，got %v", err)
	}

	// log 應出現在後綴子目錄，不應出現在基底目錄
	suffixLogDir := filecheck.DefaultLogDir(suffixDir)
	baseLogDir := filecheck.DefaultLogDir(baseDir)

	if _, err := os.Stat(baseLogDir); !os.IsNotExist(err) {
		t.Errorf("xwatch-filecheck-logs 不應出現在基底目錄 %s", baseDir)
	}
	if _, err := os.Stat(suffixLogDir); os.IsNotExist(err) {
		t.Errorf("xwatch-filecheck-logs 應出現在後綴目錄 %s", suffixDir)
	}
}

// TestSendFilecheckMail_BaseDirNotCreated_WhenDataDirFnProvided 確認提供正確 dataDirFn 後，
// 基底目錄下不會建立任何 xwatch-filecheck-logs 資料夾（孤立目錄問題不再重現）。
func TestSendFilecheckMail_BaseDirNotCreated_WhenDataDirFnProvided(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramData", tmp)

	const suffix = "plant-A"
	suffixDir := filepath.Join(tmp, "go-xwatch", suffix)
	dataDirFn := func() (string, error) { return suffixDir, nil }

	enabled := true
	s := config.Settings{
		RootDir: t.TempDir(),
		Filecheck: config.FilecheckSettings{
			Mail: config.FilecheckMailSettings{
				Enabled: &enabled,
				To:      []string{"a@b.com"},
			},
		},
	}
	fakeSend := func(_ context.Context, _ mailer.SMTPConfig, _, _ string, _ mailer.SendMailFunc) error {
		return nil
	}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	_ = sendFilecheckMail(context.Background(), discardLogger(), s, time.UTC, now, fakeSend, dataDirFn)

	// %ProgramData%\go-xwatch\ 基底目錄下不應出現 xwatch-filecheck-logs
	baseLogDir := filepath.Join(tmp, "go-xwatch", "xwatch-filecheck-logs")
	if _, err := os.Stat(baseLogDir); !os.IsNotExist(err) {
		t.Errorf("基底目錄不應建立 xwatch-filecheck-logs，路徑：%s", baseLogDir)
	}
}
