//go:build windows

package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKey(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "key.bin")

	key1, err := LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	key2, err := LoadOrCreateKey(keyPath, 32)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Fatalf("keys differ")
	}

	stat, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatalf("key file empty")
	}
}
