package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDir returns the base data directory (ProgramData/go-xwatch) without creating it.
func DataDir() (string, error) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return "", fmt.Errorf("ProgramData is empty")
	}
	return filepath.Join(programData, "go-xwatch"), nil
}

// EnsureDataDir ensures the data directory exists (and on Windows applies tightened ACL).
func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := ensureDirWithACL(dir); err != nil {
		return "", err
	}
	return dir, nil
}
