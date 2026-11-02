//go:build !windows

package paths

import (
	"fmt"
	"os"
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
