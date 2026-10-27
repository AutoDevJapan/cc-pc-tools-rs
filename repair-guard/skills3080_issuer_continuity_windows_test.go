package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type skillsNativeProducerAnchor struct {
	name, sha, revision, job, call string
}

func skillsPublishedNativeProducer(t *testing.T, anchor skillsNativeProducerAnchor) skillsProducer {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", anchor.name))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		SHA      string `json:"creator_runtime_sha256"`
		Revision string `json:"creator_runtime_revision"`
		Call     string `json:"native_call_id"`
		Original string `json:"original_CLI_stdout_sha256"`
		Count    int    `json:"successful_CreateNew_count"`
		Job      string `json:"job"`
	}
	if json.Unmarshal(raw, &receipt) != nil || !skillsDigest(receipt.SHA) || !skillsGitObjectID(receipt.Revision) || !skillsCallID.MatchString(receipt.Call) || !skillsDigest(receipt.Original) || receipt.Count != 1 {
		t.Fatal("independently published original native issuance absent")
	}
	if receipt.SHA != anchor.sha || receipt.Revision != anchor.revision || receipt.Job != anchor.job || receipt.Call != anchor.call {
		t.Fatal("published JSON changed the independently fixed original software/job/call anchor")
	}
	return skillsProducer{receipt.SHA, receipt.Revision}
}

func TestSkills3080PublishedNativeIssuersSurviveSoftwareUpdate(t *testing.T) {
	for _, anchor := range []skillsNativeProducerAnchor{
		{"skills-3080-craft-create-evidence214717.json", "02c8cad70e44f2bc4cd4e5a70dfdaf581a7ee6896eb63449fe1606eb28f5b5be", "f30f9e763e25884fa48d1cce0cac22da1cebfb69", "20261026-175504-todo-3080", "call_183eb93d62314857ac45e22e1052655d"},
		{"skills-3080-openai6-create-evidence214717.json", "434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f", "521c4c4dddd7abbb0135d13bffb380ec984374b8", "20261026-214717-todo-3080", "call_7e3cd035c2bd458f94f744abae0ef5ce"},
	} {
		t.Run(anchor.name, func(t *testing.T) {
			issuer := skillsPublishedNativeProducer(t, anchor)
			for _, kind := range []string{"valid_public_object", "unknown_sha", "different_revision", "other_writer", "other_request", "unverified_root", "changed_public_bytes"} {
				t.Run(kind, func(t *testing.T) {
					m, r, e, _ := skillsForwardFixture(t)
					body := []byte("fixed independently published public manifest bytes")
					r.ManifestSHA = hashBytes(body)
					e.ManifestSHA = r.ManifestSHA
					e.Producer = issuer
					requestSHA := e.RequestSHA
					current := skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}
					switch kind {
					case "unknown_sha":
						e.Producer.SHA = strings.Repeat("9", 64)
					case "different_revision":
						e.Producer.Revision = strings.Repeat("a", 40)
					case "other_writer":
						e.Writer.Job = "20261027-000001-todo-3080"
					case "other_request":
						requestSHA = strings.Repeat("b", 64)
					case "changed_public_bytes":
						body = append(body, '!')
					}
					reads := 0
					got, ok := skillsCreatedPublicManifestWith(e, r, requestSHA, current, func(root string) bool {
						return kind != "unverified_root" && same(root, m.Writer.Workspace)
					}, func(root, commit, path string) ([]byte, bool) {
						reads++
						if !same(root, m.Writer.Workspace) || commit != r.ManifestCommit || path != r.ManifestPath {
							t.Fatal("public identity changed")
						}
						return body, true
					})
					if ok != (kind == "valid_public_object") {
						t.Fatalf("published issuer %s accepted=%v", kind, ok)
					}
					if ok && (reads != 1 || hashBytes(got) != r.ManifestSHA) {
						t.Fatal("accepted issuer did not reach the exact public bytes")
					}
					if kind != "valid_public_object" && kind != "changed_public_bytes" && reads != 0 {
						t.Fatal("known software identity bypassed a data-access boundary")
					}
				})
			}
		})
	}
}
