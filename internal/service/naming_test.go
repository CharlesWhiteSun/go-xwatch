package service

import (
	"strings"
	"testing"
)

func TestServiceSuffixFromRoot(t *testing.T) {
	cases := []struct {
		name     string
		rootDir  string
		expected string
	}{
		{
			name:     "ASCII folder name",
			rootDir:  `D:\data\plant-A`,
			expected: "plant-A",
		},
		{
			name:     "underscore preserved",
			rootDir:  `C:\apps\my_service`,
			expected: "my_service",
		},
		{
			name:     "spaces replaced with hyphen and trimmed",
			rootDir:  `C:\apps\my service`,
			expected: "my-service",
		},
		{
			name:     "trailing slash ignored",
			rootDir:  `D:\data\plant-A\`,
			expected: "plant-A",
		},
		{
			name:     "dot becomes hyphen (not trimmed unless at boundary)",
			rootDir:  `C:\apps\v1.0`,
			expected: "v1-0",
		},
		{
			name:     "numbers preserved",
			rootDir:  `C:\apps\site2`,
			expected: "site2",
		},
		{
			name:     "short single char",
			rootDir:  `C:\a`,
			expected: "a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ServiceSuffixFromRoot(tc.rootDir)
			if got != tc.expected {
				t.Errorf("ServiceSuffixFromRoot(%q) = %q, want %q", tc.rootDir, got, tc.expected)
			}
		})
	}
}

func TestServiceNameFromRoot(t *testing.T) {
	cases := []struct {
		rootDir  string
		expected string
	}{
		{`D:\data\plant-A`, "GoXWatch-plant-A"},
		{`C:\apps\my_service`, "GoXWatch-my_service"},
		{`C:\apps\site2`, "GoXWatch-site2"},
	}
	for _, tc := range cases {
		got := ServiceNameFromRoot(tc.rootDir)
		if got != tc.expected {
			t.Errorf("ServiceNameFromRoot(%q) = %q, want %q", tc.rootDir, got, tc.expected)
		}
		if !strings.HasPrefix(got, ServicePrefix+"-") {
			t.Errorf("service name %q must start with %q", got, ServicePrefix+"-")
		}
	}
}

func TestSuffixFromServiceName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"with suffix", "GoXWatch-plant-A", "plant-A"},
		{"legacy no suffix", "GoXWatch", ""},
		{"unrelated name", "SomethingElse", ""},
		{"prefix only no suffix", "GoXWatch-", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SuffixFromServiceName(tc.input)
			if got != tc.expected {
				t.Errorf("SuffixFromServiceName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestBuildDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"legacy", "GoXWatch", "Go XWatch Service"},
		{"with suffix", "GoXWatch-plant-A", "Go XWatch Service (plant-A)"},
		{"with underscore suffix", "GoXWatch-my_service", "Go XWatch Service (my_service)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDisplayName(tc.input)
			if got != tc.expected {
				t.Errorf("buildDisplayName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestServiceSuffixNonEmpty(t *testing.T) {
	// ServiceSuffixFromRoot 永遠不應回傳空字串（退化為 "default"）
	suffix := ServiceSuffixFromRoot("")
	if suffix == "" {
		t.Error("ServiceSuffixFromRoot(\"\") must not return empty string")
	}
}
