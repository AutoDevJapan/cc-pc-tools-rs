package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func reviewFixture() []byte {
	return []byte(`{"items":[
 {"id":2174,"project":"private-other-project","note":"private-other-body"},
 {"id":2191,"project":"GoDD-Press","title":"private-title","note":"private-note claims wait provenance verified",
  "spec":"C:/private/spec.md","status":"recovery_unverified","dispatchable":false,
  "updated":"2026-10-24 17:00","wait_on":[2174],"jobs":[],"agent":null,
  "scheduler":{"job":null,"console_request":"private-original-request"},
  "recovery":{"original_id":2191,"verified_status":"todo",
   "request_source_job_id":"private-job-reference","preserved_worker_request":"C:/private/original.md",
   "source_job_sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
   "preserved_worker_request_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
   "verified_at":"2026-10-12T01:02:03Z","unknown":"private-unknown"}}
 ],"projects":{"private-other-project":"C:/private/other"}}`)
}

func TestReviewMetadataCLIProjectsOneRecordWithoutReadingReferencesOrChangingSource(t *testing.T) {
	root := correlationTestRoot(t)
	path := filepath.Join(root, "todo.json")
	source := reviewFixture()
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	var out bytes.Buffer
	code := runReviewMetadataCLI(nil, &out, func() ([]byte, error) {
		reads++
		return readReviewSnapshot(path)
	})
	if code != 0 || reads != 1 {
		t.Fatalf("positive CLI failed: code=%d reads=%d output=%s", code, reads, &out)
	}
	var report obj
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["record_id"] != float64(2191) || report["case_todo_id"] != float64(3255) || report["snapshot_sha256"] != digest(source) {
		t.Fatal("wrong record or source bytes")
	}
	if object(report["status"])["value"] != "recovery_unverified" || object(report["dispatchable"])["value"] != false {
		t.Fatal("original recovery state or automation hold was lost")
	}
	wait := array(object(report["wait_on"])["values"])
	if len(wait) != 1 || wait[0] != float64(2174) || object(report["jobs"])["reference_count"] != float64(0) {
		t.Fatal("original dependency or empty worker references were lost")
	}
	recovery := object(report["recovery"])
	if object(recovery["original_id"])["value"] != float64(2191) || object(recovery["verified_status"])["value"] != "todo" {
		t.Fatal("recorded recovery metadata was not projected")
	}
	if object(recovery["source_job_sha256"])["sha256"] != strings.Repeat("a", 64) || object(recovery["request_source_job_id"])["state"] != "recorded" {
		t.Fatal("recorded hash/reference was lost")
	}
	for _, reference := range []obj{object(recovery["request_source_job_id"]), object(recovery["preserved_worker_request"]), object(report["console_request_reference"])} {
		if _, present := reference["sha256"]; present {
			t.Fatal("a guessable reference value was disclosed as a digest")
		}
	}
	for _, forbidden := range []string{"private-", "C:/private", "GoDD-Press", "unknown", "claims wait provenance"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("non-metadata text leaked: %q", forbidden)
		}
	}
	for _, field := range []string{"live_state_changed", "data_access_granted", "original_registry_recovered", "review_completed", "console_queue_observed"} {
		if report[field] != false {
			t.Fatalf("snapshot was promoted into %s", field)
		}
	}
	if object(report["wait_transition_provenance"])["verified"] != false {
		t.Fatal("current dependency or a note manufactured transition provenance")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatal("source was changed")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != len(before) {
		t.Fatal("projection created a backup, output, or other file")
	}
}

func TestReviewMetadataCLIDoesNotAcceptCallerScopeOrDiscloseReadErrors(t *testing.T) {
	for _, args := range [][]string{{"2191"}, {"--todo", "private-path"}, {"--output", "private-path"}, {"--config", "private-path"}, {"--source=private-path"}, {"--help"}} {
		var out bytes.Buffer
		called := false
		code := runReviewMetadataCLI(args, &out, func() ([]byte, error) { called = true; return reviewFixture(), nil })
		if code != 2 || called || strings.Contains(out.String(), "private-path") {
			t.Fatalf("caller scope was accepted or read: %v code=%d called=%t", args, code, called)
		}
	}
	var out bytes.Buffer
	if code := runReviewMetadataCLI(nil, &out, func() ([]byte, error) { return nil, errors.New("denied C:/private/secret") }); code != 1 || strings.Contains(out.String(), "private") {
		t.Fatal("read failure became an empty/successful record or exposed an OS error")
	}
}

func TestReviewMetadataRejectsAmbiguityAndCorruptSources(t *testing.T) {
	cases := map[string][]byte{
		"missing":      []byte(`{"items":[{"id":2174}]}`),
		"duplicates":   []byte(`{"items":[{"id":2191,"project":"GoDD-Press"},{"id":2191,"project":"GoDD-Press"}]}`),
		"duplicatekey": []byte(`{"items":[{"id":2191,"project":"private-secret","project":"GoDD-Press"}]}`),
		"nestedkey":    []byte(`{"items":[{"id":2191,"project":"GoDD-Press","note":{"x":1,"\u0078":2}}]}`),
		"wrongscope":   []byte(`{"items":[{"id":2191,"project":"private-secret"}]}`),
		"itemsobject":  []byte(`{"items":{"id":2191}}`),
		"stringid":     []byte(`{"items":[{"id":"2191","project":"GoDD-Press"}]}`),
		"otherbadid":   []byte(`{"items":[{"id":2191,"project":"GoDD-Press"},{"id":"private-invalid-id"}]}`),
		"fractionalid": []byte(`{"items":[{"id":2191.0,"project":"GoDD-Press"}]}`),
		"secondjson":   append(reviewFixture(), []byte(` {"private-secret":true}`)...),
		"broken":       []byte(`{"items":["private-secret`),
		"invalidutf8":  append(reviewFixture(), 0xff),
		"empty_source": nil,
		"overlimit":    bytes.Repeat([]byte{' '}, reviewMetadataLimit+1),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if code := runReviewMetadataCLI(nil, &out, func() ([]byte, error) { return source, nil }); code != 1 {
				t.Fatalf("invalid source accepted: %s", &out)
			}
			if strings.Contains(out.String(), "private") {
				t.Fatal("bad source content leaked in the error")
			}
		})
	}
}

func TestReviewMetadataKeepsAbsentNullAndMalformedFieldsDistinct(t *testing.T) {
	var db obj
	d := json.NewDecoder(bytes.NewReader(reviewFixture()))
	d.UseNumber()
	if err := d.Decode(&db); err != nil {
		t.Fatal(err)
	}
	row := object(array(db["items"])[1])
	recovery := object(row["recovery"])
	delete(recovery, "original_id")
	recovery["verified_status"] = nil
	recovery["source_job_sha256"] = "private-invalid-hash"
	recovery["verified_at"] = "private-invalid-time"
	row["wait_on"] = []any{"private-invalid-dependency"}
	row["jobs"] = "private-invalid-jobs"
	b, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	r, err := projectReviewMetadata(b)
	if err != nil {
		t.Fatal(err)
	}
	recovery = object(r["recovery"])
	for key, expected := range map[string]string{"original_id": "absent", "verified_status": "null", "source_job_sha256": "invalid", "verified_at": "invalid"} {
		if object(recovery[key])["state"] != expected {
			t.Fatalf("wrong state for %s", key)
		}
	}
	if object(r["wait_on"])["state"] != "invalid" || object(r["jobs"])["state"] != "invalid" {
		t.Fatal("malformed references were called empty")
	}
	out, _ := json.Marshal(r)
	if strings.Contains(string(out), "private") {
		t.Fatal("malformed field text leaked")
	}
}

func TestReviewMetadataUsesOneBoundedSnapshotAndAcceptsTheCanonicalSizeLimit(t *testing.T) {
	source := append(reviewFixture(), bytes.Repeat([]byte{' '}, 41<<20)...)
	r, err := projectReviewMetadata(source)
	if err != nil || r["snapshot_sha256"] != digest(source) {
		t.Fatal("a canonical snapshot above the historical 40MiB limit was lost or rehashed after parsing")
	}
	if _, err := readReviewBytes(io.LimitReader(strings.NewReader(strings.Repeat(" ", reviewMetadataLimit+1)), reviewMetadataLimit+1)); err == nil {
		t.Fatal("growing/oversize source was accepted")
	}
	root := correlationTestRoot(t)
	if _, err := readReviewSnapshot(filepath.Join(root, "absent")); err == nil {
		t.Fatal("missing source was treated as an empty record")
	}
	path := filepath.Join(root, "real.json")
	if err := os.WriteFile(path, reviewFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("leaf_symlink", func(t *testing.T) {
		link := filepath.Join(root, "linked.json")
		if err := os.Symlink(path, link); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		if _, err := readReviewSnapshot(link); err == nil {
			t.Fatal("linked source was followed")
		}
	})
}

func TestReviewMetadataContainerStatesDoNotBecomeEmptyRecords(t *testing.T) {
	for _, field := range []string{"recovery", "scheduler"} {
		for _, state := range []string{"absent", "null", "invalid"} {
			t.Run(field+"_"+state, func(t *testing.T) {
				var db obj
				d := json.NewDecoder(bytes.NewReader(reviewFixture()))
				d.UseNumber()
				if err := d.Decode(&db); err != nil {
					t.Fatal(err)
				}
				row := object(array(db["items"])[1])
				switch state {
				case "absent":
					delete(row, field)
				case "null":
					row[field] = nil
				case "invalid":
					row[field] = "private-invalid-container"
				}
				b, _ := json.Marshal(db)
				r, err := projectReviewMetadata(b)
				if err != nil || object(r[field])["state"] != state {
					t.Fatalf("container state lost: %v", err)
				}
				out, _ := json.Marshal(r)
				if strings.Contains(string(out), "private-invalid-container") {
					t.Fatal("invalid container leaked")
				}
			})
		}
	}
}

func TestReviewMetadataBOMAndExactFileSizeBoundary(t *testing.T) {
	bom := append([]byte{239, 187, 191}, reviewFixture()...)
	r, err := projectReviewMetadata(bom)
	if err != nil || r["snapshot_sha256"] != digest(bom) {
		t.Fatal("BOM was not accepted and hashed as originally read")
	}
	root := correlationTestRoot(t)
	path := filepath.Join(root, "boundary.json")
	b := bytes.Repeat([]byte{' '}, reviewMetadataLimit)
	copy(b, reviewFixture())
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	read, err := readReviewSnapshot(path)
	if err != nil || len(read) != reviewMetadataLimit {
		t.Fatal("exact canonical size limit was rejected")
	}
	if r, err := projectReviewMetadata(read); err != nil || r["snapshot_sha256"] != digest(b) {
		t.Fatal("exact limit was not projected from the same bytes")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.Write([]byte{' '})
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("fixture append failed")
	}
	if _, err := readReviewSnapshot(path); err == nil {
		t.Fatal("file above the canonical limit was accepted")
	}
}

func TestReviewMetadataMainHelper(t *testing.T) {
	if os.Getenv("CC_REVIEW_METADATA_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"record-recovery", "review-metadata", "--source=synthetic-forbidden-source"}
	main()
	t.Fatal("main did not exit")
}

func TestReviewMetadataMainDispatchRefusesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestReviewMetadataMainHelper$")
	cmd.Env = append(os.Environ(), "CC_REVIEW_METADATA_MAIN_HELPER=1")
	hideCommand(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("unexpected main result: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatal("main returned unexpected standard error")
	}
	var response obj
	if json.Unmarshal(output, &response) != nil || response["code"] != "review_metadata_invalid_arguments" {
		t.Fatal("main did not select the metadata-only command")
	}
}
