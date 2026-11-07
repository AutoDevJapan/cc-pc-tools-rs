package migrate

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func forbiddenPath(path string) bool {
	for _, p := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		p, _, _ = strings.Cut(p, ":")
		if strings.EqualFold(strings.TrimRight(p, " ."), "client-projects.txt") {
			return true
		}
	}
	return false
}

func within(home, path string) (string, bool) {
	rel, err := filepath.Rel(home, path)
	return rel, err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func localReference(home, original string) (string, bool) {
	if original == "" || strings.ContainsRune(original, 0) || forbiddenPath(original) {
		return "", false
	}
	if runtime.GOOS != "windows" && (strings.Contains(original, "\\") || len(original) > 1 && original[1] == ':') {
		return "", false
	}
	p := original
	if runtime.GOOS == "windows" {
		p = filepath.FromSlash(p)
		if strings.HasPrefix(p, `\\?\UNC\`) {
			p = `\\` + p[len(`\\?\UNC\`):]
		} else {
			p = strings.TrimPrefix(p, `\\?\`)
		}
	}
	if !filepath.IsAbs(p) {
		// Reject drive-relative paths and alternate data streams on Windows.
		if strings.Contains(p, ":") || strings.HasPrefix(p, "\\") {
			return "", false
		}
		p = filepath.Join(home, p)
	}
	rel, ok := within(home, filepath.Clean(p))
	if !ok || forbiddenPath(rel) {
		return "", false
	}
	// JSON and lock files are source/control files, not specification assets.
	if strings.EqualFold(rel, "todo.json") || strings.EqualFold(rel, "todo.lock") {
		return "", false
	}
	return rel, true
}

// Check each component before opening it. os.Root additionally prevents an
// intermediate rename or symlink from escaping the explicitly authorized home.
func ordinary(root *os.Root, rel string) error {
	if !filepath.IsLocal(rel) || forbiddenPath(rel) {
		return failure("path_unverified")
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	var path string
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := root.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return failure("file_missing")
			}
			return failure("file_unavailable")
		}
		if linked(info) {
			return failure("path_unverified")
		}
		if i != len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return failure("file_unavailable")
		}
	}
	return nil
}

func readUnder(root *os.Root, rel string, limit int64) ([]byte, error) {
	if err := ordinary(root, rel); err != nil {
		return nil, err
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, failure("file_unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, failure("file_unavailable")
	}
	if info.Size() > limit {
		return nil, failure("file_too_large")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, failure("file_unavailable")
	}
	if int64(len(b)) > limit {
		return nil, failure("file_too_large")
	}
	return b, nil
}

func writeNew(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return failure("export_write_failed")
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return failure("export_write_failed")
	}
	return nil
}

func cancelled(ctx context.Context) error {
	if ctx.Err() != nil {
		return failure("cancelled")
	}
	return nil
}
