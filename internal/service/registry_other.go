//go:build !windows

package service

// FindServiceForRoot 在非 Windows 平台上始終回傳空字串（功能不支援）。
func FindServiceForRoot(_ string) (string, error) {
	return "", nil
}
