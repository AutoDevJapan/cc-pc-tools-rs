package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type capturedFile struct {
	Original, Opaque, SHA256 string
	Bytes                    int64
}

func captureHost(root string, id int64, source string) error {
	dest, e := captureDir(root, id, "control")
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(source)
	if e != nil {
		return e
	}
	var records []capturedFile
	prefix := fmt.Sprintf("repo%d", id)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		name := entry.Name()
		if !(strings.HasSuffix(name, ".private.json") || strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".private.patch") || strings.HasSuffix(name, ".private.txt")) {
			continue
		}
		r, err := opaqueCopy(filepath.Join(source, name), dest, len(records))
		if err != nil {
			return err
		}
		records = append(records, r)
	}
	return captureManifest(dest, id, "control", records)
}

func captureDir(root string, id int64, platform string) (string, error) {
	p := filepath.Join(root, fmt.Sprintf("repo%d", id), fmt.Sprintf("%s-%d", platform, time.Now().UnixNano()))
	return p, os.MkdirAll(p, 0700)
}
func opaqueCopy(src, dest string, index int) (capturedFile, error) {
	f, e := os.Open(src)
	if e != nil {
		return capturedFile{}, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil {
		return capturedFile{}, e
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return capturedFile{}, e
	}
	expected := hex.EncodeToString(h.Sum(nil))
	if _, e = f.Seek(0, 0); e != nil {
		return capturedFile{}, e
	}
	name := fmt.Sprintf("record-%06d.bin", index)
	out, e := os.OpenFile(filepath.Join(dest, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return capturedFile{}, e
	}
	h.Reset()
	n, e := io.Copy(io.MultiWriter(out, h), f)
	syncErr := out.Sync()
	closeErr := out.Close()
	after, statErr := f.Stat()
	if e != nil || syncErr != nil || closeErr != nil || statErr != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || expected != hex.EncodeToString(h.Sum(nil)) {
		return capturedFile{}, errors.New("evidence changed or copy mismatch")
	}
	b, e := os.ReadFile(filepath.Join(dest, name))
	if e != nil || digest(b) != expected {
		return capturedFile{}, errors.New("saved evidence hash mismatch")
	}
	return capturedFile{Original: src, Opaque: name, SHA256: expected, Bytes: n}, nil
}
func captureWindows(root string, id int64, source string) error {
	dest, e := captureDir(root, id, "windows")
	if e != nil {
		return e
	}
	var records []capturedFile
	e = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("evidence link rejected")
		}
		rel, _ := filepath.Rel(source, path)
		if d.IsDir() {
			first := strings.Split(rel, string(filepath.Separator))[0]
			if first == "source" || first == "target" || first == "tmp" || first == "cli-home" || first == "cargo" || first == "rustup" || first == "ci-tools" || first == "npm-cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("evidence special file rejected")
		}
		r, err := opaqueCopy(path, dest, len(records))
		if err == nil {
			records = append(records, r)
		}
		return err
	})
	if e != nil {
		return e
	}
	return captureManifest(dest, id, "windows", records)
}
func captureManifest(dest string, id int64, platform string, records []capturedFile) error {
	var total int64
	for _, r := range records {
		total += r.Bytes
	}
	if e := writeJSON(filepath.Join(dest, "manifest.private.json"), map[string]any{"repo_id": id, "platform": platform, "at": time.Now().UTC(), "source_and_saved_sha256_matched": true, "files": records, "bytes": total, "all_required_checks_success": false}); e != nil {
		return e
	}
	fmt.Printf("repo%d platform=%s preserved_files=%d preserved_bytes=%d hash_verified=true\n", id, platform, len(records), total)
	return nil
}
func captureLinux(ctx context.Context, file, root string) error {
	s, e := load(file)
	if e != nil {
		return e
	}
	dest, e := captureDir(root, s.RepoID, "linux")
	if e != nil {
		return e
	}
	// Only completed test evidence and synthetic browser evidence. Never source,
	// SDK binaries, registry caches, owner profiles or authentication files.
	setup := `set -eu; { find /work -mindepth 1 -maxdepth 1 -type d -name 'evidence*'; for p in /work/target /work/source/target; do if test -d "$p"; then find "$p" -mindepth 1 -maxdepth 1 -type d \( -name 'native-*-cdp-*' -o -name 'authored-buyer-*' -o -name 'customer-output-*-cdp-*' -o -name 'chrome-resources*' -o -name 'ci2730-virtual*' \); fi; done; } | sort -u > /work/capture-roots.txt; while IFS= read -r p; do find "$p" -type f -print0; done < /work/capture-roots.txt | xargs -0 sha256sum -z`
	hashes, e := output(ctx, "exec", s.Container, "sh", "-c", setup)
	if e != nil {
		return errors.New("original evidence hashing failed")
	}
	expected := map[string]string{}
	for _, r := range strings.Split(string(hashes), "\x00") {
		if r == "" {
			continue
		}
		if len(r) < 67 {
			return errors.New("original hash framing invalid")
		}
		expected[r[66:]] = r[:64]
	}
	x := cmd(ctx, docker, "exec", s.Container, "sh", "-c", `tar -Pcf - -T /work/capture-roots.txt`)
	pipe, e := x.StdoutPipe()
	if e != nil {
		return e
	}
	if e = x.Start(); e != nil {
		return e
	}
	tr := tar.NewReader(pipe)
	var records []capturedFile
	var readErr error
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			readErr = err
			break
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			readErr = errors.New("archive evidence link/special file rejected")
			break
		}
		want, ok := expected[h.Name]
		if !ok {
			readErr = errors.New("evidence source hash missing")
			break
		}
		name := fmt.Sprintf("record-%06d.bin", len(records))
		f, err := os.OpenFile(filepath.Join(dest, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			readErr = err
			break
		}
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, hash), tr)
		syncErr := f.Sync()
		closeErr := f.Close()
		got := hex.EncodeToString(hash.Sum(nil))
		if err != nil || syncErr != nil || closeErr != nil || n != h.Size || got != want {
			readErr = errors.New("original/archive evidence mismatch")
			break
		}
		saved, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || digest(saved) != want {
			readErr = errors.New("saved evidence hash mismatch")
			break
		}
		records = append(records, capturedFile{Original: h.Name, Opaque: name, SHA256: want, Bytes: n})
		delete(expected, h.Name)
	}
	if readErr != nil {
		pipe.Close()
		_ = x.Wait()
		return readErr
	}
	// tar.Reader stops at the end markers, before tar's final record padding.
	// Drain that padding before Wait so a full Windows pipe cannot block Docker.
	if _, e = io.Copy(io.Discard, pipe); e != nil {
		return errors.New("evidence archive tail drain failed")
	}
	if e = x.Wait(); e != nil {
		return errors.New("evidence archive transfer failed")
	}
	if len(expected) != 0 {
		return errors.New("evidence archive incomplete")
	}
	return captureManifest(dest, s.RepoID, "linux", records)
}
