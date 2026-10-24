package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resumeNativeFixture() (resumeNativeSource, string) {
	header := `{"type":"session_meta","payload":{"id":"original-session","source":"vscode","cwd":"D:/original-owner-work"}}`
	message := `{"timestamp":"2026-10-24T11:26:20.672Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"original current request"}]}}`
	p := resumeNativeSource{Session: "original-session", Cwd: "D:/original-owner-work", Line: 3,
		HeaderSHA: hashBytes([]byte(header)), MessageSHA: hashBytes([]byte(message)), At: "2026-10-24T11:26:20.672Z"}
	return p, header + "\n{}\n" + message + "\n"
}

func TestRequestedResumeLiveOriginalRecordDiagnostic(t *testing.T) {
	if os.Getenv("CC_RESUME3650_LIVE_CHECK") != "1" {
		t.Skip("finite live metadata acceptance was not requested")
	}
	db, err := readObject(productStore, 64<<20)
	if err != nil || !resume3650SourceQualified(db) {
		t.Fatal("current original source qualification failed")
	}
	r := verifyOriginalProductRecords(db, 1626, productIO{readObject, readBounded, exactPhysical, sourceOriginVerified, requested3650OriginalSessions, time.Now()}, object{"verified": false, "reason": "record_mismatch"})
	t.Logf("original_record_verified=%v reason=%v", r["verified"], r["reason"])
	if r["verified"] != true {
		t.Fatal("new source could not verify the preserved original session")
	}
}

func TestRequestedResumeKeepsOriginalRegistrationAndStops(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "original.md")
	if os.WriteFile(spec, []byte("original task bytes"), 0600) != nil {
		t.Fatal("spec fixture write failed")
	}
	p := currentProduct{task: 3277, project: "original", root: root, spec: spec, specSHA: hashBytes([]byte("original task bytes")), repository: "autodevjapan/original", repositoryID: 100, owner: "AutoDevJapan"}
	for _, kind := range []string{"pass", "source", "identity", "origin", "catalog_client", "catalog_unknown", "protected", "physical", "body", "project", "root", "spec", "stop", "owner_stop", "done", "recovery", "original"} {
		t.Run(kind, func(t *testing.T) {
			row := object{"id": float64(p.task), "project": p.project, "spec": spec, "status": "todo", "dispatchable": true}
			db := object{"projects": object{p.project: root}, "items": []any{row}}
			a := requestedResumeAccess{
				source:   func(object) bool { return kind != "source" },
				plain:    func(string, bool) bool { return kind != "physical" },
				denied:   func(string) bool { return kind == "protected" },
				identity: func(currentProduct) bool { return kind != "identity" },
				origin:   func(string, string) bool { return kind != "origin" }, spec: currentQualification,
				member: func(string, string) string {
					if kind == "catalog_client" {
						return "client_project"
					}
					if kind == "catalog_unknown" {
						return ""
					}
					return "self"
				},
				original: func(object) bool { return kind != "original" }, now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
			}
			q := p
			switch kind {
			case "body":
				q.specSHA = hashBytes([]byte("changed"))
			case "project", "spec":
				row[kind] = "other"
			case "root":
				obj(db["projects"])[p.project] = "D:/other"
			case "stop":
				row["stopped"] = true
			case "owner_stop":
				row["owner_stopped"] = true
			case "done", "recovery":
				row["status"] = kind
			case "original":
				q.task = 1626
				row["id"] = float64(q.task)
			}
			before, _ := json.Marshal(db)
			if got := requestedResumeQualified(db, q, a); got != (kind == "pass") {
				t.Fatalf("qualification = %v", got)
			}
			after, _ := json.Marshal(db)
			if string(before) != string(after) {
				t.Fatal("qualification changed original task state")
			}
		})
	}
	for _, unknown := range []int{0, 2407, 2467, 2913, 3650, 99999} {
		if _, ok := requested3650Product(unknown); ok {
			t.Fatal("unknown task admitted")
		}
	}
	for _, leaf := range []string{"done.txt", "runs/2026-10-24.json", "daily-prompt.md", "state.json"} {
		if requestedResumePathAllowed(filepath.Join("D:/CC/Tools/x-bookmarks", filepath.FromSlash(leaf))) {
			t.Fatal("scheduler location qualification became X operational data access")
		}
	}
}

func TestRequestedResumeNativeChecksOriginalUserSource(t *testing.T) {
	p, raw := resumeNativeFixture()
	if !resumeNativeMatches(strings.NewReader(raw+"later append\n"), p) {
		t.Fatal("original native user event was rejected")
	}
	for _, kind := range []string{"assistant", "tool", "subagent", "session", "cwd", "timestamp", "hash", "header_hash", "line", "truncated", "json"} {
		t.Run(kind, func(t *testing.T) {
			q, input := p, raw
			lines := strings.Split(input, "\n")
			var h, m object
			if json.Unmarshal([]byte(lines[0]), &h) != nil || json.Unmarshal([]byte(lines[2]), &m) != nil {
				t.Fatal("invalid fixture")
			}
			switch kind {
			case "assistant", "tool":
				obj(m["payload"])["role"] = kind
			case "subagent":
				obj(h["payload"])["source"] = object{"subagent": object{"thread_spawn": object{"parent_thread_id": "original-session"}}}
			case "session":
				obj(h["payload"])["id"] = "other-session"
			case "cwd":
				obj(h["payload"])["cwd"] = "D:/other"
			case "timestamp":
				m["timestamp"] = "2026-10-24T11:26:21.672Z"
			}
			hb, _ := json.Marshal(h)
			mb, _ := json.Marshal(m)
			// Re-pin fixture bytes too: role/source/identity checks must have
			// substance beyond merely comparing the caller's expected hash.
			q.HeaderSHA, q.MessageSHA = hashBytes(hb), hashBytes(mb)
			input = string(hb) + "\n{}\n" + string(mb) + "\n"
			switch kind {
			case "hash":
				q.MessageSHA = strings.Repeat("a", 64)
			case "header_hash":
				q.HeaderSHA = strings.Repeat("a", 64)
			case "line":
				q.Line = 2
			case "truncated":
				input = string(hb) + "\n{}\n"
			case "json":
				input = string(hb) + "\n{}\nbad json\n"
				q.MessageSHA = hashBytes([]byte("bad json"))
			}
			if resumeNativeMatches(strings.NewReader(input), q) {
				t.Fatal("a quote, different native source or changed original event was accepted")
			}
		})
	}
}
