package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

type retinaFileIdentity struct {
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
	Bytes int    `json:"bytes"`
}

type retinaAppend struct {
	retinaFileIdentity
	BeforeSHA   string `json:"before_sha256"`
	BeforeBytes int    `json:"before_bytes"`
	Content     string `json:"append_content"`
	Prefix      bool   `json:"original_prefix_preserved"`
}

func validateRetinaAppends(appends []retinaAppend, before, manifest map[string]retinaFileIdentity) error {
	seen := map[string]bool{}
	for _, a := range appends {
		b, bok := before[a.Path]
		m, mok := manifest[a.Path]
		if (a.Path != "SKILL.md" && a.Path != "references/index.md") || seen[a.Path] || !bok || !mok || !a.Prefix || !validHex(a.BeforeSHA, 64) || a.BeforeSHA != b.SHA || a.BeforeBytes != b.Bytes || a.Content == "" || a.Bytes != a.BeforeBytes+len([]byte(a.Content)) || a.SHA != m.SHA || a.Bytes != m.Bytes {
			return errors.New("append identity, preserved prefix or UTF-8 byte arithmetic differs from deployment evidence")
		}
		seen[a.Path] = true
	}
	if len(seen) != 2 {
		return errors.New("both appended files must be checked")
	}
	return nil
}

func TestRetinaIntegrationKeepsExactReferencesAndReportsDiscoveryLimit(t *testing.T) {
	var e struct {
		Applied bool                 `json:"applied"`
		Written int                  `json:"central_files_written"`
		Added   int                  `json:"added"`
		Changed int                  `json:"replaced"`
		New     int                  `json:"new_entrypoints"`
		Appends []retinaAppend       `json:"appends"`
		Before  []retinaFileIdentity `json:"before"`
		Texts   []struct {
			Path    string `json:"path"`
			SHA     string `json:"sha256"`
			Bytes   int    `json:"bytes"`
			Content string `json:"content"`
		} `json:"reference_texts"`
		Discovery struct {
			Found    int  `json:"targets_found"`
			Enabled  int  `json:"enabled"`
			Errors   int  `json:"parse_errors"`
			Warnings int  `json:"scan_traversal_limit_warnings"`
			Full     bool `json:"full_catalog_walk_proven"`
			Budget   bool `json:"context_budget_proven"`
		} `json:"discovery"`
	}
	loadPublicReceipt(t, "skills-3080-retina-integration-evidence.json", &e)
	var m struct {
		Targets []struct {
			Name  string               `json:"name"`
			Files []retinaFileIdentity `json:"files"`
		} `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-installed-retina-integration-manifest.json", &m)
	if !e.Applied || e.Written != 6 || e.Added != 4 || e.Changed != 2 || e.New != 0 || len(m.Targets) != 1 || m.Targets[0].Name != "webgl-effects" || len(m.Targets[0].Files) != 6 || len(e.Texts) != 4 {
		t.Fatal("integration lost its exact six-file scope")
	}
	manifest := map[string]string{}
	manifestFiles, before := map[string]retinaFileIdentity{}, map[string]retinaFileIdentity{}
	for _, f := range m.Targets[0].Files {
		if f.Path == "" || manifest[f.Path] != "" || !validHex(f.SHA, 64) {
			t.Fatal("invalid manifest identity")
		}
		manifest[f.Path] = f.SHA
		manifestFiles[f.Path] = f
	}
	for _, f := range e.Before {
		if f.Path == "" || before[f.Path].Path != "" || !validHex(f.SHA, 64) || f.Bytes < 1 {
			t.Fatal("invalid before identity")
		}
		before[f.Path] = f
	}
	if err := validateRetinaAppends(e.Appends, before, manifestFiles); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range e.Texts {
		h := sha256.Sum256([]byte(f.Content))
		if seen[f.Path] || manifest[f.Path] != f.SHA || f.SHA != hex.EncodeToString(h[:]) || f.Bytes != len([]byte(f.Content)) {
			t.Fatal("preserved text differs from its actual deployed manifest")
		}
		seen[f.Path] = true
		switch f.Path {
		case "references/retina-resolution-upstream.txt":
			if f.SHA != "632232040749513e129f58b55ed3361378af9c6803dfd29f8ca596e273d55174" {
				t.Fatal("fixed upstream body changed")
			}
		case "references/retina-resolution-mit.txt":
			if f.SHA != "b2a071ba22fefd803f4e9d1f5c777dd79c62d191695586a1a73fd4d0ebd051c6" {
				t.Fatal("upstream MIT notice changed")
			}
		case "references/retina-resolution-source.json":
			var source map[string]json.RawMessage
			if err := json.Unmarshal([]byte(f.Content), &source); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"review_pending", "central_written"} {
				if _, exists := source[key]; exists {
					t.Fatalf("shipped provenance contains workflow state %s", key)
				}
			}
		}
	}
	for _, path := range []string{"references/retina-resolution.md", "references/retina-resolution-upstream.txt", "references/retina-resolution-mit.txt", "references/retina-resolution-source.json"} {
		if !seen[path] {
			t.Fatal("required reference is missing")
		}
	}
	if e.Discovery.Found != 1 || e.Discovery.Enabled != 1 || e.Discovery.Errors != 0 || e.Discovery.Warnings != 2 || e.Discovery.Full || e.Discovery.Budget {
		t.Fatal("target discovery was conflated with a complete catalog or budget check")
	}
}

func TestRetinaAppendsRejectMissingAndInconsistentEvidence(t *testing.T) {
	var e struct {
		Appends []retinaAppend       `json:"appends"`
		Before  []retinaFileIdentity `json:"before"`
	}
	var m struct {
		Targets []struct {
			Files []retinaFileIdentity `json:"files"`
		} `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-retina-integration-evidence.json", &e)
	loadPublicReceipt(t, "skills-3080-installed-retina-integration-manifest.json", &m)
	before, manifest := map[string]retinaFileIdentity{}, map[string]retinaFileIdentity{}
	for _, f := range e.Before {
		before[f.Path] = f
	}
	for _, f := range m.Targets[0].Files {
		manifest[f.Path] = f
	}
	for _, kind := range []string{"valid", "missing", "duplicate", "sha", "bytes", "before_sha", "before_bytes", "prefix", "utf8_content"} {
		t.Run(kind, func(t *testing.T) {
			a := append([]retinaAppend(nil), e.Appends...)
			switch kind {
			case "missing":
				a = a[:1]
			case "duplicate":
				a[1] = a[0]
			case "sha":
				a[1].SHA = a[0].SHA
			case "bytes":
				a[1].Bytes++
			case "before_sha":
				a[1].BeforeSHA = a[0].BeforeSHA
			case "before_bytes":
				a[1].BeforeBytes++
			case "prefix":
				a[1].Prefix = false
			case "utf8_content":
				a[1].Content += "日本語"
			}
			if err := validateRetinaAppends(a, before, manifest); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
