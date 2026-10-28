package deploy3686

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSkills3080CrashBoundaryChild(t *testing.T) {
	path := os.Getenv("CC_UPDATE_CRASH_FIXTURE")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var input struct {
		Operation        operation
		Record, Boundary string
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("fixture")
	}
	stop := func() error {
		fmt.Fprintln(os.Stdout, "READY")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return errors.New("test input ended")
	}
	h := hooks{}
	switch input.Boundary {
	case "validated":
		h.afterValidation = stop
	case "old_moved":
		h.afterOldMove = stop
	case "published":
		h.afterPublish = stop
	default:
		t.Fatal("boundary")
	}
	_ = applyAt(input.Operation, input.Record, h)
}
func TestSkills3080RealProcessKillRecoversJournalBoundObjects(t *testing.T) {
	for _, boundary := range []string{"validated", "old_moved", "published"} {
		t.Run(boundary, func(t *testing.T) {
			o, record := fixture(t)
			path := filepath.Join(filepath.Dir(record), "crash-fixture.json")
			raw, _ := json.Marshal(struct {
				Operation        operation
				Record, Boundary string
			}{o, record, boundary})
			if os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("fixture output")
			}
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSkills3080CrashBoundaryChild$")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			cmd.Env = append(os.Environ(), "CC_UPDATE_CRASH_FIXTURE="+path)
			out, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			in, e := cmd.StdinPipe()
			if e != nil {
				t.Fatal(e)
			}
			if cmd.Start() != nil {
				t.Fatal("test child start")
			}
			defer in.Close()
			line, e := bufio.NewReader(out).ReadString('\n')
			if e != nil || line != "READY\n" {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("child did not reach boundary: %q %v", line, e)
			}
			if cmd.Process.Kill() != nil {
				t.Fatal("test child kill")
			}
			_ = cmd.Wait()
			// The killed process did not run any deferred rollback.
			r := recoverAt(o, record)
			if boundary == "published" {
				if !r.Installed || !r.Published || sum(read(t, o.Target)) != o.New || sum(read(t, o.Backup)) != o.Old {
					t.Fatalf("published recovery failed: %+v", r)
				}
			} else if !r.Restored || r.Installed || sum(read(t, o.Target)) != o.Old {
				t.Fatalf("original recovery failed: %+v", r)
			}
		})
	}
}
func TestSkills3080RecoveryPreservesUnknownCurrentIdentity(t *testing.T) {
	o, record := fixture(t)
	r := applyAt(o, record, hooks{afterOldMove: func() error {
		if os.WriteFile(o.Target, []byte("foreign current"), 0600) != nil {
			return errors.New("fixture")
		}
		return errors.New("injected stop")
	}})
	if !r.RestoreFailed {
		t.Fatal("foreign current not reached")
	}
	before := read(t, o.Target)
	recovered := recoverAt(o, record)
	if recovered.Installed || recovered.Restored || string(read(t, o.Target)) != string(before) || sum(read(t, o.Backup)) != o.Old {
		t.Fatal("unknown current was adopted or overwritten")
	}
}
func TestSkills3080OutcomePreservesFirstFailure(t *testing.T) {
	_, record := fixture(t)
	r := result{Reason: "target_collision_or_publish_failed", RestoreFailed: true}
	if !SaveOutcome(record, "apply", r) || SaveOutcome(record, "apply", result{Installed: true}) {
		t.Fatal("outcome missing or replaced")
	}
	var got result
	if json.Unmarshal(read(t, record+".apply-outcome.json"), &got) != nil || got.Installed || !got.RestoreFailed {
		t.Fatal("original failure outcome lost")
	}
}
