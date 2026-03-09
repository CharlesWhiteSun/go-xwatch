package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type channelHandler struct {
	ch chan string
}

func (h *channelHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *channelHandler) Handle(_ context.Context, rec slog.Record) error {
	var path string
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "path", "路徑":
			path = fmt.Sprint(a.Value)
		}
		return true
	})
	if path != "" {
		select {
		case h.ch <- path:
		default:
		}
	}
	return nil
}

func (h *channelHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *channelHandler) WithGroup(_ string) slog.Handler      { return h }

type msgHandler struct {
	ch chan string
}

func (h *msgHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *msgHandler) Handle(_ context.Context, rec slog.Record) error {
	select {
	case h.ch <- rec.Message:
	default:
	}
	return nil
}
func (h *msgHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *msgHandler) WithGroup(_ string) slog.Handler      { return h }

func TestWatcherDetectsFileCreate(t *testing.T) {
	tmp := t.TempDir()
	ch := make(chan string, 10)
	logger := slog.New(&channelHandler{ch: ch})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, tmp, logger, nil) }()
	defer func() {
		cancel()
		_ = <-errCh
	}()

	// give watcher time to register initial directories
	time.Sleep(250 * time.Millisecond)

	path := filepath.Join(tmp, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	select {
	case p := <-ch:
		if filepath.Clean(p) != filepath.Clean(path) {
			t.Fatalf("unexpected path: %s", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for file create event")
	}
}

func TestWatcherAddsNewDirectories(t *testing.T) {
	tmp := t.TempDir()
	ch := make(chan string, 10)
	logger := slog.New(&channelHandler{ch: ch})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, tmp, logger, nil) }()
	defer func() {
		cancel()
		_ = <-errCh
	}()

	time.Sleep(250 * time.Millisecond)

	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	fileInSub := filepath.Join(sub, "b.txt")
	if err := os.WriteFile(fileInSub, []byte("world"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	deadline := time.After(7 * time.Second)
	for {
		select {
		case p := <-ch:
			if filepath.Clean(p) == filepath.Clean(fileInSub) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for event in new dir")
		}
	}
}

func TestShouldIgnore(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/a/.git", true},
		{"/a/.git/config", true},
		{"/a/node_modules", true},
		{"/a/node_modules/pkg", true},
		{"/a/file.tmp", true},
		{"/a/file.swp", true},
		{"/a/file.txt", false},
	}

	for _, c := range cases {
		if got := shouldIgnore(c.path); got != c.want {
			t.Fatalf("shouldIgnore(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

func TestRunWithOptionsFormatterAndHook(t *testing.T) {
	tmp := t.TempDir()
	msgCh := make(chan string, 4)

	var hookPath string
	logger := slog.New(&msgHandler{ch: msgCh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger: logger,
			Formatter: func(_ string, ev Event) string {
				return "CUSTOM:" + filepath.Base(ev.Path)
			},
			OnEvent: func(ev Event) { hookPath = ev.Path },
		})
	}()

	time.Sleep(200 * time.Millisecond)
	path := filepath.Join(tmp, "c.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	want := "CUSTOM:c.txt"
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-msgCh:
			if msg == want {
				cancel()
				goto done
			}
		case <-deadline:
			cancel()
			t.Fatalf("timeout waiting for formatted message; last got %q", hookPath)
		}
	}
done:

	if hookPath == "" || filepath.Clean(hookPath) != filepath.Clean(path) {
		t.Fatalf("hook path not set, got %q", hookPath)
	}
	_ = <-errCh
}

// --- ShouldSkipFn 測試 ---

func TestShouldSkipFn_ExcludesTargetDir(t *testing.T) {
	tmp := t.TempDir()

	// 建立被排除的子目錄及其中的檔案
	excludedDir := filepath.Join(tmp, "storage")
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 建立允許的子目錄
	allowedDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	received := make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger: slog.Default(),
			OnEvent: func(e Event) {
				received <- e.Path
			},
			ShouldSkipFn: func(path string) bool {
				clean := strings.ToLower(filepath.ToSlash(path))
				excl := strings.ToLower(filepath.ToSlash(excludedDir))
				return clean == excl || strings.HasPrefix(clean, excl+"/")
			},
		})
	}()

	time.Sleep(200 * time.Millisecond)

	// 寫入被排除目錄中的檔案 — 不應收到事件
	os.WriteFile(filepath.Join(excludedDir, "secret.txt"), []byte("x"), 0o644)

	// 寫入允許目錄中的檔案 — 應收到事件
	allowedFile := filepath.Join(allowedDir, "ok.txt")
	os.WriteFile(allowedFile, []byte("hello"), 0o644)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-received:
			if got == filepath.Join(excludedDir, "secret.txt") {
				cancel()
				<-errCh
				t.Fatalf("received event for excluded path: %q", got)
			}
			if filepath.Clean(got) == filepath.Clean(allowedFile) {
				cancel()
				<-errCh
				return // success
			}
		case <-deadline:
			cancel()
			<-errCh
			t.Fatal("timeout: did not receive event for allowed file")
		}
	}
}

func TestShouldSkipFn_NilIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	received := make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger:       slog.Default(),
			OnEvent:      func(e Event) { received <- e.Path },
			ShouldSkipFn: nil, // nil should be safe
		})
	}()

	time.Sleep(200 * time.Millisecond)
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("x"), 0o644)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-received:
			if filepath.Clean(got) == filepath.Clean(path) {
				cancel()
				<-errCh
				return
			}
		case <-deadline:
			cancel()
			<-errCh
			t.Fatal("timeout: nil ShouldSkipFn should not block events")
		}
	}
}

// buildTestSkipFn 依 excludedDir 建立路徑比對的排除函式，與 runner.buildExcludeSkipFn 邏輯相同。
func buildTestSkipFn(excludedDir string) func(string) bool {
	excl := strings.ToLower(filepath.ToSlash(excludedDir))
	return func(path string) bool {
		clean := strings.ToLower(filepath.ToSlash(path))
		return clean == excl || strings.HasPrefix(clean, excl+"/")
	}
}

// TestShouldSkipFn_ExcludedDirNotExistAtStart
// 確認排除目錄「啟動時尚不存在」的場景：
// watcher 啟動後使用者才建立排除目錄，其內部的檔案事件不應被回報。
func TestShouldSkipFn_ExcludedDirNotExistAtStart(t *testing.T) {
	tmp := t.TempDir()
	excludedDir := filepath.Join(tmp, "storage")
	// 注意：exclusedDir 在 watcher 啟動時尚未建立

	received := make(chan string, 20)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger:       slog.Default(),
			OnEvent:      func(e Event) { received <- e.Path },
			ShouldSkipFn: buildTestSkipFn(excludedDir),
		})
	}()

	time.Sleep(200 * time.Millisecond)

	// 建立排除目錄（此 CREATE 事件本身應被略過）
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatalf("mkdir excluded dir failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 在排除目錄內寫入檔案 — 不應收到事件
	os.WriteFile(filepath.Join(excludedDir, "secret.txt"), []byte("x"), 0o644)
	time.Sleep(100 * time.Millisecond)

	// 建立允許目錄的檔案 — 應收到事件，用於確認 watcher 仍在運作
	allowedFile := filepath.Join(tmp, "ok.txt")
	os.WriteFile(allowedFile, []byte("hello"), 0o644)

	exclLower := strings.ToLower(filepath.ToSlash(excludedDir))
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-received:
			gotLower := strings.ToLower(filepath.ToSlash(got))
			if gotLower == exclLower || strings.HasPrefix(gotLower, exclLower+"/") {
				cancel()
				<-errCh
				t.Fatalf("不應收到排除目錄的事件（dir not exist at start）：%q", got)
			}
			if filepath.Clean(got) == filepath.Clean(allowedFile) {
				cancel()
				<-errCh
				return // success
			}
		case <-deadline:
			cancel()
			<-errCh
			t.Fatal("timeout: 未收到允許目錄的事件（watcher 可能未正常啟動）")
		}
	}
}

// TestShouldSkipFn_ExcludedDirDeletedAndRecreated
// 確認排除目錄「被刪除再重新建立」的場景：
// watcher 啟動後，排除目錄存在 → 被刪除 → 再次被建立，期間不應有其內部事件被回報。
func TestShouldSkipFn_ExcludedDirDeletedAndRecreated(t *testing.T) {
	tmp := t.TempDir()
	excludedDir := filepath.Join(tmp, "storage")
	// 啟動時已存在
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatalf("mkdir excluded dir failed: %v", err)
	}

	received := make(chan string, 20)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- RunWithOptions(ctx, tmp, Options{
			Logger:       slog.Default(),
			OnEvent:      func(e Event) { received <- e.Path },
			ShouldSkipFn: buildTestSkipFn(excludedDir),
		})
	}()

	time.Sleep(200 * time.Millisecond)

	// 刪除排除目錄
	if err := os.RemoveAll(excludedDir); err != nil {
		t.Fatalf("remove excluded dir failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 重新建立排除目錄
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatalf("re-mkdir excluded dir failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 在重建後的排除目錄內寫入檔案 — 不應收到事件
	os.WriteFile(filepath.Join(excludedDir, "after_recreate.txt"), []byte("x"), 0o644)
	time.Sleep(100 * time.Millisecond)

	// 建立允許目錄的檔案確認 watcher 正常
	allowedFile := filepath.Join(tmp, "ok2.txt")
	os.WriteFile(allowedFile, []byte("world"), 0o644)

	exclLower := strings.ToLower(filepath.ToSlash(excludedDir))
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-received:
			gotLower := strings.ToLower(filepath.ToSlash(got))
			if gotLower == exclLower || strings.HasPrefix(gotLower, exclLower+"/") {
				cancel()
				<-errCh
				t.Fatalf("不應收到排除目錄的事件（deleted and recreated）：%q", got)
			}
			if filepath.Clean(got) == filepath.Clean(allowedFile) {
				cancel()
				<-errCh
				return // success
			}
		case <-deadline:
			cancel()
			<-errCh
			t.Fatal("timeout: 未收到允許目錄的事件")
		}
	}
}

// ── addRecursive 錯誤處理測試 ─────────────────────────────────────────────

// addMock 實作 watchAdder 介面，記錄所有 Add 呼叫。
type addMock struct {
	added []string
}

func (m *addMock) Add(path string) error {
	m.added = append(m.added, filepath.ToSlash(strings.ToLower(path)))
	return nil
}

// TestAddRecursive_AddsAllDirs 驗證 addRecursive 在無錯誤時正確遍歷並 Add 所有目錄。
func TestAddRecursive_AddsAllDirs(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "a"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "b"), 0o755)

	mock := &addMock{}
	noSkip := func(string) bool { return false }
	if err := addRecursive(mock, tmp, noSkip); err != nil {
		t.Fatalf("addRecursive should not error: %v", err)
	}

	want := map[string]bool{
		filepath.ToSlash(strings.ToLower(tmp)):                     true,
		filepath.ToSlash(strings.ToLower(filepath.Join(tmp, "a"))): true,
		filepath.ToSlash(strings.ToLower(filepath.Join(tmp, "b"))): true,
	}
	got := make(map[string]bool, len(mock.added))
	for _, p := range mock.added {
		got[p] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("期望 Add(%q) 被呼叫，但未呼叫", k)
		}
	}
}

// TestAddRecursive_SkipsExcludedDir 驗證 skipFn 回傳 true 時整個目錄樹被略過。
func TestAddRecursive_SkipsExcludedDir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "storage", "sub"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "ok"), 0o755)

	skipStorage := func(path string) bool {
		clean := filepath.ToSlash(strings.ToLower(path))
		target := filepath.ToSlash(strings.ToLower(filepath.Join(tmp, "storage")))
		return clean == target || strings.HasPrefix(clean, target+"/")
	}
	mock := &addMock{}
	if err := addRecursive(mock, tmp, skipStorage); err != nil {
		t.Fatalf("addRecursive error: %v", err)
	}

	storageKey := filepath.ToSlash(strings.ToLower(filepath.Join(tmp, "storage")))
	for _, p := range mock.added {
		if p == storageKey || strings.HasPrefix(p, storageKey+"/") {
			t.Errorf("排除目錄不應被 Add：%q", p)
		}
	}
	okKey := filepath.ToSlash(strings.ToLower(filepath.Join(tmp, "ok")))
	found := false
	for _, p := range mock.added {
		if p == okKey {
			found = true
		}
	}
	if !found {
		t.Error("允許的目錄 ok 應被 Add")
	}
}

// TestAddRecursive_RootError 驗證根目錄不存在時回傳錯誤。
func TestAddRecursive_RootError(t *testing.T) {
	mock := &addMock{}
	noSkip := func(string) bool { return false }
	err := addRecursive(mock, "/nonexistent/path/that/does/not/exist", noSkip)
	if err == nil {
		t.Fatal("根目錄不存在時應回傳錯誤")
	}
}
