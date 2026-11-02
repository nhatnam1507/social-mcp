//go:build windows

package paths

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// checkHome verifies the directory exists and restricts it to the current user with a
// protected DACL, so a home outside the user profile is not readable by other users.
func checkHome(home string) (string, error) {
	fi, err := os.Stat(home)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("home %s is not a directory", home)
	}
	if err := restrictToCurrentUser(home); err != nil {
		return "", fmt.Errorf("restrict access to %s: %w", home, err)
	}
	return "", nil
}

func restrictToCurrentUser(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// The system temp dir is already per-user on Windows.
func privateDirName() string { return "social-mcp" }

// CheckOwned is a no-op on Windows; the socket lives in a per-user, DACL-protected directory.
func CheckOwned(string) error { return nil }
