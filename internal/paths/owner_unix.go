//go:build !windows

package paths

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

func checkHome(home string) (string, error) {
	fi, err := os.Lstat(home)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("home %s is a symlink; point %s at the real directory", home, EnvHome)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("home %s is not a directory", home)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return "", fmt.Errorf("home %s is owned by uid %d, not the current user", home, st.Uid)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(home, 0o700); err != nil {
			return "", fmt.Errorf("tighten permissions on %s: %w", home, err)
		}
		return fmt.Sprintf("tightened permissions on %s from %v to 0700", home, fi.Mode().Perm()), nil
	}
	return "", nil
}

func privateDirName() string { return "social-mcp-" + strconv.Itoa(os.Getuid()) }

// CheckOwned verifies that path, if it exists, is not a symlink and belongs to the current
// user, so the CLI never talks to a socket planted by someone else.
func CheckOwned(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to use it", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d, not the current user; refusing to use it", path, st.Uid)
	}
	return nil
}
