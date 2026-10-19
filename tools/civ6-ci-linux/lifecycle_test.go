package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fixtureCrate = `#[test]
fn child_is_drained() {
    for key in ["CARGO_BUILD_JOBS", "RUST_TEST_THREADS"] {
        let granted = std::env::var(key).unwrap().parse::<usize>().unwrap();
        assert_eq!(granted, 1, "container cap exceeded: {key}");
    }
    let child = std::process::Command::new("sleep").arg("600").spawn().unwrap();
    std::fs::write("/work/fixture-child.pid", child.id().to_string()).unwrap();
    println!("credential-free Rust Cargo fixture");
    if std::env::var("CC_CIL_CANCEL_FIXTURE").unwrap() == "true" {
        loop { std::thread::sleep(std::time::Duration::from_secs(1)); }
    }
    while !std::path::Path::new("/work/fixture-release").exists() {
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
}
`

// Explicit real-namespace test with the shared Windows Cargo gate. Its only
// Rust source is the synthetic crate above: no product source, API, token,
// runner registration or GPU is involved. Set CC_CIL_FIXTURE_DIR to a NEW
// safe-delete owned directory.
func TestLifecycleFixtureNormalCancelAndReceiptLoss(t *testing.T) {
	dir := os.Getenv("CC_CIL_FIXTURE_DIR")
	if dir == "" || runtime.GOOS != "windows" {
		t.Skip("explicit safe-delete owned fixture directory (Windows + Docker Desktop) required")
	}
	elf := filepath.Join(dir, "civ6-ci-linux-guest")
	build := exec.Command("go", "build", "-trimpath", "-o", elf, ".")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("guest build: %v %s", e, out)
	}
	guest, e := os.ReadFile(elf)
	if e != nil || !isELF(guest) {
		t.Fatal("guest ELF unavailable")
	}
	for _, name := range []string{"normal", "cancel", "receipt-loss"} {
		t.Run(name, func(t *testing.T) {
			cancelCase, lostCase := name == "cancel", name == "receipt-loss"
			caseDir := filepath.Join(dir, fmt.Sprintf("ns-%s-%d", name, time.Now().UnixNano()))
			if e := os.Mkdir(caseDir, 0700); e != nil {
				t.Fatal(e)
			}
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Minute)
			defer stop()
			p := rustPlan()
			n, e := newNamespace(ctx, caseDir, KindRust, p)
			if n != nil && n.ID != "" {
				defer func() {
					ce := n.dispose()
					if e := saveNew(filepath.Join(caseDir, "namespace-result.private.json"), n); e != nil {
						t.Error(e)
					}
					if ce != nil {
						t.Error(ce)
					}
					if !n.TreeGone || !n.Removed {
						t.Error("namespace PID tree/removal not confirmed")
					}
				}()
			}
			if e != nil {
				t.Fatal(e)
			}
			// Real VM free memory is observable from the fresh namespace.
			if vm, e := n.vmMemAvailable(ctx); e != nil || vm == 0 {
				t.Fatalf("VM memory observation: %d %v", vm, e)
			}
			if e = n.execStdin(ctx, guest, "cat > "+guestBinary+" && chmod 755 "+guestBinary); e != nil {
				t.Fatal(e)
			}
			if e = cmd(ctx, dockerExe, "exec", n.ID, "mkdir", "-p", workspace+"/tools/src", "/runner/_diag").Run(); e != nil {
				t.Fatal(e)
			}
			key := rep("a", 64)
			packet := guestPacket{Plan: p, Kind: KindRust, Policy: fixturePolicy(), Name: n.Name, Nonce: key}
			put := func(path string, data []byte) {
				if e := n.execStdin(ctx, data, "cat > "+path); e != nil {
					t.Fatal(e)
				}
			}
			put(guestPlanFile, mustJSON(packet))
			put("/toolchain/protected", []byte("credential-free"))
			put(workspace+"/tools/Cargo.toml", []byte("[package]\nname=\"civ6-ci-credential-free-fixture\"\nversion=\"0.0.0\"\nedition=\"2021\"\n[workspace]\n"))
			put(workspace+"/tools/src/lib.rs", []byte(fixtureCrate))
			if name == "normal" {
				b, e := cmd(ctx, dockerExe, "exec", n.ID, guestBinary, "guest-listener-probe").CombinedOutput()
				if e != nil {
					t.Fatalf("offline restricted listener: %v %s", e, b)
				}
				_ = os.WriteFile(filepath.Join(caseDir, "listener-version.private.log"), b, 0600)
			}
			b, e := cmd(ctx, dockerExe, "exec", n.ID, guestBinary, "guest-restriction-fixture").CombinedOutput()
			if e != nil || !bytes.Contains(b, []byte("landlock-direct-copy-proc-toolchain-denied")) {
				t.Fatalf("real kernel boundary: %v %s", e, b)
			}
			_ = os.WriteFile(filepath.Join(caseDir, "landlock.private.log"), b, 0600)
			h := &host{plan: p, kind: KindRust, policy: fixturePolicy(), key: key, ns: n, dir: caseDir, target: filepath.Join(caseDir, "cargo-target"), started: true,
				recheck: func(context.Context) error { return nil }, loseReceiptFixture: lostCase}
			flight, cancel := context.WithCancel(ctx)
			defer cancel()
			type outcome struct {
				r cargoResult
				e error
			}
			done := make(chan outcome, 1)
			h.execute = func(c context.Context, index int) (cargoResult, error) {
				r, e := h.runWithFixture(c, index, true, cancelCase)
				done <- outcome{r, e}
				return r, e
			}
			endpoint, closeServer, e := startHost(flight, h)
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				cancel()
				if e := closeServer(); e != nil {
					t.Error(e)
				}
			}()
			packet.Callback, packet.Nonce = endpoint, h.key
			put(guestPlanFile, mustJSON(packet))
			callbackLog, e := os.OpenFile(filepath.Join(caseDir, "guest-callback.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer callbackLog.Close()
			callback := cmd(ctx, dockerExe, "exec", n.ID, guestBinary, "guest-fixture-callback")
			callback.Stdout, callback.Stderr = callbackLog, callbackLog
			if e = callback.Start(); e != nil {
				t.Fatal(e)
			}
			callbackDone := make(chan error, 1)
			go func() { callbackDone <- callback.Wait() }()
			ready := false
			for deadline := time.Now().Add(120 * time.Second); !ready && time.Now().Before(deadline); {
				if cmd(ctx, dockerExe, "exec", n.ID, "sh", "-c", "test -s /work/fixture-child.pid && kill -0 $(cat /work/fixture-child.pid)").Run() == nil {
					ready = true
					break
				}
				select {
				case e := <-callbackDone:
					t.Fatalf("guest callback ended before the real child was observed: %v", e)
				case o := <-done:
					t.Fatalf("fixture ended before the real child was observed: %v exit=%d log=%s", o.e, o.r.Exit, o.r.Log)
				case <-time.After(100 * time.Millisecond):
				}
			}
			if !ready {
				cancel()
				o := <-done
				t.Fatalf("real fixture child absent: %v %s", o.e, o.r.Log)
			}
			if cancelCase {
				cancel()
			} else if e = cmd(ctx, dockerExe, "exec", n.ID, "touch", "/work/fixture-release").Run(); e != nil {
				cancel()
				<-done
				t.Fatal(e)
			}
			o := <-done
			callbackExit := <-callbackDone
			if lostCase {
				if o.e == nil || !o.r.Drained || !h.broken || !n.TreeGone {
					t.Fatal("missing-receipt recovery did not disable and stop the namespace")
				}
				_ = saveNew(filepath.Join(caseDir, "receipt-loss.private.json"), map[string]any{"broken": h.broken, "tree_gone": n.TreeGone, "failure": o.e.Error(), "cargo_exit": o.r.Exit})
				return
			}
			if !cancelCase && callbackExit != nil {
				t.Fatalf("guest callback failed: %v", callbackExit)
			}
			if o.e != nil || !o.r.Drained {
				t.Fatalf("fixture drain: %v %s", o.e, o.r.Log)
			}
			if (!cancelCase && o.r.Exit != 0) || (cancelCase && o.r.Exit != 125) {
				t.Fatalf("fixture exit=%d cancel=%t %s", o.r.Exit, cancelCase, o.r.Log)
			}
			if cmd(ctx, dockerExe, "exec", n.ID, "sh", "-c", "! kill -0 $(cat /work/fixture-child.pid) 2>/dev/null").Run() != nil {
				t.Fatal("Cargo child survived the host gate release")
			}
			res, _ := json.Marshal(map[string]any{"cancel": cancelCase, "exit": o.r.Exit, "drained": true, "product_source_supplied": false, "all_required_checks_success": false})
			if e = os.WriteFile(filepath.Join(caseDir, "fixture-result.private.json"), res, 0600); e != nil {
				t.Fatal(e)
			}
			_ = strings.TrimSpace
		})
	}
}
