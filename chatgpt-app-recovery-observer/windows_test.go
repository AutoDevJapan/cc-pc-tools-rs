package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hangXML() []byte {
	return []byte(`<Event><System><Provider Name="Application Hang"/><EventID>1002</EventID><TimeCreated SystemTime="2030-01-02T11:59:59Z"/><EventRecordID>101</EventRecordID></System><EventData><Data Name="AppName">ChatGPT.exe</Data><Data Name="ProcessId">0x42</Data><Data Name="StartTime">0x1234</Data><Data Name="PackageFullName">synthetic-package</Data><Data Name="PackageRelativeAppId">App</Data></EventData></Event>`)
}

func TestHangRequiresExactKnownGenerationAndAbsence(t *testing.T) {
	scope := windowsScope{fullPackage: "synthetic-package", target: Fingerprint("target")}
	last := instanceFingerprint(scope.target, 0x42, 0x1234)
	p := ProcessSample{Target: scope.target, Presence: Absent, ScopeKnown: true, InScope: true}
	called := 0
	missing := func(pid uint32, start uint64) bool { called++; return pid == 0x42 && start == 0x1234 }
	e, err := parseHangXML(hangXML(), scope, "epoch", last, p, missing)
	if err != nil || e == nil || !e.ExitVerified || !e.SessionMatches || called != 1 {
		t.Fatal(e, err, called)
	}
	for _, scenario := range []string{"never_seen", "other_generation", "present", "pid_reused"} {
		t.Run(scenario, func(t *testing.T) {
			prior := last
			sample := p
			probe := missing
			if scenario == "never_seen" {
				prior = ""
			}
			if scenario == "other_generation" {
				prior = Fingerprint("other")
			}
			if scenario == "present" {
				sample.Presence = Present
			}
			if scenario == "pid_reused" {
				probe = func(uint32, uint64) bool { return false }
			}
			e, err := parseHangXML(hangXML(), scope, "epoch", prior, sample, probe)
			if err != nil || e == nil || e.ExitVerified {
				t.Fatal(e, err)
			}
		})
	}
}

func TestHangSchemaRejectsSubstitutes(t *testing.T) {
	scope := windowsScope{fullPackage: "synthetic-package", target: Fingerprint("target")}
	for _, pair := range [][2]string{{"ChatGPT.exe", "CodexCoreCommandRunner.exe"}, {"synthetic-package", "other-package"}, {`Name="PackageRelativeAppId">App`, `Name="PackageRelativeAppId">Other`}, {"Application Hang", "Unknown Provider"}, {"0x1234", "not-a-start"}} {
		x := bytes.ReplaceAll(hangXML(), []byte(pair[0]), []byte(pair[1]))
		e, _ := parseHangXML(x, scope, "epoch", "", ProcessSample{}, func(uint32, uint64) bool { t.Fatal("unscoped exit probe"); return true })
		if e != nil && e.ExitVerified {
			t.Fatal("accepted substitute", pair)
		}
	}
}

func TestDefaultCLIAndNoActivationFlag(t *testing.T) {
	for _, args := range [][]string{nil, {"explain"}, {"activate"}, {"observe-once", "--live"}, {"observe-once", "--state", "D:/CC/Tools/observer-state.json"}} {
		var out bytes.Buffer
		code := runCLI(args, &out, func(context.Context, string) Result { t.Fatal("unexpected OS read"); return Result{} })
		if len(args) == 0 || args[0] == "explain" {
			if code != 0 {
				t.Fatal(code)
			}
		} else if code != 2 {
			t.Fatal(code)
		}
		var fields map[string]any
		if json.Unmarshal(out.Bytes(), &fields) != nil || fields["live_activation_enabled"] != false || fields["live_activation_calls"] != float64(0) {
			t.Fatal(out.String())
		}
	}
}

func TestExplicitCLIIsFiniteAndMetadataOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var out bytes.Buffer
	called := 0
	code := runCLI([]string{"observe-once", "--state", path}, &out, func(ctx context.Context, p string) Result {
		called++
		if p != path {
			t.Fatal("path changed")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("unbounded observation")
		}
		return Result{Stage: "decision", Decision: "unknown", Reason: "native_intent_unknown"}
	})
	if code != 0 || called != 1 || strings.Contains(out.String(), path) {
		t.Fatal(code, called, out.String())
	}
}

// This invariant is independent of a default-valued counter in Result. Check
// every production import and native procedure binding against a closed list.
func TestProductionHasNoActivationCapability(t *testing.T) {
	allowed := map[string]bool{}
	for _, name := range []string{"LockFileEx", "MoveFileExW", "GetPackagesByPackageFamily", "GetPackagePathByFullName", "GetApplicationUserModelId", "GetPackageFullName", "QueryFullProcessImageNameW", "ProcessIdToSessionId", "WTSQuerySessionInformationW", "WTSFreeMemory", "EvtQuery", "EvtNext", "EvtClose", "EvtRender", "EvtCreateRenderContext", "EvtOpenLog", "EvtGetLogInfo"} {
		allowed[name] = true
	}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, i := range f.Imports {
			if i.Path.Value == `"os/exec"` || strings.Contains(i.Path.Value, "net/") {
				return fmt.Errorf("unexpected process/network import in %s", path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "NewProc" {
					v, ok := call.Args[0].(*ast.BasicLit)
					if !ok || !allowed[strings.Trim(v.Value, `"`)] {
						t.Errorf("non-observation native binding in %s", path)
					}
				}
				for _, forbidden := range []string{"StartProcess", "CreateProcess", "TerminateProcess", "Kill", "ShellExecute", "ActivateApplication"} {
					if sel.Sel.Name == forbidden {
						t.Errorf("activation/process mutation in %s", path)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s, o := synthetic()
	e := testEngine(t, s, o)
	var fake fakeAdapter
	r := runSynthetic(e, o)
	fake.deliver(t, e, o, r)
	if fake.calls.Load() != 1 || r.LiveActivationEnabled || r.LiveActivationCalls != 0 {
		t.Fatal("positive fake case did not reach the tested consume/delivery boundary", r)
	}
}

func TestDecisionTimeAndRateBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*State, *Observation)
		want   string
	}{
		{"ttl_inclusive", func(s *State, o *Observation) { o.Event.At = o.At.Add(-EventTTL) }, "eligible_in_fake_only"},
		{"ttl_expired", func(s *State, o *Observation) { o.Event.At = o.At.Add(-EventTTL - time.Nanosecond) }, "expired"},
		{"future_event", func(s *State, o *Observation) { o.Event.At = o.At.Add(time.Second) }, "unknown"},
		{"gap_inclusive", func(s *State, o *Observation) { s.Requests = []time.Time{o.At.Add(-IncidentGap)} }, "eligible_in_fake_only"},
		{"rolling_expired", func(s *State, o *Observation) {
			s.Requests = []time.Time{o.At.Add(-time.Hour), o.At.Add(-40 * time.Minute), o.At.Add(-20 * time.Minute)}
		}, "eligible_in_fake_only"},
		{"unknown_never_times_out_to_consent", func(s *State, o *Observation) { o.syntheticNoStop = false; o.At = o.At.Add(2 * time.Minute) }, "unknown"},
		{"new_stop_generation", func(s *State, o *Observation) { o.Directives.Generation++ }, "suppressed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, o := synthetic()
			tc.change(&s, &o)
			if got := Decide(s, o); got.Decision != tc.want {
				t.Fatal(got)
			}
		})
	}
}
