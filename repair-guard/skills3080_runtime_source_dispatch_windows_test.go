package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSkills3080SourceStartExplainsNormalAdmission(t *testing.T) {
	for _, c := range []struct{ name, raw, want string }{
		{"priority", `{"id":6291,"status":"todo","phase":"retry","active":false,"launched":null,"priority_wait":6291,"allowance_now":1,"busy_resources":null,"not_before":0,"preparation_reason":null}`, "priority_wait"},
		{"active", `{"id":6291,"status":"doing","phase":"running","active":true,"launched":null,"priority_wait":null,"allowance_now":null,"busy_resources":null,"not_before":null,"preparation_reason":null}`, "active"},
		{"capacity", `{"id":6291,"status":"todo","phase":"queued","active":false,"launched":null,"priority_wait":null,"allowance_now":0,"busy_resources":null,"not_before":null,"preparation_reason":null}`, "capacity"},
		{"preparation", `{"id":6291,"status":"todo","phase":"retry","active":false,"launched":null,"priority_wait":null,"allowance_now":1,"busy_resources":null,"not_before":null,"preparation_reason":"tool_failed"}`, "preparation"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output bytes.Buffer
			code := runSkillsCLI([]string{"--start-skills3080-runtime-source", "D:/synthetic/request.json"}, &output, func(string, string) object {
				return skillsRuntimeSourceDispatchReceipt([]byte(c.raw), 6291, hashBytes([]byte("request")), strings.Repeat("a", 32))
			})
			var v object
			if json.Unmarshal(output.Bytes(), &v) != nil || code != 1 || v["qualified"] != false || v["failure_stage"] != "source_normal_dispatch" {
				t.Fatal("normal non-launch was promoted to success or lost its stage")
			}
			d := obj(v["normal_dispatch"])
			if d["outcome"] != "not_launched" || d["admission_reason"] != c.want {
				t.Fatalf("normal admission cause unavailable: %s", output.Bytes())
			}
		})
	}
}

func sourceDispatchFixtureEnv(mode, root string) []string {
	return append(os.Environ(), "CC_SOURCE_DISPATCH_FIXTURE="+mode, "CC_SOURCE_DISPATCH_FIXTURE_ROOT="+root)
}

// This is a local test process, never the scheduler or a model worker.
func TestSkills3080SourceDispatchFixtureProcess(t *testing.T) {
	mode := os.Getenv("CC_SOURCE_DISPATCH_FIXTURE")
	if mode == "" {
		return
	}
	switch mode {
	case "failure":
		fmt.Fprintln(os.Stderr, "subwork dispatch failed TimeoutError")
		os.Exit(1)
	case "private_failure":
		fmt.Fprintln(os.Stderr, "synthetic-private-value")
		os.Exit(9)
	case "timeout":
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		<-timer.C
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", (1<<20)+1))
	case "metadata":
		root := os.Getenv("CC_SOURCE_DISPATCH_FIXTURE_ROOT")
		if os.WriteFile(filepath.Join(root, "job.json.next"), []byte(`{"status":"running"}`), 0600) != nil {
			os.Exit(11)
		}
		if os.Rename(filepath.Join(root, "job.json.next"), filepath.Join(root, "job.json")) != nil {
			os.Exit(17)
		}
		if os.WriteFile(filepath.Join(root, "trace", "new-input.json"), []byte(`{"synthetic":true}`), 0600) != nil {
			os.Exit(12)
		}
		f, e := os.OpenFile(filepath.Join(root, "native.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			os.Exit(13)
		}
		_, e = f.WriteString("new synthetic append\n")
		f.Close()
		if e != nil {
			os.Exit(14)
		}
		f, e = os.OpenFile(filepath.Join(root, "scope.json"), os.O_WRONLY, 0600)
		if e == nil {
			f.Close()
			os.Exit(15)
		}
		fmt.Fprint(os.Stdout, `{"immutable_write_refused":true,"metadata_written":true,"trace_written":true,"native_appended":true}`)
	default:
		os.Exit(16)
	}
	os.Exit(0)
}

func TestSkills3080SourceDispatchCapturesBoundedActualCommandOutcomes(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		mode, outcome, errorCode string
		limit                    time.Duration
	}{
		{"failure", "command_failed", "TimeoutError", 5 * time.Second},
		{"private_failure", "command_failed", "", 5 * time.Second},
		{"timeout", "timeout", "", 100 * time.Millisecond},
		{"large", "output_limit", "", 5 * time.Second},
	} {
		t.Run(c.mode, func(t *testing.T) {
			r := skillsRuntimeSourceRunHidden(exe, []string{"-test.run=^TestSkills3080SourceDispatchFixtureProcess$"}, t.TempDir(), sourceDispatchFixtureEnv(c.mode, ""), c.limit)
			if r.ok || r.diagnostic["outcome"] != c.outcome || str(r.diagnostic["command_error"]) != c.errorCode {
				t.Fatalf("command result=%v", r.diagnostic)
			}
			if len(r.stdout) > 1<<20 {
				t.Fatal("unbounded retained output")
			}
			var output bytes.Buffer
			runSkillsCLI([]string{"--start-skills3080-runtime-source", "D:/synthetic/request.json"}, &output, func(string, string) object {
				return skillsRuntimeSourceDispatch(r, 6291, hashBytes([]byte("request")), strings.Repeat("a", 32))
			})
			var v object
			json.Unmarshal(output.Bytes(), &v)
			if obj(v["normal_dispatch"])["outcome"] != c.outcome || strings.Contains(output.String(), "synthetic-private-value") {
				t.Fatal("command diagnostic lost or private stderr returned")
			}
		})
	}
	r := skillsRuntimeSourceRunHidden(filepath.Join(t.TempDir(), "absent.exe"), nil, t.TempDir(), os.Environ(), time.Second)
	if r.ok || r.diagnostic["outcome"] != "start_failed" {
		t.Fatal("start failure was reported as a child exit")
	}
}

func TestSkills3080SourceDispatchPinsDoNotBlockNormalMutableFiles(t *testing.T) {
	rawRoot := t.TempDir()
	root, err := ownerPhysicalPath(rawRoot)
	if err != nil {
		t.Fatal("physical fixture resolution", err)
	}
	t.Logf("temporary fixture alias resolved=%t", !same(rawRoot, root))
	for name, body := range map[string]string{"job.json": `{"status":"succeeded"}`, "scope.json": `{"synthetic":"immutable"}`, "native.jsonl": "old synthetic append\n"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Mkdir(filepath.Join(root, "trace"), 0700); e != nil {
		t.Fatal(e)
	}
	s := skillsFS{blocked: func(string) bool { return false }}
	defer s.close()
	if _, ok := s.read(filepath.Join(root, "scope.json"), 1024); !ok {
		t.Fatal("fixed scope pin")
	}
	probe := skillsFS{blocked: func(string) bool { return false }}
	if _, ok := probe.read(filepath.Join(root, "job.json"), 1024); !ok {
		t.Fatal("mutable metadata observation")
	}
	if f, e := os.OpenFile(filepath.Join(root, "job.json"), os.O_WRONLY, 0600); e == nil {
		f.Close()
		t.Fatal("negative control did not retain deny-write")
	}
	probe.close()
	ph, ok := s.directory(root)
	if !ok {
		t.Fatal("physical parent")
	}
	h, ok := skillsOpenAtSharing(ph, "native.jsonl", false, false, true)
	if !ok {
		t.Fatal("append-log pin")
	}
	f := os.NewFile(uintptr(h), filepath.Join(root, "native.jsonl"))
	s.files = append(s.files, f)
	if _, ok := skillsSourceDirectoryID(&s, filepath.Join(root, "trace")); !ok {
		t.Fatal("physical trace directory")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	r := skillsRuntimeSourceRunHidden(exe, []string{"-test.run=^TestSkills3080SourceDispatchFixtureProcess$"}, root, sourceDispatchFixtureEnv("metadata", root), 5*time.Second)
	if !r.ok {
		t.Fatalf("normal mutable writes incompatible with retained pins: %v", r.diagnostic)
	}
	var v struct {
		Immutable bool `json:"immutable_write_refused"`
		Metadata  bool `json:"metadata_written"`
		Trace     bool `json:"trace_written"`
		Native    bool `json:"native_appended"`
	}
	if !skillsStrict(r.stdout, &v) || !v.Immutable || !v.Metadata || !v.Trace || !v.Native {
		t.Fatal("not all independent file operations were confirmed")
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil {
		t.Fatal("append-log handle lost")
	}
}

func TestSkills3080SourceDispatchPinsUnderAliasedTemp(t *testing.T) {
	root := skillsFixtureRoot(t)
	real, alias := filepath.Join(root, "real"), filepath.Join(root, "alias")
	if os.Mkdir(real, 0700) != nil {
		t.Fatal("physical temporary directory")
	}
	c := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, real)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if _, err := c.CombinedOutput(); err != nil {
		t.Fatal("temporary junction fixture", err)
	}
	defer os.Remove(alias)
	t.Setenv("TEMP", alias)
	t.Setenv("TMP", alias)
	t.Run("physical_metadata_and_immutable_scope", TestSkills3080SourceDispatchPinsDoNotBlockNormalMutableFiles)
}

func TestSkills3080SourceDispatchFailureNeverForwardsUnselectedValues(t *testing.T) {
	const secret = "synthetic-private-value"
	for _, raw := range []string{
		`{"id":6291,"launched":null,"active":false,"preparation_reason":"` + secret + `","path":"` + secret + `"}`,
		`{"id":6291,"launched":null,"active":"` + secret + `","status":"` + secret + `","phase":"` + secret + `"}`,
		`{"id":6291,"launched":null,"priority_wait":6292,"allowance_now":"` + secret + `"}`,
	} {
		var output bytes.Buffer
		runSkillsCLI([]string{"--start-skills3080-runtime-source", "D:/synthetic/request.json"}, &output, func(string, string) object {
			return skillsRuntimeSourceDispatchReceipt([]byte(raw), 6291, hashBytes([]byte("request")), strings.Repeat("a", 32))
		})
		if strings.Contains(output.String(), secret) {
			t.Fatal("normal refusal disclosed unselected data")
		}
	}
}

func TestSkills3080SourceStartPreservesOnlyObservedPriorIssuer(t *testing.T) {
	current := skillsProducer{strings.Repeat("b", 64), strings.Repeat("c", 40)}
	prior := skillsProducer{official3894SourceDispatchPredecessorSHA, official3894SourceDispatchPredecessorHead}
	for _, p := range []skillsProducer{current, prior} {
		if !skillsSourceStartProducerAccepted(p, current) {
			t.Fatal("current or independently observed source issuer lost")
		}
	}
	for _, p := range []skillsProducer{
		{prior.SHA, strings.Repeat("d", 40)},
		{strings.Repeat("d", 64), prior.Revision},
		{official3894RuntimeOriginPredecessorSHA, official3894RuntimeOriginPredecessorHead},
		{strings.Repeat("d", 64), strings.Repeat("d", 40)},
	} {
		if skillsSourceStartProducerAccepted(p, current) {
			t.Fatal("unknown, mismatched, or source-less old issuer accepted")
		}
	}
}

func TestSkills3080SourceDispatchDiagnosticCannotCarryExtraData(t *testing.T) {
	good := skillsSourceDispatchAdmission([]byte(`{"id":6291,"launched":null,"active":true}`), 6291)
	for _, field := range []string{"outcome", "command_error", "admission_reason", "status", "phase", "preparation_reason", "private_path", "stderr"} {
		t.Run(field, func(t *testing.T) {
			bad := object{}
			for k, v := range good {
				bad[k] = v
			}
			bad[field] = "synthetic-private-value"
			var out bytes.Buffer
			runSkillsCLI([]string{"--start-skills3080-runtime-source", "D:/synthetic/request.json"}, &out, func(string, string) object {
				return object{"qualified": false, "reason": "source", "failure_stage": "source_normal_dispatch", "normal_dispatch": bad}
			})
			var v object
			json.Unmarshal(out.Bytes(), &v)
			if v["normal_dispatch"] != nil || strings.Contains(out.String(), "synthetic-private-value") {
				t.Fatal("invalid diagnostic passed refusal sanitizer")
			}
		})
	}
	var out bytes.Buffer
	runSkillsCLI([]string{"--prepare-skills3080-runtime-source", "D:/synthetic/request.json"}, &out, func(string, string) object {
		return object{"qualified": false, "reason": "source", "failure_stage": "source_normal_dispatch", "normal_dispatch": good}
	})
	var v object
	json.Unmarshal(out.Bytes(), &v)
	if v["normal_dispatch"] != nil {
		t.Fatal("start diagnostic admitted on another operation")
	}
}
