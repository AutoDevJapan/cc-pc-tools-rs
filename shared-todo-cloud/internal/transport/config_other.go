//go:build !windows

package transport

import (
	"errors"
	"os"
)

func protect(b []byte) ([]byte, error) { return b, nil }
func unprotect(path string, b []byte) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("todo_remote_secret_permissions")
	}
	return b, nil
}
