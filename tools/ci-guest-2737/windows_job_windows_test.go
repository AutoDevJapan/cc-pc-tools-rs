package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"encoding/json"
	"errors"
	wr "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func structProof(q linuxJobRequest) sourcepacket.Receipt {
	return sourcepacket.Receipt{RepoID: 1154066087, PR: 3613, HeadSHA: q.Source.HeadSHA, TreeSHA: q.Source.TreeSHA, BlobCount: 1, PhysicalBlobsVerified: true}
}

func windowsFixture(t *testing.T) *windowsController {
	t.Helper()
	q := linuxJobRequest{Source: fixedSourcePin(), RunID: 17, Attempt: 1, Role: wr.Role, RouteLease: strings.Repeat("a", 32)}
	b := jobBinding{RepoID: 1154066087, PR: 3613, RunID: 17, JobID: 29, Attempt: 1, SHA: q.Source.HeadSHA, Tree: q.Source.TreeSHA, WorkflowSHA: q.Source.WorkflowSHA256, Role: wr.Role, OS: "Windows", TimeoutMinutes: 20, Label: normalci.RuntimeLabel(wr.Role, 17), JobName: wr.Role}
	s, e := wr.NewSeed(q.Source, b, strings.Repeat("b", 32), strings.Repeat("c", 64), 3, 4)
	if e != nil {
		t.Fatal(e)
	}
	w := &windowsController{q: q, seed: s, binding: b, save: func(string, any) error { return nil }, bind: func(context.Context, int64, string) (jobBinding, error) { return b, nil }, list: func(context.Context) ([]originalRunner, error) { return nil, nil }}
	w.api = func(_ context.Context, method, endpoint string, _ any, out any) error {
		if method != "POST" || endpoint != "repos/"+originalRepo+"/actions/runners/registration-token" {
			t.Fatal("unexpected API mutation")
		}
		return json.Unmarshal([]byte(`{"Token":"synthetic-token"}`), out)
	}
	return w
}
func TestWindowsHostUsesOriginalBindingAndPersistsBeforeListen(t *testing.T) {
	w := windowsFixture(t)
	p := w.seed.Plan
	ctx := context.Background()
	if !validOfficialBinding(w.q, w.binding) {
		t.Fatal("Windows original schema rejected")
	}
	if _, e := w.Token(ctx, p); e == nil {
		t.Fatal("token before physical source ack")
	}
	if e := w.Observe(ctx, wr.Event{Instance: p.Instance, Stage: wr.SourceReady}); e != nil {
		t.Fatal(e)
	}
	saved := false
	w.save = func(name string, v any) error {
		if name == "registration-intent.private.json" {
			saved = true
		}
		return nil
	}
	w.api = func(_ context.Context, _, _ string, _ any, out any) error {
		if !saved {
			t.Fatal("token API before flushed intent")
		}
		return json.Unmarshal([]byte(`{"Token":"synthetic-token"}`), out)
	}
	if token, e := w.Token(ctx, p); e != nil || len(token) == 0 {
		t.Fatal(e)
	}
	r := originalRunner{ID: 41, Name: p.Name, Status: "offline"}
	r.Labels = append(r.Labels, struct{ Name string }{p.Label})
	w.list = func(context.Context) ([]originalRunner, error) { return []originalRunner{r}, nil }
	w.save = func(name string, _ any) error {
		if name == "runner-identity.private.json" {
			return errors.New("fixture disk full")
		}
		return nil
	}
	if id, e := w.Registered(ctx, p); e == nil || id != 0 || w.runnerID != 0 {
		t.Fatal("unpersisted ID acknowledged")
	}
	w.save = func(string, any) error { return nil }
	if id, e := w.Registered(ctx, p); e != nil || id != 41 {
		t.Fatal("valid numeric offline ID rejected", e)
	}
}
func TestWindowsHostRefusesChangedQueueNonceAndRegistration(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"queue", "nonce", "duplicate", "busy", "label", "online", "persist"} {
		t.Run(change, func(t *testing.T) {
			w := windowsFixture(t)
			p := w.seed.Plan
			w.sourceReady = true
			calls := 0
			w.api = func(context.Context, string, string, any, any) error {
				calls++
				return errors.New("API should not be called")
			}
			switch change {
			case "queue":
				w.bind = func(context.Context, int64, string) (jobBinding, error) {
					b := w.binding
					b.SHA = strings.Repeat("d", 40)
					return b, nil
				}
			case "nonce":
				w.list = func(context.Context) ([]originalRunner, error) { return []originalRunner{{Name: p.Name}}, nil }
			case "persist":
				w.save = func(string, any) error { return errors.New("read-only disk") }
			default:
				w.mayExist = true
				r := originalRunner{ID: 41, Name: p.Name, Status: "offline"}
				r.Labels = append(r.Labels, struct{ Name string }{p.Label})
				if change == "busy" {
					r.Busy = true
				}
				if change == "online" {
					r.Status = "online"
				}
				if change == "label" {
					r.Labels[0].Name = normalci.RuntimeLabel(wr.Role, 18)
				}
				list := []originalRunner{r}
				if change == "duplicate" {
					list = append(list, r)
				}
				w.list = func(context.Context) ([]originalRunner, error) { return list, nil }
				if _, e := w.Registered(ctx, p); e == nil {
					t.Fatal("changed registration acknowledged")
				}
				return
			}
			if _, e := w.Token(ctx, p); e == nil || calls != 0 {
				t.Fatal("refusal did not precede mutation")
			}
		})
	}
}
func TestWindowsHostClaimsOneOriginalJobInOrder(t *testing.T) {
	w := windowsFixture(t)
	p := w.seed.Plan
	w.runnerID = 41
	event := wr.Event{Instance: p.Instance, RunnerID: 41, Stage: wr.Start, Proof: structProof(w.q), Context: wr.HookContext{Repository: originalRepo, SHA: p.Source.HeadSHA, WorkflowSHA: p.Source.HeadSHA, Ref: "refs/heads/codex/todo2641-http-response-pseudo-20261015", Event: "workflow_dispatch", Role: wr.Role, Name: p.Name, RunID: 17, Attempt: 1}}
	ctx := context.Background()
	event.Stage = wr.Complete
	if w.Claim(ctx, p, 41, event) == nil {
		t.Fatal("complete before start")
	}
	event.Stage = wr.Start
	if e := w.Claim(ctx, p, 41, event); e != nil {
		t.Fatal(e)
	}
	if w.Claim(ctx, p, 41, event) == nil {
		t.Fatal("second start")
	}
	event.Stage = wr.Complete
	event.Context.RunID = 18
	if w.Claim(ctx, p, 41, event) == nil {
		t.Fatal("foreign run")
	}
	event.Context.RunID = 17
	w.bind = func(_ context.Context, id int64, name string) (jobBinding, error) {
		if id != 41 || name != p.Name {
			t.Fatal("numeric claim absent")
		}
		return w.binding, nil
	}
	if e := w.Claim(ctx, p, 41, event); e != nil {
		t.Fatal(e)
	}
}
func TestWindowsRequestCannotChooseProgramAndHelperMustBeBuild(t *testing.T) {
	w := windowsFixture(t)
	root := t.TempDir()
	file := filepath.Join(root, "request.json")
	b, e := json.Marshal(w.q)
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(file, b, 0600) != nil {
		t.Fatal("fixture write")
	}
	if _, e = readJobRequest(file); e != nil {
		t.Fatal(e)
	}
	var injection map[string]any
	_ = json.Unmarshal(b, &injection)
	injection["ProductCI"] = "C:\\Windows\\System32\\cmd.exe"
	b, _ = json.Marshal(injection)
	_ = os.WriteFile(file, b, 0600)
	if _, e = readJobRequest(file); e == nil {
		t.Fatal("executable selection accepted")
	}
	if e = verifyWindowsHelper(file, map[string]any{"executor_code_sha": strings.Repeat("d", 40)}); e == nil {
		t.Fatal("non-build helper accepted")
	}
}

func TestWindowsOfflineStagePowerShellParsesWithoutExecution(t *testing.T) {
	// Parse the exact privileged script with PowerShell's parser. No broker,
	// hive, disk mount, VM or network operation is invoked by this acceptance.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parser := `$tokens=$null;$faults=$null;$null=[Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(),[ref]$tokens,[ref]$faults);if($faults.Count -ne 0){exit 1}`
	c := command(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-Command", parser)
	c.Stdin = strings.NewReader(windowsOfflineStageScript(`C:\owned\a'b`))
	if e := c.Run(); e != nil {
		t.Fatal("offline staging script syntax rejected", e)
	}
}
