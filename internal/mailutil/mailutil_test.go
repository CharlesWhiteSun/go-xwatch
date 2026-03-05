package mailutil_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/mailer"
	"go-xwatch/internal/mailutil"
)

// ── helper ───────────────────────────────────────────────────────────────────

func readMailLogLastLine(t *testing.T, dir string, now time.Time) string {
	t.Helper()
	loc := time.FixedZone("CST", 8*60*60)
	localTime := now.In(loc)
	file := filepath.Join(dir, fmt.Sprintf("mail_%s.log", localTime.Format("2006-01-02")))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("readMailLogLastLine: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[len(lines)-1]
}

// ── PrepareBody ───────────────────────────────────────────────────────────────

// TestPrepareBodyMissingAddsNote 確認日誌不存在且模板有值時，內文會加上「未附檔」說明。
func TestPrepareBodyMissingAddsNote(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "missing.log") // 不存在

	body, missing := mailutil.PrepareBody(logPath, "2026-01-01", "自訂內文模板", "預設內文", "無日誌")
	if !missing {
		t.Fatal("日誌不存在時 missing 應為 true")
	}
	if !strings.Contains(body, "未附檔") {
		t.Errorf("模板有值時內文應含「未附檔」，實際：%q", body)
	}
}

// TestPrepareBodyWithExistingLog 確認日誌存在時 {day} 模板正常替換。
func TestPrepareBodyWithExistingLog(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-01-01.log")
	if err := os.WriteFile(logPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	body, missing := mailutil.PrepareBody(logPath, "2026-01-01", "自訂模板 {day}", "預設內文", "無日誌")
	if missing {
		t.Fatal("日誌存在時 missing 應為 false")
	}
	if !strings.Contains(body, "2026-01-01") {
		t.Errorf("內文應含日期，實際：%q", body)
	}
}

// TestPrepareBody_MissingLogEmptyTemplate 確認日誌不存在且模板為空時直接回傳 missingBody。
func TestPrepareBody_MissingLogEmptyTemplate(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "missing.log")

	body, missing := mailutil.PrepareBody(logPath, "2026-01-01", "", "預設內文", "無日誌內文")
	if !missing {
		t.Fatal("日誌不存在時 missing 應為 true")
	}
	if body != "無日誌內文" {
		t.Errorf("模板為空時應回傳 missingBody，實際：%q", body)
	}
}

// ── WriteMailLog ──────────────────────────────────────────────────────────────

// TestWriteMailLog 確認 WriteMailLog 正常寫入成功記錄。
func TestWriteMailLog(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := mailutil.WriteMailLog(tmp, now, "ok", "2026-03-02",
		[]string{"test@example.com"}, "測試主旨", "attached", "")
	if err != nil {
		t.Fatalf("WriteMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "狀態=成功") {
		t.Errorf("log 行應含「狀態=成功」，實際：%s", line)
	}
	if !strings.Contains(line, "test@example.com") {
		t.Errorf("log 行應含收件人，實際：%s", line)
	}
	if !strings.Contains(line, "測試主旨") {
		t.Errorf("log 行應含主旨，實際：%s", line)
	}
	if !strings.Contains(line, "附件=已附檔") {
		t.Errorf("log 行應含「附件=已附檔」，實際：%s", line)
	}
	if strings.Contains(line, "錯誤=") {
		t.Errorf("errMsg 為空時不應含「錯誤=」欄位，實際：%s", line)
	}
}

// TestWriteMailLog_FailWithExistingAttachment 確認寄信失敗且附件存在時，
// 附件欄位顯示「已附檔」而非「失敗」。
func TestWriteMailLog_FailWithExistingAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := mailutil.WriteMailLog(tmp, now, "fail", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "attached", "SMTP 錯誤")
	if err != nil {
		t.Fatalf("WriteMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=已附檔") {
		t.Errorf("寄信失敗但附件存在時附件欄位應為「已附檔」，實際：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "狀態=失敗") {
		t.Errorf("狀態欄位應為「失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "錯誤=") {
		t.Errorf("應有「錯誤=」欄位，實際：%s", line)
	}
}

// TestWriteMailLog_FailWithMissingAttachment 確認寄信失敗且附件不存在時，
// 附件欄位顯示「未附檔」而非「失敗」。
func TestWriteMailLog_FailWithMissingAttachment(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := mailutil.WriteMailLog(tmp, now, "fail", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "missing", "SMTP 錯誤")
	if err != nil {
		t.Fatalf("WriteMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=未附檔") {
		t.Errorf("寄信失敗且無附件時附件欄位應為「未附檔」，實際：%s", line)
	}
	if strings.Contains(line, "附件=失敗") {
		t.Errorf("附件欄位不應顯示「附件=失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "狀態=失敗") {
		t.Errorf("狀態欄位應為「失敗」，實際：%s", line)
	}
	if !strings.Contains(line, "錯誤=") {
		t.Errorf("應有「錯誤=」欄位，實際：%s", line)
	}
}

// TestWriteMailLog_ErrorAttachmentStatus 確認 "error" 附件狀態映射為「失敗」。
func TestWriteMailLog_ErrorAttachmentStatus(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)

	err := mailutil.WriteMailLog(tmp, now, "fail", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "error", "壓縮失敗")
	if err != nil {
		t.Fatalf("WriteMailLog error: %v", err)
	}

	line := readMailLogLastLine(t, tmp, now)
	if !strings.Contains(line, "附件=失敗") {
		t.Errorf("error 附件狀態應對應「附件=失敗」，實際：%s", line)
	}
}

// TestWriteMailLog_EmptyDir 確認 dir 為空時不寫檔案且不回傳錯誤。
func TestWriteMailLog_EmptyDir(t *testing.T) {
	err := mailutil.WriteMailLog("", time.Now(), "ok", "2026-03-02",
		[]string{"test@example.com"}, "主旨", "attached", "")
	if err != nil {
		t.Fatalf("dir 為空時應直接回傳 nil，實際：%v", err)
	}
}

// ── BuildMailContent ──────────────────────────────────────────────────────────

func TestBuildMailContent_LogMissing_Immediate(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log") // 不存在

	const mode = "(即時)"
	subject, body, missing := mailutil.BuildMailContent("MyProject", "2026-03-02", logPath, mode, true)

	if !missing {
		t.Fatal("日誌不存在時 attachmentMissing 應為 true")
	}
	if !strings.Contains(subject, "無資料夾異動紀錄") {
		t.Errorf("主旨應含「無資料夾異動紀錄」，實際：%q", subject)
	}
	if !strings.Contains(subject, mode) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", mode, subject)
	}
	if !strings.Contains(subject, "MyProject") {
		t.Errorf("主旨應含目錄名稱，實際：%q", subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "特此通知") {
		t.Errorf("內文應含「特此通知」，實際：%q", body)
	}
}

func TestBuildMailContent_LogMissing_Scheduled(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")

	const mode = "(排程)"
	subject, body, missing := mailutil.BuildMailContent("MyProject", "2026-03-02", logPath, mode, false)

	if !missing {
		t.Fatal("日誌不存在時 attachmentMissing 應為 true")
	}
	if !strings.Contains(subject, "無資料夾異動紀錄") {
		t.Errorf("主旨應含「無資料夾異動紀錄」，實際：%q", subject)
	}
	if !strings.Contains(subject, mode) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", mode, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "特此通知") {
		t.Errorf("內文應含「特此通知」，實際：%q", body)
	}
}

func TestBuildMailContent_EmptyLog_TreatedAsMissing(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, missing := mailutil.BuildMailContent("MyProject", "2026-03-02", logPath, "(即時)", true)
	if !missing {
		t.Fatal("空日誌檔應被視為 missing，attachmentMissing 應為 true")
	}
}

func TestBuildMailContent_LogExists_Immediate_NoColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	const mode = "(即時)"
	subject, body, missing := mailutil.BuildMailContent("MyProject", "2026-03-02", logPath, mode, true)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	// 即時模式有日誌：主旨不含冒號（用空格連接）
	if strings.Contains(subject, ":") {
		t.Errorf("即時模式有日誌時主旨不應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.Contains(subject, mode) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", mode, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}

func TestBuildMailContent_LogExists_Scheduled_HasColon(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("some log data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	const mode = "(排程)"
	subject, body, missing := mailutil.BuildMailContent("MyProject", "2026-03-02", logPath, mode, false)

	if missing {
		t.Fatal("日誌存在時 attachmentMissing 應為 false")
	}
	// 排程模式有日誌：主旨含冒號
	if !strings.Contains(subject, ":") {
		t.Errorf("排程模式有日誌時主旨應有冒號，實際：%q", subject)
	}
	if !strings.Contains(subject, "已撈出資料") {
		t.Errorf("主旨應含「已撈出資料」，實際：%q", subject)
	}
	if !strings.Contains(subject, mode) {
		t.Errorf("主旨應含模式標籤 %q，實際：%q", mode, subject)
	}
	if !strings.HasPrefix(subject, "XWatch ") {
		t.Errorf("主旨應以 \"XWatch \" 開頭，實際：%q", subject)
	}
	if !strings.Contains(body, "壓縮檔") {
		t.Errorf("內文應含「壓縮檔」，實際：%q", body)
	}
}

func TestBuildMailContent_RootDirNameAndDayInOutput(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "watch_2026-03-02.log")
	if err := os.WriteFile(logPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject, body, _ := mailutil.BuildMailContent("UniqueProject", "2026-03-02", logPath, "(即時)", true)

	if !strings.Contains(subject, "UniqueProject") {
		t.Errorf("主旨應含目錄名稱，實際：%q", subject)
	}
	if !strings.Contains(body, "UniqueProject") {
		t.Errorf("內文應含目錄名稱，實際：%q", body)
	}
	if !strings.Contains(subject, "2026-03-02") {
		t.Errorf("主旨應含日期字串，實際：%q", subject)
	}
	if !strings.Contains(body, "2026-03-02") {
		t.Errorf("內文應含日期字串，實際：%q", body)
	}
}

// ── RenderWithDay ─────────────────────────────────────────────────────────────

func TestRenderWithDay_EmptyTemplate(t *testing.T) {
	got := mailutil.RenderWithDay("", "2026-03-02", "fallback text")
	if got != "fallback text" {
		t.Errorf("模板為空應回傳 fallback，實際：%q", got)
	}
}

func TestRenderWithDay_CurlyBraceDay(t *testing.T) {
	got := mailutil.RenderWithDay("日期是 {day} 的報告", "2026-03-02", "")
	if !strings.Contains(got, "2026-03-02") {
		t.Errorf("{day} 應被替換為日期，實際：%q", got)
	}
	if strings.Contains(got, "{day}") {
		t.Errorf("{day} 應已被替換，實際：%q", got)
	}
}

func TestRenderWithDay_PercentS(t *testing.T) {
	got := mailutil.RenderWithDay("日期是 %s 的報告", "2026-03-02", "")
	if !strings.Contains(got, "2026-03-02") {
		t.Errorf("%%s 應被替換為日期，實際：%q", got)
	}
}

func TestRenderWithDay_NoPlaceholder(t *testing.T) {
	got := mailutil.RenderWithDay("固定文字", "2026-03-02", "")
	if !strings.Contains(got, "固定文字") {
		t.Errorf("無替換符號應保留原文，實際：%q", got)
	}
	if !strings.Contains(got, "2026-03-02") {
		t.Errorf("無替換符號時應補充日期，實際：%q", got)
	}
}

// ── LoadLocation ──────────────────────────────────────────────────────────────

func TestLoadLocation_ValidTZ(t *testing.T) {
	loc := mailutil.LoadLocation("Asia/Taipei")
	if loc == nil {
		t.Fatal("LoadLocation 應回傳非 nil 的 Location")
	}
	if !strings.Contains(loc.String(), "Taipei") {
		t.Errorf("回傳 Location 應含 Taipei，實際：%q", loc.String())
	}
}

func TestLoadLocation_InvalidTZ_FallbackToFixedZone(t *testing.T) {
	loc := mailutil.LoadLocation("Invalid/Zone/Name")
	if loc == nil {
		t.Fatal("LoadLocation 載入失敗時應回傳固定時區，不應為 nil")
	}
}

func TestLoadLocation_EmptyTZ_UsesDefault(t *testing.T) {
	loc := mailutil.LoadLocation("")
	if loc == nil {
		t.Fatal("LoadLocation 空字串時應使用預設時區，不應為 nil")
	}
}

// ── MakeAbsPath ───────────────────────────────────────────────────────────────

func TestMakeAbsPath_EmptyString(t *testing.T) {
	got := mailutil.MakeAbsPath("")
	if got != "" {
		t.Errorf("空字串應回傳空字串，實際：%q", got)
	}
}

func TestMakeAbsPath_AbsPath(t *testing.T) {
	tmp := t.TempDir()
	got := mailutil.MakeAbsPath(tmp)
	if got != tmp {
		t.Errorf("絕對路徑應原樣回傳，期望：%q，實際：%q", tmp, got)
	}
}

// ── NormalizeList ─────────────────────────────────────────────────────────────

func TestNormalizeList_FiltersBlank(t *testing.T) {
	got := mailutil.NormalizeList([]string{"a@a.com", "", "  ", "b@b.com"})
	if len(got) != 2 {
		t.Fatalf("應過濾空白，期望 2 個，實際 %d：%v", len(got), got)
	}
	if got[0] != "a@a.com" || got[1] != "b@b.com" {
		t.Errorf("結果不符預期：%v", got)
	}
}

func TestNormalizeList_AllBlank(t *testing.T) {
	got := mailutil.NormalizeList([]string{"", "  ", "\t"})
	if len(got) != 0 {
		t.Errorf("全空白應回傳空切片，實際：%v", got)
	}
}

func TestNormalizeList_Nil(t *testing.T) {
	got := mailutil.NormalizeList(nil)
	if len(got) != 0 {
		t.Errorf("nil 輸入應回傳空切片，實際：%v", got)
	}
}

// ── FirstNonEmpty ─────────────────────────────────────────────────────────────

func TestFirstNonEmpty_ReturnsFirst(t *testing.T) {
	got := mailutil.FirstNonEmpty("", "  ", "third", "fourth")
	if got != "third" {
		t.Errorf("應回傳第一個非空值，期望 \"third\"，實際：%q", got)
	}
}

func TestFirstNonEmpty_AllEmpty(t *testing.T) {
	got := mailutil.FirstNonEmpty("", "  ", "\t")
	if got != "" {
		t.Errorf("全空時應回傳空字串，實際：%q", got)
	}
}

func TestFirstNonEmpty_NoArgs(t *testing.T) {
	got := mailutil.FirstNonEmpty()
	if got != "" {
		t.Errorf("無參數時應回傳空字串，實際：%q", got)
	}
}

// ── ResolveSMTPConfig ─────────────────────────────────────────────────────────

func TestResolveSMTPConfig_AppliesDefaults(t *testing.T) {
	mail := config.MailSettings{} // 全空值
	recipients := []string{"a@a.com"}

	cfg := mailutil.ResolveSMTPConfig(mail, recipients)

	if cfg.Host == "" {
		t.Error("Host 為空時應套用預設值")
	}
	if cfg.Port == 0 {
		t.Error("Port 為 0 時應套用預設值")
	}
	if cfg.Username == "" {
		t.Error("Username 為空時應套用預設值")
	}
	if cfg.From == "" {
		t.Error("From 為空時應套用預設值（fallback to user）")
	}
	if len(cfg.To) != 1 || cfg.To[0] != "a@a.com" {
		t.Errorf("To 應與 recipients 相同，實際：%v", cfg.To)
	}
}

func TestResolveSMTPConfig_UsesSettingsValues(t *testing.T) {
	mail := config.MailSettings{
		SMTPHost:        "smtp.custom.com",
		SMTPPort:        465,
		SMTPUser:        "user@custom.com",
		SMTPPass:        "secret",
		SMTPFrom:        "from@custom.com",
		SMTPDialTimeout: 10,
	}
	recipients := []string{"r1@r.com", "r2@r.com"}

	cfg := mailutil.ResolveSMTPConfig(mail, recipients)

	if cfg.Host != "smtp.custom.com" {
		t.Errorf("Host 期望 smtp.custom.com，實際：%q", cfg.Host)
	}
	if cfg.Port != 465 {
		t.Errorf("Port 期望 465，實際：%d", cfg.Port)
	}
	if cfg.Username != "user@custom.com" {
		t.Errorf("Username 期望 user@custom.com，實際：%q", cfg.Username)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password 期望 secret，實際：%q", cfg.Password)
	}
	if cfg.From != "from@custom.com" {
		t.Errorf("From 期望 from@custom.com，實際：%q", cfg.From)
	}
	if cfg.DialTimeout != 10*time.Second {
		t.Errorf("DialTimeout 期望 10s，實際：%v", cfg.DialTimeout)
	}
	if len(cfg.To) != 2 {
		t.Errorf("To 應有 2 位，實際：%v", cfg.To)
	}
}

func TestResolveSMTPConfig_FromFallsBackToUser(t *testing.T) {
	mail := config.MailSettings{
		SMTPUser: "user@example.com",
		SMTPFrom: "", // 未設定
	}
	cfg := mailutil.ResolveSMTPConfig(mail, nil)
	if cfg.From != "user@example.com" {
		t.Errorf("From 未設定時應 fallback 到 user，期望 user@example.com，實際：%q", cfg.From)
	}
}

// 確保 mailer 套件被引用（避免 unused import 編譯錯誤）
var _ mailer.SMTPConfig
