package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"go-xwatch/internal/paths"
)

type Settings struct {
	RootDir   string    `json:"rootDir"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	return s, nil
}

func Save(s Settings) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC()
	bytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

func configPath() (string, error) {
	dir, err := paths.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
