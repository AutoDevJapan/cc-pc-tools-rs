//go:build !windows

package transport

import "os"

func retryableOpen(error) bool { return false }

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func replaceFile(from, to string) error { return os.Rename(from, to) }
