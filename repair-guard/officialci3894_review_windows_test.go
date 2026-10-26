package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

func official3894LineageFixture() skillsRegistry {
	b := oc.FixedBinding("", "", "", "")
	reg := skillsRegistry{Tasks: map[int]skillsTask{
		3387: {ID: 3387, Project: "cc-pc-tools-rs", Status: "wait", Jobs: []string{b.ParentJob}},
		3888: {ID: 3888, Project: "localai-web-rs", Status: "wait", Jobs: []string{b.OriginalProviderJob}},
		3894: {ID: 3894, Project: "cc-pc-tools-rs", Status: "doing", Jobs: []string{official3894OriginalCreator}},
	}}
	for id, row := range reg.Tasks {
		row.Scheduler.Worktree = "D:/synthetic/todo-" + strconv.Itoa(id)
		row.Scheduler.Branch = "scheduler/todo-" + strconv.Itoa(id) + "-fixture"
		reg.Tasks[id] = row
	}
	return reg
}

func official3894OriginFixture(reg skillsRegistry) map[string]object {
	jobs := map[string]object{}
	for id, row := range reg.Tasks {
		repo, kind := "autodevjapan/cc-pc-tools-rs", "implementation"
		if id == 3387 {
			kind = "research"
		}
		if id == 3888 {
			repo = "autodevjapan/local-ai-console"
		}
		job := row.Jobs[0]
		jobs[job] = object{"id": job, "todo_id": json.Number(strconv.Itoa(id)), "project": row.Project, "task_type": kind, "scope_source": "launcher", "cwd": row.Scheduler.Worktree,
			"work_owner": object{"repository": repo, "git_common": "D:/synthetic/.git", "worktree": row.Scheduler.Worktree, "branch": row.Scheduler.Branch}}
	}
	return jobs
}

func TestOfficialCI3894OriginalJobLineage(t *testing.T) {
	reg := official3894LineageFixture()
	check := func(jobs map[string]object) (string, bool) {
		return official3894OriginalLineage(reg, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
	}
	first, ok := check(official3894OriginFixture(reg))
	if !ok || len(first) != 64 {
		t.Fatal("normal original metadata rejected")
	}
	for _, which := range []int{3387, 3888, 3894} {
		for _, change := range []string{"missing", "id", "todo_id", "project", "task_type", "parent_job_id", "subwork", "source", "repository", "worktree", "branch", "no-common", "duplicate-owner", "malformed-parent"} {
			t.Run(strconv.Itoa(which)+"/"+change, func(t *testing.T) {
				jobs := official3894OriginFixture(reg)
				id := reg.Tasks[which].Jobs[0]
				job := jobs[id]
				owner := obj(job["work_owner"])
				switch change {
				case "missing":
					delete(jobs, id)
				case "id", "project", "task_type", "parent_job_id":
					job[change] = "different"
				case "todo_id":
					job[change] = 3893
				case "subwork":
					job[change] = object{"kind": "check"}
				case "malformed-parent":
					job["parent_job_id"] = json.Number("3387")
				case "source":
					job["scope_source"] = "caller"
				case "repository", "worktree", "branch":
					owner[change] = "different"
				case "no-common":
					delete(owner, "git_common")
				case "duplicate-owner":
					owner["Repository"] = owner["repository"]
				}
				if _, ok := check(jobs); ok {
					t.Fatal("changed original registration metadata accepted")
				}
			})
		}
	}
}

func TestOfficialCI3894JobMetadataProjection(t *testing.T) {
	id := official3894OriginalCreator
	raw := `{"id":"` + id + `","todo_id":3894,"prompt":"synthetic ignored original request","last_message":{"synthetic":"ignored"},"work_owner":{"repository":"autodevjapan/cc-pc-tools-rs"}}`
	got, ok := official3894JobProjection(strings.NewReader(raw), id)
	if !ok || len(got) != 3 || got["prompt"] != nil || got["last_message"] != nil {
		t.Fatal("non-metadata was decoded into the projection")
	}
	for _, bad := range []string{
		strings.Replace(raw, `"todo_id":3894`, `"todo_id":3894,"todo_id":3893`, 1),
		strings.Replace(raw, `"repository":"autodevjapan/cc-pc-tools-rs"`, `"repository":"autodevjapan/cc-pc-tools-rs","repository":"other"`, 1),
		raw + ` {}`, raw[:len(raw)-1],
	} {
		if _, ok := official3894JobProjection(strings.NewReader(bad), id); ok {
			t.Fatal("ambiguous/truncated original metadata accepted")
		}
	}
}

func TestOfficialCI3894IndependentLineage(t *testing.T) {
	if !official3894TaskLineage(official3894LineageFixture()) {
		t.Fatal("the nominated ordinary independent implementation/provider were rejected")
	}
	for _, which := range []int{3888, 3894} {
		for _, change := range []string{"typed-parent", "typed-origin", "scheduler-parent", "admission", "original-missing", "other-task-id"} {
			t.Run(strconv.Itoa(which)+"/"+change, func(t *testing.T) {
				reg := official3894LineageFixture()
				row := reg.Tasks[which]
				switch change {
				case "typed-parent":
					row.Subwork.Parent = 3387
				case "typed-origin":
					row.Subwork.ParentJob = "another-parent"
				case "scheduler-parent":
					row.Scheduler.SubworkParent = "another-parent"
				case "admission":
					row.Scheduler.Admission = &skillsAdmission{}
				case "original-missing":
					row.Jobs = []string{"another-original"}
				case "other-task-id":
					row.ID = 3387
				}
				reg.Tasks[which] = row
				if official3894TaskLineage(reg) {
					t.Fatal("coexisting task rows accepted a missing or changed independent registration")
				}
			})
		}
	}
}

// The core separately tests the same state machine with native pinned files.
// This seam asserts that the CLI's actual encoder is inside that callback.
type official3894HandoffFixture struct {
	state oc.State
	calls int
}

func (f *official3894HandoffFixture) Consume(_ context.Context, _ string, use func([]oc.Payload) error) error {
	f.calls++
	f.state = oc.Consuming
	if e := use([]oc.Payload{{Name: "public.txt", Bytes: []byte("synthetic public bytes")}}); e != nil {
		f.state = oc.Failed
		return e
	}
	f.state = oc.Consumed
	return nil
}

type official3894BrokenWriter struct {
	writes int
	err    error
}

func (f *official3894BrokenWriter) Write(p []byte) (int, error) {
	f.writes++
	return len(p) / 2, f.err
}

func TestOfficialCI3894HandoffWriteFailure(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic broken pipe"), nil} {
		t.Run(strconv.FormatBool(failure != nil), func(t *testing.T) {
			engine := &official3894HandoffFixture{}
			out := &official3894BrokenWriter{err: failure}
			code := runOfficial3894CLI([]string{"--consume-official-ci3387", strings.Repeat("a", 32)}, out,
				func(ctx context.Context, _, id string, handoff func(object) error) (object, error) {
					e := official3894Consume(ctx, engine, id, func() object { return official3894Failure("purpose_or_io") }, handoff)
					return nil, e
				})
			if code != 1 || engine.calls != 1 || engine.state != oc.Failed || out.writes != 1 {
				t.Fatalf("failed stdout must fail within consumption, preserve failed state, and never append JSON: code=%d state=%s calls=%d writes=%d", code, engine.state, engine.calls, out.writes)
			}
		})
	}
}

func TestOfficialCI3894ExactWriter(t *testing.T) {
	_, err := (official3894ExactWriter{&official3894BrokenWriter{}}).Write([]byte("synthetic public bytes"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short nil-error write was reported as a complete delivery")
	}
}

func TestOfficialCI3894InstalledSoftwarePin(t *testing.T) {
	raw, e := os.ReadFile("../docs/evidence/official-ci3387-known-guard-baseline-f30f9e76.json")
	if e != nil {
		t.Fatal(e)
	}
	var published struct {
		Domain       string `json:"domain"`
		RepositoryID int64  `json:"repository_id"`
		SHA          string `json:"reported_current_sha256"`
		Head         string `json:"reported_current_source_head"`
	}
	if json.Unmarshal(raw, &published) != nil || published.Domain != "official-ci3387-independent-guard-baseline-v1" || published.RepositoryID != 1406818713 || !official3894InstalledBaseline(published.SHA, published.Head) {
		t.Fatal("compiled pin differs from the independent producer's accepted software metadata")
	}
	if !official3894InstalledBaseline(official3894PreviousGuardSHA, official3894PreviousGuardHead) {
		t.Fatal("independent published pair missing")
	}
	for _, pair := range [][2]string{
		{"f3927b8850258098d54dae87196ae382960dc83a5b6e7dd11354ab97ffd07cdf", "f5f846a8470b544846e4c9ce43306061bf5668f9"},
		{"ccb5e797e6b5627d98ade2a562f2f98f3c121a5403e1523071afc07f73af4556", "038bc2570014132321919bf6758d68979506b741"},
		{"916bff404f54548814152efcd186dd772b33fd32872fd86fa88bc8d8718df9e3", "60a1b82b62d60cfcd66136aa4527041af643c4d6"},
		{strings.Repeat("a", 64), official3894PreviousGuardHead},
		{official3894PreviousGuardSHA, strings.Repeat("a", 40)},
		{strings.ToUpper(official3894PreviousGuardSHA), official3894PreviousGuardHead},
		{"", ""},
	} {
		if official3894InstalledBaseline(pair[0], pair[1]) {
			t.Fatal("unknown observed software adopted")
		}
	}
	var out, errout bytes.Buffer
	code := runGuardCLI([]string{"--verify-official-ci3387-update", "--guard-path", "synthetic"}, &out, &errout, nil)
	var r object
	if code != 2 || json.Unmarshal(out.Bytes(), &r) != nil || r["purpose_verified"] != false || errout.Len() != 0 {
		t.Fatal("software comparison mixed CLI escaped machine refusal")
	}
}

func TestOfficialCI3387UpdaterMatchesIndependentCurrentSoftware(t *testing.T) {
	if !official3894InstalledBaseline("02c8cad70e44f2bc4cd4e5a70dfdaf581a7ee6896eb63449fe1606eb28f5b5be", "f30f9e763e25884fa48d1cce0cac22da1cebfb69") {
		t.Fatal("the updater cannot compare the independently accepted current software")
	}
	for _, path := range []string{
		"../docs/skills-3080-craft-create-evidence214717.json",
		"../docs/skills-3080-codex3-after542-current20-evidence170221.json",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var reported struct {
			SHA     string `json:"creator_runtime_sha256"`
			Head    string `json:"creator_runtime_revision"`
			Current struct {
				SHA  string `json:"sha256"`
				Head string `json:"revision"`
			} `json:"current_runtime"`
		}
		if json.Unmarshal(raw, &reported) != nil {
			t.Fatal("invalid independent software report")
		}
		if reported.Current.SHA != "" {
			reported.SHA, reported.Head = reported.Current.SHA, reported.Current.Head
		}
		if !official3894InstalledBaseline(reported.SHA, reported.Head) {
			t.Fatal("compiled software pair differs from the independent accepted reports")
		}
	}
}

func TestOfficialCI3894HandoffSuccessOnce(t *testing.T) {
	engine := &official3894HandoffFixture{}
	var out bytes.Buffer
	code := runOfficial3894CLI([]string{"--consume-official-ci3387", strings.Repeat("a", 32)}, &out,
		func(ctx context.Context, _, id string, handoff func(object) error) (object, error) {
			e := official3894Consume(ctx, engine, id, func() object {
				if engine.state != oc.Consuming {
					t.Error("output was delayed until after the consumption commit")
				}
				return object{"state": engine.state, "access_allowed": false}
			}, handoff)
			return nil, e
		})
	var result struct {
		Purpose bool         `json:"purpose_verified"`
		Files   []oc.Payload `json:"files"`
	}
	if code != 0 || engine.state != oc.Consumed || engine.calls != 1 || json.Unmarshal(out.Bytes(), &result) != nil || !result.Purpose || len(result.Files) != 1 || string(result.Files[0].Bytes) != "synthetic public bytes" {
		t.Fatalf("single public response/consumed commit failed: code=%d state=%s", code, engine.state)
	}
}
