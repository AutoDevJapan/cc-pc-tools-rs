package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func createIsolated(ctx context.Context, name, image string) (string, error) {
	c := cmd(ctx, docker, "create", "--name", name, "--label", "cc.todo=2648", "--init", "--user", "1001:1001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--network", "none", "--cpus", "2", "--memory", "2g", "--pids-limit", "256", "--tmpfs", "/work:rw,nosuid,size=1g,uid=1001,gid=1001", "--tmpfs", "/tmp:rw,nosuid,noexec,size=256m,uid=1001,gid=1001", "--workdir", "/work", image, "sleep", "infinity")
	b, e := c.Output()
	if e != nil {
		return "", errors.New("isolated container creation failed")
	}
	id := strings.TrimSpace(string(b))
	if len(id) != 64 {
		return "", errors.New("container identity unavailable")
	}
	return id, nil
}
func disposeOwned(id string) (gone, removed bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var inspected []struct {
		State struct {
			Running bool
			Pid     int
		}
		Config struct{ Labels map[string]string }
	}
	inspect := func() error {
		b, e := cmd(ctx, docker, "inspect", id).Output()
		if e != nil || json.Unmarshal(b, &inspected) != nil || len(inspected) != 1 || inspected[0].Config.Labels["cc.todo"] != "2648" {
			return errors.New("owned container identity unverified")
		}
		return nil
	}
	if e := inspect(); e != nil {
		return false, false, e
	}
	if e := cmd(ctx, docker, "stop", "--time", "10", id).Run(); e != nil {
		return false, false, errors.New("container stop failed")
	}
	if e := inspect(); e != nil {
		return false, false, e
	}
	if inspected[0].State.Running || inspected[0].State.Pid != 0 {
		return false, false, errors.New("container tree absence unverified")
	}
	if e := cmd(ctx, docker, "rm", "-v", id).Run(); e != nil {
		return true, false, errors.New("owned container removal failed")
	}
	return true, true, nil
}

// No repository or credential is involved in these real normal/cancel fixtures.
// A child remains alive until the owned Docker PID namespace is stopped.
func lifecycleSmoke(ctx context.Context, dir, image string) (ret error) {
	for _, cancelCase := range []bool{false, true} {
		name := fmt.Sprintf("todo2648-lifecycle-%t-%d", cancelCase, time.Now().UnixNano())
		id, e := createIsolated(ctx, name, image)
		if e != nil {
			return e
		}
		func() {
			result := struct {
				ContainerID   string `json:"container_id"`
				Cancelled     bool   `json:"cancelled"`
				ChildObserved bool   `json:"child_observed"`
				Isolation     bool   `json:"isolation_verified"`
				TreeGone      bool   `json:"tree_gone"`
				Removed       bool   `json:"container_removed"`
			}{ContainerID: id, Cancelled: cancelCase}
			defer func() {
				var e error
				result.TreeGone, result.Removed, e = disposeOwned(id)
				ret = errors.Join(ret, e)
				b, _ := json.MarshalIndent(result, "", "  ")
				if e = os.WriteFile(filepath.Join(dir, fmt.Sprintf("lifecycle-%t.private.json", cancelCase)), b, 0600); e != nil {
					ret = errors.Join(ret, e)
				}
			}()
			if e := cmd(ctx, docker, "start", id).Run(); e != nil {
				ret = errors.Join(ret, errors.New("lifecycle container start failed"))
				return
			}
			b, e := cmd(ctx, docker, "inspect", id).Output()
			if e != nil {
				ret = errors.Join(ret, errors.New("lifecycle inspection unavailable"))
				return
			}
			if e = checkIsolation(b, image); e != nil {
				ret = errors.Join(ret, e)
				return
			}
			result.Isolation = true
			// The child writes a marker after starting and deliberately outlives
			// the normal command. Cleanup must kill the actual remaining child.
			script := "sleep 600 & child=$!; echo $child > /work/child.pid; "
			if cancelCase {
				script += "wait"
			} else {
				script += "exit 0"
			}
			childCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			c := cmd(childCtx, docker, "exec", id, "sh", "-c", script)
			if e = c.Start(); e != nil {
				ret = errors.Join(ret, errors.New("lifecycle child start failed"))
				return
			}
			done := make(chan error, 1)
			go func() { done <- c.Wait() }()
			// Poll for an actual PID marker and kernel liveness, not a fixed sleep.
			ready := false
			deadline := time.Now().Add(10 * time.Second)
			for !ready && time.Now().Before(deadline) && ctx.Err() == nil {
				e = cmd(ctx, docker, "exec", id, "sh", "-c", "test -s /work/child.pid && kill -0 $(cat /work/child.pid)").Run()
				ready = e == nil
				if !ready {
					time.Sleep(25 * time.Millisecond)
				}
			}
			result.ChildObserved = ready
			if !ready {
				cancel()
				<-done
				ret = errors.Join(ret, errors.New("lifecycle child not observed"))
				return
			}
			if cancelCase {
				cancel()
				if e = <-done; e == nil {
					ret = errors.Join(ret, errors.New("lifecycle cancellation not observed"))
				}
			} else {
				if e = <-done; e != nil {
					ret = errors.Join(ret, errors.New("normal lifecycle command failed"))
				}
			}
		}()
		if ret != nil {
			return ret
		}
	}
	return nil
}
