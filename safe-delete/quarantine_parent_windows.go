//go:build windows

package main

import (
	"path/filepath"
	"strings"
)

const defaultQuarantinePath = `C:\Users\makhmeto\Downloads\削除希望`

// Destination selection never grants access to a source. Quarantine calls this
// only after checking the original owned receipt and measuring its source.
func ownedQuarantineParent(source, configured string) string {
	if !same(configured, defaultQuarantinePath) {
		return configured
	}
	volume := filepath.VolumeName(source)
	if len(volume) != 2 || volume[1] != ':' || strings.EqualFold(volume, filepath.VolumeName(configured)) {
		return configured
	}
	return filepath.Join(volume+`\`, "削除希望")
}
