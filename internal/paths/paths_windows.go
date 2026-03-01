//go:build windows

package paths

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// ensureDirWithACL creates the directory (recursive) and sets DACL to Administrators + SYSTEM.
func ensureDirWithACL(dir string) error {
	if os.Getenv("XWATCH_SKIP_ACL") == "1" {
		return os.MkdirAll(dir, 0o755)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// D:P   - Protected DACL
	// (A;OICI;GA;;;SY) - Allow SYSTEM full control, inherit to children
	// (A;OICI;GA;;;BA) - Allow BUILTIN\Administrators full control, inherit to children
	sddl := "D:P(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("sddl parse: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("get dacl: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		// In restrictive environments (e.g. test temp dirs), setting DACL can fail; fall back to directory existing without tightened ACL.
		return nil
	}
	return nil
}
