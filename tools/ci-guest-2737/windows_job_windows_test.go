package main

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	wr "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsCOMRejectionKeepsOnlyTypedDiagnostic(t *testing.T) {
	_, rejection := sdk.SendCOMBootstrap(bytes.NewBufferString("sensitive-2737-marker\n"), "nonce-2737")
	if got := windowsCOMRejection(rejection).Error(); !strings.Contains(got, "COM_BOOTSTRAP_JSON_REJECTED") || strings.Contains(got, "sensitive-2737-marker") {
		t.Fatal("typed diagnostic omitted or rejected frame exposed")
	}
	if got := windowsCOMRejection(errors.New("sensitive-2737-marker")).Error(); got != "Windows COM bootstrap rejected" {
		t.Fatal("untyped transport error exposed")
	}
}

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
func TestWindowsClockMustBePersistedBeforeSourceAndToken(t *testing.T) {
	w := windowsFixture(t)
	p := w.seed.Plan
	ctx := context.Background()
	source := wr.Event{Instance: p.Instance, Stage: wr.SourceReady}
	if e := w.Observe(ctx, source); e == nil || w.sourceReady {
		t.Fatal("source admitted before UTC synchronization")
	}
	ms := time.Now().UnixMilli()
	r := wr.ClockReceipt{Instance: p.Instance, HostUnixMillis: ms, BeforeUnixMillis: ms + 57600000, AfterUnixMillis: ms}
	bad := r
	bad.Instance = strings.Repeat("e", 32)
	if e := w.ClockObserved(ctx, p, bad); e == nil || w.clockReady {
		t.Fatal("another guest's UTC receipt admitted")
	}
	w.save = func(string, any) error { return errors.New("fixture receipt write failure") }
	if e := w.ClockObserved(ctx, p, r); e == nil || w.clockReady {
		t.Fatal("unpersisted UTC receipt admitted")
	}
	w.save = func(string, any) error { return nil }
	if e := w.ClockObserved(ctx, p, r); e != nil {
		t.Fatal(e)
	}
	if e := w.Observe(ctx, source); e != nil {
		t.Fatal(e)
	}
	if token, e := w.Token(ctx, p); e != nil || len(token) == 0 {
		t.Fatal("positive clock/source admission failed", e)
	}
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
	if e := w.ClockObserved(ctx, p, wr.ClockReceipt{Instance: p.Instance, HostUnixMillis: time.Now().UnixMilli(), BeforeUnixMillis: time.Now().Add(16 * time.Hour).UnixMilli(), AfterUnixMillis: time.Now().UnixMilli()}); e != nil {
		t.Fatal(e)
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
			w.clockReady = true
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

func TestWindowsCleanupBeforeVMRetainsIntentAndAdmissionOnUnknownClone(t *testing.T) {
	for _, started := range []bool{false, true} {
		for _, detached := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage_%v_detached_%v", started, detached), func(t *testing.T) {
				root := t.TempDir()
				intent := filepath.Join(root, "intent.json")
				if e := os.WriteFile(intent, []byte("owned-test-intent"), 0600); e != nil {
					t.Fatal(e)
				}
				w := windowsFixture(t)
				calls := 0
				ret := finishWindowsCleanup(context.Background(), nil, root, intent, w.seed.Plan.Instance, vmState{}, true, w, windowsCloneLease{StageStarted: started}, func() error {
					calls++
					if !detached {
						return errors.New("fixture attached clone or unavailable broker")
					}
					return nil
				})
				if started && calls != 1 || !started && calls != 0 {
					t.Fatal("clone observation stage mismatch")
				}
				var fence *unsafeVMLease
				unknown := started && !detached
				if errors.As(ret, &fence) != unknown {
					t.Fatal("unknown clone did not fence admission", ret)
				}
				_, stat := os.Stat(intent)
				if unknown && stat != nil || !unknown && !errors.Is(stat, os.ErrNotExist) {
					t.Fatal("cleanup intent lifetime incorrect", stat)
				}
				if unknown && !fence.WindowsClone {
					t.Fatal("clone fence lost recovery obligation")
				}
				b, e := os.ReadFile(filepath.Join(root, "job-cleanup.private.json"))
				if e != nil {
					t.Fatal(e)
				}
				var proof map[string]any
				if json.Unmarshal(b, &proof) != nil || proof["clone_detached"] == unknown || proof["vm_gone"] != true {
					t.Fatal("cleanup proof misrepresented clone or VM", string(b))
				}
			})
		}
	}
}

func TestWindowsCloneObservationIsReadOnlyAndRejectsAttachedOrLoadedHive(t *testing.T) {
	for _, fixture := range []string{"detached", "attached", "hive", "read-error", "missing-state"} {
		t.Run(fixture, func(t *testing.T) {
			// Execute ONLY the exact observation script, with all Hyper-V/registry
			// reads stubbed. No disk mount, VM, broker or host registry is touched.
			prefix := `function Import-Module {param($Name)}
function Test-Path {param($LiteralPath,$PathType);if($LiteralPath.StartsWith('Registry::')){return `
			if fixture == "hive" {
				prefix += "$true"
			} else {
				prefix += "$false"
			}
			prefix += `};return $true}
function Get-VHD {param($Path,$ErrorAction);`
			if fixture == "read-error" {
				prefix += `throw 'fixture read failure'`
			} else if fixture == "missing-state" {
				prefix += `return [pscustomobject]@{}`
			} else if fixture == "attached" {
				prefix += `return [pscustomobject]@{Attached=$true}`
			} else {
				prefix += `return [pscustomobject]@{Attached=$false}`
			}
			prefix += "}\n"
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := command(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; & ([ScriptBlock]::Create([Console]::In.ReadToEnd()))")
			c.Stdin = strings.NewReader(prefix + windowsCloneDetachedScript(`C:\owned\a'b`))
			e := c.Run()
			if fixture == "detached" && e != nil || fixture != "detached" && e == nil {
				t.Fatal("clone observation classification wrong", fixture, e)
			}
		})
	}
}
