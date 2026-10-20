package main

import (
	"errors"
	"os"
	"path/filepath"
)

const burstWorkerActiveExit = 73
const burstConfigurationPresentExit = 76
const burstSetupUnverifiedExit = 71

// Check existence and file type only. Never open SDK authentication contents.
func burstConfigurationAbsent(root string) (bool, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("SDK directory rejected")
	}
	absent := true
	for _, name := range []string{".runner", ".credentials", ".credentials_rsaparams"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("SDK configuration type rejected")
		}
		absent = false
	}
	return absent, nil
}
