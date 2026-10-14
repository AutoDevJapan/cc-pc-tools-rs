package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		t.Run(fmt.Sprintf("cancel-%t", cancelCase), func(t *testing.T) {
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
	}
}

func errorsJoinClose(prior error, file *os.File) error {
	closeErr := file.Close()
	if prior != nil {
		return prior
	}
	return closeErr
}
