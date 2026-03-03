package humanize

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/journal"
)

// Input is a lightweight view of an event to format.
type Input struct {
	TS    time.Time
	Op    string
	Path  string
	IsDir bool
	Size  int64
}

// Options controls how human-readable strings are generated.
type Options struct {
	Root       string // optional root to show relative paths
	TimeFormat string // defaults to 2006-01-02 15:04:05
	ShowSize   bool   // include size for files when >0
	ShowOp     bool   // append the raw op code for debugging
	HideTime   bool   // hide timestamp prefix (default false)
}

// Format returns a Traditional Chinese, human-readable sentence for an event.
func Format(in Input, opt Options) string {
	tf := opt.TimeFormat
	if tf == "" {
		tf = "2006-01-02 15:04:05.000"
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	tsText := in.TS.In(loc).Format(tf)
	if opt.HideTime {
		tsText = ""
	}

	verb := describeOp(in.Op, in.IsDir)
	pathText := displayPath(in.Path, opt.Root)

	sizeText := ""
	if opt.ShowSize && !in.IsDir && in.Size > 0 {
		sizeText = fmt.Sprintf("，大小 %s", humanSize(in.Size))
	}

	opRaw := ""
	if opt.ShowOp && strings.TrimSpace(in.Op) != "" {
		opRaw = fmt.Sprintf("（%s）", strings.ToUpper(strings.TrimSpace(in.Op)))
	}
	if tsText != "" {
		return fmt.Sprintf("%s %s%s：%s%s", tsText, verb, opRaw, pathText, sizeText)
	}
	return fmt.Sprintf("%s%s：%s%s", verb, opRaw, pathText, sizeText)
}

// FormatJournalEntry is a convenience wrapper for journal entries.
func FormatJournalEntry(e journal.Entry, opt Options) string {
	return Format(Input{TS: e.TS, Op: e.Op, Path: e.Path, IsDir: e.IsDir, Size: e.Size}, opt)
}

func describeOp(op string, isDir bool) string {
	upper := strings.ToUpper(strings.TrimSpace(op))
	switch {
	case strings.Contains(upper, "CREATE"):
		if isDir {
			return "新增資料夾"
		}
		return "新增檔案"
	case strings.Contains(upper, "WRITE") || strings.Contains(upper, "MODIFY"):
		return "寫入/變更"
	case strings.Contains(upper, "REMOVE") || strings.Contains(upper, "DELETE"):
		if isDir {
			return "刪除資料夾"
		}
		return "刪除檔案"
	case strings.Contains(upper, "RENAME") || strings.Contains(upper, "MOVE"):
		return "重新命名/移動"
	case strings.Contains(upper, "CHMOD") || strings.Contains(upper, "ATTRIB"):
		return "調整權限/屬性"
	default:
		return "檔案事件"
	}
}

func displayPath(path string, root string) string {
	clean := filepath.Clean(path)
	if root == "" {
		return clean
	}
	rel, err := filepath.Rel(filepath.Clean(root), clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return clean
	}
	return rel
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for ; n/div >= unit && exp < 4; exp++ {
		div *= unit
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", value, suffix[exp])
}
