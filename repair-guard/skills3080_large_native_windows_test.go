package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSkills3080LargeCanonicalNativeKeepsOriginalProofs(t *testing.T) {
	// The Windows service runner can expose a temporary directory through an
	// alias. Use the existing physical-root fixture, as the native reader must
	// continue to reject a handle whose resolved path differs from its input.
	nativeRoot := skillsFixtureRoot(t)
	t.Setenv("USERPROFILE", nativeRoot)
	_, _, e, _ := skillsForwardFixture(t)
	thread := "11111111-1111-4111-8111-111111111111"
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	cwd := e.Writer.Workspace
	body := "fixed owner and project instructions"
	prompt := "fixed public review input"
	final := "verified public final"
	dir := filepath.Join(nativeRoot, ".codex", "sessions", start.Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-synthetic-"+thread+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	write := func(v object) {
		t.Helper()
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	write(object{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": thread, "cwd": cwd}})
	write(object{"type": "response_item", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, body, false)}}}})
	write(object{"type": "turn_context", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"cwd": cwd, "model": "gpt-6-luna", "effort": "max"}})
	write(object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": prompt}}}})
	// Reuse one buffer: the regression is an actual valid >256MiB append log,
	// not a fabricated size flag or an allocation proportional to the log.
	padding := append([]byte(`{"type":"unrelated_metadata"}`), bytes.Repeat([]byte(" "), 1<<20)...)
	padding = append(padding, '\n')
	for i := 0; i < 257; i++ {
		if _, err := f.Write(padding); err != nil {
			t.Fatal(err)
		}
	}
	c := skillsConsumeRequest{Creator: e.Writer, RequestPath: e.Writer.Workspace + "/.ci-work/forward-request.json", RequestSHA: e.RequestSHA, CallID: "call_large_native_fixture"}
	records := skillsNativeCreationFixture(t, e, c)
	for _, v := range records[1:] {
		write(v)
	}
	write(object{"type": "response_item", "timestamp": start.Add(4 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "assistant", "phase": "final_answer", "content": []object{{"type": "output_text", "text": final}}}})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 256<<20 {
		t.Fatal("large canonical fixture was not produced")
	}
	job := object{"id": e.Writer.Job, "thread_id": thread, "cwd": cwd, "started_at": start.Format(time.RFC3339Nano), "finished_at": end.Format(time.RFC3339Nano), "status": "succeeded"}
	t.Run("bootstrap_prefix", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		got, ok := skillsBootstrapForJob(&fs, job)
		if !ok || got != hashBytes([]byte(body)) {
			t.Fatal("canonical long-session bootstrap was rejected")
		}
	})
	t.Run("review_input_and_final", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		got, ok := skillsHistoricalNative(&fs, job, &skillsNativeResultProof{prompt: []byte(prompt), finalSHA: hashBytes([]byte(final)), exactInput: true, bootstrapSHA: hashBytes([]byte(body)), cwd: cwd})
		if !ok || len(got) != 1 {
			t.Fatal("unchanged exact review input/final proof was rejected")
		}
	})
	t.Run("actual_creation_call", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		got, ok := skillsReadCreationNative(&fs, job, c)
		if !ok || skillsProjectionSHA(got) != skillsProjectionSHA(e) {
			t.Fatal("canonical creation call after former size boundary was rejected")
		}
	})
	t.Run("ordinary_registered_review", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		got, ok := skillsNativeJob(&fs, job)
		if !ok || len(got) != 1 {
			t.Fatal("ordinary registered review rejected a canonical log above 256MiB")
		}
	})
	t.Run("snapshot_rejects_truncated_duplicate_call", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		writer, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		defer writer.Truncate(info.Size())
		if _, err := writer.Seek(0, io.SeekEnd); err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(writer).Encode(records[1]); err != nil {
			t.Fatal(err)
		}
		reader, ok := skillsNativeSnapshot(&fs, job)
		if !ok {
			t.Fatal("initial snapshot unavailable")
		}
		// Remove a duplicate call after freezing the longer size. The remaining
		// prefix contains every otherwise valid proof and must still be denied.
		if err := writer.Truncate(info.Size()); err != nil {
			t.Fatal(err)
		}
		if _, ok := skillsCreationNativeProjection(reader, thread, cwd, start, end, c); ok {
			t.Fatal("truncation hid the frozen duplicate call behind a valid prefix")
		}
	})
	t.Run("same_snapshot_ignores_later_append", func(t *testing.T) {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		reader, ok := skillsNativeSnapshot(&fs, job)
		if !ok {
			t.Fatal("snapshot unavailable")
		}
		appendFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = appendFile.WriteString("late bytes must not join this snapshot\n")
		appendFile.Close()
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, reader)
		if err != nil || n != info.Size() {
			t.Fatal("snapshot followed appended bytes")
		}
	})
}

func TestSkills3080ReadOnlyReviewTaskIsExactPublicInput(t *testing.T) {
	for _, kind := range []string{"valid", "changed_body", "other_path", "missing_pin", "repair_body", "private_path", "header_only"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			body := "SKILLS3080_READ_ONLY_REVIEW_TASK_V1\nConfirm the concrete existing finding within the fixed read-only scope.\n"
			path := "docs/skills-3080-read-only-review-task.md"
			c.Purpose, c.PurposePath = body, path
			c.Files = append(c.Files, skillsPublicReviewFile{Path: path, Blob: skillsGitBlobID([]byte(body)), SHA: hashBytes([]byte(body)), Bytes: int64(len(body))})
			switch kind {
			case "changed_body":
				c.Purpose += "changed"
			case "other_path":
				c.PurposePath += ".other"
			case "missing_pin":
				c.Files = c.Files[:1]
			case "repair_body":
				c.Purpose = "implement and deploy the repair"
			case "private_path":
				c.PurposePath = "private/notes.md"
			case "header_only":
				c.Purpose = skillsReadOnlyReviewTaskHeader + " \n"
				c.Files[1].Bytes = int64(len(c.Purpose))
				c.Files[1].SHA = hashBytes([]byte(c.Purpose))
				c.Files[1].Blob = skillsGitBlobID([]byte(c.Purpose))
			}
			raw, _ := json.Marshal(c)
			_, ok := skillsParsePublicReview(raw)
			if ok != (kind == "valid") {
				t.Fatalf("read-only review task %s accepted=%v", kind, ok)
			}
		})
	}
}
