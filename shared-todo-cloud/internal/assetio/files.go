package assetio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func forbidden(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		part, _, _ = strings.Cut(part, ":")
		if strings.EqualFold(strings.TrimRight(part, " ."), "client-projects.txt") {
			return true
		}
	}
	return false
}

// Ownership is lexical and rooted at home, never at the process cwd. External
// paths are not stat'ed. os.Root and ordinary then enforce the filesystem bound.
func localReference(home, original string) (rel string, owned bool, err error) {
	if original == "" {
		return "", false, nil
	}
	if strings.ContainsRune(original, 0) {
		return "", false, ErrPathUnverified
	}
	if strings.Contains(original, "://") {
		return "", false, nil
	}
	if runtime.GOOS != "windows" && (strings.Contains(original, "\\") || len(original) > 1 && original[1] == ':') {
		return "", false, nil
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
		if strings.HasPrefix(p, "\\") {
			return "", false, nil
		}
		if strings.Contains(p, ":") {
			if runtime.GOOS == "windows" && filepath.VolumeName(p) == "" {
				return "", false, ErrPathUnverified
			}
			return "", false, nil
		}
		p = filepath.Join(home, p)
	}
	rel, err = filepath.Rel(home, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false, nil
	}
	if !filepath.IsLocal(rel) || rel == "." || forbidden(original) || forbidden(rel) || runtime.GOOS == "windows" && strings.Contains(rel, ":") {
		return "", false, ErrPathUnverified
	}
	base := strings.TrimRight(rel, " .")
	if strings.EqualFold(base, "todo.json") || strings.EqualFold(base, "todo.lock") {
		return "", false, ErrPathUnverified
	}
	return rel, true, nil
}

func ordinary(root *os.Root, rel string) (os.FileInfo, error) {
	if !filepath.IsLocal(rel) || forbidden(rel) {
		return nil, ErrPathUnverified
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	var path string
	var final os.FileInfo
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := root.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, ErrFileMissing
			}
			return nil, ErrFileUnavailable
		}
		if linked(info) {
			return nil, ErrPathUnverified
		}
		if i != len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, ErrFileUnavailable
		}
		final = info
	}
	return final, nil
}

func readFile(ctx context.Context, root *os.Root, rel string) ([]byte, error) {
	previous, err := ordinary(root, rel)
	if err != nil {
		return nil, err
	}
	if previous.Size() > MaxAssetBytes {
		return nil, ErrAssetTooLarge
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, ErrFileUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || linked(info) || !os.SameFile(previous, info) {
		return nil, ErrPathUnverified
	}
	if info.Size() > MaxAssetBytes {
		return nil, ErrAssetTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: f}, MaxAssetBytes+1))
	if err != nil {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		return nil, ErrFileUnavailable
	}
	if len(raw) > MaxAssetBytes {
		return nil, ErrAssetTooLarge
	}
	return raw, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := cancelled(r.ctx); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
