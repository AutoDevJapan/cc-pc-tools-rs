package main

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func unzipSDK(file, dest string) error {
	if !strings.HasPrefix(filepath.Clean(file), "/work/") || !strings.HasPrefix(filepath.Clean(dest), "/work/") {
		return errors.New("SDK path outside owned scratch")
	}
	z, e := zip.OpenReader(file)
	if e != nil {
		return e
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return errors.New("SDK archive link unsupported")
		}
		p := filepath.Join(dest, f.Name)
		rel, e := filepath.Rel(dest, p)
		if e != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return errors.New("SDK archive escape")
		}
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(p, 0700); e != nil {
				return e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		w, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode().Perm()&0777)
		if e != nil {
			r.Close()
			return e
		}
		_, e = io.Copy(w, r)
		r.Close()
		closeErr := w.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
