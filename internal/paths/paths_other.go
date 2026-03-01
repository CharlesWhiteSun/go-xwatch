//go:build !windows

package paths

import "os"

func ensureDirWithACL(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
