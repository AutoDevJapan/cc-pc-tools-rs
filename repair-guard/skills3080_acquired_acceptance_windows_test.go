//go:build skills3080_acquired_acceptance

package main

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit finite acceptance against the original retained public collection.
// This checks provenance only. It never creates a Skill, grants license scope,
// fetches a missing object, or reads any existing central Skill.
func TestSkills3080OriginalAcquiredMetadataAndObjects(t *testing.T) {
	cases := []struct{ repo, path, source, noticePath, notice string }{
		{"mattpocock/skills", "skills/engineering/codebase-design/SKILL.md", "2c20617f87ec8af6a434859f381b2f061a69b530444e74eb39e78bb016a6d1e2", "LICENSE", "0e7ac423bf2c6e223b7c5b156f8cf72da49d748e56a1641402c31f22ad07dbb5"},
		{"6mile/skills", "skills/0xspeter/klaviyo-1-0-4/SKILL.md", "c655611b4780d5b42e99ce3e6862602eb6d1fb644e61e1d0d0ee16e18da23092", "skills/0xspeter/klaviyo-1-0-4/LICENSE.txt", "ec9e952fec4d029fab649746d033bf91802b15f59ffa40a0d3a3a0e1a1587802"},
	}
	for _, sample := range cases {
		t.Run(sample.repo, func(t *testing.T) {
			fs := skillsFS{blocked: skillsBlocked}
			defer fs.close()
			m := skillsParsedFixture(t)
			m.Sources = []skillsSource{{Repository: sample.repo, Commit: skillsPublicSources[sample.repo], Path: sample.path, SHA: sample.source, Acquisition: "already-acquired", License: skillsLicense{Path: sample.noticePath, SHA: sample.notice, Scope: "provenance-only acceptance fixture; no adoption or deployment grant", Accepted: true}}}
			for i, digest := range []string{sample.source, sample.notice} {
				path := filepath.Join(skillsCorpusRoot, "github", strings.ReplaceAll(sample.repo, "/", "__"), "objects", digest+".gz")
				stream, ok := fs.open(path, 4<<20)
				if !ok {
					t.Fatal("selected original object unavailable; no fetch attempted")
				}
				z, e := gzip.NewReader(stream)
				if e != nil {
					t.Fatal("invalid selected saved object")
				}
				raw, e := io.ReadAll(io.LimitReader(z, (2<<20)+1))
				closeErr := z.Close()
				if e != nil || closeErr != nil || hashBytes(raw) != digest {
					t.Fatal("selected object changed")
				}
				m.Files = append(m.Files, skillsOutput{Target: []string{"sample/references/original.md", "sample/references/NOTICE.txt"}[i], SHA: digest, Content: base64.StdEncoding.EncodeToString(raw)})
			}
			raw, e := json.Marshal(m)
			if e != nil {
				t.Fatal(e)
			}
			parsed, ok := skillsParseManifest(raw)
			if !ok || !skillsSourcesBound(parsed, &fs) {
				t.Fatal("actual fixed source/index/tree/object correspondence failed")
			}
			t.Logf("actual source and individual notice matched; manifest_sha256=%s; central_reads=0 writes=0 fetches=0", hashBytes(raw))
		})
	}
}
