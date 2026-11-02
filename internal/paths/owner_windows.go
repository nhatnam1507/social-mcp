//go:build windows

package paths

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// checkHome verifies the directory exists and restricts it to the current user with a
// protected DACL, so a home outside the user profile is not readable by other users. It only
// touches a directory dedicated to social-mcp, never one holding unrelated files.
func checkHome(home string) (string, error) {
	fi, err := os.Stat(home)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("home %s is not a directory", home)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !ownedName(e.Name()) {
			return fmt.Sprintf("%s contains files social-mcp did not create (%s); its permissions were left unchanged. Use a dedicated directory for SOCIAL_MCP_HOME.", home, e.Name()), nil
		}
	}
	if err := restrictToCurrentUser(home); err != nil {
		return fmt.Sprintf("could not restrict %s to the current user (%v); other local users may be able to read it", home, err), nil
	}
	return "", nil
}

// ownedName reports whether a home entry is one social-mcp creates.
func ownedName(name string) bool {
	for _, prefix := range []string{"config.toml", "social-mcp", "master.key", "mcp-token", ".mcp-token", "daemon.", "backups", "journals"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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

// privateDirName includes the user's SID, so accounts sharing a temp dir (services, SYSTEM)
// never share a socket directory.
func privateDirName() string {
	if user, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		return "social-mcp-" + user.User.Sid.String()
	}
	return "social-mcp-" + os.Getenv("USERNAME")
}

// CheckOwned is a no-op on Windows; the socket lives in a per-user, DACL-protected directory.
func CheckOwned(string) error { return nil }
