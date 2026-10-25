package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills3080CreatorConsumeAfterProducerUpdate(t *testing.T) {
	for _, kind := range []string{"same_producer", "accepted_03a", "accepted_29118", "unknown_producer", "wrong_revision", "other_writer", "other_request", "other_manifest", "other_review", "old_receipt", "missing_file", "changed_bytes", "replacement", "hardlink", "protected_leaf"} {
		t.Run(kind, func(t *testing.T) {
			m, creation, evidence, root := skillsForwardFixture(t)
			sha := evidence.RequestSHA
			current := evidence.Producer
			if kind != "same_producer" {
				evidence.Producer = skillsProducer{"03a2e0f115c8435b1a876eda3efba75ba6120a668bfed39539f6ef2503c5f165", "3154c8ac9135424e2caf2034dc24d4e7406d6368"}
			}
			path := filepath.Join(root, filepath.FromSlash(m.Files[0].Target))
			blocked := func(string) bool { return false }
			switch kind {
			case "accepted_29118":
				evidence.Producer = skillsProducer{"29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb", "e1a4c21dc6241c140d4d7364d34f42b16d7005be"}
			case "unknown_producer":
				evidence.Producer.SHA = strings.Repeat("a", 64)
			case "wrong_revision":
				evidence.Producer.Revision = strings.Repeat("a", 40)
			case "other_writer":
				evidence.Writer.Job = "20261026-010000-todo-3080"
			case "other_request":
				evidence.RequestSHA = strings.Repeat("a", 64)
			case "other_manifest":
				evidence.ManifestSHA = strings.Repeat("a", 64)
			case "other_review":
				evidence.Review.FinalSHA = strings.Repeat("a", 64)
			case "old_receipt":
				evidence.Domain = "skills-3080-created-v1"
			case "missing_file":
				evidence.Files = evidence.Files[:0]
			case "changed_bytes":
				b := append([]byte(nil), m.Files[0].body...)
				b[0] ^= 1
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := os.Rename(path, filepath.Join(root, "retained-original")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, m.Files[0].body, 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			case "protected_leaf":
				blocked = func(p string) bool { return same(p, path) }
			}
			fs := skillsFS{blocked: blocked}
			defer fs.close()
			result, reason := skillsConsumeCreatedCurrent(m, creation, sha, evidence, root, &fs, []int{0}, current)
			positive := kind == "same_producer" || kind == "accepted_03a" || kind == "accepted_29118"
			if positive {
				if reason != "" || result["qualified"] != true {
					t.Fatalf("known issued output rejected: %s", reason)
				}
				files := result["files"].([]object)
				if len(files) != 1 || files[0]["content_utf8"] != string(m.Files[0].body) || result["producer"] != evidence.Producer || result["same_read_buffer_consumed"] != true || result["historical_physical_continuity_proven"] != false || result["current_os_writer"] != "unproved" || result["general_access_granted"] != false {
					t.Fatal("current buffer or original issuer identity lost")
				}
				if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
					f.Close()
					t.Fatal("verified consuming handle allowed modification")
				}
			} else {
				if reason == "" || result != nil {
					t.Fatal("unqualified output returned content")
				}
				bindingNegative := kind != "changed_bytes" && kind != "replacement" && kind != "hardlink" && kind != "protected_leaf"
				if bindingNegative && (reason != "creation_evidence" || len(fs.files) != 0 || len(fs.dirs) != 0) {
					t.Fatal("unqualified receipt reached target filesystem")
				}
				if kind == "protected_leaf" && (reason != "protection" || len(fs.files) != 0 || len(fs.dirs) != 0) {
					t.Fatal("protection did not precede target IO")
				}
				if kind == "changed_bytes" && reason != "current_bytes" {
					t.Fatalf("accepted issuance did not check current content: %s", reason)
				}
				if (kind == "replacement" || kind == "hardlink") && reason != "current_identity" {
					t.Fatalf("accepted issuance did not check current identity: %s", reason)
				}
			}
		})
	}
}
