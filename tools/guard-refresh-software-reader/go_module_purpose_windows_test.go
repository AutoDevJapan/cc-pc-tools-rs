//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

const fixtureModuleH1 = "h1:e3n/RkV1zoVyin0TgBxJ8KdnqO5RyRzSFm9qzlsDVeM="
const fixtureGoModH1 = "h1:v7FqyP2fRWlwZyYcPuV613ZOl8L6QAHe5qvE+ndgVsQ="

type goPurposeFixture struct {
	name string
	body []byte
	mode fs.FileMode
}

func goPurposeFixtureArchive(t *testing.T, entries []goPurposeFixture, method uint16) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: method}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func goPurposeFixtureInputs(t *testing.T) ([]byte, []byte, goModulePurposeProof) {
	t.Helper()
	mod := []byte("module gopkg.in/yaml.v3\n")
	prefix := goPurposeModule + "@" + goPurposeVersion + "/"
	archive := goPurposeFixtureArchive(t, []goPurposeFixture{
		{name: prefix + "sample.go", body: []byte("package synthetic\n")},
		{name: prefix + "go.mod", body: mod},
	}, zip.Deflate)
	return archive, mod, goModulePurposeProof{
		Producer: goPurposeProducer, Module: goPurposeModule, Version: goPurposeVersion,
		ModuleH1: fixtureModuleH1, ModH1: fixtureGoModH1,
		ArchiveSHA256: goPurposeSHA(archive), GoModSHA256: goPurposeSHA(mod),
	}
}

func TestGoPurposeSyntheticHashesAndHeldInput(t *testing.T) {
	archive, mod, proof := goPurposeFixtureInputs(t)
	result, err := validateGoModulePurpose(archive, mod, proof)
	if err != nil || !bytes.Equal(result.Archive, archive) || !bytes.Equal(result.GoMod, mod) {
		t.Fatal("valid synthetic module was rejected")
	}
	originalArchive, originalMod := bytes.Clone(result.Archive), bytes.Clone(result.GoMod)
	archive[0] ^= 1
	mod[0] ^= 1
	if !bytes.Equal(result.Archive, originalArchive) || !bytes.Equal(result.GoMod, originalMod) {
		t.Fatal("validated input changed with caller-owned buffers")
	}
	// The h1 covers names and uncompressed bytes, not ZIP compression/order.
	prefix := goPurposeModule + "@" + goPurposeVersion + "/"
	stored := goPurposeFixtureArchive(t, []goPurposeFixture{
		{name: prefix + "go.mod", body: originalMod},
		{name: prefix + "sample.go", body: []byte("package synthetic\n")},
	}, zip.Store)
	proof.ArchiveSHA256 = goPurposeSHA(stored)
	if _, err := validateGoModulePurpose(stored, originalMod, proof); err != nil {
		t.Fatal("equivalent synthetic ZIP was rejected")
	}
}

func TestGoPurposeRejectsUnboundOrChangedProof(t *testing.T) {
	for _, fault := range []string{"producer", "module", "version", "module-h1", "mod-h1", "swap-h1", "archive-sha", "mod-sha", "malformed-h1", "uppercase-sha", "changed-archive", "changed-mod", "embedded-mod-mismatch"} {
		t.Run(fault, func(t *testing.T) {
			archive, mod, proof := goPurposeFixtureInputs(t)
			switch fault {
			case "producer":
				proof.Producer = "unknown"
			case "module":
				proof.Module = "example.com/other"
			case "version":
				proof.Version = "v3.0.0"
			case "module-h1":
				proof.ModuleH1 = proof.ModH1
			case "mod-h1":
				proof.ModH1 = proof.ModuleH1
			case "swap-h1":
				proof.ModuleH1, proof.ModH1 = proof.ModH1, proof.ModuleH1
			case "archive-sha":
				proof.ArchiveSHA256 = strings.Repeat("0", 64)
			case "mod-sha":
				proof.GoModSHA256 = strings.Repeat("0", 64)
			case "malformed-h1":
				proof.ModH1 = "h1:bad"
			case "uppercase-sha":
				proof.GoModSHA256 = strings.ToUpper(proof.GoModSHA256)
			case "changed-archive":
				archive[0] ^= 1
			case "changed-mod":
				mod[0] ^= 1
			case "embedded-mod-mismatch":
				mod = []byte("module example.com/different\n")
				proof.GoModSHA256 = goPurposeSHA(mod)
				proof.ModH1 = goPurposeH1Files([]goPurposeEntry{{name: "go.mod", data: mod}})
			}
			if value, err := validateGoModulePurpose(archive, mod, proof); err == nil || value != nil {
				t.Fatal("invalid synthetic proof was accepted")
			}
		})
	}
}

func TestGoPurposeRejectsUnsafeZIPEntries(t *testing.T) {
	prefix := goPurposeModule + "@" + goPurposeVersion + "/"
	validMod := goPurposeFixture{name: prefix + "go.mod", body: []byte("module gopkg.in/yaml.v3\n")}
	for _, tc := range []struct {
		name    string
		entries []goPurposeFixture
	}{
		{"wrong-prefix", []goPurposeFixture{validMod, {name: "other@v1/a.go"}}},
		{"traversal", []goPurposeFixture{validMod, {name: prefix + "../a.go"}}},
		{"absolute", []goPurposeFixture{validMod, {name: prefix + "/a.go"}}},
		{"backslash", []goPurposeFixture{validMod, {name: prefix + `a\b.go`}}},
		{"duplicate", []goPurposeFixture{validMod, {name: prefix + "a.go"}, {name: prefix + "a.go"}}},
		{"case-collision", []goPurposeFixture{validMod, {name: prefix + "a.go"}, {name: prefix + "A.go"}}},
		{"parent-case-collision", []goPurposeFixture{validMod, {name: prefix + "dir/a.go"}, {name: prefix + "DIR/b.go"}}},
		{"file-as-parent", []goPurposeFixture{validMod, {name: prefix + "dir"}, {name: prefix + "dir/a.go"}}},
		{"reserved", []goPurposeFixture{validMod, {name: prefix + "nul.txt"}}},
		{"trailing-dot", []goPurposeFixture{validMod, {name: prefix + "file."}}},
		{"alternate-stream", []goPurposeFixture{validMod, {name: prefix + "file:stream"}}},
		{"non-ascii", []goPurposeFixture{validMod, {name: prefix + "名.go"}}},
		{"nested-module", []goPurposeFixture{validMod, {name: prefix + "sub/go.mod"}}},
		{"vendor", []goPurposeFixture{validMod, {name: prefix + "vendor/x.go"}}},
		{"link", []goPurposeFixture{validMod, {name: prefix + "link", mode: fs.ModeSymlink | 0777}}},
		{"directory", []goPurposeFixture{validMod, {name: prefix + "dir/", mode: fs.ModeDir | 0755}}},
		{"missing-mod", []goPurposeFixture{{name: prefix + "a.go"}}},
		{"wrong-module", []goPurposeFixture{{name: prefix + "go.mod", body: []byte("module example.com/other\n")}}},
		{"duplicate-module", []goPurposeFixture{{name: prefix + "go.mod", body: []byte("module gopkg.in/yaml.v3\nmodule gopkg.in/yaml.v3\n")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := goPurposeFixtureArchive(t, tc.entries, zip.Store)
			if _, _, err := goPurposeZipH1(archive); err == nil {
				t.Fatal("unsafe synthetic ZIP was accepted")
			}
		})
	}
}

func TestGoPurposeEnforcesZIPLimits(t *testing.T) {
	prefix := goPurposeModule + "@" + goPurposeVersion + "/"
	entries := make([]goPurposeFixture, 0, goPurposeEntryLimit+1)
	entries = append(entries, goPurposeFixture{name: prefix + "go.mod", body: []byte("module gopkg.in/yaml.v3\n")})
	for i := 0; i < goPurposeEntryLimit; i++ {
		entries = append(entries, goPurposeFixture{name: fmt.Sprintf("%sf%d.go", prefix, i)})
	}
	tooMany := goPurposeFixtureArchive(t, entries, zip.Store)
	if _, _, err := goPurposeZipH1(tooMany); err == nil {
		t.Fatal("entry limit was not enforced")
	}
	tooLarge := goPurposeFixtureArchive(t, []goPurposeFixture{
		entries[0], {name: prefix + "large.go", body: bytes.Repeat([]byte{'x'}, goPurposeFileLimit+1)},
	}, zip.Deflate)
	if _, _, err := goPurposeZipH1(tooLarge); err == nil {
		t.Fatal("expanded file limit was not enforced")
	}
}

// These declarations are made from the general Go module grammar, not from
// any local or upstream copy of the purpose's actual module payload.
func TestGoPurposeModuleDeclarationSyntheticGoSyntax(t *testing.T) {
	for _, tc := range []struct { name, raw string; want bool }{
		{"identifier", "module "+goPurposeModule+"\n", true},
		{"interpreted", "module \""+goPurposeModule+"\"\n", true},
		{"raw", "module `"+goPurposeModule+"`\n", true},
		{"escape", "module \"\\x67opkg.in/yaml.v3\"\n", true},
		{"comment", "// synthetic declaration\nmodule "+goPurposeModule+" // synthetic suffix\n", true},
		{"block", "module (\n\t\""+goPurposeModule+"\"\n)\n", true},
		{"crlf", "module\t"+goPurposeModule+"\r\n", true},
		{"other", "module example.com/other\n", false},
		{"quoted-other", "module \"example.com/other\"\n", false},
		{"duplicate", "module "+goPurposeModule+"\nmodule \""+goPurposeModule+"\"\n", false},
		{"missing", "// module "+goPurposeModule+"\n", false},
		{"unfinished", "module \""+goPurposeModule+"\n", false},
		{"bad-escape", "module \"\\qgopkg.in/yaml.v3\"\n", false},
		{"trailing", "module "+goPurposeModule+" other\n", false},
		{"two-block-paths", "module (\n"+goPurposeModule+"\nexample.com/other\n)\n", false},
		{"unclosed-block", "module (\n"+goPurposeModule+"\n", false},
		{"nested-block", "module (\n(\n"+goPurposeModule+"\n)\n)\n", false},
		{"block-comment", "module /* ignored? */ "+goPurposeModule+"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goPurposeModuleDeclaration([]byte(tc.raw)); got != tc.want { t.Fatal("module declaration syntax mismatch", tc.name, got, tc.want) }
		})
	}
}

func TestGoPurposeQuotedSyntheticZIPKeepsIndependentH1Binding(t *testing.T) {
	prefix := goPurposeModule+"@"+goPurposeVersion+"/"
	for _, tc := range []struct { name, mod, moduleH1, modH1 string }{
		{"interpreted", "module \"gopkg.in/yaml.v3\"\n", "h1:B6xzZ5fhjc6KRcruT0r4O5IP+mrS4oyr56FbmopG9ag=", "h1:1ahYjIJ/HbvfI0hgGaHwMbBlYgTQyra1Zto1CL1k36Y="},
		{"raw", "module `gopkg.in/yaml.v3`\n", "h1:xDvk3CeSJ4aL5+t4RojoNcoRVBZcdFlKMq133BdEb1c=", "h1:GdWIPI89QlKa+8cJRC06eXNZgSIILKKB+YAvF32IUMY="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := []byte(tc.mod)
			archive := goPurposeFixtureArchive(t, []goPurposeFixture{{name:prefix+"go.mod", body:mod}, {name:prefix+"sample.go", body:[]byte("package synthetic\n")}}, zip.Deflate)
			proof := goModulePurposeProof{Producer:goPurposeProducer, Module:goPurposeModule, Version:goPurposeVersion, ModuleH1:tc.moduleH1, ModH1:tc.modH1, ArchiveSHA256:goPurposeSHA(archive), GoModSHA256:goPurposeSHA(mod)}
			result, err := validateGoModulePurpose(archive, mod, proof)
			if err != nil || result == nil { t.Fatal("synthetic quoted module rejected", err) }
			if !bytes.Equal(result.Archive, archive) || !bytes.Equal(result.GoMod, mod) { t.Fatal("different bytes returned") }
			clear(result.Archive); clear(result.GoMod)
			// Sums from an unquoted declaration must not authenticate quoted
			// bytes, even though they declare the same module identity.
			proof.ModuleH1, proof.ModH1 = fixtureModuleH1, fixtureGoModH1
			if value, err := validateGoModulePurpose(archive, mod, proof); err == nil || value != nil { t.Fatal("old h1 adopted for changed declaration bytes") }
		})
	}
}
