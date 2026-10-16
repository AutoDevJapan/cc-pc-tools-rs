package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in real kernel/process tests. No GitHub registration or credentials.
// The caller supplies a newly safe-delete-owned evidence directory.
func TestCodeQLRealLifecycle(t *testing.T) {
	dir := os.Getenv("CC_PRODUCT_LIFECYCLE_DIR")
	if dir == "" {
		t.Skip("explicit owned lifecycle evidence directory required")
	}
	for _, cancelCase := range []bool{false, true} {
		ok := t.Run(fmt.Sprintf("cancel-%t", cancelCase), func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 60*time.Second)
			defer stop()
			name := fmt.Sprintf("todo2648-codeql-smoke-%t-%d", cancelCase, time.Now().UnixNano())
			path := filepath.Join(dir, fmt.Sprintf("codeql-lifecycle-%t.private.json", cancelCase))
			file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			result := struct {
				Instance, ContainerID                                  string
				Cancelled, ChildObserved, Isolation, TreeGone, Removed bool
			}{Instance: name, Cancelled: cancelCase}
			if e = json.NewEncoder(file).Encode(result); e == nil {
				e = file.Sync()
			}
			e = errorsJoinClose(e, file)
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				b, _ := json.MarshalIndent(result, "", "  ")
				if e := os.WriteFile(path, b, 0600); e != nil {
					t.Error(e)
				}
			}()
			b, createErr := cmd(ctx, docker, codeQLCreateArgs(name)...).Output()
			// Resolve by exact persisted name even when CLI completion is uncertain.
			inspect, e := cmd(ctx, docker, "inspect", name).Output()
			var entries []struct {
				ID, Name string
				Config   struct {
					Image  string
					Labels map[string]string
				}
			}
			if e != nil || json.Unmarshal(inspect, &entries) != nil || len(entries) != 1 || entries[0].Name != "/"+name || entries[0].Config.Image != codeQLSDK || entries[0].Config.Labels["cc.instance"] != name || entries[0].Config.Labels["cc.todo"] != "2648" {
				t.Fatal("create recovery unverified; retain recorded instance")
			}
			result.ContainerID = entries[0].ID
			defer func() {
				var e error
				result.TreeGone, result.Removed, e = disposeOwned(result.ContainerID)
				if e != nil {
					t.Error(e)
				}
			}()
			if createErr != nil || strings.TrimSpace(string(b)) != result.ContainerID {
				t.Fatal("creation completion unverified")
			}
			if e = codeQLIsolation(inspect); e != nil {
				t.Fatal(e)
			}
			result.Isolation = true
			if e = cmd(ctx, docker, "start", result.ContainerID).Run(); e != nil {
				t.Fatal(e)
			}
			if !cancelCase && os.Getenv("CC_PRODUCT_GUEST_BINARY") != "" {
				testRealCodeQLAdmission(t, ctx, result.ContainerID, dir)
			}
			script := "sleep 600 & echo $! > /work/child.pid; exit 0"
			if cancelCase {
				script = "sleep 600 & echo $! > /work/child.pid; wait"
			}
			childCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			command := cmd(childCtx, docker, "exec", result.ContainerID, "sh", "-c", script)
			if e = command.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
				if cmd(ctx, docker, "exec", result.ContainerID, "sh", "-c", "test -s /work/child.pid && kill -0 $(cat /work/child.pid)").Run() == nil {
					result.ChildObserved = true
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if cancelCase || !result.ChildObserved {
				cancel()
			}
			e = <-done
			if !result.ChildObserved {
				t.Fatal("real child not observed")
			}
			if cancelCase && e == nil {
				t.Fatal("cancellation was not observed")
			}
			if !cancelCase && e != nil {
				t.Fatal(e)
			}
		})
		if !ok {
			break
		} // Never create a second namespace after uncertain recovery.
	}
}

func testRealCodeQLAdmission(t *testing.T, ctx context.Context, id, dir string) {
	t.Helper()
	guest, e := os.ReadFile(os.Getenv("CC_PRODUCT_GUEST_BINARY"))
	if e != nil {
		t.Fatal(e)
	}
	transfer := cmd(ctx, docker, "exec", "-i", id, "sh", "-c", "cat > /runner/product-ci && chmod 755 /runner/product-ci")
	transfer.Stdin = bytes.NewReader(guest)
	if e = transfer.Run(); e != nil {
		t.Fatal(e)
	}
	p := codeQLPlan{RepoID: candidates[0].ID, RunID: 7, Attempt: 2, SHA: strings.Repeat("a", 40), Repo: "AutoDevJapan/opaque-fixture"}
	var stale atomic.Bool
	var calls atomic.Int32
	key := strings.Repeat("c", 64)
	server := httptest.NewServer(codeQLAdmissionHandler(p, key, func(context.Context) error {
		calls.Add(1)
		if stale.Load() {
			return errors.New("head advanced")
		}
		return nil
	}))
	defer server.Close()
	endpoint := "http://host.docker.internal:" + fmt.Sprint(server.Listener.Addr().(*net.TCPAddr).Port) + "/admit"
	packet, _ := json.Marshal(struct {
		Plan                codeQLPlan
		Repo, Callback, Key string
	}{p, p.Repo, endpoint, key})
	put := cmd(ctx, docker, "exec", "-i", id, "sh", "-c", "umask 077; cat > /home/runner/product-plan.json")
	put.Stdin = bytes.NewReader(packet)
	if e = put.Run(); e != nil {
		t.Fatal(e)
	}
	args := []string{"exec", "-e", "GITHUB_REPOSITORY=" + p.Repo, "-e", "GITHUB_SHA=" + p.SHA, "-e", "GITHUB_RUN_ID=7", "-e", "GITHUB_RUN_ATTEMPT=2", id, "/runner/product-ci", "codeql-hook-start"}
	first, firstErr := cmd(ctx, docker, args...).CombinedOutput()
	if e = os.WriteFile(filepath.Join(dir, "codeql-live-first.private.log"), first, 0600); e != nil {
		t.Fatal(e)
	}
	accepted := firstErr == nil
	stale.Store(true)
	second, secondErr := cmd(ctx, docker, args...).CombinedOutput()
	if e = os.WriteFile(filepath.Join(dir, "codeql-live-stale.private.log"), second, 0600); e != nil {
		t.Fatal(e)
	}
	rejected := secondErr != nil
	proof := struct {
		Accepted, AdvancedHeadRejected bool
		LiveChecks                     int32
	}{accepted, rejected, calls.Load()}
	b, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(dir, "codeql-live-admission.private.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if !accepted || !rejected || proof.LiveChecks != 2 {
		t.Fatal("real guest live admission did not enforce the changed head")
	}
}

func errorsJoinClose(prior error, file *os.File) error {
	closeErr := file.Close()
	if prior != nil {
		return prior
	}
	return closeErr
}
