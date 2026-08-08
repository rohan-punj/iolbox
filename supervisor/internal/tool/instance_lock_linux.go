//go:build linux

package tool

import (
	"fmt"
	"os"
	"syscall"
)

func instWithFileLock(path string, fn func() error) (result error) {
	lockFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("tool: open state lock %q: %w", path, err)
	}
	defer func() {
		if closeErr := lockFile.Close(); closeErr != nil && result == nil {
			result = fmt.Errorf("tool: close state lock %q: %w", path, closeErr)
		}
	}()
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("tool: set state lock mode %q: %w", path, err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("tool: lock state %q: %w", path, err)
	}
	defer func() {
		if unlockErr := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); unlockErr != nil && result == nil {
			result = fmt.Errorf("tool: unlock state %q: %w", path, unlockErr)
		}
	}()
	return fn()
}
