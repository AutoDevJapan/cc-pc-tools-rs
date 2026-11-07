package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
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
	home := "C:/Users/makhmeto"
	if resolved, err := physical(home); err == nil {
		home = resolved
	}
	temp := retirement3250Temp
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

// Retain the previous ignored-item depth bound while the standard streaming
// decoder discards values without holding a whole history element. The old
// bound was 127 item containers plus the document and ignored outer container.
// Syntax remains the standard decoder's responsibility; this reader retains
// only nesting/string state across bounded chunks, never item values.
type ownerProjectNestingReader struct {
	input           io.Reader
	depth           int
	quoted, escaped bool
}

func (r *ownerProjectNestingReader) Read(p []byte) (int, error) {
	n, err := r.input.Read(p)
	for i := 0; i < n; {
		if r.quoted {
			if r.escaped {
				r.escaped = false
				i++
				continue
			}
			at := bytes.IndexAny(p[i:n], "\\\"")
			if at < 0 {
				break
			}
			i += at
			if p[i] == '\\' {
				if i+1 == n {
					r.escaped = true
					i++
				} else {
					i += 2
				}
			} else {
				r.quoted = false
				i++
			}
			continue
		}
		at := bytes.IndexAny(p[i:n], "\"[{]}")
		if at < 0 {
			break
		}
		i += at
		switch p[i] {
		case '"':
			r.quoted = true
		case '[', '{':
			r.depth++
			if r.depth > 129 {
				return 0, errors.New("registration nesting exceeds bound")
			}
		case ']', '}':
			r.depth--
			if r.depth < 0 {
				return 0, errors.New("registration nesting is invalid")
			}
		}
		i++
	}
	return n, err
}

// Decode only projects. SkipValue validates unrelated compound history as a
// stream without retaining whole objects/arrays. Atomic strings still use the
// standard decoder's buffering. Keep the v1 handling
// of ignored duplicate/invalid-Unicode content, while explicitly rejecting
// duplicate top-level/project keys and invalid project strings as before.
func ownerProjectProjection(ctx context.Context, input io.Reader) (object, bool) {
	r := &ownerProjectNestingReader{input: ownerDeadlineReader{ctx, input}}
	d := jsontext.NewDecoder(bufio.NewReaderSize(r, 64<<10), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	first, err := d.ReadToken()
	if err != nil || first.Kind() != '{' {
		return nil, false
	}
	seen := map[string]bool{}
	projects := object(nil)
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil || key.Kind() != '"' || ctx.Err() != nil {
			return nil, false
		}
		name := strings.Clone(key.String())
		if seen[name] {
			return nil, false
		}
		seen[name] = true
		if name != "projects" {
			if d.SkipValue() != nil {
				return nil, false
			}
			continue
		}
		start, err := d.ReadToken()
		if err != nil || start.Kind() != '{' {
			return nil, false
		}
		projects = object{}
		for d.PeekKind() != '}' {
			key, err := d.ReadToken()
			if err != nil || key.Kind() != '"' {
				return nil, false
			}
			name := strings.Clone(key.String())
			if len(name) > 4096 || strings.ContainsRune(name, utf8.RuneError) || len(projects) >= 4096 {
				return nil, false
			}
			if _, duplicate := projects[name]; duplicate {
				return nil, false
			}
			value, err := d.ReadToken()
			if err != nil || value.Kind() != '"' {
				return nil, false
			}
			path := strings.Clone(value.String())
			if len(path) > 32768 || strings.ContainsRune(path, utf8.RuneError) {
				return nil, false
			}
			projects[name] = path
		}
		end, err := d.ReadToken()
		if err != nil || end.Kind() != '}' {
			return nil, false
		}
	}
	end, err := d.ReadToken()
	if err != nil || end.Kind() != '}' || projects == nil {
		return nil, false
	}
	if _, err = d.ReadToken(); err != io.EOF || ctx.Err() != nil {
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

// Only an observable change of the ordinary, still-admissible registration
// file permits one new complete qualification attempt. A denied/missing/aliased
// file or an expired deadline is not a retry signal and never becomes access.
func (s *ownerProjectSnapshot) replaced() bool {
	if s == nil || s.ctx.Err() != nil || s.denied(s.path) || !currentPlain(s.path, false) {
		return false
	}
	named, err := os.Lstat(s.path)
	return err == nil && !named.IsDir() && plainEntry(named) &&
		(!os.SameFile(s.info, named) || s.info.Size() != named.Size() || !s.info.ModTime().Equal(named.ModTime()))
}

// Cache the read file's native identity, then close it promptly so the normal
// Windows atomic writer can proceed during Git/API probes. The final check
// detects normal replacement/observable mutation; it is not a transaction or
// proof that adversarial in-place changes and metadata restoration never occur.
func ownerReadProjectSnapshot(ctx context.Context, path string, denied func(string) bool) (*ownerProjectSnapshot, bool) {
	return ownerReadProjectSnapshotUsing(ctx, path, denied, func(input io.Reader) (object, bool) { return ownerProjectProjection(ctx, input) })
}

func ownerReadProjectSnapshotUsing(ctx context.Context, path string, denied func(string) bool, project func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
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
	projects, equal := project(io.LimitReader(f, 1<<30))
	s.projects = projects
	if !equal || !s.current() {
		return s, false
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
