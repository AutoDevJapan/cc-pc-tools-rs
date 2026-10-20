package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Export only the bounded, owned, ended diagnostic root; never /runner or its
// authentication/configuration files. The caller holds the supervisor lock.
func archiveEndedDiagnostics(root string, output io.Writer) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) > diagnosticCount {
		return errors.New("diagnostic archive count exceeds bound")
	}
	type item struct {
		path, name string
		info       os.FileInfo
	}
	var items []item
	var total int64
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		ended, err := diagnosticEnded(path)
		if err != nil || !ended || !entry.IsDir() {
			return errors.New("unfinished/unowned diagnostics retained")
		}
		err = filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return errors.New("diagnostic archive link/special file rejected")
			}
			if info.Mode().IsRegular() {
				total += info.Size()
			}
			if total > diagnosticBudget || len(items) >= 10000 {
				return errors.New("diagnostic archive size exceeds bound")
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			items = append(items, item{path, filepath.ToSlash(name), info})
			return nil
		})
		if err != nil {
			return err
		}
	}
	gz := gzip.NewWriter(output)
	tw := tar.NewWriter(gz)
	for _, entry := range items {
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			return err
		}
		header.Name = entry.name
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.info.Mode().IsRegular() {
			f, err := os.Open(entry.path)
			if err != nil {
				return err
			}
			info, statErr := f.Stat()
			if statErr != nil || !os.SameFile(entry.info, info) || info.Size() != entry.info.Size() {
				f.Close()
				return errors.New("diagnostic archive input changed")
			}
			_, copyErr := io.CopyN(tw, f, entry.info.Size())
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
