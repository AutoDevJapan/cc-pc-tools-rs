package windowsrunner

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func unpackRunner(raw []byte, destination string) error {
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != RunnerSHA256 {
		return errors.New("stock runner archive digest rejected")
	}
	return unpackCheckedZip(raw, destination)
}
func unpackCheckedZip(raw []byte, destination string) error {
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		return errors.New("stock archive rejected")
	}
	if len(z.File) > 20000 {
		return errors.New("stock archive entry limit")
	}
	if e = os.Mkdir(destination, 0700); e != nil {
		return errors.New("stock runner root not fresh")
	}
	seen := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		key := strings.ToLower(name)
		if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || !filepath.IsLocal(filepath.FromSlash(name)) || f.Mode()&os.ModeSymlink != 0 || seen[key] {
			return errors.New("stock archive path rejected")
		}
		seen[key] = true
		for _, part := range strings.Split(name, "/") {
			if strings.TrimRight(part, ". ") != part || strings.ContainsAny(part, "<>\"|?*") {
				return errors.New("stock archive path rejected")
			}
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			if os.MkdirAll(target, 0700) != nil {
				return errors.New("stock archive directory failed")
			}
			continue
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 > 128<<20 || f.UncompressedSize64 > 1<<30-total {
			return errors.New("stock archive expansion rejected")
		}
		total += f.UncompressedSize64
		if os.MkdirAll(filepath.Dir(target), 0700) != nil {
			return errors.New("stock archive directory failed")
		}
		in, e := f.Open()
		if e != nil {
			return errors.New("stock archive member failed")
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if e != nil {
			in.Close()
			return errors.New("stock archive collision")
		}
		_, ce := io.CopyN(out, in, int64(f.UncompressedSize64))
		var extra [1]byte
		n, tail := in.Read(extra[:])
		a, b := in.Close(), out.Close()
		if ce != nil || n != 0 || tail != io.EOF || a != nil || b != nil {
			return errors.New("stock archive extraction failed")
		}
	}
	return nil
}
