package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

const iosCompilerDomain = "original-ios-compiler-abi-read-v1"
const iosCompilerSource = "05c636ee1cfbf51db47772d334aff993d0470adf"
const iosCompilerDigest = "9306d77be483c1146d74b0ef14d332d86cfaad8afa7e1c7fb3cce99e27814244"

var errIOSCompiler = errors.New("original compiler artifact unavailable")

// This is a separate CI-artifact purpose. No caller supplies a path, digest,
// provider observation, signing input, or native-image-generation receipt.
var iosCompilerNames = [...]string{
	"input.swift", "input-sha256.txt", "scope.txt", "versions.txt",
	"root-replacement.sil", "root-replacement.ll", "root-replacement.s",
	"silgen.stderr.txt", "ir.stderr.txt", "asm.stderr.txt", "status.txt",
}

type iosCompilerManifestFile struct {
	Name, Identity, SHA256 string
	Size                   int
}

type iosCompilerManifest struct {
	Domain, Source, ArchiveSHA256, ArchiveIdentity string
	Repository, Run, Job, Artifact                 int64
	Files                                          []iosCompilerManifestFile
}

type iosCompilerSnapshot interface {
	Archive() ([]byte, string)
	File(string) ([]byte, string, error)
	Recheck() error
	Close() error
}

type iosCompilerAuthority interface {
	// Provider validates the fixed repository/run/attempt/job/upload/artifact
	// metadata before any original file is opened.
	Provider(context.Context) error
	PinOriginal(context.Context) (iosCompilerSnapshot, error)
	Seal([]byte) ([]byte, error)
	Unseal([]byte) ([]byte, error)
}

func iosCompilerHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// The same archive buffer is hashed and parsed. Only the eleven declared
// snapshots are compared/consumed; other bounded regular diagnostic entries
// are never opened. No extraction, copy, download, or execution occurs here.
func iosCompilerMatchArchive(raw []byte, digest string, files map[string][]byte) bool {
	if len(raw) == 0 || len(raw) > 16<<20 || iosCompilerHash(raw) != digest || len(files) != len(iosCompilerNames) {
		return false
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(z.File) > 64 {
		return false
	}
	seen := map[string]bool{}
	matched, total := 0, uint64(0)
	for _, entry := range z.File {
		name := entry.Name
		if !iosCompilerSafeEntry(name) || seen[asciiLower(name)] || !entry.Mode().IsRegular() || entry.UncompressedSize64 > 8<<20 {
			return false
		}
		seen[asciiLower(name)] = true
		total += entry.UncompressedSize64
		if total > 32<<20 {
			return false
		}
		want, needed := files[name]
		if !needed {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			return false
		}
		got, readErr := io.ReadAll(io.LimitReader(r, 8<<20+1))
		closeErr := r.Close()
		equal := readErr == nil && closeErr == nil && len(got) <= 8<<20 && bytes.Equal(got, want)
		clear(got)
		if !equal {
			return false
		}
		matched++
	}
	return matched == len(iosCompilerNames)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func iosCompilerSafeEntry(s string) bool {
	if s == "" || s == "." || s == ".." || s[len(s)-1] == '.' || s[len(s)-1] == ' ' {
		return false
	}
	for _, b := range []byte(s) {
		if b < 32 || b > 126 || b == '\\' || b == '/' || b == ':' || b == '<' || b == '>' || b == '"' || b == '|' || b == '?' || b == '*' {
			return false
		}
	}
	return true
}

type iosCompilerResult struct {
	Domain                     string               `json:"domain"`
	Stage                      string               `json:"stage"`
	Qualified                  bool                 `json:"qualified"`
	Consumed                   bool                 `json:"consumed"`
	DataAccessGranted          bool                 `json:"data_access_granted"`
	OriginalRegistryRecovered  bool                 `json:"original_registry_recovered"`
	ReceiptSHA256              string               `json:"receipt_sha256,omitempty"`
	FileCount                  int                  `json:"file_count"`
	CleanupComplete            bool                 `json:"cleanup_complete"`
	Manifest                   *iosCompilerManifest `json:"manifest,omitempty"`
	HistoricalCreationVerified bool                 `json:"historical_creation_verified"`
}

// Seal is a real platform issuer operation over independently observed current
// snapshots. It is verified in this invocation; no caller token is accepted.
// The resulting metadata is not historical extraction/creation attestation.
func iosCompilerRead(ctx context.Context, authority iosCompilerAuthority, digest string, consume func(iosCompilerManifest, map[string][]byte) error) (result iosCompilerResult) {
	result = iosCompilerResult{Domain: iosCompilerDomain, Stage: "provider_unavailable", CleanupComplete: true}
	if authority == nil || ctx.Err() != nil || authority.Provider(ctx) != nil {
		return
	}
	result.Stage = "original_unavailable"
	snapshot, err := authority.PinOriginal(ctx)
	if err != nil || snapshot == nil {
		if snapshot != nil {
			result.CleanupComplete = snapshot.Close() == nil
		}
		return
	}
	files := make(map[string][]byte, len(iosCompilerNames))
	defer func() {
		for _, data := range files {
			clear(data)
		}
		if snapshot.Close() != nil {
			result.CleanupComplete = false
		}
	}()
	raw, identity := snapshot.Archive()
	defer clear(raw)
	if ctx.Err() != nil || identity == "" {
		return
	}
	result.Stage = "original_bytes_mismatch"
	if iosCompilerHash(raw) != digest {
		return
	}
	manifest := iosCompilerManifest{Domain: iosCompilerDomain, Source: iosCompilerSource, ArchiveSHA256: digest, ArchiveIdentity: identity, Repository: 1413129506, Run: 43067567260, Job: 130686487618, Artifact: 13876552306}
	total := 0
	for _, name := range iosCompilerNames {
		data, id, readErr := snapshot.File(name)
		total += len(data)
		if readErr != nil || id == "" || len(data) > 8<<20 || total > 32<<20 || ctx.Err() != nil {
			clear(data)
			return
		}
		files[name] = data
		manifest.Files = append(manifest.Files, iosCompilerManifestFile{name, id, iosCompilerHash(data), len(data)})
	}
	result.Stage = "original_bytes_mismatch"
	if !iosCompilerMatchArchive(raw, digest, files) || snapshot.Recheck() != nil {
		return
	}
	result.Stage = "issuer_unavailable"
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	payload, err := json.Marshal(manifest)
	if err != nil || ctx.Err() != nil {
		return
	}
	defer clear(payload)
	sealed, err := authority.Seal(payload)
	defer clear(sealed)
	if err != nil || len(sealed) == 0 || ctx.Err() != nil {
		return
	}
	verified, err := authority.Unseal(sealed)
	defer clear(verified)
	if err != nil || !bytes.Equal(verified, payload) || snapshot.Recheck() != nil || ctx.Err() != nil {
		return
	}
	result.Qualified, result.FileCount, result.ReceiptSHA256 = true, len(files), iosCompilerHash(sealed)
	result.Manifest = &manifest
	result.Stage = "verified"
	if consume != nil {
		result.Stage = "consumer_failed"
		if ctx.Err() != nil || consume(manifest, files) != nil {
			return
		}
		result.Consumed, result.Stage = true, "consumed"
	}
	return
}
