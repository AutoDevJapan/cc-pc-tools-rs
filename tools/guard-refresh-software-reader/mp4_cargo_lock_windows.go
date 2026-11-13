//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Cargo may prune unused workspace members when materializing the six-crate
// build. Admit only an offline resolution whose complete package identity,
// registry source and checksum already occur in the original locked input.
func cargoLockIdentities(raw []byte) (map[string]bool, bool) {
	if len(raw) > 1<<20 {
		return nil, false
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	blocks := strings.Split(text, "[[package]]")
	if len(blocks) < 2 || !strings.Contains(blocks[0], "version = 4") {
		return nil, false
	}
	result := map[string]bool{}
	for _, block := range blocks[1:] {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			for _, key := range []string{"name", "version", "source", "checksum"} {
				prefix := key + " = "
				if strings.HasPrefix(line, prefix) {
					if _, ok := fields[key]; ok {
						return nil, false
					}
					v, e := strconv.Unquote(strings.TrimPrefix(line, prefix))
					if e != nil {
						return nil, false
					}
					fields[key] = v
				}
			}
		}
		if fields["name"] == "" || fields["version"] == "" {
			return nil, false
		}
		if fields["source"] != "" && (fields["source"] != "registry+https://github.com/rust-lang/crates.io-index" || len(fields["checksum"]) != 64) {
			return nil, false
		}
		key := fields["name"] + "\n" + fields["version"] + "\n" + fields["source"] + "\n" + fields["checksum"]
		if result[key] {
			return nil, false
		}
		result[key] = true
	}
	return result, true
}

func cargoLockKeepsOriginalPins(original, derived []byte) bool {
	a, ok := cargoLockIdentities(original)
	if !ok {
		return false
	}
	b, ok := cargoLockIdentities(derived)
	if !ok || len(b) == 0 {
		return false
	}
	for key := range b {
		if !a[key] {
			return false
		}
	}
	return true
}

func (s *softwareBuildSnapshot) resolveCargoLock(ctx context.Context, original []byte) error {
	path := filepath.Join(s.Root, "Cargo.lock")
	if err := os.WriteFile(path, original, 0600); err != nil {
		return err
	}
	cwd, _ := ctx.Value(mp4CargoDirectory{}).(string)
	if cwd == "" {
		return errUpdateObject
	}
	argv := []string{"tree", "--offline", "--manifest-path", filepath.Join(s.Root, "Cargo.toml"), "-p", "cc-ask", "--depth", "0"}
	argv = append(argv, cargoVendorBuildArgs(s.Root)...)
	r := askOwnedChildAt(ctx, cwd, "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe", argv...)
	defer clear(r.Raw)
	if r.Exit != 0 || !r.Reaped || r.TimedOut {
		return errUpdateObject
	}
	o, err := openUpdateObject(path)
	if err != nil {
		return err
	}
	n := o.(*nativeUpdateObject)
	if !cargoLockKeepsOriginalPins(original, n.Raw()) {
		n.Close()
		return errUpdateObject
	}
	s.LockSHA = digest(n.Raw())
	s.files = append(s.files, n.f)
	n.f = nil
	s.dirs = append(s.dirs, n.parents...)
	n.parents = nil
	clear(n.raw)
	n.raw = nil
	s.paths[path] = true
	return nil
}

func (s *softwareBuildSnapshot) prepareLockedCargoInputsWith(ctx context.Context, original []byte, read cargoArchiveReader, resolve func(context.Context, []byte) error) error {
	if resolve == nil {
		return errUpdateObject
	}
	if err := s.admitCargoVendor(ctx, original, read); err != nil {
		return err
	}
	return resolve(ctx, original)
}
