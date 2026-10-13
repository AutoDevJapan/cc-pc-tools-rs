package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testThread = "1554792868974039150"

func correlationTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if external := os.Getenv("CC_RECORD_RECOVERY_TEST_ROOT"); external != "" {
		var e error
		root, e = os.MkdirTemp(external, "correlate-test-")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	return root
}

func testAcquisition(t *testing.T, root string) obj {
	t.Helper()
	if e := privateDir(root); e != nil {
		t.Fatal(e)
	}
	q := message("1554792868974039151", testThread, recoveryBot, "架空の私的案件と秘密値\na 保存\nb 削除\n判断を教えてください", true)
	r := message("1554792868974039152", testThread, recoveryOwner, "a 架空の秘密値", false)
	r["message_reference"] = obj{"channel_id": testThread, "message_id": q["id"]}
	stage, meta := stageHistory(testThread, recoveryBot, recoveryOwner, []obj{q, r}, true)
	stage["guild_id"] = recoveryGuild
	stage["parent_id"] = requestForum
	if e := writeNew(filepath.Join(root, testThread+".private.json"), stage); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, testThread+".private.json"))
	meta["stage_sha256"] = digest(b)
	meta["status"] = "staged"
	report := obj{"kind": "original-record-report", "guild_id": recoveryGuild, "owner_id": recoveryOwner, "bot_id": recoveryBot, "threads": []any{meta}}
	if e := writeNew(filepath.Join(root, "report.json"), report); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(filepath.Join(root, "report.json"))
	return obj{"path": root, "report_sha256": digest(b), "report": report}
}

func TestCorrelationPreservesPrivateBodiesAndDoesNotApply(t *testing.T) {
	root := correlationTestRoot(t)
	source := filepath.Join(root, "source")
	jobs := filepath.Join(root, "jobs")
	if e := privateDir(source); e != nil {
		t.Fatal(e)
	}
	os.Mkdir(jobs, 0700)
	a := testAcquisition(t, filepath.Join(root, "acquired"))
	q := obj{"request_thread_key": testThread, "after": testThread, "job": nil}
	if e := writeNew(filepath.Join(source, "survey.private.json"), obj{"kind": "original-correspondence-survey", "acquisitions": []any{a}, "questions": []any{obj{"question": q}}}); e != nil {
		t.Fatal(e)
	}
	todo := filepath.Join(root, "todo.json")
	writeNew(todo, obj{"items": []any{obj{"id": float64(42), "jobs": []any{"worker"}, "title": "架空の秘密値"}}})
	os.Mkdir(filepath.Join(jobs, "worker"), 0700)
	writeNew(filepath.Join(jobs, "worker", "job.json"), obj{"id": "worker", "todo_id": float64(42), "discord_thread_id": testThread})
	before, _ := os.ReadFile(todo)
	out := filepath.Join(root, "output")
	report, e := correlate(source, root, todo, jobs, out)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(report)
	for _, private := range []string{"架空", "秘密値", testThread, root} {
		if strings.Contains(string(b), private) {
			t.Fatal("private content appeared in summary")
		}
	}
	if report["explicit_reference_bundles"] != 1 || report["deliveries"] != 0 || report["applications_verified"] != 0 || report["verified_decision_scopes"] != 0 {
		t.Fatal("receipt treated as application")
	}
	private, _, e := readJSON(filepath.Join(out, "correlation.private.json"))
	if e != nil {
		t.Fatal(e)
	}
	if len(array(private["reply_bundles"])) != 1 {
		t.Fatal("explicit body bundle missing")
	}
	body := str(object(object(array(private["reply_bundles"])[0])["owner_reply"])["content"])
	if body != "a 架空の秘密値" {
		t.Fatal("owner original body changed")
	}
	after, _ := os.ReadFile(todo)
	if string(before) != string(after) {
		t.Fatal("live ToDo changed")
	}
}

func TestAcquisitionTamperFailsBeforeOutput(t *testing.T) {
	root := correlationTestRoot(t)
	a := testAcquisition(t, filepath.Join(root, "acquired"))
	p := filepath.Join(str(a["path"]), testThread+".private.json")
	os.WriteFile(p, []byte(`{"changed":true}`), 0600)
	_, _, e := acquiredHistories(obj{"acquisitions": []any{a}})
	if e == nil || e.Error() != "acquisition_stage_mismatch" {
		t.Fatal("tampered private acquisition accepted")
	}
}

func TestOriginalRequestAndQuestionRecordsStaySeparate(t *testing.T) {
	root := correlationTestRoot(t)
	source := filepath.Join(root, "source")
	jobs := filepath.Join(root, "jobs")
	if e := privateDir(source); e != nil {
		t.Fatal(e)
	}
	os.Mkdir(jobs, 0700)
	a := testAcquisition(t, filepath.Join(root, "acquired"))
	rows := []any{obj{"record_kind": "question", "question": obj{"thread_id": testThread}}, obj{"record_kind": "request", "question": obj{"request_thread_key": testThread}}}
	writeNew(filepath.Join(source, "survey.private.json"), obj{"kind": "original-correspondence-survey", "acquisitions": []any{a}, "records": rows})
	todo := filepath.Join(root, "todo.json")
	writeNew(todo, obj{"items": []any{}})
	out := filepath.Join(root, "output")
	r, e := correlate(source, root, todo, jobs, out)
	if e != nil {
		t.Fatal(e)
	}
	if r["old_request_rows"] != 1 || r["old_question_rows"] != 1 || len(array(r["requests"])) != 1 {
		t.Fatal("old question counted as request")
	}
	v, _, e := readJSON(filepath.Join(out, "correlation.private.json"))
	if e != nil {
		t.Fatal(e)
	}
	if len(array(v["request_bundles"])) != 1 || len(array(v["question_record_bundles"])) != 1 {
		t.Fatal("mixed original record bundles")
	}
}

func TestAcquisitionRequiresPrivateOwnershipAndPermissions(t *testing.T) {
	root := correlationTestRoot(t)
	path := filepath.Join(root, "inherited")
	if e := os.Mkdir(path, 0755); e != nil {
		t.Fatal(e)
	}
	if verifyPrivateSource(path) == nil {
		t.Fatal("unverified acquisition folder accepted")
	}
	private := filepath.Join(root, "private")
	if e := privateDir(private); e != nil {
		t.Fatal(e)
	}
	if e := verifyPrivateSource(private); e != nil {
		t.Fatal(e)
	}
}

func TestConflictingAcquisitionsDoNotVerifyOrBundleReplies(t *testing.T) {
	root := correlationTestRoot(t)
	source := filepath.Join(root, "survey")
	jobs := filepath.Join(root, "jobs")
	if e := privateDir(source); e != nil {
		t.Fatal(e)
	}
	os.Mkdir(jobs, 0700)
	a := testAcquisition(t, filepath.Join(root, "first"))
	b := testAcquisition(t, filepath.Join(root, "second"))
	path := filepath.Join(str(b["path"]), testThread+".private.json")
	stage, _, e := readJSON(path)
	if e != nil {
		t.Fatal(e)
	}
	q := object(array(stage["messages"])[0])
	q["content"] = str(q["content"]) + " changed"
	q["content_sha256"] = digest([]byte(str(q["content"])))
	raw, _ := json.MarshalIndent(stage, "", "  ")
	os.WriteFile(path, raw, 0600)
	report := object(b["report"])
	object(array(report["threads"])[0])["stage_sha256"] = digest(raw)
	raw, _ = json.MarshalIndent(report, "", "  ")
	os.WriteFile(filepath.Join(str(b["path"]), "report.json"), raw, 0600)
	b["report_sha256"] = digest(raw)
	writeNew(filepath.Join(source, "survey.private.json"), obj{"kind": "original-correspondence-survey", "acquisitions": []any{a, b}, "records": []any{obj{"record_kind": "request", "question": obj{"request_thread_key": testThread}}}})
	todo := filepath.Join(root, "todo.json")
	writeNew(todo, obj{"items": []any{}})
	out := filepath.Join(root, "output")
	result, e := correlate(source, root, todo, jobs, out)
	if e != nil {
		t.Fatal(e)
	}
	if result["acquisition_content_conflicts"] != 1 || result["explicit_reference_bundles"] != 0 || object(array(result["requests"])[0])["history_verified"] != false {
		t.Fatal("conflicting evidence verified")
	}
	private, _, e := readJSON(filepath.Join(out, "correlation.private.json"))
	if e != nil {
		t.Fatal(e)
	}
	if len(array(private["reply_bundles"])) != 0 {
		t.Fatal("conflicting source bundled for use")
	}
}

func TestReferencesDifferEvenIfBodiesMatch(t *testing.T) {
	base := obj{"parent_id": requestForum, "messages": []any{obj{"id": "1", "content": "same", "content_sha256": digest([]byte("same")), "message_reference": obj{"message_id": "2"}}}}
	other := obj{"parent_id": requestForum, "messages": []any{obj{"id": "1", "content": "same", "content_sha256": digest([]byte("same")), "message_reference": obj{"message_id": "3"}}}}
	if messageFingerprint(base) == messageFingerprint(other) {
		t.Fatal("generation reference conflict collapsed")
	}
}

func TestPromptAssociationsAndQuestionRoutesNeverBecomeRequestOwners(t *testing.T) {
	job := obj{"id": "worker", "discord_thread_ids_source": "prompt", "discord_thread_ids": []any{testThread}}
	association := obj{"source": "prompt", "thread_ids": []any{testThread}, "question_thread_ids": []any{testThread}, "question_routes": obj{testThread: obj{"posting_job": "worker", "question_thread_id": testThread}}}
	req, q := jobThreadMatches(job, association, testThread)
	if req || !q {
		t.Fatal("prompt quote or question route became request ownership")
	}
	object(object(association["question_routes"])[testThread])["posting_job"] = "other"
	_, q = jobThreadMatches(job, association, testThread)
	if q {
		t.Fatal("wrong posting worker route accepted")
	}
}

func TestTodoCandidatesKeepAmbiguity(t *testing.T) {
	db := obj{"items": []any{obj{"id": float64(1), "jobs": []any{"worker"}}, obj{"id": float64(2), "jobs": []any{"worker"}}}}
	rows := todoCandidates(db, obj{"id": "worker", "todo_id": float64(1)})
	if len(rows) != 2 {
		t.Fatal("multiple original ToDos guessed into one")
	}
}

func TestMultiPartQuestionIsNotVerifiedFromStarterOnly(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "question-progress"), 0700)
	v := obj{"guild": recoveryGuild, "parent": "1554276307997692004", "sender": recoveryBot, "complete": true, "question_thread_id": testThread, "question": obj{"starter": "1554792868974039151"}, "text_parts": []any{"first", "second"}}
	writeNew(filepath.Join(root, "question-progress", "one.json"), v)
	stage := obj{"messages": []any{obj{"id": "1554792868974039151", "author_id": recoveryBot, "content": "first"}}}
	matched, n, e := savedQuestionRecords(root, map[string]obj{testThread: stage})
	if e != nil || n != 1 || len(matched) != 0 {
		t.Fatal("partial question mistaken for complete generation")
	}
}
