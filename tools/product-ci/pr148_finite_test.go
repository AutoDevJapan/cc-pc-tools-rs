package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("CC_PR148_GATE_CHILD") == "1" {
		os.Exit(pr148GatedChild(os.Args[1:]))
	}
	os.Exit(m.Run())
}
func pr148SyntheticPlan() pr148LinuxPlan {
	return pr148LinuxPlan{strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 64), 101, 102, 1}
}
func pr148SyntheticPolicy() pr148CargoPolicy {
	var p pr148CargoPolicy
	for i, args := range pr148CargoArgs {
		p[i] = strings.Join(args, " ")
	}
	return p
}

func TestPR148NewSourcePinRequired(t *testing.T) {
	p := pr148SyntheticPlan()
	if !p.valid() {
		t.Fatal("fixture pin rejected")
	}
	p.Head = pr148Head
	if p.valid() {
		t.Fatal("original head accepted as new source")
	}
}
func TestPR148TreeDelta(t *testing.T) {
	old := pr148APITree{SHA: pr148Tree}
	next := pr148APITree{SHA: strings.Repeat("9", 40)}
	want := map[string]string{}
	for i, w := range pr148Workflows {
		old.Entries = append(old.Entries, pr148TreeEntry{Path: w.Path, Mode: "100644", Type: "blob", SHA: strings.Repeat(strconv.Itoa(i+1), 40)})
		s := strings.Repeat(strconv.Itoa(i+4), 40)
		want[w.Path] = s
		next.Entries = append(next.Entries, pr148TreeEntry{Path: w.Path, Mode: "100644", Type: "blob", SHA: s})
	}
	old.Entries = append(old.Entries, pr148TreeEntry{Path: "tools/source.rs", Mode: "100644", Type: "blob", SHA: strings.Repeat("7", 40)})
	next.Entries = append(next.Entries, old.Entries[3])
	if e := pr148TreeDelta(old, next, want); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"content", "mode", "extra", "duplicate", "truncated"} {
		t.Run(bad, func(t *testing.T) {
			n := next
			n.Entries = append([]pr148TreeEntry{}, next.Entries...)
			switch bad {
			case "content":
				n.Entries[3].SHA = strings.Repeat("8", 40)
			case "mode":
				n.Entries[3].Mode = "100755"
			case "extra":
				n.Entries = append(n.Entries, pr148TreeEntry{Path: "extra", Mode: "100644", Type: "blob", SHA: strings.Repeat("8", 40)})
			case "duplicate":
				n.Entries[3] = n.Entries[0]
			case "truncated":
				n.Truncated = true
			}
			if pr148TreeDelta(old, n, want) == nil {
				t.Fatal("unreviewed tree accepted")
			}
		})
	}
}

func TestPR148GateTranslation(t *testing.T) {
	p := pr148GatePacket{Policy: pr148SyntheticPolicy(), Index: 1, Target: `D:\owned\cargo-target`}
	for k, v := range map[string]string{"CARGO_BUILD_JOBS": "2", "RUST_TEST_THREADS": "2", "CODEX_JOBS_CARGO_LOCK_HELD": "1", "CODEX_JOBS_CARGO_TARGET_HELD": "fixed-key"} {
		t.Setenv(k, v)
	}
	args := append(append([]string{}, pr148CargoArgs[1]...), "--target-dir", p.Target, "--jobs", "2")
	got, jobs, tests, e := pr148TranslateGate(args, p)
	if e != nil || jobs != 2 || tests != 2 || !reflect.DeepEqual(got, pr148CargoArgs[1]) {
		t.Fatalf("gate additions mishandled: %v", e)
	}
	for _, extra := range [][]string{{"--target-dir", `D:\other`}, {"--config", "evil"}, {"-j", "10"}} {
		bad := append(append([]string{}, args...), extra...)
		if _, _, _, e = pr148TranslateGate(bad, p); e == nil {
			t.Fatal("caller override accepted")
		}
	}
	for _, key := range []string{"CARGO_BUILD_JOBS", "RUST_TEST_THREADS"} {
		t.Setenv(key, "3")
		if _, _, _, err := pr148TranslateGate(args, p); err == nil {
			t.Fatal("namespace cap exceeded")
		}
		t.Setenv(key, "2")
	}
	t.Setenv("CODEX_JOBS_CARGO_LOCK_HELD", "")
	if _, _, _, e = pr148TranslateGate(args, p); e == nil {
		t.Fatal("ungated heavy command accepted")
	}
}

func TestPR148GuestRejectsExcessiveCapsBeforeOpeningPlan(t *testing.T) {
	old := os.Stdin
	defer func() { os.Stdin = old }()
	for _, args := range [][]string{{"3", "2", "1"}, {"2", "3", "1"}} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := w.Write([]byte("G"))
		_ = w.Close()
		os.Stdin = r
		err = pr148ExecChild(args)
		_ = r.Close()
		if writeErr != nil || err == nil || err.Error() != "executor caps rejected" {
			t.Fatalf("excessive guest cap reached source access: %v", err)
		}
	}
}

func TestPR148FiniteCallbackRejectsIdentityAndReplay(t *testing.T) {
	p := pr148SyntheticPlan()
	policy := pr148SyntheticPolicy()
	calls := 0
	h := &pr148CargoHost{plan: p, policy: policy, key: strings.Repeat("a", 64), started: true, recheck: func(context.Context) error { return nil }, execute: func(context.Context, int) (pr148CargoResult, error) {
		calls++
		return pr148CargoResult{Exit: 9, Drained: true}, nil
	}}
	for _, bad := range []string{"run", "attempt", "repo", "nonce", "args", "host-field", ""} {
		q := pr148CargoRequest{p, 1392152644, h.key, pr148CargoArgs[1]}
		switch bad {
		case "run":
			q.Plan.RunID++
		case "attempt":
			q.Plan.Attempt++
		case "repo":
			q.RepoID++
		case "nonce":
			q.Nonce = strings.Repeat("b", 64)
		case "args":
			q.Args = append(append([]string{}, q.Args...), "--config", "evil")
		}
		body, _ := json.Marshal(q)
		if bad == "host-field" {
			body = append(body[:len(body)-1], []byte(`,"HostCommand":"evil"}`)...)
		}
		r := httptest.NewRequest("POST", "/pr148/cargo", bytes.NewReader(body))
		r.Header.Set("X-PR148-Nonce", h.key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if bad == "" {
			if w.Code != 200 {
				t.Fatalf("fixed request: %d %s", w.Code, w.Body)
			}
			var result pr148CargoResult
			_ = json.Unmarshal(w.Body.Bytes(), &result)
			if result.Exit != 9 {
				t.Fatal("failed exit lost")
			}
		} else if w.Code != 403 {
			t.Fatalf("%s accepted: %d", bad, w.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("rejected requests invoked gate %d times", calls)
	}
	q := pr148CargoRequest{p, 1392152644, h.key, pr148CargoArgs[1]}
	b, _ := json.Marshal(q)
	r := httptest.NewRequest("POST", "/pr148/cargo", bytes.NewReader(b))
	r.Header.Set("X-PR148-Nonce", h.key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 409 || calls != 1 {
		t.Fatal("request replay accepted")
	}
}

// Explicit real namespace + shared Windows gate test. Its only Rust source is
// the synthetic fixture below. No product source/API/token/runner is involved.
func TestPR148LinuxFiniteNamespaceCargoFixture(t *testing.T) {
	dir := os.Getenv("CC_PR148_FIXTURE_DIR")
	binaryFile := os.Getenv("CC_PR148_FIXTURE_ELF")
	if dir == "" || binaryFile == "" {
		t.Skip("explicit safe-delete owned fixture directory and ELF required")
	}
	guest, e := os.ReadFile(binaryFile)
	if e != nil {
		t.Fatal(e)
	}
	for _, caseName := range []string{"normal", "cancel", "overflow", "receipt-loss"} {
		t.Run(caseName, func(t *testing.T) {
			cancelCase := caseName == "cancel"
			largeCase := caseName == "overflow"
			lostCase := caseName == "receipt-loss"
			caseDir := filepath.Join(dir, fmt.Sprintf("namespace-%t-%d", cancelCase, time.Now().UnixNano()))
			if e = os.Mkdir(caseDir, 0700); e != nil {
				t.Fatal(e)
			}
			ctx, stop := context.WithTimeout(context.Background(), 4*time.Minute)
			defer stop()
			n, e := newPR148Namespace(ctx, caseDir)
			if n != nil && n.ID != "" {
				defer func() {
					ce := n.dispose()
					if e := pr148SaveNew(filepath.Join(caseDir, "namespace-result.private.json"), n); e != nil {
						t.Error(e)
					}
					if ce != nil {
						t.Error(ce)
					}
				}()
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = pr148Transfer(ctx, n, guest); e != nil {
				t.Fatal(e)
			}
			if e = cmd(ctx, docker, "exec", n.ID, "mkdir", "-p", pr148Workspace+"/tools/src", "/runner/_diag").Run(); e != nil {
				t.Fatal(e)
			}
			p := pr148SyntheticPlan()
			policy := pr148SyntheticPolicy()
			key := strings.Repeat("a", 64)
			if cancelCase {
				key = strings.Repeat("b", 64)
			}
			packet := pr148GuestPacket{Plan: p, Policy: policy, Repo: pr148Repo, Name: n.Name, Nonce: key}
			b, _ := json.Marshal(packet)
			transfer := func(path string, data []byte) {
				c := cmd(ctx, docker, "exec", "-i", n.ID, "sh", "-c", "cat > "+path)
				c.Stdin = bytes.NewReader(data)
				if e := c.Run(); e != nil {
					t.Fatal(e)
				}
			}
			transfer("/runner/pr148-plan.json", b)
			transfer("/toolchain/protected", []byte("credential-free"))
			transfer(pr148Workspace+"/tools/Cargo.toml", []byte("[package]\nname=\"todo2736-credential-free-fixture\"\nversion=\"0.0.0\"\nedition=\"2021\"\n[workspace]\n"))
			transfer(pr148Workspace+"/tools/src/lib.rs", []byte(`#[test]
fn child_is_drained() {
    for key in ["CARGO_BUILD_JOBS", "RUST_TEST_THREADS"] {
        let granted = std::env::var(key).unwrap().parse::<usize>().unwrap();
        assert!((1..=2).contains(&granted), "container cap exceeded: {key}");
    }
    let child = std::process::Command::new("sleep").arg("600").spawn().unwrap();
    std::fs::write("/work/fixture-child.pid", child.id().to_string()).unwrap();
    println!("credential-free Rust Cargo fixture");
    if std::path::Path::new("/work/fixture-large").exists() {
        use std::io::Write;
        let data=vec![b'L'; 65536];
        let mut stdout=std::io::stdout().lock();
        for _ in 0..528 { stdout.write_all(&data).unwrap(); }
    }
    if std::env::var("CC_PR148_CANCEL_FIXTURE").unwrap() == "true" {
        loop { std::thread::sleep(std::time::Duration::from_secs(1)); }
    }
    while !std::path::Path::new("/work/fixture-release").exists() {
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
}

`))
			if largeCase {
				transfer("/work/fixture-large", []byte("synthetic-overflow"))
			}
			if caseName == "normal" {
				b, e = cmd(ctx, docker, "exec", n.ID, "/runner/product-ci", "pr148-listener-probe").CombinedOutput()
				if e != nil {
					t.Fatalf("offline restricted listener: %v %s", e, b)
				}
				_ = os.WriteFile(filepath.Join(caseDir, "listener-version.private.log"), b, 0600)
			}
			b, e = cmd(ctx, docker, "exec", n.ID, "/runner/product-ci", "pr148-restriction-fixture").CombinedOutput()
			if e != nil {
				t.Fatalf("real kernel boundary: %v %s", e, b)
			}
			if !bytes.Contains(b, []byte("landlock-direct-copy-proc-toolchain-denied")) {
				t.Fatal("kernel restrictions not confirmed")
			}
			_ = os.WriteFile(filepath.Join(caseDir, "landlock.private.log"), b, 0600)
			h := &pr148CargoHost{plan: p, policy: policy, key: key, ns: n, dir: caseDir, target: filepath.Join(caseDir, "cargo-target"), started: true, recheck: func(context.Context) error { return nil }}
			h.loseReceiptFixture = lostCase
			flight, cancel := context.WithCancel(ctx)
			defer cancel()
			done := make(chan struct {
				r pr148CargoResult
				e error
			}, 1)
			h.execute = func(callCtx context.Context, index int) (pr148CargoResult, error) {
				r, e := h.runWithFixture(callCtx, index, true, cancelCase)
				done <- struct {
					r pr148CargoResult
					e error
				}{r, e}
				return r, e
			}
			endpoint, closeServer, e := startPR148Cargo(flight, h)
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				cancel()
				if e := closeServer(); e != nil {
					t.Error(e)
				}
			}()
			packet.Callback = endpoint
			packet.Nonce = h.key
			b, _ = json.Marshal(packet)
			transfer("/runner/pr148-plan.json", b)
			callbackLog, e := os.OpenFile(filepath.Join(caseDir, "guest-callback.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer callbackLog.Close()
			callback := cmd(ctx, docker, "exec", n.ID, "/runner/product-ci", "pr148-fixture-callback")
			callback.Stdout = callbackLog
			callback.Stderr = callbackLog
			if e = callback.Start(); e != nil {
				t.Fatal(e)
			}
			callbackDone := make(chan error, 1)
			go func() { callbackDone <- callback.Wait() }()
			ready := false
			deadline := time.Now().Add(90 * time.Second)
			for !ready && time.Now().Before(deadline) {
				if e = cmd(ctx, docker, "exec", n.ID, "sh", "-c", "test -s /work/fixture-child.pid && kill -0 $(cat /work/fixture-child.pid)").Run(); e == nil {
					ready = true
					break
				}
				select {
				case e := <-callbackDone:
					t.Fatalf("guest callback ended before real child observation: %v", e)
				case result := <-done:
					t.Fatalf("fixture ended before real child observation: %v exit=%d log=%s", result.e, result.r.Exit, result.r.Log)
				case <-time.After(100 * time.Millisecond):
				}
			}
			if !ready {
				cancel()
				result := <-done
				t.Fatalf("real fixture child absent: %v %s", result.e, result.r.Log)
			}
			if cancelCase {
				cancel()
			} else {
				if e = cmd(ctx, docker, "exec", n.ID, "touch", "/work/fixture-release").Run(); e != nil {
					cancel()
					<-done
					t.Fatal(e)
				}
			}
			result := <-done
			callbackExit := <-callbackDone
			if lostCase {
				if result.e == nil || !result.r.Drained || !h.broken || !n.TreeGone {
					t.Fatal("missing-receipt recovery did not disable and stop namespace")
				}
				if e = pr148SaveNew(filepath.Join(caseDir, "receipt-loss-recovery.private.json"), map[string]any{"broken": h.broken, "tree_gone": n.TreeGone, "failure": result.e.Error(), "actual_cargo_exit": result.r.Exit}); e != nil {
					t.Fatal(e)
				}
				return
			}
			if largeCase {
				if !result.r.LogTruncated || result.r.FullLogBytes <= 32<<20 || callbackExit == nil {
					t.Fatal("oversized full log/failed preview not preserved")
				}
			} else if !cancelCase && callbackExit != nil {
				t.Fatalf("guest callback failed: %v", callbackExit)
			}
			if result.e != nil || !result.r.Drained {
				t.Fatalf("fixture drain: %v %s", result.e, result.r.Log)
			}
			if (!cancelCase && result.r.Exit != 0) || (cancelCase && result.r.Exit != 125) {
				t.Fatalf("fixture exit=%d cancel=%t %s", result.r.Exit, cancelCase, result.r.Log)
			}
			if e = cmd(ctx, docker, "exec", n.ID, "sh", "-c", "! kill -0 $(cat /work/fixture-child.pid) 2>/dev/null").Run(); e != nil {
				t.Fatal("Cargo child survived host gate release")
			}
			if e = pr148SaveNew(filepath.Join(caseDir, "fixture-result.private.json"), struct {
				Cancel, ChildObserved, Drained, ProductSourceSupplied, AllRequiredChecksSuccess bool
				Exit, Bytes                                                                     int
				SHA                                                                             string
			}{cancelCase, true, true, false, false, result.r.Exit, len(result.r.Log), result.r.SHA}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
