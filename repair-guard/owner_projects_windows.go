package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

func ownerRepositoryCandidate(path string) bool {
	_, ok := ownerRepositoryRoot(path)
	return ok
}

func ownerRepositoryRoot(path string) (string, bool) {
	home, _ := os.UserHomeDir()
	if resolved, err := physical(home); err == nil {
		home = resolved
	}
	temp := os.TempDir()
	if resolved, err := physical(temp); err == nil {
		temp = resolved
	}
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
		if same(path, temp) || same(path, home) {
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

// Decoder validates the JSON before invoking this method. Check the retained
// depth bound, but neither copy the bytes nor decode individual item fields.
type ownerDiscardJSON struct{}

func (*ownerDiscardJSON) UnmarshalJSON(data []byte) error {
	depth, quoted, escaped := 1, false, false
	for _, b := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		switch b {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > 128 {
				return errors.New("registration nesting exceeds bound")
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

// Decode only projects. Unrelated arrays are skipped one element at a time,
// without maps, arrays or field tokens. Duplicate top-level/project keys are
// rejected; item content is never used as authority, returned or logged.
func ownerProjectProjection(ctx context.Context, input io.Reader) (object, bool) {
	d := json.NewDecoder(bufio.NewReaderSize(ownerDeadlineReader{ctx, input}, 64<<10))
	d.UseNumber()
	skip := func() bool {
		if ctx.Err() != nil {
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
			var ignored ownerDiscardJSON
			if ctx.Err() != nil || d.Decode(&ignored) != nil {
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
			if !skip() {
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
			if err != nil || !ok || len(name) > 4096 || strings.ContainsRune(name, utf8.RuneError) || len(projects) >= 4096 {
				return nil, false
			}
			if _, duplicate := projects[name]; duplicate {
				return nil, false
			}
			value, err := d.Token()
			path, ok := value.(string)
			if err != nil || !ok || len(path) > 32768 || strings.ContainsRune(path, utf8.RuneError) {
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

type ownerProjectSnapshot struct {
	projects object
	path     string
	info     os.FileInfo
	ctx      context.Context
	denied   func(string) bool
}

func (s *ownerProjectSnapshot) current() bool {
	if s == nil || s.ctx.Err() != nil {
		return false
	}
	named, err := os.Lstat(s.path)
	return err == nil && os.SameFile(s.info, named) && s.info.Size() == named.Size() && s.info.ModTime().Equal(named.ModTime()) && currentPlain(s.path, false) && !s.denied(s.path)
}

// Cache the read file's native identity, then close it promptly so the normal
// Windows atomic writer can proceed during Git/API probes. The final check
// detects normal replacement/observable mutation; it is not a transaction or
// proof that adversarial in-place changes and metadata restoration never occur.
func ownerReadProjectSnapshot(ctx context.Context, path string, denied func(string) bool) (*ownerProjectSnapshot, bool) {
	if ctx.Err() != nil || denied(path) || !currentPlain(path, false) {
		return nil, false
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if err != nil {
		return nil, false
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	info, err := f.Stat()
	named, e := os.Lstat(path)
	if err != nil || e != nil || !plainEntry(info) || !os.SameFile(info, named) {
		_ = f.Close()
		return nil, false
	}
	final, err := ownerHandlePath(h)
	if err != nil || !same(final, path) {
		_ = f.Close()
		return nil, false
	}
	s := &ownerProjectSnapshot{path: path, info: info, ctx: ctx, denied: denied}
	projects, equal := ownerProjectProjection(ctx, io.LimitReader(f, 1<<30))
	s.projects = projects
	if !equal || !s.current() {
		return nil, false
	}
	return s, true
}

func ownerSourceGit(ctx context.Context, root string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 20 * time.Millisecond
	cmd.Env = civ6GitEnvironment(os.Environ())
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err == nil && ctx.Err() == nil
}
