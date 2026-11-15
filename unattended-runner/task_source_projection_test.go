package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type taskSourceFixture struct {
	root, taskRoot, store string
	row                   map[string]any
	q                     taskSourceProjectionRequest
	d                     taskSourceProjectionDependencies
	reads                 int
	beforeRead            func(int)
	deny                  map[string]bool
	guarded               []string
}

func newTaskSourceFixture(t *testing.T) *taskSourceFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &taskSourceFixture{root: root, store: filepath.Join(root, "task-store.json"), deny: map[string]bool{}}
	f.taskRoot = filepath.Join(root, "registered-project")
	if err := os.Mkdir(f.taskRoot, 0700); err != nil {
		t.Fatal(err)
	}
	f.q = taskSourceProjectionRequest{Todo: 42, Root: root, Body: filepath.Join(root, "question.txt"), Decision: filepath.Join(root, "decision.json"), Image: filepath.Join(root, "original.jpg")}
	body := []byte("Choose this fictional example: a or b")
	decision := []byte(`{"todo_id":42,"decision":"direction","resources":[{"kind":"repository","id":"900"},{"kind":"url","id":"https://example.invalid/"}],"scope":{"preserve":"original comparison"}}`)
	image := []byte("\xff\xd8\xffowned synthetic JPEG fixture")
	for path, data := range map[string][]byte{f.q.Body: body, f.q.Decision: decision, f.q.Image: image} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.q.BodySHA, f.q.DecisionSHA, f.q.ImageSHA = bytesHash(body), bytesHash(decision), bytesHash(image)
	f.row = map[string]any{
		"id": 42, "project": "owned", "title": "TITLE_MUST_NOT_BE_EXPORTED", "note": "NOTE_MUST_NOT_BE_EXPORTED",
		"spec": map[string]any{"private": "SPEC_MUST_NOT_BE_EXPORTED"}, "created_s": 1700000000,
		"status": "doing", "jobs": []string{"original-job"}, "resume_generation": 3,
		"worker_receipt": map[string]any{"job": "original-job", "generation": 4}, "scheduler": map[string]any{"job": "original-job"},
	}
	f.save(t, nil)
	f.d = taskSourceProjectionDependencies{
		Task: func(ctx context.Context, id uint64) (taskQuestionState, error) {
			f.reads++
			if f.beforeRead != nil {
				f.beforeRead(f.reads)
			}
			if ctx.Err() != nil {
				return taskQuestionState{}, ctx.Err()
			}
			// Real selected-row reader and real IO, not a serialized projection
			// supplied by the test or by the public CLI caller.
			return taskQuestionReadState(f.store, id)
		},
		Guard: func(_ context.Context, path string) bool {
			f.guarded = append(f.guarded, path)
			return !f.deny[path]
		},
	}
	return f
}

func (f *taskSourceFixture) save(t *testing.T, extra []any) {
	t.Helper()
	items := append([]any{f.row}, extra...)
	b, err := json.Marshal(map[string]any{"projects": map[string]string{"owned": f.taskRoot}, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.store, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func taskSourceExpectFailure(t *testing.T, p taskSourceProjection, err error, want string) {
	t.Helper()
	var code taskSourceProjectionFailure
	if err == nil || !errors.As(err, &code) || string(code) != want {
		t.Fatalf("expected %s, got %v", want, err)
	}
	if p.Task != nil || p.Inputs != nil || p.Registered || p.SendAllowed || p.TaskToRepositoryProved || p.SourceQuiescenceProved {
		t.Fatal("failed inspection exported metadata or authority")
	}
}

func taskSourceArgs(q taskSourceProjectionRequest) []string {
	return []string{"--todo-id", "42", "--cwd", q.Root, "--body-file", q.Body, "--decision-file", q.Decision, "--attach", q.Image,
		"--body-sha256", q.BodySHA, "--decision-sha256", q.DecisionSHA, "--image-sha256", q.ImageSHA}
}

func TestTaskSourceProjectionNormalProviderAndOriginalInputs(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.save(t, []any{map[string]any{"id": 99, "project": "unrelated", "note": "OTHER_TASK_MUST_NOT_BE_EXPORTED"}})
	before, _ := os.ReadFile(f.store)
	var out bytes.Buffer
	if exit := taskSourceProjectionCLI(taskSourceArgs(f.q), &out, f.d); exit != 0 {
		t.Fatalf("normal provider exit %d: %s", exit, out.String())
	}
	var p taskSourceProjection
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Task == nil || p.Inputs == nil {
		t.Fatal("missing normal projection", err)
	}
	if p.Task.ID != 42 || p.Task.Project != "owned" || p.Task.Root != f.taskRoot || p.Task.Status != "doing" ||
		p.Task.Worker.Job == nil || *p.Task.Worker.Job != "original-job" || p.Task.Worker.Generation == nil || *p.Task.Worker.Generation != 4 ||
		p.Task.ResumeGeneration == nil || *p.Task.ResumeGeneration != 3 || p.Task.SchedulerJob == nil || *p.Task.SchedulerJob != "original-job" {
		t.Fatal("stored metadata was not preserved")
	}
	if p.Inputs.BodySHA != f.q.BodySHA || p.Inputs.DecisionSHA != f.q.DecisionSHA || p.Inputs.ImageSHA != f.q.ImageSHA || p.Inputs.ImageMIME != "image/jpeg" {
		t.Fatal("original inputs were not bound")
	}
	if p.Domain != taskSourceProjectionDomain || !p.MetadataOnly || p.Registered || p.SendAllowed || p.TaskToRepositoryProved || p.SourceQuiescenceProved || f.reads != 2 {
		t.Fatal("inspection became authority or did not reread")
	}
	for _, withheld := range []string{"TITLE_MUST_NOT", "NOTE_MUST_NOT", "SPEC_MUST_NOT", "OTHER_TASK_MUST_NOT", "Choose this fictional", "original comparison"} {
		if strings.Contains(out.String(), withheld) {
			t.Fatalf("private body was exported: %s", withheld)
		}
	}
	after, _ := os.ReadFile(f.store)
	if !bytes.Equal(before, after) {
		t.Fatal("read-only inspection changed Task store")
	}
	for _, path := range []string{f.q.Body, f.q.Decision, f.q.Image} {
		if err := os.Rename(path, path+".closed"); err != nil {
			t.Fatal("input handle was retained after completion", err)
		}
	}
}

func TestTaskSourceProjectionMissingAssignmentIsNull(t *testing.T) {
	f := newTaskSourceFixture(t)
	for _, key := range []string{"worker_receipt", "resume_generation", "scheduler", "jobs"} {
		delete(f.row, key)
	}
	f.save(t, nil)
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	if err != nil || p.Task == nil || p.Task.Worker.Job != nil || p.Task.Worker.Generation != nil || p.Task.ResumeGeneration != nil || p.Task.SchedulerJob != nil {
		t.Fatal("missing metadata was inferred", err)
	}
}

func TestTaskSourceProjectionStoredZeroIsNotMissing(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.row["resume_generation"] = 0
	f.row["worker_receipt"] = map[string]any{"job": "", "generation": 0}
	f.save(t, nil)
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	if err != nil || p.Task == nil || p.Task.ResumeGeneration == nil || *p.Task.ResumeGeneration != 0 ||
		p.Task.Worker.Generation == nil || *p.Task.Worker.Generation != 0 || p.Task.Worker.Job == nil || *p.Task.Worker.Job != "" {
		t.Fatal("stored zero/empty metadata was converted into missing", err)
	}
}

func TestTaskSourceProjectionDoesNotInterpretUnrelatedTaskBody(t *testing.T) {
	f := newTaskSourceFixture(t)
	b, err := os.ReadFile(f.store)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]json.RawMessage
	if err := json.Unmarshal(b, &store); err != nil {
		t.Fatal(err)
	}
	items := bytes.TrimSpace(store["items"])
	items = append(append([]byte(nil), items[:len(items)-1]...), []byte(`,{"id":99,"project":"unrelated","spec":{"same":1,"same":2},"note":"DO_NOT_EXPORT"}]`)...)
	store["items"] = items
	b, err = json.Marshal(store)
	if err != nil || os.WriteFile(f.store, b, 0600) != nil {
		t.Fatal("fixture store")
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	if err != nil || p.Task == nil || p.Task.ID != 42 {
		t.Fatal("projection inspected an unrelated spec/note", err)
	}
}

func TestTaskSourceProjectionStoreFieldCaseKeepsNormalReaderSemantics(t *testing.T) {
	f := newTaskSourceFixture(t)
	b, err := os.ReadFile(f.store)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.Replace(b, []byte(`"projects":`), []byte(`"Projects":`), 1)
	b = bytes.Replace(b, []byte(`"items":`), []byte(`"Items":`), 1)
	if err := os.WriteFile(f.store, b, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	if err != nil || p.Task == nil || p.Task.Root != f.taskRoot {
		t.Fatal("duplicate-key normalization changed field lookup", err)
	}
}

func TestTaskSourceProjectionRejectsProtectedBeforeMediaRead(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.deny[f.taskRoot] = true
	if err := os.Remove(f.q.Image); err != nil {
		t.Fatal(err)
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	taskSourceExpectFailure(t, p, err, "task_root_unqualified")
	for _, path := range f.guarded {
		if path == f.q.Image {
			t.Fatal("media was reached before Task scope rejection")
		}
	}
}

func TestTaskSourceProjectionRefusesChangedGenerationAtSecondRead(t *testing.T) {
	for _, change := range []string{"worker", "resume", "root", "stop"} {
		t.Run(change, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			f.beforeRead = func(n int) {
				if n != 2 {
					return
				}
				switch change {
				case "worker":
					f.row["worker_receipt"] = map[string]any{"job": "new-job", "generation": 5}
				case "resume":
					f.row["resume_generation"] = 4
				case "root":
					f.taskRoot = f.root
				case "stop":
					f.row["owner_stopped"] = true
				}
				f.save(t, nil)
			}
			p, err := taskSourceProject(context.Background(), f.q, f.d)
			want := "task_changed"
			if change == "stop" {
				want = "task_unavailable"
			}
			taskSourceExpectFailure(t, p, err, want)
			if f.reads != 2 {
				t.Fatal("targeted second provider boundary was not reached")
			}
		})
	}
}

func TestTaskSourceProjectionInputChangeDuringProviderRead(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.beforeRead = func(n int) {
		if n == 2 {
			if err := os.WriteFile(f.q.Body, []byte("changed original body"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	taskSourceExpectFailure(t, p, err, "input_changed")
}

func TestTaskSourceProjectionRootReplacementDuringProviderRead(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.beforeRead = func(n int) {
		if n == 2 {
			if err := os.Rename(f.taskRoot, f.taskRoot+"-original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(f.taskRoot, 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	taskSourceExpectFailure(t, p, err, "task_root_changed")
}

func TestTaskSourceProjectionInputGuardRevokedDuringProviderRead(t *testing.T) {
	f := newTaskSourceFixture(t)
	f.beforeRead = func(n int) {
		if n == 2 {
			f.deny[f.q.Image] = true
		}
	}
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	taskSourceExpectFailure(t, p, err, "input_unqualified")
}

func TestTaskSourceProjectionRefusesBadInputAndStore(t *testing.T) {
	for _, name := range []string{"missing_task", "duplicate_task", "duplicate_document", "wrong_decision", "changed_hash", "protected_input", "protected_source", "invalid_generation", "invalid_project_root_type"} {
		t.Run(name, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			want := "task_unavailable"
			switch name {
			case "missing_task":
				f.row["id"] = 43
				f.save(t, nil)
			case "duplicate_task":
				f.save(t, []any{f.row})
			case "duplicate_document":
				b, _ := os.ReadFile(f.store)
				b = bytes.Replace(b, []byte(`"items":`), []byte(`"items":[],"items":`), 1)
				if err := os.WriteFile(f.store, b, 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong_decision":
				b := []byte(`{"todo_id":43,"decision":"direction","resources":[{"kind":"repository","id":"900"}]}`)
				if err := os.WriteFile(f.q.Decision, b, 0600); err != nil {
					t.Fatal(err)
				}
				f.q.DecisionSHA = bytesHash(b)
				want = "question_scope_mismatch"
			case "changed_hash":
				f.q.ImageSHA = strings.Repeat("a", 64)
				want = "input_changed"
			case "protected_input":
				f.deny[f.q.Image] = true
				want = "input_unqualified"
			case "protected_source":
				f.deny[f.q.Root] = true
				want = "source_root_unqualified"
			case "invalid_generation":
				f.row["resume_generation"] = "3"
				f.save(t, nil)
			case "invalid_project_root_type":
				b, readErr := os.ReadFile(f.store)
				var document map[string]json.RawMessage
				if readErr != nil || json.Unmarshal(b, &document) != nil {
					t.Fatal("fixture store")
				}
				projects := bytes.TrimSpace(document["projects"])
				document["projects"] = append(append([]byte(nil), projects[:len(projects)-1]...), []byte(`,"other":123}`)...)
				b, marshalErr := json.Marshal(document)
				if marshalErr != nil || os.WriteFile(f.store, b, 0600) != nil {
					t.Fatal("fixture project types")
				}
			}
			p, err := taskSourceProject(context.Background(), f.q, f.d)
			taskSourceExpectFailure(t, p, err, want)
		})
	}
}

type taskSourceShortWriter struct{ writes int }

func (w *taskSourceShortWriter) Write(b []byte) (int, error) {
	w.writes++
	return len(b) - 1, nil
}

func TestTaskSourceProjectionCLIRejectsAliasesAndShortOutput(t *testing.T) {
	f := newTaskSourceFixture(t)
	for _, args := range [][]string{
		append(taskSourceArgs(f.q), "--todo-id", "42"),
		append(taskSourceArgs(f.q), "--store", f.store),
		{"--todo-id", "0042"},
		{"--todo-id", "-1"},
	} {
		var out bytes.Buffer
		if exit := taskSourceProjectionCLI(args, &out, f.d); exit == 0 || strings.Contains(out.String(), f.store) || strings.Contains(out.String(), f.root) {
			t.Fatal("invalid argument accepted or echoed")
		}
	}
	writer := &taskSourceShortWriter{}
	if exit := taskSourceProjectionCLI(taskSourceArgs(f.q), writer, f.d); exit == 0 || writer.writes != 1 {
		t.Fatal("short output was accepted or followed by fallback")
	}
}

func TestTaskSourceProjectionCancelledBeforeLookup(t *testing.T) {
	f := newTaskSourceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := taskSourceProject(ctx, f.q, f.d)
	taskSourceExpectFailure(t, p, err, "invalid_input")
	if f.reads != 0 {
		t.Fatal("cancelled inspection reached provider")
	}
}

func taskSourceWriteRawStore(t *testing.T, f *taskSourceFixture, selected []byte, others ...[]byte) {
	t.Helper()
	projects, err := json.Marshal(map[string]string{"owned": f.taskRoot})
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	raw.WriteString(`{"projects":`)
	raw.Write(projects)
	raw.WriteString(`,"items":[`)
	raw.Write(selected)
	for _, other := range others {
		raw.WriteByte(',')
		raw.Write(other)
	}
	raw.WriteString(`]}`)
	if err := os.WriteFile(f.store, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTaskSourceProjectionStrictShallowRowIDs(t *testing.T) {
	for _, rawID := range []string{
		`{"id":42,"ID":99}`,
		`{"ID":42,"id":99}`,
		`{"id":99,"ID":42}`,
		`{"ID":99,"id":42}`,
		`{"id":42,"id":99}`,
		`{"id":42,"\u0049\u0044":99}`,
		`{"id":"99"}`,
		`{"id":null}`,
		`{"id":99.0}`,
		`{"id":9.9e1}`,
		`{"id":0}`,
		`{"id":-1}`,
		`{"id":18446744073709551616}`,
		`{"project":"unrelated"}`,
	} {
		t.Run(rawID, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			selected, err := json.Marshal(f.row)
			if err != nil {
				t.Fatal(err)
			}
			if rawID == `{"id":42,"ID":99}` {
				var oldHeader struct{ ID uint64 }
				if err := json.Unmarshal([]byte(rawID), &oldHeader); err != nil || oldHeader.ID != 99 {
					t.Fatal("old case-insensitive last-value contrast missed its boundary", err)
				}
			}
			// A valid selected row must not hide an ambiguous or invalid ID in
			// another row. This is the full provider/CLI input path, not just a
			// unit call to the new ID helper.
			taskSourceWriteRawStore(t, f, selected, []byte(rawID))
			p, err := taskSourceProject(context.Background(), f.q, f.d)
			taskSourceExpectFailure(t, p, err, "task_unavailable")
		})
	}
}

func TestTaskSourceProjectionLosslessMetadataStringFailures(t *testing.T) {
	for _, member := range []string{"worker", "scheduler", "project", "status"} {
		for _, bad := range []string{`"\uD800"`, `"\uDC00"`, `"\uD800x"`, "\"\xff\""} {
			t.Run(member+bad, func(t *testing.T) {
				f := newTaskSourceFixture(t)
				selected, err := json.Marshal(f.row)
				if err != nil {
					t.Fatal(err)
				}
				needle, replacement := `"job":"original-job"`, `"job":`+bad
				switch member {
				case "project":
					needle, replacement = `"project":"owned"`, `"project":`+bad
				case "status":
					needle, replacement = `"status":"doing"`, `"status":`+bad
				}
				if member == "worker" || member == "scheduler" {
					// Marshal sorts scheduler before worker_receipt. Replace only
					// the requested nested job, leaving the other valid and present.
					key := `"worker_receipt":`
					if member == "scheduler" {
						key = `"scheduler":`
					}
					from := bytes.Index(selected, []byte(key))
					if from < 0 {
						t.Fatal("fixture nested key absent")
					}
					tail := bytes.Replace(selected[from:], []byte(needle), []byte(replacement), 1)
					selected = append(append([]byte(nil), selected[:from]...), tail...)
				} else {
					selected = bytes.Replace(selected, []byte(needle), []byte(replacement), 1)
				}
				taskSourceWriteRawStore(t, f, selected)
				p, err := taskSourceProject(context.Background(), f.q, f.d)
				taskSourceExpectFailure(t, p, err, "task_unavailable")
			})
		}
	}
}

func TestTaskSourceProjectionLosslessMetadataStringPositive(t *testing.T) {
	for _, value := range []struct{ raw, expected string }{
		{`"�"`, "�"},
		{`"\ufffd"`, "�"},
		{`"\uD83D\uDE00"`, "😀"},
		{`"\\uD800"`, `\uD800`},
	} {
		t.Run(value.raw, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			selected, err := json.Marshal(f.row)
			if err != nil {
				t.Fatal(err)
			}
			selected = bytes.ReplaceAll(selected, []byte(`"job":"original-job"`), []byte(`"job":`+value.raw))
			taskSourceWriteRawStore(t, f, selected)
			p, err := taskSourceProject(context.Background(), f.q, f.d)
			if err != nil || p.Task == nil || p.Task.Worker.Job == nil || p.Task.SchedulerJob == nil ||
				*p.Task.Worker.Job != value.expected || *p.Task.SchedulerJob != value.expected {
				t.Fatal("valid string identity changed or rejected", err)
			}
		})
	}
}

func TestTaskSourceProjectionSelectedRootRejectsLossyString(t *testing.T) {
	for _, bad := range []string{`"\uD800"`, `"\uDC00"`, "\"\xff\""} {
		t.Run(bad, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			selected, err := json.Marshal(f.row)
			if err != nil {
				t.Fatal(err)
			}
			store := []byte(`{"projects":{"owned":` + bad + `},"items":[` + string(selected) + `]}`)
			if err := os.WriteFile(f.store, store, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := taskQuestionReadState(f.store, 42); err == nil {
				t.Fatal("lossy selected root was accepted by the normal provider")
			}
		})
	}
}

func TestTaskSourceProjectionLossyIdentityCannotMatchLiteralReplacement(t *testing.T) {
	for _, member := range []string{"project", "project_key", "root"} {
		t.Run(member, func(t *testing.T) {
			f := newTaskSourceFixture(t)
			project := "owned"
			root := f.taskRoot
			if member != "root" {
				project = "owned�"
				f.row["project"] = project
			} else {
				root = filepath.Join(f.root, "replacement-�")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			selected, err := json.Marshal(f.row)
			if err != nil {
				t.Fatal(err)
			}
			projects, err := json.Marshal(map[string]string{project: root})
			if err != nil {
				t.Fatal(err)
			}
			if member == "project" {
				selected = bytes.Replace(selected, []byte("owned�"), []byte(`owned\uD800`), 1)
			} else {
				projects = bytes.Replace(projects, []byte("�"), []byte(`\uD800`), 1)
			}
			store := append([]byte(`{"projects":`), projects...)
			store = append(store, []byte(`,"items":[`)...)
			store = append(store, selected...)
			store = append(store, []byte(`]}`)...)
			// A normal decoder repairs these original bytes into an existing
			// literal-U+FFFD registry key/path. That is precisely the identity
			// change the selected-string boundary must refuse.
			var legacy struct {
				Projects map[string]string
				Items    []struct{ Project string }
			}
			if err := json.Unmarshal(store, &legacy); err != nil || len(legacy.Items) != 1 || legacy.Projects[legacy.Items[0].Project] != root {
				t.Fatal("legacy identity-repair contrast did not reach the intended value", err)
			}
			if err := os.WriteFile(f.store, store, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := taskSourceProject(context.Background(), f.q, f.d)
			taskSourceExpectFailure(t, p, err, "task_unavailable")
		})
	}
}

func TestTaskSourceProjectionShallowIDLeavesOtherDuplicatesOpaque(t *testing.T) {
	f := newTaskSourceFixture(t)
	selected, err := json.Marshal(f.row)
	if err != nil {
		t.Fatal(err)
	}
	other := []byte(`{"id":99,"spec":{"key":1,"key":2},"note":"first","note":"second","spec":{"other":3}}`)
	taskSourceWriteRawStore(t, f, selected, other)
	p, err := taskSourceProject(context.Background(), f.q, f.d)
	if err != nil || p.Task == nil || p.Task.ID != 42 {
		t.Fatal("non-ID fields of another Task were interpreted", err)
	}
}
