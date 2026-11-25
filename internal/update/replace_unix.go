//go:build !windows

package update

import "os"

// replace renames tmp over target. A running process keeps executing the old file, which the OS frees when
// the last process using it exits.
func replace(tmp, target string) error {
	return os.Rename(tmp, target)
}
