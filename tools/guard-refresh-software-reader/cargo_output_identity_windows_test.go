//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCargoOutputRequiresCompleteExpectedIdentityBeforeConsume(t *testing.T) {
	root := physicalSoftwareFixture(t)
	a, b := filepath.Join(root, "output.exe"), filepath.Join(root, "alias.exe")
	if os.WriteFile(a, []byte("owned artifact"), 0600) != nil || os.Link(a, b) != nil {
		t.Fatal("fixture")
	}
	var actual softwareIdentity
	if !withPinnedCargoOutput(a, b, softwareIdentity{}, func(_ []byte, id softwareIdentity) bool { actual = id; return true }) {
		t.Fatal("bootstrap identity")
	}
	for _, mode := range []string{"normal", "volume", "fileid", "created", "missing-created"} {
		t.Run(mode, func(t *testing.T) {
			expected := actual
			switch mode {
			case "volume":
				expected.Volume = "other"
			case "fileid":
				expected.FileID = "other"
			case "created":
				expected.Created = "other"
			case "missing-created":
				expected.Created = ""
			}
			calls := 0
			ok := withPinnedCargoOutput(a, b, expected, func(_ []byte, id softwareIdentity) bool { calls++; return id == actual })
			if ok != (mode == "normal") || calls != map[bool]int{true: 1, false: 0}[mode == "normal"] {
				t.Fatal("partial identity reached consumer", mode, ok, calls)
			}
		})
	}
}
