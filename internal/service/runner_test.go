package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go-xwatch/internal/config"
	"go-xwatch/internal/journal"
	"go-xwatch/internal/pipeline"
	"go-xwatch/internal/watcher"

	"github.com/fsnotify/fsnotify"
)

func TestRunnerFlushesAggregatedEvents(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	received := make([]string, 0)

	sink := pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
		return nil
	})

	sink2 := pipeline.EventSinkFunc(func(ctx context.Context, entries []journal.Entry) error {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range entries {
			received = append(received, e.Op+"|"+e.Path)
		}
		return nil
	})

	watchCalled := 0
	watchFn := func(ctx context.Context, root string, logger *slog.Logger, onEvent func(watcher.Event)) error {
		watchCalled++
		p := filepath.Join(root, "a.txt")
		onEvent(watcher.Event{Path: p, Op: fsnotify.Create, TS: time.Unix(0, 0)})
		onEvent(watcher.Event{Path: p, Op: fsnotify.Write, TS: time.Unix(1, 0)})
		return nil
	}

	r := &Runner{
		Settings:  config.Settings{RootDir: tmp},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: watchFn,
		Sinks:     []pipeline.EventSink{sink, sink2},
		Now:       func() time.Time { return time.Unix(2, 0) },
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if watchCalled != 1 {
		t.Fatalf("expected watcher called once, got %d", watchCalled)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(received))
	}
	if received[0] != "WRITE|"+filepath.Join(tmp, "a.txt") {
		t.Fatalf("unexpected entry: %v", received[0])
	}
}

func TestRunnerReturnsErrorOnEmptyRoot(t *testing.T) {
	r := &Runner{Settings: config.Settings{RootDir: ""}, Logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))}
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("expected error for empty root dir")
	}
}

// TestRunnerHeartbeat_LogDirFnCalled 確認當 HeartbeatEnabled=true 時
// HeartbeatLogDirFn 被呼叫。
func TestRunnerHeartbeat_LogDirFnCalled(t *testing.T) {
	tmp := t.TempDir()
	called := false

	r := &Runner{
		Settings: config.Settings{
			RootDir:           tmp,
			HeartbeatEnabled:  true,
			HeartbeatInterval: 60,
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		// 等待 30ms，讓心跳 goroutine 完成初始化後再結束
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(30 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			called = true
			return filepath.Join(tmp, "hb-logs"), nil
		},
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if !called {
		t.Fatal("expected HeartbeatLogDirFn to be called when HeartbeatEnabled=true")
	}
}

// TestRunnerHeartbeat_WritesLogFiles 確認 HeartbeatEnabled=true 時
// runner 不報錯且 HeartbeatLogDirFn 被呼叫（使用預設 60s 間隔，watcher 50ms 後結束）。
func TestRunnerHeartbeat_WritesLogFiles(t *testing.T) {
	tmp := t.TempDir()
	hbLogDir := filepath.Join(tmp, "hb-logs")
	called := false

	r := &Runner{
		Settings: config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: true,
			// HeartbeatInterval=0 → 預設 60s，watcher 50ms 後結束，ticker 不會觸發
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			called = true
			return hbLogDir, nil
		},
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if !called {
		t.Fatal("expected HeartbeatLogDirFn to be called when HeartbeatEnabled=true")
	}
}

// TestRunnerHeartbeat_DisabledNoLogDir 確認 HeartbeatEnabled=false 時
// HeartbeatLogDirFn 不被呼叫。
func TestRunnerHeartbeat_DisabledNoLogDir(t *testing.T) {
	tmp := t.TempDir()
	called := false

	r := &Runner{
		Settings: config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: false,
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			called = true
			return filepath.Join(tmp, "hb-logs"), nil
		},
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if called {
		t.Fatal("HeartbeatLogDirFn should NOT be called when HeartbeatEnabled=false")
	}
}

// TestRunnerHeartbeat_LogDirFnErrorContinues 確認 HeartbeatLogDirFn 錯誤時
// runner 仍能正常完成（心跳停用、服務不中斷）。
func TestRunnerHeartbeat_LogDirFnErrorContinues(t *testing.T) {
	tmp := t.TempDir()

	r := &Runner{
		Settings: config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: true,
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			return "", fmt.Errorf("mock log dir error")
		},
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner should continue even if HeartbeatLogDirFn fails, got: %v", err)
	}
}

// TestRunnerHeartbeat_ActualTickWritesFile 使用短間隔驗證 heartbeat
// 在服務執行期間確實寫入 log 檔（透過 HeartbeatLogDirFn 回傳的暫存目錄）。
// 此測試模擬 HeartbeatInterval=1s + watcher 等待 1.5s 讓至少一次 tick 發生。
func TestRunnerHeartbeat_ActualTickWritesFile(t *testing.T) {
	if testing.Short() {
		t.Skip("跳過耗時整合測試（-short）")
	}
	tmp := t.TempDir()
	hbLogDir := filepath.Join(tmp, "hb-logs")

	r := &Runner{
		Settings: config.Settings{
			RootDir:           tmp,
			HeartbeatEnabled:  true,
			HeartbeatInterval: 1, // 1 秒
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(1500 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			return hbLogDir, nil
		},
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	// 應至少有一個日期 log 檔
	entries, err := os.ReadDir(hbLogDir)
	if err != nil {
		t.Fatalf("heartbeat log 目錄不存在: %v", err)
	}
	var logFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "heartbeat_") && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, e.Name())
		}
	}
	if len(logFiles) == 0 {
		t.Fatal("預期至少有一個心跳 log 檔，但目錄為空")
	}
}

// TestRunnerHeartbeat_HotReloadEnablesHeartbeat 確認：
// 服務啟動時 HeartbeatEnabled=false，在下一個 reload 週期 config 改為 true，
// 心跳 log 目錄函式應被自動呼叫（不需重啟服務）。
func TestRunnerHeartbeat_HotReloadEnablesHeartbeat(t *testing.T) {
	tmp := t.TempDir()
	hbLogDir := filepath.Join(tmp, "hb-hot-logs")

	var mu sync.Mutex
	logDirCalled := false
	reloadCallCount := 0

	// ConfigLoadFn：第一次仍 disabled；第二次起改為 enabled
	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		enabled := reloadCallCount > 1
		return config.Settings{
			RootDir:           tmp,
			HeartbeatEnabled:  enabled,
			HeartbeatInterval: 60,
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: false, // 初始停用
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		// watcher 等待足夠讓熱重載 ticker 觸發兩次（2 × 20ms + buffer）
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			mu.Lock()
			logDirCalled = true
			mu.Unlock()
			return hbLogDir, nil
		},
		ConfigLoadFn:            cfgFn,
		HeartbeatReloadInterval: 20 * time.Millisecond, // 極短週期，方便測試
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !logDirCalled {
		t.Fatal("hot-reload: HeartbeatLogDirFn 應在設定從 disabled 改為 enabled 後被呼叫")
	}
}

// TestRunnerHeartbeat_HotReloadDisablesHeartbeat 確認：
// 服務啟動時 HeartbeatEnabled=true，在下一個 reload 週期 config 改為 false，
// 心跳應停止（stopHB 不 panic，runner 正常結束）。
func TestRunnerHeartbeat_HotReloadDisablesHeartbeat(t *testing.T) {
	tmp := t.TempDir()
	hbLogDir := filepath.Join(tmp, "hb-stop-logs")

	var mu sync.Mutex
	reloadCallCount := 0

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		enabled := reloadCallCount <= 1 // 第一次 reload 後改為 disabled
		return config.Settings{
			RootDir:           tmp,
			HeartbeatEnabled:  enabled,
			HeartbeatInterval: 60,
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir:           tmp,
			HeartbeatEnabled:  true, // 初始啟用
			HeartbeatInterval: 60,
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			return hbLogDir, nil
		},
		ConfigLoadFn:            cfgFn,
		HeartbeatReloadInterval: 20 * time.Millisecond,
	}

	// 主要驗證：runner 正常結束，stopHB 不會 panic
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner should not error when heartbeat is hot-reload disabled: %v", err)
	}
}

// TestServiceAccount_UnknownService 確認查詢不存在的服務時回傳 error。
// 不需要 Admin 權限（無法連接 SCM 也是 error，同樣符合預期）。
func TestServiceAccount_UnknownService(t *testing.T) {
	_, err := ServiceAccount("go-xwatch-nonexistent-svc-test-99999")
	if err == nil {
		t.Fatal("expected error for non-existent service; got nil")
	}
}

// ── Mail 熱重載測試 ────────────────────────────────────────────────

// TestRunnerMail_InitiallyEnabled 確認服務啟動時 mail.Enabled=true，
// MailSchedulerFn 立即被呼叫一次。
func TestRunnerMail_InitiallyEnabled(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	callCount := 0

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: "00:00",
				To:       []string{"test@example.com"},
			},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			callCount++
			mu.Unlock()
			<-ctx.Done()
		},
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount == 0 {
		t.Fatal("MailSchedulerFn 應在 mail.Enabled=true 時被立即呼叫")
	}
}

// TestRunnerMail_InitiallyDisabled 確認服務啟動時 mail.Enabled=false，
// MailSchedulerFn 不被呼叫。
func TestRunnerMail_InitiallyDisabled(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	callCount := 0

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail:    config.MailSettings{Enabled: config.BoolPtr(false)},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			callCount++
			mu.Unlock()
			<-ctx.Done()
		},
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount != 0 {
		t.Fatalf("MailSchedulerFn 不應在 mail.Enabled=false 時被呼叫，但被呼叫了 %d 次", callCount)
	}
}

// TestRunnerMail_HotReloadEnablesMailScheduler 確認：
// 服務啟動時 mail.Enabled=false，在下一個 reload 週期 config 改為 true，
// MailSchedulerFn 應被自動呼叫（不需重啟服務）。
func TestRunnerMail_HotReloadEnablesMailScheduler(t *testing.T) {
	tmp := t.TempDir()

	var mu sync.Mutex
	schedulerCalled := false
	reloadCallCount := 0

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		enabled := reloadCallCount > 1 // 第一次 reload 後改為 enabled
		return config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(enabled),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
			},
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail:    config.MailSettings{Enabled: config.BoolPtr(false)}, // 初始停用
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			schedulerCalled = true
			mu.Unlock()
			<-ctx.Done()
		},
		ConfigLoadFn:       cfgFn,
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !schedulerCalled {
		t.Fatal("hot-reload：MailSchedulerFn 應在設定從 disabled 改為 enabled 後被呼叫")
	}
}

// TestRunnerMail_HotReloadDisablesMailScheduler 確認：
// 服務啟動時 mail.Enabled=true，在下一個 reload 週期 config 改為 false，
// 排程應停止（mailCancel 被呼叫，runner 正常結束不 panic）。
func TestRunnerMail_HotReloadDisablesMailScheduler(t *testing.T) {
	tmp := t.TempDir()

	var mu sync.Mutex
	reloadCallCount := 0
	cancelCalled := false

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		enabled := reloadCallCount <= 1 // 第一次 reload 後改為 disabled
		return config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(enabled),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
			},
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
			},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			<-ctx.Done()
			mu.Lock()
			cancelCalled = true
			mu.Unlock()
		},
		ConfigLoadFn:       cfgFn,
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner should not error when mail scheduler is hot-reload disabled: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !cancelCalled {
		t.Fatal("hot-reload：mail 排程應在設定從 enabled 改為 disabled 後，其 context 被取消")
	}
}

// TestRunnerMail_HotReloadChangesSchedule 確認排程時間改變時，
// 舊排程被取消並以新設定重啟。
func TestRunnerMail_HotReloadChangesSchedule(t *testing.T) {
	tmp := t.TempDir()

	var mu sync.Mutex
	callCount := 0
	reloadCallCount := 0
	var schedules []string

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		schedule := "10:00"
		if reloadCallCount > 1 {
			schedule = "20:00"
		}
		return config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: schedule,
				To:       []string{"test@example.com"},
			},
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: "10:00",
				To:       []string{"test@example.com"},
			},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, mail config.MailSettings, _ func() time.Time) {
			mu.Lock()
			callCount++
			schedules = append(schedules, mail.Schedule)
			mu.Unlock()
			<-ctx.Done()
		},
		ConfigLoadFn:       cfgFn,
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount < 2 {
		t.Fatalf("排程時間改變時應重啟排程，MailSchedulerFn 至少呼叫 2 次，實際 %d 次", callCount)
	}
	found20 := false
	for _, s := range schedules {
		if s == "20:00" {
			found20 = true
			break
		}
	}
	if !found20 {
		t.Fatalf("應有一次以 schedule=20:00 呼叫 MailSchedulerFn，實際 schedules=%v", schedules)
	}
}

// TestRunnerMail_HotReloadDetectsSmtpChanges 確認 SMTP 設定（如 SMTPHost）改變時，
// mailSchedulerKey 差異被偵測，排程器以新 SMTP 重啟（不需人工重啟服務）。
func TestRunnerMail_HotReloadDetectsSmtpChanges(t *testing.T) {
	tmp := t.TempDir()

	var mu sync.Mutex
	callCount := 0
	reloadCallCount := 0
	var smtpHosts []string

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		defer mu.Unlock()
		reloadCallCount++
		host := "smtp1.test.local"
		if reloadCallCount > 1 {
			host = "smtp2.test.local"
		}
		return config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
				SMTPHost: host,
				SMTPPort: 587,
				SMTPUser: "user@test.local",
				SMTPPass: "pass",
			},
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(true),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
				SMTPHost: "smtp1.test.local",
				SMTPPort: 587,
				SMTPUser: "user@test.local",
				SMTPPass: "pass",
			},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, mail config.MailSettings, _ func() time.Time) {
			mu.Lock()
			callCount++
			smtpHosts = append(smtpHosts, mail.SMTPHost)
			mu.Unlock()
			<-ctx.Done()
		},
		ConfigLoadFn:       cfgFn,
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount < 2 {
		t.Fatalf("SMTP 設定改變時應重啟排程，MailSchedulerFn 至少呼叫 2 次，實際 %d 次", callCount)
	}
	foundSmtp2 := false
	for _, h := range smtpHosts {
		if h == "smtp2.test.local" {
			foundSmtp2 = true
			break
		}
	}
	if !foundSmtp2 {
		t.Fatalf("應有一次以 SMTPHost=smtp2.test.local 呼叫 MailSchedulerFn，實際 smtpHosts=%v", smtpHosts)
	}
}

// TestRunnerMail_NilEnabledNotStarted 確認 mail.Enabled=nil（從未設定）時，
// 服務啟動不自動啟動 mail scheduler，需明確執行 mail enable 才能啟用。
func TestRunnerMail_NilEnabledNotStarted(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	callCount := 0

	r := &Runner{
		Settings: config.Settings{
			RootDir: tmp,
			Mail:    config.MailSettings{}, // Enabled = nil（從未設定）
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			callCount++
			mu.Unlock()
			<-ctx.Done()
		},
		MailReloadInterval: 20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount != 0 {
		t.Fatalf("mail.Enabled=nil 時 MailSchedulerFn 不應被呼叫（需明確 mail enable），但被呼叫了 %d 次", callCount)
	}
}

// TestRunnerFreshInitSettings_NoAutoStart 模擬 init --install-service 後
// 服務啟動時的初始 config（HeartbeatEnabled=false, Mail.Enabled=nil），
// 確認心跳與郵件排程都不自動啟動。防迴歸問題1&2。
func TestRunnerFreshInitSettings_NoAutoStart(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	hbLogDirCalled := false
	mailSchedulerCalled := false

	// 模擬 init --install-service 後儲存的最小 config
	freshInitSettings := config.Settings{
		RootDir:           tmp,
		HeartbeatEnabled:  false,                 // init 預設不開啟心跳
		HeartbeatInterval: 60,                    // ValidateAndFillDefaults 填入
		Mail:              config.MailSettings{}, // Enabled=nil，需明確 mail enable
	}

	r := &Runner{
		Settings:  freshInitSettings,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			return nil // 立即結束
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			mu.Lock()
			hbLogDirCalled = true
			mu.Unlock()
			return filepath.Join(tmp, "hb-logs"), nil
		},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			mailSchedulerCalled = true
			mu.Unlock()
			<-ctx.Done()
		},
		HeartbeatReloadInterval: 20 * time.Millisecond,
		MailReloadInterval:      20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hbLogDirCalled {
		t.Error("init --install-service 後 HeartbeatEnabled=false，HeartbeatLogDirFn 不應被呼叫（心跳不應啟動）")
	}
	if mailSchedulerCalled {
		t.Error("init --install-service 後 Mail.Enabled=nil，MailSchedulerFn 不應被呼叫（需明確 mail enable）")
	}
}

// TestRunnerHeartbeat_MailEnableDoesNotTriggerHeartbeat 確認：
// 服務啟動時 HeartbeatEnabled=false、Mail.Enabled=false，
// 當 mail 熱重載啟用（hot-reload Mail.Enabled=true）時，
// 心跳仍不啟動（mail 與 heartbeat 完全獨立）。防迴歸問題2。
func TestRunnerHeartbeat_MailEnableDoesNotTriggerHeartbeat(t *testing.T) {
	tmp := t.TempDir()
	var mu sync.Mutex
	hbLogDirCalled := false
	mailSchedulerCalled := false
	reloadCount := 0

	cfgFn := func() (config.Settings, error) {
		mu.Lock()
		reloadCount++
		enabled := reloadCount > 1 // 第一次 reload 後 mail 啟用
		mu.Unlock()
		return config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: false, // 心跳始終停用
			Mail: config.MailSettings{
				Enabled:  config.BoolPtr(enabled),
				Schedule: "23:59",
				To:       []string{"test@example.com"},
			},
		}, nil
	}

	r := &Runner{
		Settings: config.Settings{
			RootDir:          tmp,
			HeartbeatEnabled: false,
			Mail:             config.MailSettings{Enabled: config.BoolPtr(false)},
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		DataDirFn: func() (string, error) { return tmp, nil },
		WatcherFn: func(ctx context.Context, root string, _ *slog.Logger, _ func(watcher.Event)) error {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
			}
			return nil
		},
		Sinks: []pipeline.EventSink{pipeline.EventSinkFunc(func(_ context.Context, _ []journal.Entry) error { return nil })},
		HeartbeatLogDirFn: func() (string, error) {
			mu.Lock()
			hbLogDirCalled = true
			mu.Unlock()
			return filepath.Join(tmp, "hb-logs"), nil
		},
		MailSchedulerFn: func(ctx context.Context, _ *slog.Logger, _ config.MailSettings, _ func() time.Time) {
			mu.Lock()
			mailSchedulerCalled = true
			mu.Unlock()
			<-ctx.Done()
		},
		ConfigLoadFn:            cfgFn,
		HeartbeatReloadInterval: 20 * time.Millisecond,
		MailReloadInterval:      20 * time.Millisecond,
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hbLogDirCalled {
		t.Error("mail enable 熱重載不應觸發 heartbeat（HeartbeatEnabled 始終為 false），但 HeartbeatLogDirFn 被呼叫")
	}
	if !mailSchedulerCalled {
		t.Error("mail enable 熱重載應啟動 MailSchedulerFn，但未被呼叫")
	}
}
