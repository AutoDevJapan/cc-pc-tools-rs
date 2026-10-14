package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalDeliveryVerifiesOnlyExactSinglePartGeneration(t *testing.T) {
	q := obj{"id": "1554792868974039151", "thread_id": testThread, "content": "架空の非公開の質問\na 保存\nb 停止"}
	ledger := obj{"thread_id": testThread, "first": q["id"], "next": float64(1), "complete": true, "attachment_hashes": []any{}, "fingerprint": digest([]byte(str(q["content"])))}
	if !exactDeliveredQuestion(ledger, q) {
		t.Fatal("exact original posted generation not recognized")
	}
	for _, change := range []obj{{"first": "1554792868974039152"}, {"thread_id": "1554792868974039159"}, {"next": float64(2)}, {"complete": false}, {"attachment_hashes": []any{obj{"hash": "unverified"}}}, {"fingerprint": "wrong"}, {"attachment_hashes": nil}} {
		copy := obj{}
		for k, v := range ledger {
			copy[k] = v
		}
		for k, v := range change {
			copy[k] = v
		}
		if exactDeliveredQuestion(copy, q) {
			t.Fatal("cursor, partial body or attachment evidence promoted to generation proof")
		}
	}
}

func TestPostingFooterMustBeTerminalOutsideQuotesAndCode(t *testing.T) {
	line := "-# ジョブ worker。続きはこのスレッドに返信してください。"
	if postingJob("質問\n"+line) != "worker" {
		t.Fatal("canonical footer missing")
	}
	if postingJob("> 引用\n\n"+line) != "worker" || postingJob("```\n> コード\n```\n"+line) != "worker" {
		t.Fatal("footer after quote boundary or closed code missing")
	}
	for _, body := range []string{"", "\n", "> 引用\n" + line} {
		if postingJob(body) != "" {
			t.Fatal("empty or lazy quote became identity")
		}
	}
	for _, body := range []string{"質問\n> " + line, "質問\n```\n" + line, "質問\n    " + line, "    " + line, "\t" + line, "質問\n````\n```\n" + line, "質問\n```\n~~~\n" + line, "質問\n```\n```text\n" + line, "質問\n" + line + "\n別の文", "質問\n-# ジョブ ../outside。続きはこのスレッドに返信してください。"} {
		if postingJob(body) != "" {
			t.Fatal("quoted or non-terminal text became posting identity")
		}
	}
}

func TestDeliveryRecordFallbackOnlyForMissingScopedFile(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "attachment-delivery.json")
	if e := writeNew(legacy, obj{"kind": "legacy"}); e != nil {
		t.Fatal(e)
	}
	scoped := filepath.Join(root, "attachment-delivery-"+testThread+".json")
	v, _, e := originalDeliveryRecord(root, testThread)
	if e != nil || v["kind"] != "legacy" {
		t.Fatal("missing scoped file did not use legacy")
	}
	if e = writeNew(scoped, obj{"kind": "scoped"}); e != nil {
		t.Fatal(e)
	}
	v, _, e = originalDeliveryRecord(root, testThread)
	if e != nil || v["kind"] != "scoped" {
		t.Fatal("scoped ledger not preferred")
	}
	if e = os.WriteFile(scoped, []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = originalDeliveryRecord(root, testThread); e == nil {
		t.Fatal("corrupt scoped ledger fell back")
	}
	if e = os.Remove(scoped); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(scoped, 0700); e != nil {
		t.Fatal(e)
	}
	if _, _, e = originalDeliveryRecord(root, testThread); e == nil {
		t.Fatal("unreadable scoped ledger fell back")
	}
	// Preserve permission identity without disclosing the underlying private path.
	e = sourceOpenError{&os.PathError{Op: "open", Path: root, Err: os.ErrPermission}}
	if !errors.Is(e, os.ErrPermission) || errors.Is(e, os.ErrNotExist) || e.Error() != "source_unavailable" || strings.Contains(e.Error(), root) {
		t.Fatal("open error identity or privacy lost")
	}
}

func TestTraceDeliveryKeepsGenerationSeparateFromPermission(t *testing.T) {
	root := correlationTestRoot(t)
	surveyDir, correlationDir := filepath.Join(root, "survey"), filepath.Join(root, "correlation")
	if e := privateDir(surveyDir); e != nil {
		t.Fatal(e)
	}
	if e := privateDir(correlationDir); e != nil {
		t.Fatal(e)
	}
	acquired := testAcquisition(t, filepath.Join(root, "acquired"))
	// Insert an original posting footer, then update acquisition hashes.
	stagePath := filepath.Join(str(acquired["path"]), testThread+".private.json")
	stage, _, e := readJSON(stagePath)
	if e != nil {
		t.Fatal(e)
	}
	q := object(array(stage["messages"])[0])
	q["content"] = str(q["content"]) + "\n-# ジョブ worker。続きはこのスレッドに返信してください。"
	q["content_sha256"] = digest([]byte(str(q["content"])))
	if e = os.Remove(stagePath); e != nil {
		t.Fatal(e)
	}
	if e = writeNew(stagePath, stage); e != nil {
		t.Fatal(e)
	}
	reportPath := filepath.Join(str(acquired["path"]), "report.json")
	receipt, _, e := readJSON(reportPath)
	if e != nil {
		t.Fatal(e)
	}
	_, stageBody, e := readJSON(stagePath)
	if e != nil {
		t.Fatal(e)
	}
	object(array(receipt["threads"])[0])["stage_sha256"] = digest(stageBody)
	if e = os.Remove(reportPath); e != nil {
		t.Fatal(e)
	}
	if e = writeNew(reportPath, receipt); e != nil {
		t.Fatal(e)
	}
	_, receiptBody, e := readJSON(reportPath)
	if e != nil {
		t.Fatal(e)
	}
	acquired["report_sha256"] = digest(receiptBody)
	if e = writeNew(filepath.Join(surveyDir, "survey.private.json"), obj{"kind": "original-correspondence-survey", "acquisitions": []any{acquired}}); e != nil {
		t.Fatal(e)
	}
	if e = writeNew(filepath.Join(correlationDir, "correlation.private.json"), obj{"kind": "original-correspondence-correlation", "survey_source": surveyDir}); e != nil {
		t.Fatal(e)
	}
	jobs := filepath.Join(root, "jobs")
	dir := filepath.Join(jobs, "worker")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = writeNew(filepath.Join(dir, "job.json"), obj{"id": "worker", "todo_id": nil}); e != nil {
		t.Fatal(e)
	}
	if e = writeNew(filepath.Join(dir, "attachment-delivery.json"), obj{"thread_id": testThread, "first": q["id"], "next": 1, "complete": true, "attachment_hashes": []any{}, "fingerprint": digest([]byte(str(q["content"])))}); e != nil {
		t.Fatal(e)
	}
	todo := filepath.Join(root, "todo.json")
	if e = writeNew(todo, obj{"items": []any{}}); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(todo)
	if e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(root, "out")
	report, e := traceDeliveries(correlationDir, todo, jobs, output)
	if e != nil {
		t.Fatal(e)
	}
	if report["posted_generations_verified"] != 1 || report["explicit_reference_rows"] != 1 {
		t.Fatal("original generation not verified")
	}
	row := object(array(report["rows"])[0])
	if row["original_todo_candidates"] != 0 || row["decision_scope_verified"] != false || row["delivery_to_worker"] != false || row["application_verified"] != false || report["original_task_complete"] != false {
		t.Fatal("posting proof escalated into application")
	}
	after, e := os.ReadFile(todo)
	if e != nil || string(before) != string(after) {
		t.Fatal("live todo changed")
	}
	public, e := os.ReadFile(filepath.Join(output, "delivery-trace-report.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(public), "架空") || strings.Contains(string(public), `"worker"`) || strings.Contains(string(public), testThread) {
		t.Fatal("private source in public report")
	}
	private, _, e := readJSON(filepath.Join(output, "delivery-trace.private.json"))
	if e != nil {
		t.Fatal(e)
	}
	saved := object(array(private["rows"])[0])
	if object(saved["question"])["content"] != q["content"] || saved["delivery_record_sha256"] == "" || saved["acquisition_stage_sha256"] == "" {
		t.Fatal("private evidence lost")
	}
}
