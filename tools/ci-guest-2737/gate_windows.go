package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const sharedCargoGate = `D:\CC\Tools\codex-jobs\cargo_gate.exe`
const sharedGateDirectory = `D:\CC\Tools\codex-jobs`

type gatedHello struct{ ID, Mode, Root, SDKRoot, Helper, Request string }
type gatedControl struct{ ID, Kind string }
type gateFrame struct {
	ID, Stage, Target string
	Jobs, Threads     int
	Cancelled         bool
}

func decodeGateLine(r *bufio.Reader, out any) error {
	b, e := r.ReadSlice('\n')
	if e != nil || len(b) > 32768 {
		return errors.New("gate control frame unavailable")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("gate control frame rejected")
	}
	return nil
}
func gateChild() error {
	// This internal mode checks the gate's published limits. Environment markers
	// alone are not authority; the controller launches the canonical gate and
	// probes its real target byte lock before acknowledging any work.
	jobs, e := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	if e != nil || jobs < 1 || jobs > 10 {
		return errors.New("gate build cap rejected")
	}
	threads, e := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if e != nil || threads < 1 || threads > 10 || os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" {
		return errors.New("gate test cap rejected")
	}
	target := os.Getenv("CODEX_JOBS_CARGO_TARGET_HELD")
	if target == "" {
		return errors.New("gate target lease absent")
	}
	r := bufio.NewReaderSize(os.Stdin, 32769)
	var q gatedHello
	if decodeGateLine(r, &q) != nil || !validInstance(q.ID) || (q.Mode != "fixture" && q.Mode != "offline-bootstrap" && q.Mode != "linux-job" && q.Mode != "windows-job") || !filepath.IsAbs(q.Root) {
		return errors.New("gate work rejected")
	}
	out := json.NewEncoder(os.Stdout)
	if e = out.Encode(gateFrame{ID: q.ID, Stage: "admitted", Target: target, Jobs: jobs, Threads: threads}); e != nil {
		return e
	}
	var start gatedControl
	if decodeGateLine(r, &start) != nil || start.ID != q.ID || start.Kind != "start" {
		return errors.New("gate start not acknowledged")
	}
	if q.Mode == "offline-bootstrap" || q.Mode == "linux-job" || q.Mode == "windows-job" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			for {
				var control gatedControl
				if decodeGateLine(r, &control) != nil || control.ID != q.ID || control.Kind != "finish" {
					cancel()
					return
				}
			}
		}()
		var e error
		if q.Mode == "windows-job" {
			e = executeWindowsJob(ctx, q.Root, q.Helper, q.Request, jobs, threads)
		} else if q.Mode == "linux-job" {
			e = executeLinuxJob(ctx, q.Root, q.SDKRoot, q.Helper, q.Request, jobs, threads)
		} else {
			e = bootstrapNormalRunner(ctx, q.Root, q.SDKRoot, q.Helper)
		}
		if e != nil {
			var fence *unsafeVMLease
			if errors.As(e, &fence) {
				fmt.Fprintln(os.Stderr, "VM_LEASE_FENCED: retain Cargo admission; inspect durable normal CI intent")
				for {
					probe, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
					observed := observeVMQuiescence(probe, fence)
					cancel()
					if observed == nil {
						break
					}
					timer := time.NewTimer(30 * time.Second)
					<-timer.C
				}
			}
			return e
		}
		return out.Encode(gateFrame{ID: q.ID, Stage: "drained", Jobs: jobs, Threads: threads})
	}
	// No work is permitted before this acknowledgement. Once started, EOF
	// cancels work; the parent drains the child instead of killing the gate.
	var finish gatedControl
	cancelled := decodeGateLine(r, &finish) != nil || finish.ID != q.ID || finish.Kind != "finish"
	return out.Encode(gateFrame{ID: q.ID, Stage: "drained", Jobs: jobs, Threads: threads, Cancelled: cancelled})
}

type gateSession struct {
	cmd              *exec.Cmd
	input            io.WriteCloser
	reader           *bufio.Reader
	log              *os.File
	root, id, target string
	frame            gateFrame
}

func gateEnvironment(exe, target string) []string {
	var env []string
	for _, key := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP", "PATH", "PATHEXT", "ComSpec", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "CC_JOB_ID"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return append(env, "CODEX_JOBS_REAL_CARGO="+exe, "CODEX_JOBS_CARGO_LOCK_DIR="+sharedGateDirectory, "CARGO_TARGET_DIR="+target, "CC_CI_2737_GATE_CHILD=1")
}
func startGateFixture(ctx context.Context, root string) (s *gateSession, ret error) {
	return startGatedWork(ctx, gatedHello{Mode: "fixture", Root: root})
}
func startGatedWork(ctx context.Context, q gatedHello) (s *gateSession, ret error) {
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	s = &gateSession{root: q.Root, id: newID(), target: filepath.Join(q.Root, "ci-target-lease")}
	s.cmd = command(context.Background(), sharedCargoGate, "test", "--locked")
	s.cmd.Env = gateEnvironment(exe, s.target)
	s.log, e = os.OpenFile(filepath.Join(q.Root, "gate-"+s.id+".private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	s.cmd.Stderr = s.log
	s.input, e = s.cmd.StdinPipe()
	if e != nil {
		_ = s.log.Close()
		return nil, e
	}
	output, e := s.cmd.StdoutPipe()
	if e != nil {
		_ = s.input.Close()
		_ = s.log.Close()
		return nil, e
	}
	s.reader = bufio.NewReaderSize(output, 32769)
	if e = s.cmd.Start(); e != nil {
		_ = s.input.Close()
		_ = s.log.Close()
		return nil, e
	}
	owned := s
	defer func() {
		if ret != nil {
			_ = owned.input.Close()
			_ = owned.cmd.Process.Kill()
			_ = owned.cmd.Wait()
			_ = owned.log.Close()
		}
	}()
	q.ID = s.id
	if e = json.NewEncoder(s.input).Encode(q); e != nil {
		return nil, e
	}
	observed := make(chan error, 1)
	go func() { observed <- decodeGateLine(s.reader, &s.frame) }()
	select {
	case e = <-observed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e != nil || s.frame.ID != s.id || s.frame.Stage != "admitted" || s.frame.Jobs < 1 || s.frame.Jobs > 10 || s.frame.Threads < 1 || s.frame.Threads > 10 || !strings.EqualFold(s.frame.Target, s.target) {
		return nil, errors.New("real gate admission unverified")
	}
	return s, nil
}
func (s *gateSession) finish(cancel bool) (gateFrame, error) {
	var sendErr error
	if cancel {
		_ = s.input.Close()
	} else {
		if e := json.NewEncoder(s.input).Encode(gatedControl{s.id, "finish"}); e != nil {
			sendErr = e
			_ = s.input.Close()
		}
	}
	var result gateFrame
	e := decodeGateLine(s.reader, &result)
	_, drain := io.Copy(io.Discard, s.reader)
	_ = s.input.Close()
	wait := s.cmd.Wait()
	_ = s.log.Close()
	if e != nil || sendErr != nil || drain != nil || wait != nil || result.ID != s.id || result.Stage != "drained" || result.Cancelled != cancel {
		return result, errors.New("gated child drain unverified")
	}
	return result, nil
}
func (s *gateSession) start() error {
	return json.NewEncoder(s.input).Encode(gatedControl{s.id, "start"})
}

var lockFileProc = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
var unlockFileProc = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")

func probeTargetLease(target string) (bool, error) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(target)))
	file := filepath.Join(sharedGateDirectory, fmt.Sprintf("cargo-target-%016x.lock", h.Sum64()))
	f, e := os.OpenFile(file, os.O_RDWR, 0600)
	if e != nil {
		return false, e
	}
	defer f.Close()
	var o syscall.Overlapped
	ok, _, err := lockFileProc.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&o)))
	if ok != 0 {
		_, _, _ = unlockFileProc.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&o)))
		return false, nil
	}
	if err == syscall.Errno(33) {
		return true, nil
	}
	return false, errors.New("target lease probe failed")
}
func testGateFixtures(ctx context.Context, root string) error {
	for _, cancel := range []bool{false, true} {
		s, e := startGateFixture(ctx, root)
		if e != nil {
			return e
		}
		held, e := probeTargetLease(s.frame.Target)
		if e != nil || !held {
			_ = s.start()
			_, _ = s.finish(true)
			return errors.New("target byte lock not held during admission")
		}
		if e = s.start(); e != nil {
			_ = s.input.Close()
			_ = s.cmd.Wait()
			return e
		}
		result, e := s.finish(cancel)
		if e != nil {
			return e
		}
		held, e = probeTargetLease(s.frame.Target)
		if e != nil || held {
			return errors.New("target byte lock not released after drain")
		}
		if e = record(root, "gate-fixture-"+s.id+".private.json", map[string]any{"jobs": result.Jobs, "test_threads": result.Threads, "cancelled": result.Cancelled, "target_held_before": true, "target_released_after": true, "child_drained": true, "cargo_compiled": false, "official_ci_success": false}); e != nil {
			return e
		}
	}
	return nil
}
