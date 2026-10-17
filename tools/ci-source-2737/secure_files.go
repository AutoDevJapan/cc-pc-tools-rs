package sourcepacket

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Directory handles anchor EVERY component from the filesystem root. Files are
// opened without following links and bytes are consumed through that handle.
// Revalidation rejects an ancestor renamed/replaced while proof was in flight.
type secureFiles struct {
	root  string
	dirs  map[string]*os.File
	files map[string]os.FileInfo
}

func secureRoot(root string) (s *secureFiles, err error) {
	if !filepath.IsAbs(root) {
		return nil, fixedErr()
	}
	root = filepath.Clean(root)
	s = &secureFiles{root: root, dirs: map[string]*os.File{}, files: map[string]os.FileInfo{}}
	owned := s
	defer func() {
		if err != nil {
			owned.Close()
		}
	}()
	volume := filepath.VolumeName(root)
	base := volume + string(filepath.Separator)
	if strings.HasPrefix(volume, `\\`) {
		return nil, fixedErr()
	}
	f, e := openRootDirectory(base)
	if e != nil {
		return nil, fixedErr()
	}
	s.dirs[base] = f
	rel, e := filepath.Rel(base, root)
	if e != nil {
		return nil, fixedErr()
	}
	parent := base
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			next := filepath.Join(parent, part)
			f, e := openDirectoryAt(s.dirs[parent], part, next)
			if e != nil {
				return nil, fixedErr()
			}
			s.dirs[next] = f
			parent = next
		}
	}
	return s, nil
}
func (s *secureFiles) Close() {
	for _, f := range s.dirs {
		_ = f.Close()
	}
}
func secureRelative(rel string) bool {
	return rel != "" && !filepath.IsAbs(rel) && filepath.Clean(rel) == rel && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !strings.ContainsAny(rel, "\x00\r\n:")
}
func (s *secureFiles) directory(rel string) (*os.File, error) {
	if rel == "." {
		return s.dirs[s.root], nil
	}
	if !secureRelative(rel) {
		return nil, fixedErr()
	}
	parent := s.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		next := filepath.Join(parent, part)
		if s.dirs[next] == nil {
			if len(s.dirs) > 4096 {
				return nil, fixedErr()
			}
			f, e := openDirectoryAt(s.dirs[parent], part, next)
			if e != nil {
				return nil, fixedErr()
			}
			s.dirs[next] = f
		}
		parent = next
	}
	return s.dirs[parent], nil
}
func (s *secureFiles) open(rel string) (*os.File, error) {
	if !secureRelative(rel) {
		return nil, fixedErr()
	}
	parent, e := s.directory(filepath.Dir(rel))
	if e != nil {
		return nil, e
	}
	f, e := openRegularAt(parent, filepath.Base(rel), filepath.Join(s.root, rel))
	if e != nil {
		return nil, fixedErr()
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fixedErr()
	}
	return f, nil
}
func unchangedFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime() == b.ModTime()
}
func (s *secureFiles) read(rel string, limit int64) ([]byte, os.FileInfo, error) {
	f, e := s.open(rel)
	if e != nil {
		return nil, nil, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || before.Size() < 0 || before.Size() > limit {
		return nil, nil, fixedErr()
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	after, se := f.Stat()
	if e != nil || se != nil || int64(len(b)) != before.Size() || !unchangedFile(before, after) {
		return nil, nil, fixedErr()
	}
	s.files[rel] = after
	return b, after, nil
}
func (s *secureFiles) revalidate() error {
	fresh, e := secureRoot(s.root)
	if e != nil {
		return e
	}
	defer fresh.Close()
	for rel, want := range s.files {
		f, e := fresh.open(rel)
		if e != nil {
			return e
		}
		got, e := f.Stat()
		f.Close()
		if e != nil || !unchangedFile(want, got) {
			return fixedErr()
		}
	}
	for absolute, old := range s.dirs {
		if fresh.dirs[absolute] == nil {
			rel, e := filepath.Rel(s.root, absolute)
			if e != nil || !secureRelative(rel) {
				return fixedErr()
			}
			if _, e = fresh.directory(rel); e != nil {
				return e
			}
		}
		a, e := old.Stat()
		if e != nil {
			return fixedErr()
		}
		b, e := fresh.dirs[absolute].Stat()
		if e != nil || !os.SameFile(a, b) {
			return fixedErr()
		}
	}
	return nil
}
