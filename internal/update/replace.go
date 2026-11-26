package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Replace puts newBinary at target, the path of the running bfc, on
// Linux, macOS and Windows. Windows lets a running .exe be renamed but
// not overwritten or deleted, so the old binary is moved aside to
// <target>.old and the new one renamed into its place; both renames stay
// in target's directory and so on one filesystem. The new binary gets
// the old one's file mode. When a step fails the old binary is put back.
// <target>.old is removed afterwards where the system allows it, and a
// leftover from an earlier update is removed first.
func Replace(target, newBinary string) error {
	return replace(target, newBinary, os.Rename)
}

func replace(target, newBinary string, rename func(from, to string) error) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("replace bfc: %w", err)
	}
	old := target + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace bfc: remove the leftover %s: %w", old, err)
	}

	tmp, err := copyNextTo(target, newBinary, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("replace bfc: %w", err)
	}
	if err := rename(target, old); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace bfc: move %s aside: %w", target, err)
	}
	if err := rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		if rerr := rename(old, target); rerr != nil {
			return fmt.Errorf("replace bfc: %w; and putting the old binary back failed: %w (it is at %s)", err, rerr, old)
		}
		return fmt.Errorf("replace bfc: %w", err)
	}
	// A running .exe cannot be deleted on Windows; the next update
	// removes it.
	_ = os.Remove(old)
	return nil
}

// copyNextTo copies src to a new file in target's directory with mode
// and returns its path.
func copyNextTo(target, src string, mode os.FileMode) (string, error) {
	in, err := os.Open(src) //nolint:gosec // the binary bfc update downloaded
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".new-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(out.Name(), mode)
	}
	if err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}
