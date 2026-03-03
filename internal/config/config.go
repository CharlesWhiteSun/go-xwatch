package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-xwatch/internal/paths"
)

type Settings struct {
	RootDir         string    `json:"rootDir"`
	DailyCSVEnabled bool      `json:"dailyCsvEnabled"`
	DailyCSVDir     string    `json:"dailyCsvDir"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func Load() (Settings, error) {
	path, err := configPath()
	if err != nil {
		return Settings{}, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(bytes, &s); err != nil {
		return Settings{}, err
	}
	return ValidateAndFillDefaults(s)
}

func Save(s Settings) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	validated, err := ValidateAndFillDefaults(s)
	if err != nil {
		return err
	}
	validated.UpdatedAt = time.Now().UTC()
	bytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

// ValidateAndFillDefaults trims/normalizes settings and returns filled defaults.
func ValidateAndFillDefaults(s Settings) (Settings, error) {
	trimmedRoot := strings.TrimSpace(s.RootDir)
	if trimmedRoot == "" {
		return s, errors.New("rootDir is required")
	}
	absRoot, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return s, err
	}
	s.RootDir = absRoot

	if s.DailyCSVEnabled && strings.TrimSpace(s.DailyCSVDir) == "" {
		s.DailyCSVDir = "daily"
	}

	return s, nil
}

func configPath() (string, error) {
	dir, err := paths.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
