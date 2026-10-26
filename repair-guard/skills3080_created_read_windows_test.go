package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSkills3080CreatedReadersShareNativePublicationAndCurrentBuffers(t *testing.T) {
	for _, kind := range []string{"valid_after_update_offline", "installed_unavailable", "native_forged", "unknown_issuer", "wrong_revision", "wrong_review", "wrong_request", "local_manifest_missing", "manifest_changed", "source_changed", "creation_ID_changed"} {
		t.Run(kind, func(t *testing.T) {
			source, notice := []byte("original public source\n"), []byte("individual notice\n")
			acquired := skillsAcquiredFixture(t, source, notice, false)
			m := skillsManifestWithOriginals(t, source, notice)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			r := skillsRequest{Domain: "skills-3080-operation-v1", Writer: m.Writer, ManifestCommit: strings.Repeat("a", 40), ManifestPath: "docs/skills-3080-operations/synthetic.json", ManifestSHA: hashBytes(raw), Review: &skillsResultRequest{Task: 3800, Job: "20261025-100100-todo-3800", SpecSHA: strings.Repeat("b", 64), FinalSHA: strings.Repeat("c", 64)}}
			issuer := skillsProducer{"ccb5e797e6b5627d98ade2a562f2f98f3c121a5403e1523071afc07f73af4556", "038bc2570014132321919bf6758d68979506b741"}
			current := skillsProducer{strings.Repeat("d", 64), strings.Repeat("e", 40)}
			root := skillsFixtureRoot(t)
			creationFS := skillsFS{blocked: func(string) bool { return false }}
			if _, reason := skillsCreate(m, root, &creationFS); reason != "" {
				t.Fatal(reason)
			}
			e, ok := skillsIssueCreated(r, strings.Repeat("f", 64), m, root, &creationFS, issuer)
			creationFS.close()
			if !ok {
				t.Fatal("same-handle native issuance fixture")
			}
			c := skillsConsumeRequest{Creator: r.Writer, RequestPath: r.Writer.Workspace + "/.ci-work/created-request.json", RequestSHA: e.RequestSHA, CallID: "call_created_read_fixture", Indices: []int{0}}
			centralOpens, blobReads, nativeReads := 0, 0, 0
			fs := skillsFS{blocked: func(path string) bool {
				if within(root, path) {
					centralOpens++
				}
				return false
			}}
			defer fs.close()
			switch kind {
			case "unknown_issuer":
				e.Producer.SHA = strings.Repeat("9", 64)
			case "wrong_revision":
				e.Producer.Revision = strings.Repeat("9", 40)
			case "wrong_review":
				e.Review.FinalSHA = strings.Repeat("9", 64)
			case "wrong_request":
				e.RequestSHA = strings.Repeat("9", 64)
			case "creation_ID_changed":
				e.Files[0].ID.File = "not-a-file-id"
			}
			checks := skillsCreatedChecks{
				installed: func(*skillsFS) (skillsProducer, bool) { return current, kind != "installed_unavailable" },
				native: func(_ *skillsFS, _ object, request skillsConsumeRequest) (skillsCreatedEvidence, bool) {
					nativeReads++
					records := skillsNativeCreationFixture(t, e, request)
					if kind == "native_forged" {
						obj(records[1]["payload"])["input"] = `text({"qualified":true})`
					}
					var stream bytes.Buffer
					for _, row := range records {
						if err := json.NewEncoder(&stream).Encode(row); err != nil {
							t.Fatal(err)
						}
					}
					start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
					return skillsCreationNativeProjection(&stream, "synthetic-thread", request.Creator.Workspace, start, start.Add(time.Minute), request)
				},
				manifest: func(e skillsCreatedEvidence, r skillsRequest, sha string, current skillsProducer) ([]byte, bool) {
					return skillsCreatedPublicManifestWith(e, r, sha, current, func(path string) bool { return same(path, m.Writer.Workspace) }, func(_, _, _ string) ([]byte, bool) {
						blobReads++
						if kind == "manifest_changed" {
							return append(append([]byte(nil), raw...), '!'), true
						}
						return raw, kind != "local_manifest_missing"
					})
				},
				sources: func(got skillsManifest, fs *skillsFS) bool {
					sha := acquired.catalogSHA
					if kind == "source_changed" {
						sha = strings.Repeat("9", 64)
					}
					return skillsSourcesBoundAt(got, fs, acquired.root, sha, acquired.anchors)
				},
			}
			got, native, runtime, stage := skillsCreatedProvenanceWith(r, c, object{}, &fs, checks)
			if centralOpens != 0 {
				t.Fatal("provenance opened a central target before acceptance")
			}
			if kind != "valid_after_update_offline" {
				if stage == "" || skillsCreatedFailureReason(stage) == "" {
					t.Fatal("invalid provenance accepted")
				}
				if (kind == "unknown_issuer" || kind == "wrong_revision" || kind == "wrong_review" || kind == "wrong_request" || kind == "native_forged" || kind == "installed_unavailable") && blobReads != 0 {
					t.Fatal("unaccepted issuer opened a manifest")
				}
				if kind == "installed_unavailable" && nativeReads != 0 {
					t.Fatal("unavailable installed runtime opened native input")
				}
				if kind == "source_changed" && stage != "source_catalog_integrity" {
					t.Fatalf("source failure collapsed: %s", stage)
				}
				return
			}
			if stage != "" || runtime != current || native.Producer != issuer || blobReads != 1 || nativeReads != 1 {
				t.Fatalf("valid existing creation rejected at %s", stage)
			}
			result, reason := skillsConsumeCreatedCurrent(got, r, c.RequestSHA, native, root, &fs, c.Indices, runtime)
			if reason != "" || result["same_read_buffer_consumed"] != true || result["creation_id_matches_current"] != true || result["general_access_granted"] != false || result["historical_physical_continuity_proven"] != false || result["current_os_writer"] != "unproved" {
				t.Fatalf("same native/current object consumption failed: %s", reason)
			}
			files := result["files"].([]object)
			if centralOpens == 0 || len(files) != 1 || files[0]["content_utf8"] != string(m.Files[0].body) {
				t.Fatal("success did not consume the actual pinned current buffer")
			}
		})
	}
}
