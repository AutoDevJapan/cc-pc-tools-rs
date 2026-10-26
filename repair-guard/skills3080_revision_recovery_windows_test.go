package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This fixture contains only the explicitly requested public call/output
// projection. A test copy is never used by the live canonical-session reader.
func skillsKnownPrepareFixture(t *testing.T) ([]byte, skillsConsumeRequest, time.Time, time.Time) {
	t.Helper()
	raw, err := os.ReadFile("testdata/skills3080-known-prepare090101.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	w := skillsWriter{Task: 3080, Job: "20261026-090101-todo-3080", SpecSHA: "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", Workspace: `D:\CC\Work\unattended-operations-20261013-wt\todo-3080-16-skills-3048`}
	c := skillsConsumeRequest{Creator: w, RequestPath: w.Workspace + `\.ci-work\todo3080\design-reference-own-revision1-prepare-request090101.json`, RequestSHA: "f05d30623e76c8cc886017877311340143a5ef1e3bc4f938051dbb83f91fb802", CallID: "call_31fa406e37984d8c8b532a62dd25b922"}
	start, _ := time.Parse(time.RFC3339, "2026-10-26T00:02:56Z")
	return raw, c, start, start.Add(6 * time.Hour)
}

func TestSkills3080KnownPrepareRejectsOtherEvidence(t *testing.T) {
	for _, kind := range []string{"creator", "spec", "request", "path", "call", "thread", "cwd", "time", "late", "operation", "input", "blocks", "stdout", "producer", "pending_id", "duplicate", "missing_output", "canonical_copy", "null", "alias", "extra_syntax"} {
		t.Run(kind, func(t *testing.T) {
			raw, c, start, end := skillsKnownPrepareFixture(t)
			var records []object
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
				v, ok := conversationJSON(line)
				if !ok {
					t.Fatal("fixture")
				}
				records = append(records, v)
			}
			thread, cwd, op := "01a16bab-c238-7293-953d-d0f447cf2c05", c.Creator.Workspace, "prepare-skills3080-revision"
			call, response := obj(records[1]["payload"]), obj(records[2]["payload"])
			switch kind {
			case "creator":
				c.Creator.Job = "20261026-090102-todo-3080"
			case "spec":
				c.Creator.SpecSHA = strings.Repeat("a", 64)
			case "request":
				c.RequestSHA = strings.Repeat("a", 64)
			case "path":
				c.RequestPath += ".copy"
			case "call":
				c.CallID = "call_other"
			case "thread":
				thread = "other"
			case "cwd":
				cwd += "."
			case "time":
				records[1]["timestamp"] = "2026-10-26T05:40:15.631Z"
			case "late":
				end = start
			case "operation":
				op = "apply-skills3080-revision"
			case "input":
				call["input"] = str(call["input"]) + " "
			case "blocks":
				response["output"].([]any)[0] = object{"type": "input_text", "text": "Script completed\nWall time 2.0 seconds\nOutput:\n"}
			case "stdout", "producer", "pending_id":
				blocks := response["output"].([]any)
				block := obj(blocks[1])
				wrapper, _ := conversationJSON([]byte(str(block["text"])))
				result, _ := conversationJSON([]byte(str(wrapper["output"])))
				if kind == "stdout" {
					wrapper["output"] = str(wrapper["output"]) + " "
				} else {
					e := obj(result["revision_evidence"])
					if kind == "producer" {
						obj(e["producer"])["sha256"] = strings.Repeat("a", 64)
					} else {
						obj(obj(e["pending"])["object"])["file_id_128"] = strings.Repeat("a", 32)
					}
					b, _ := json.Marshal(result)
					wrapper["output"] = string(b) + "\n"
				}
				b, _ := json.Marshal(wrapper)
				block["text"] = string(b)
			case "duplicate":
				records = append(records, records[1])
			case "missing_output":
				records = records[:2]
			case "canonical_copy":
				call["input"] = skillsRevisionInvocation(op, c.RequestPath, cwd)
				call["type"] = "message"
			case "null":
				call["input"] = strings.Replace(str(call["input"]), `"login":false`, `"login":null`, 1)
			case "alias":
				call["input"] = strings.Replace(str(call["input"]), "scrub391.exe", "scrub3~1.exe", 1)
			case "extra_syntax":
				call["input"] = "const r=" + str(call["input"])
			}
			var stream bytes.Buffer
			enc := json.NewEncoder(&stream)
			enc.SetEscapeHTML(false)
			// Preserve the original raw output for all cases except explicit mutations;
			// remarshal would change the pinned original tool-block representation.
			for i, r := range records {
				if i == 2 && kind != "blocks" && kind != "stdout" && kind != "producer" && kind != "pending_id" {
					var env map[string]json.RawMessage
					json.Unmarshal(bytes.Split(bytes.TrimSpace(raw), []byte("\n"))[2], &env)
					if kind != "duplicate" && kind != "missing_output" {
						stream.Write(bytes.Split(bytes.TrimSpace(raw), []byte("\n"))[2])
						stream.WriteByte('\n')
						continue
					}
				}
				enc.Encode(r)
			}
			if _, ok := skillsRevisionNativeProjection(&stream, thread, cwd, start, end, c, op); ok {
				t.Fatal("other evidence accepted")
			}
		})
	}
}

func TestSkills3080PrepareChecksInFlightCanonicalCall(t *testing.T) {
	_, c, start, end := skillsKnownPrepareFixture(t)
	for _, kind := range []string{"valid", "valid_resumed", "doubled", "completed", "replayed", "wrong_path", "variable", "extra_call", "other_exe", "ads", "short_name", "duplicate", "missing_session", "early", "late", "yielded", "null", "missing_login"} {
		t.Run(kind, func(t *testing.T) {
			records := skillsRevisionNativeFixture(skillsRevisionEvidence{}, c, "prepare-skills3080-revision")[:2]
			records[1]["timestamp"] = "2026-10-26T05:40:15.630Z"
			call := obj(records[1]["payload"])
			switch kind {
			case "valid_resumed":
				older := object{"type": "response_item", "timestamp": start.Add(-time.Minute).Format(time.RFC3339), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": "call_previous_job", "input": "unrelated old call"}}
				records = []object{records[0], older, records[1]}
			case "doubled":
				call["input"] = strings.ReplaceAll(str(call["input"]), `\\`, `\\\\`)
			case "completed", "yielded":
				records = append(records, object{"type": "response_item", "timestamp": "2026-10-26T05:40:16Z", "payload": object{"type": "custom_tool_call_output", "call_id": c.CallID}})
			case "replayed":
				records = append(records, records[1], records[1])
			case "wrong_path":
				call["input"] = skillsRevisionInvocation("prepare-skills3080-revision", c.RequestPath+".copy", c.Creator.Workspace)
			case "variable":
				call["input"] = "const command=" + str(call["input"])
			case "extra_call":
				call["input"] = str(call["input"]) + str(call["input"])
			case "other_exe":
				call["input"] = strings.Replace(str(call["input"]), "scrub391.exe", "other.exe", 1)
			case "ads":
				call["input"] = strings.Replace(str(call["input"]), "request090101.json", "request090101.json:stream", 1)
			case "short_name":
				call["input"] = strings.Replace(str(call["input"]), "scrub391.exe", "scrub3~1.exe", 1)
			case "duplicate":
				records = append(records, records[1])
			case "missing_session":
				records = records[1:]
			case "early":
				records[1]["timestamp"] = start.Add(-time.Second).Format(time.RFC3339)
			case "late":
				records[1]["timestamp"] = end.Add(time.Second).Format(time.RFC3339)
			case "null":
				call["input"] = strings.Replace(str(call["input"]), `"login":false`, `"login":null`, 1)
			case "missing_login":
				call["input"] = strings.Replace(str(call["input"]), `"login":false,`, "", 1)
			}
			var stream bytes.Buffer
			for _, r := range records {
				json.NewEncoder(&stream).Encode(r)
			}
			if ok := skillsRevisionPendingCall(&stream, "synthetic-thread", c.Creator.Workspace, c.RequestPath, start, end); ok != (kind == "valid" || kind == "valid_resumed") {
				t.Fatalf("inflight call accepted=%v", ok)
			}
		})
	}
}

func TestSkills3080PreparePreflightMustMatchReceipt(t *testing.T) {
	for _, kind := range []string{"valid", "old_same_call_id", "old_orphan_output", "orphan_output", "duplicate_envelope_key", "duplicate_payload_key"} {
		t.Run(kind, func(t *testing.T) {
			_, c, start, end := skillsKnownPrepareFixture(t)
			evidence := skillsRevisionEvidence{Writer: c.Creator, RequestSHA: c.RequestSHA, ManifestSHA: skillsRevisionManifestSHA}
			records := skillsRevisionNativeFixture(evidence, c, "prepare-skills3080-revision")
			if kind == "old_same_call_id" || kind == "old_orphan_output" || kind == "orphan_output" {
				older := object{"type": "response_item", "timestamp": start.Add(-time.Minute).Format(time.RFC3339), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": c.CallID, "input": "old call"}}
				if kind != "old_same_call_id" {
					obj(older["payload"])["type"] = "custom_tool_call_output"
				}
				if kind == "orphan_output" {
					older["timestamp"] = start.Add(time.Second).Format(time.RFC3339)
				}
				records = []object{records[0], older, records[1], records[2]}
			}
			var pending bytes.Buffer
			for _, r := range records[:len(records)-1] {
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if obj(r["payload"])["type"] == "custom_tool_call" && obj(r["payload"])["input"] != "old call" {
					if kind == "duplicate_envelope_key" {
						b = bytes.Replace(b, []byte(`"timestamp":`), []byte(`"timestamp":"2026-10-26T00:00:00Z","timestamp":`), 1)
					}
					if kind == "duplicate_payload_key" {
						b = bytes.Replace(b, []byte(`"call_id":`), []byte(`"call_id":"call_earlier","call_id":`), 1)
					}
				}
				pending.Write(b)
				pending.WriteByte('\n')
			}
			preflight := skillsRevisionPendingCall(bytes.NewReader(pending.Bytes()), "synthetic-thread", c.Creator.Workspace, c.RequestPath, start, end)
			complete := append([]byte(nil), pending.Bytes()...)
			b, _ := json.Marshal(records[len(records)-1])
			complete = append(complete, b...)
			complete = append(complete, '\n')
			_, receivable := skillsRevisionNativeProjection(bytes.NewReader(complete), "synthetic-thread", c.Creator.Workspace, start, end, c, "prepare-skills3080-revision")
			if preflight != (kind == "valid") || preflight != receivable {
				t.Fatalf("preflight=%v but complete receipt=%v", preflight, receivable)
			}
		})
	}
}

func TestSkills3080RecoveredStateAndUnchangedFiles(t *testing.T) {
	for _, kind := range []string{"prepared", "archived", "published", "changed_unaffected", "replaced_unaffected", "pending_bytes", "pending_id", "pending_hardlink", "history_id", "protected", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			old, next, original, root := skillsRevisionFixture(t)
			fs := skillsFS{blocked: func(string) bool { return false }}
			e, reason := skillsRevisionPrepare(old, original, root, next, &fs)
			fs.close()
			if reason != "" {
				t.Fatal(reason)
			}
			proof := skillsRevisionRecoveryAudit()
			e.Recovery = &proof
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(root, filepath.FromSlash(original.Files[1].Target))
			pending := filepath.Join(root, filepath.FromSlash(skillsRevisionPending))
			switch kind {
			case "changed_unaffected":
				must(os.WriteFile(target, []byte("changed"), 0600))
			case "replaced_unaffected":
				must(os.Rename(target, target+".held"))
				must(os.WriteFile(target, old.Files[1].body, 0600))
			case "pending_bytes":
				must(os.WriteFile(pending, []byte("changed"), 0600))
			case "pending_id":
				must(os.Rename(pending, pending+".held"))
				must(os.WriteFile(pending, next.Files[27].body, 0600))
			case "pending_hardlink":
				must(os.Link(pending, pending+".alias"))
			case "history_id":
				e.History.ID.File = strings.Repeat("f", 32)
			case "archived", "published", "stopped":
				fs = skillsFS{blocked: func(string) bool { return false }}
				changes, r := skillsRevisionApply(e, original, root, &fs, func(s string) bool {
					if kind == "archived" {
						return s != "after_archive"
					}
					if kind == "stopped" {
						return false
					}
					return true
				})
				fs.close()
				if kind == "published" && (changes != 2 || r != "") {
					t.Fatal("publish fixture")
				}
			}
			fs = skillsFS{blocked: func(p string) bool { return kind == "protected" && same(p, pending) }}
			reason = skillsRevisionRecoveryFiles(e, original, root, &fs)
			positive := kind == "prepared" || kind == "archived" || kind == "published" || kind == "stopped"
			if (reason == "") != positive {
				fs.close()
				t.Fatalf("state accepted=%v reason=%s", positive, reason)
			}
			if positive {
				if f, err := os.OpenFile(target, os.O_WRONLY, 0); err == nil {
					f.Close()
					fs.close()
					t.Fatal("unaffected object not locked")
				}
				n, r := skillsRevisionApply(e, original, root, &fs, func(string) bool { return true })
				if r != "" || (kind == "published" && n != 0) {
					fs.close()
					t.Fatalf("resume %d/%s", n, r)
				}
			}
			fs.close()
		})
	}
}

func TestSkills3080KnownPrepareRecovery(t *testing.T) {
	raw, c, start, end := skillsKnownPrepareFixture(t)
	e, ok := skillsRevisionNativeProjection(bytes.NewReader(raw), "01a16bab-c238-7293-953d-d0f447cf2c05", c.Creator.Workspace, start, end, c, "prepare-skills3080-revision")
	if !ok || e.RequestSHA != c.RequestSHA || e.Pending.Size != 10097 || e.Original.Size != 8896 {
		t.Fatal("the fixed issued prepare cannot resume")
	}
}

func TestSkills3080KnownPrepareProducerSurvivesUpdate(t *testing.T) {
	old := skillsProducer{"3a0f5065eb6ae56fdcc0a1f0ef78131f999e2331a0412b01faea91260adffeaa", "a067ff63db64a88fc218fc4c8400552454d5d691"}
	current := skillsProducer{strings.Repeat("a", 64), strings.Repeat("b", 40)}
	if !skillsAcceptedProducer(old, current) {
		t.Fatal("previously deployed producer was lost")
	}
	old.Revision = strings.Repeat("c", 40)
	if skillsAcceptedProducer(old, current) {
		t.Fatal("hash alone admitted another revision")
	}
}
