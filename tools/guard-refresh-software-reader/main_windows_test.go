//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestSoftwareReaderBadArgsReadNothing(t *testing.T) {
	for _, args := range [][]string{nil, {"--verify-installed-refresh-guard", "D:/other"}, {"--verify-installed-refresh-guard=true"}, {"--guard-path", "D:/other"}, {"--verify-installed-refresh-guard", "--verify-installed-refresh-guard"}} {
		calls := 0
		var out bytes.Buffer
		if run(args, &out, func() softwareObservation { calls++; return softwareObservation{Verified: true} }) != 2 || calls != 0 {
			t.Fatal("caller arguments reached software")
		}
	}
}

func TestSoftwareReaderExactBytesBeforeBuildParsing(t *testing.T) {
	raw := []byte("owned synthetic software")
	hash := sha256.Sum256(raw)
	want := hex.EncodeToString(hash[:])
	good := func() *debug.BuildInfo {
		return &debug.BuildInfo{Path: installedGuardModule, Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: installedGuardRevision}, {Key: "vcs.modified", Value: "false"}, {Key: "GOOS", Value: "windows"}, {Key: "GOARCH", Value: "amd64"}}}
	}
	for _, name := range []string{"valid", "hash", "module", "revision", "dirty", "duplicate", "parse"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			expected := want
			if name == "hash" {
				expected = "unknown"
			}
			ok := pinnedBuildMatches(raw, expected, func([]byte) (*debug.BuildInfo, error) {
				calls++
				info := good()
				switch name {
				case "module":
					info.Path = "other"
				case "revision":
					info.Settings[1].Value = "other"
				case "dirty":
					info.Settings[2].Value = "true"
				case "duplicate":
					info.Settings = append(info.Settings, info.Settings[0])
				case "parse":
					return nil, errors.New("untrusted parser detail")
				}
				return info, nil
			})
			if ok != (name == "valid") || name == "hash" && calls != 0 {
				t.Fatal("unverified software accepted or parsed")
			}
		})
	}
}

func TestSoftwareReaderHoldsActualFileAgainstReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "owned.exe")
	if os.WriteFile(path, []byte("owned synthetic software"), 0600) != nil {
		t.Fatal("fixture")
	}
	called := false
	if !withPinnedSoftware(path, func(raw []byte, id softwareIdentity) bool {
		called = true
		if string(raw) != "owned synthetic software" || id.FileID == "" || id.Created == "" {
			t.Fatal("pinned observation")
		}
		if os.WriteFile(path, []byte("replacement"), 0600) == nil {
			t.Fatal("write not excluded")
		}
		if os.Rename(path, path+".renamed") == nil {
			t.Fatal("delete not excluded")
		}
		return true
	}) || !called {
		t.Fatal("normal file not consumed")
	}
	if os.WriteFile(path, []byte("after release"), 0600) != nil {
		t.Fatal("fixture lease leaked")
	}
}

func TestSoftwareReaderRejectsHardlinkBeforeConsume(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "owned.exe")
	if os.WriteFile(path, []byte("synthetic"), 0600) != nil {
		t.Fatal("fixture")
	}
	if os.Link(path, path+".link") != nil {
		t.Fatal("hardlink fixture")
	}
	calls := 0
	if withPinnedSoftware(path, func([]byte, softwareIdentity) bool { calls++; return true }) || calls != 0 {
		t.Fatal("hardlink accepted")
	}
}

func TestSoftwareReaderFailureOutputDoesNotExposeMetadata(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"--verify-installed-refresh-guard"}, &out, func() softwareObservation {
		return softwareObservation{Domain: "private detail", SHA256: "private", Identity: &softwareIdentity{Created: "secret"}}
	})
	var obj map[string]any
	if code != 1 || json.Unmarshal(out.Bytes(), &obj) != nil || len(obj) != 3 || obj["verified"] != false || bytes.Contains(out.Bytes(), []byte("private")) {
		t.Fatal("failure details exposed")
	}
}
