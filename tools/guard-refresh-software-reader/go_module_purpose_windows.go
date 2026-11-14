//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// This is an unconnected validator for synthetic input. It performs no file
// access and grants neither source qualification nor permission to build.
const (
	goPurposeModule       = "gopkg.in/yaml.v3"
	goPurposeVersion      = "v3.0.1"
	goPurposeProducer     = "guard-classification-4559-public-module"
	goPurposeArchiveLimit = 32 << 20
	goPurposeExpanded     = 64 << 20
	goPurposeFileLimit    = 16 << 20
	goPurposeEntryLimit   = 4096
)

var errGoPurposeInput = errors.New("go_module_purpose_invalid_input")

// A proof supplied by a future binder must be bound independently to held,
// qualified objects. These fields are not themselves such a proof.
type goModulePurposeProof struct {
	Producer      string
	Module        string
	Version       string
	ModuleH1      string
	ModH1         string
	ArchiveSHA256 string
	GoModSHA256   string
}

// Successful validation returns private copies of the exact buffers from which
// both h1 hashes were computed. The future binder must consume these copies,
// rather than re-opening a path or adopting a caller-supplied producer label.
type goModulePurposeInputs struct {
	Archive []byte
	GoMod   []byte
}

func validateGoModulePurpose(archive, goMod []byte, proof goModulePurposeProof) (*goModulePurposeInputs, error) {
	if proof.Producer != goPurposeProducer || proof.Module != goPurposeModule || proof.Version != goPurposeVersion ||
		!goPurposeDigest(proof.ArchiveSHA256) || !goPurposeDigest(proof.GoModSHA256) ||
		!goPurposeH1(proof.ModuleH1) || !goPurposeH1(proof.ModH1) ||
		len(archive) == 0 || len(archive) > goPurposeArchiveLimit || len(goMod) == 0 || len(goMod) > goPurposeFileLimit {
		return nil, errGoPurposeInput
	}
	archiveCopy := bytes.Clone(archive)
	modCopy := bytes.Clone(goMod)
	success := false
	defer func() {
		if !success {
			clear(archiveCopy)
			clear(modCopy)
		}
	}()
	if goPurposeSHA(archiveCopy) != proof.ArchiveSHA256 || goPurposeSHA(modCopy) != proof.GoModSHA256 {
		return nil, errGoPurposeInput
	}
	moduleH1, rootMod, err := goPurposeZipH1(archiveCopy)
	defer clear(rootMod)
	if err != nil || moduleH1 != proof.ModuleH1 || !bytes.Equal(rootMod, modCopy) ||
		goPurposeH1Files([]goPurposeEntry{{name: "go.mod", data: modCopy}}) != proof.ModH1 {
		return nil, errGoPurposeInput
	}
	success = true
	return &goModulePurposeInputs{Archive: archiveCopy, GoMod: modCopy}, nil
}

type goPurposeEntry struct {
	name string
	data []byte
}

func goPurposeZipH1(raw []byte) (string, []byte, error) {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(z.File) == 0 || len(z.File) > goPurposeEntryLimit {
		return "", nil, errGoPurposeInput
	}
	prefix := goPurposeModule + "@" + goPurposeVersion + "/"
	entries := make([]goPurposeEntry, 0, len(z.File))
	// Include implicit parents: x and x/y, or A/x and a/y, must not be
	// materialized to the same Windows path with incompatible meanings.
	type node struct {
		name                string
		directory, explicit bool
	}
	nodes := make(map[string]node)
	var total uint64
	var rootMod []byte
	keepRoot := false
	defer func() {
		for _, entry := range entries {
			clear(entry.data)
		}
		if !keepRoot {
			clear(rootMod)
		}
	}()
	for _, file := range z.File {
		if !strings.HasPrefix(file.Name, prefix) {
			return "", nil, errGoPurposeInput
		}
		rel := strings.TrimPrefix(file.Name, prefix)
		// Fixed-purpose producer archives are required to contain files only.
		// This is intentionally stricter than general Go module zip files.
		if !goPurposePath(rel) || !file.Mode().IsRegular() || file.UncompressedSize64 > goPurposeFileLimit ||
			file.UncompressedSize64 > goPurposeExpanded-total {
			return "", nil, errGoPurposeInput
		}
		total += file.UncompressedSize64
		parts := strings.Split(rel, "/")
		for i := range parts {
			name := strings.Join(parts[:i+1], "/")
			dir := i < len(parts)-1
			key := strings.ToLower(name) // goPurposePath permits ASCII only.
			if prior, exists := nodes[key]; exists {
				if prior.name != name || prior.directory != dir || (!dir && prior.explicit) {
					return "", nil, errGoPurposeInput
				}
			} else {
				nodes[key] = node{name: name, directory: dir, explicit: !dir}
			}
		}
		r, err := file.Open()
		if err != nil {
			return "", nil, errGoPurposeInput
		}
		data, readErr := io.ReadAll(io.LimitReader(r, int64(file.UncompressedSize64)+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || uint64(len(data)) != file.UncompressedSize64 {
			clear(data)
			return "", nil, errGoPurposeInput
		}
		entries = append(entries, goPurposeEntry{name: file.Name, data: data})
		if rel == "go.mod" {
			rootMod = bytes.Clone(data)
		}
	}
	if rootMod == nil {
		return "", nil, errGoPurposeInput
	}
	if !goPurposeModuleDeclaration(rootMod) {
		return "", nil, errGoPurposeInput
	}
	keepRoot = true
	return goPurposeH1Files(entries), rootMod, nil
}

func goPurposeModuleDeclaration(raw []byte) bool {
	// Purpose-specific strict subset, not a general go.mod parser. The binder
	// may not treat a declared path merely prefixed by the expected module as
	// the same module, and duplicate declarations fail closed.
	seen := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(line, "module ") && !strings.HasPrefix(line, "module\t") && line != "module" {
			continue
		}
		if seen || len(strings.Fields(line)) != 2 || strings.Fields(line)[1] != goPurposeModule {
			return false
		}
		seen = true
	}
	return seen
}

func goPurposePath(rel string) bool {
	if rel == "" || len(rel) > 1024 || strings.HasPrefix(rel, "/") || strings.HasSuffix(rel, "/") {
		return false
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "" || len(part) > 255 || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, c := range part {
			// ASCII-only is deliberate for this fixed module purpose; it avoids
			// an incomplete emulation of Windows Unicode filename equivalence.
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(" !#$%&()+,-.=@[]^_{}~", c) {
				continue
			}
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
		if strings.EqualFold(part, "vendor") || (strings.EqualFold(part, "go.mod") && (i != 0 || part != "go.mod")) {
			return false
		}
	}
	return true
}

func goPurposeH1Files(entries []goPurposeEntry) string {
	ordered := append([]goPurposeEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].name < ordered[j].name })
	h := sha256.New()
	for _, entry := range ordered {
		fileHash := sha256.Sum256(entry.data)
		_, _ = fmt.Fprintf(h, "%x  %s\n", fileHash, entry.name)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func goPurposeSHA(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func goPurposeDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}
func goPurposeH1(s string) bool {
	if !strings.HasPrefix(s, "h1:") {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "h1:"))
	return err == nil && len(b) == sha256.Size && "h1:"+base64.StdEncoding.EncodeToString(b) == s
}
