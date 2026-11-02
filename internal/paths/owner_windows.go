//go:build windows

package paths

import (
	"fmt"
	"os"
)

// checkHome only verifies the directory exists; Windows relies on the per-user profile ACL.
func checkHome(home string) (string, error) {
	fi, err := os.Stat(home)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("home %s is not a directory", home)
	}
	return "", nil
}
