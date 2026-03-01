//go:build windows

package crypto

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect uses Windows DPAPI to protect data (machine scope).
func Protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	var out windows.DataBlob
	if err := windows.CryptProtectData(bytesToBlob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_LOCAL_MACHINE, &out); err != nil {
		return nil, err
	}
	return blobToBytes(&out), nil
}

// Unprotect reverses Protect.
func Unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(bytesToBlob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_LOCAL_MACHINE, &out); err != nil {
		return nil, err
	}
	return blobToBytes(&out), nil
}

// LoadOrCreateKey loads a DPAPI-protected key from file or creates a new random key of keyLen bytes.
func LoadOrCreateKey(path string, keyLen int) ([]byte, error) {
	if keyLen <= 0 {
		return nil, errors.New("invalid key length")
	}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return Unprotect(data)
	}

	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	sealed, err := Protect(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func bytesToBlob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func blobToBytes(b *windows.DataBlob) []byte {
	if b == nil || b.Data == nil || b.Size == 0 {
		return nil
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data)))
	slice := unsafe.Slice((*byte)(unsafe.Pointer(b.Data)), b.Size)
	out := make([]byte, len(slice))
	copy(out, slice)
	return out
}
