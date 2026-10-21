package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func ownerRepositoryCandidate(path string) bool {
	_, ok := ownerRepositoryRoot(path)
	return ok
}

func ownerRepositoryRoot(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		path = filepath.Dir(path)
	}
	for depth := 0; depth < 64; depth++ {
		// The system temporary container is not a source registration. A real
		// repository created inside it must carry its own Git marker below it.
		if same(path, os.TempDir()) {
			return "", false
		}
		if !currentPlain(path, true) {
			return "", false
		}
		if marker, err := os.Lstat(filepath.Join(path, ".git")); err == nil && plainEntry(marker) {
			return path, true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", false
		}
		path = parent
	}
	return "", false
}

type ownerDeadlineReader struct {
	ctx   context.Context
	input io.Reader
}

func (r ownerDeadlineReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}

// Decode only projects. Other values are skipped without constructing maps or
// arrays. Duplicate top-level/project keys are rejected; item content is not
// interpreted as source authority and never returned or logged.
func ownerProjectProjection(ctx context.Context, input io.Reader) (object, bool) {
	d := json.NewDecoder(bufio.NewReaderSize(ownerDeadlineReader{ctx, input}, 64<<10))
	d.UseNumber()
	var skip func(int) bool
	skip = func(depth int) bool {
		if depth > 128 || ctx.Err() != nil {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return true
		}
		if delim != '{' && delim != '[' {
			return false
		}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				if err != nil {
					return false
				}
				if _, ok := key.(string); !ok {
					return false
				}
			}
			if !skip(depth + 1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, false
	}
	seen := map[string]bool{}
	projects := object(nil)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return nil, false
		}
		seen[name] = true
		if name != "projects" {
			if !skip(0) {
				return nil, false
			}
			continue
		}
		start, err := d.Token()
		if err != nil || start != json.Delim('{') {
			return nil, false
		}
		projects = object{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || len(name) > 4096 || len(projects) >= 4096 {
				return nil, false
			}
			if _, duplicate := projects[name]; duplicate {
				return nil, false
			}
			value, err := d.Token()
			path, ok := value.(string)
			if err != nil || !ok || len(path) > 32768 {
				return nil, false
			}
			projects[name] = path
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, false
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || projects == nil {
		return nil, false
	}
	if _, err = d.Token(); err != io.EOF || ctx.Err() != nil {
		return nil, false
	}
	return projects, true
}

func ownerReadProjects(ctx context.Context, path string) (object, bool) {
	if legacyMatch(path) || !currentPlain(path, false) {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	before, err := os.Lstat(path)
	opened, e := f.Stat()
	if err != nil || e != nil || !os.SameFile(before, opened) {
		return nil, false
	}
	projects, ok := ownerProjectProjection(ctx, io.LimitReader(f, 1<<30))
	return projects, ok && currentPlain(path, false) && !legacyMatch(path)
}

func ownerSourceGit(ctx context.Context, root string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 20 * time.Millisecond
	cmd.Env = civ6GitEnvironment(os.Environ())
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err == nil && ctx.Err() == nil
}
