package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ManifestFile struct {
	Rel    string `json:"rel"`
	Source string `json:"source"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Files     []ManifestFile `json:"files"`
	Truncated bool           `json:"truncated,omitempty"`
}

const maxEvidenceFile = 8 << 20
const maxEvidenceTotal = 64 << 20
const maxEvidenceFiles = 400

var evidenceExt = map[string]bool{".log": true, ".json": true, ".txt": true, ".out": true, ".err": true, ".ps1": true}
var skipDirs = map[string]bool{"cargo": true, "rustup": true, "sccache": true, "target": true, "_work": true, "sdk": true, "node_modules": true, "home": true, "tmp": true}

// preserveEvidence copies the helper's original logs/reports (and the
// supervisor's own step logs) byte for byte into a retained directory, checks
// every copy against its source hash, and returns the SHA-256 of the manifest.
// Any unreadable or oversize-by-count case fails closed: the owned root is then
// kept instead of deleted.
func preserveEvidence(root, entryDir, dest string) (string, error) {
	if !absClean(root) || !absClean(dest) {
		return "", errors.New("evidence paths must be absolute")
	}
	if e := os.MkdirAll(dest, 0700); e != nil {
		return "", e
	}
	var m Manifest
	var total int64
	add := func(srcRoot, source string) error {
		return filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				return errors.New("link or reparse entry in evidence source")
			}
			rel, e := filepath.Rel(srcRoot, p)
			if e != nil {
				return e
			}
			if d.IsDir() {
				if rel != "." && skipDirs[strings.ToLower(d.Name())] {
					return filepath.SkipDir
				}
				return nil
			}
			inDiag := strings.Contains(filepath.ToSlash(rel), "_diag/")
			// Top-level extensionless files are the helpers' receipts (for
			// example the Linux intent markers); deeper ones need a log-like name.
			topLevel := !strings.Contains(filepath.ToSlash(rel), "/")
			if !evidenceExt[strings.ToLower(filepath.Ext(p))] && !inDiag && !(topLevel && filepath.Ext(p) == "") {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if info.Size() > maxEvidenceFile || total+info.Size() > maxEvidenceTotal || len(m.Files) >= maxEvidenceFiles {
				m.Truncated = true
				return nil
			}
			sum, e := copyHashed(p, filepath.Join(dest, source, filepath.FromSlash(filepath.ToSlash(rel))))
			if e != nil {
				return e
			}
			total += info.Size()
			m.Files = append(m.Files, ManifestFile{Rel: filepath.ToSlash(rel), Source: source, Size: info.Size(), SHA256: sum})
			return nil
		})
	}
	if e := add(root, "root"); e != nil {
		return "", errors.New("owned root evidence unreadable")
	}
	if st, e := os.Stat(entryDir); e == nil && st.IsDir() {
		if e := add(entryDir, "entry"); e != nil {
			return "", errors.New("supervisor evidence unreadable")
		}
	}
	if m.Truncated {
		return "", errors.New("evidence exceeds the retention bounds")
	}
	sort.Slice(m.Files, func(i, j int) bool {
		return m.Files[i].Source+"/"+m.Files[i].Rel < m.Files[j].Source+"/"+m.Files[j].Rel
	})
	b, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return "", e
	}
	if e = os.WriteFile(filepath.Join(dest, "manifest.json"), b, 0600); e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func copyHashed(src, dst string) (string, error) {
	in, e := os.Open(src)
	if e != nil {
		return "", e
	}
	defer in.Close()
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return "", e
	}
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	_, e = io.Copy(io.MultiWriter(out, h), io.LimitReader(in, maxEvidenceFile+1))
	if c := out.Close(); e == nil {
		e = c
	}
	if e != nil {
		return "", e
	}
	want := hex.EncodeToString(h.Sum(nil))
	back, e := os.Open(dst)
	if e != nil {
		return "", e
	}
	defer back.Close()
	h2 := sha256.New()
	if _, e = io.Copy(h2, back); e != nil || hex.EncodeToString(h2.Sum(nil)) != want {
		return "", errors.New("evidence copy differs from its source")
	}
	return want, nil
}
