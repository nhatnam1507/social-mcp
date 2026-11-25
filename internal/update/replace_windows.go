//go:build windows

package update

import "os"

// replace moves the running target aside and puts tmp in its place, restoring target if the second step
// fails. Windows allows renaming, but not deleting or overwriting, an executable that is running.
func replace(tmp, target string) error {
	old := target + ".old"
	os.Remove(old) // a leftover from an earlier update; still in use only if a process from it runs
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		if rerr := os.Rename(old, target); rerr != nil {
			return &restoreError{err: err, restore: rerr, old: old}
		}
		return err
	}
	os.Remove(old) // fails while the old binary is still running; CleanupOld retries later
	return nil
}

type restoreError struct {
	err, restore error
	old          string
}

func (e *restoreError) Error() string {
	return e.err.Error() + "; the previous binary is at " + e.old + " (restoring it failed: " + e.restore.Error() + ")"
}

func (e *restoreError) Unwrap() error { return e.err }
