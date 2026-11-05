//go:build windows

package transport

import (
	"errors"
	"golang.org/x/sys/windows"
	"time"
)

func retryableOpen(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// The replacement above uses MOVEFILE_WRITE_THROUGH on Windows.
func syncDirectory(string) error { return nil }

func replaceFile(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err = windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil || attempt >= 49 || (err != windows.ERROR_ACCESS_DENIED && err != windows.ERROR_SHARING_VIOLATION && err != windows.ERROR_LOCK_VIOLATION) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
