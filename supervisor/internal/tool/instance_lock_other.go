//go:build !linux

package tool

import (
	"fmt"
	"os"
	"time"
)

func instWithFileLock(path string, fn func() error) (result error) {
	for {
		lockFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := lockFile.Close(); closeErr != nil {
				_ = os.Remove(path)
				return fmt.Errorf("tool: close state lock %q: %w", path, closeErr)
			}
			break
		}
		if !os.IsExist(err) {
			return fmt.Errorf("tool: create state lock %q: %w", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) && result == nil {
			result = fmt.Errorf("tool: remove state lock %q: %w", path, removeErr)
		}
	}()
	return fn()
}
