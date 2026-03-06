package service

import "testing"

func TestParseExeFromBinaryPath_QuotedWithArgs(t *testing.T) {
	input := `"C:\path\xwatch.exe" --service --name GoXWatch-A`
	want := `C:\path\xwatch.exe`
	got := parseExeFromBinaryPath(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseExeFromBinaryPath_UnquotedWithArgs(t *testing.T) {
	input := `C:\path\xwatch.exe --service --name GoXWatch-A`
	want := `C:\path\xwatch.exe`
	got := parseExeFromBinaryPath(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseExeFromBinaryPath_QuotedPathWithSpaces(t *testing.T) {
	input := `"C:\my path\xwatch.exe" --service`
	want := `C:\my path\xwatch.exe`
	got := parseExeFromBinaryPath(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseExeFromBinaryPath_QuotedNoArgs(t *testing.T) {
	input := `"C:\xwatch.exe"`
	want := `C:\xwatch.exe`
	got := parseExeFromBinaryPath(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseExeFromBinaryPath_UnquotedNoArgs(t *testing.T) {
	input := `C:\xwatch.exe`
	want := `C:\xwatch.exe`
	got := parseExeFromBinaryPath(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseExeFromBinaryPath_EmptyString(t *testing.T) {
	got := parseExeFromBinaryPath("")
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestParseExeFromBinaryPath_OnlyWhitespace(t *testing.T) {
	got := parseExeFromBinaryPath("   ")
	if got != "" {
		t.Errorf("whitespace-only input should return empty, got %q", got)
	}
}
