//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestUnslopNativeCompilerChild(t *testing.T) {
	if os.Getenv("UNSLOP_TEST_COMPILER") != "1" {
		return
	}
	fmt.Printf("{\"reason\":\"compiler-artifact\",\"fresh\":false,\"executable\":%q,\"target\":{\"name\":\"godd-unslop\",\"kind\":[\"bin\"]}}\n", unslopCaptureOutput)
	fmt.Println(`{"reason":"build-finished","success":true}`)
	os.Exit(0)
}

func TestUnslopNormalRoutingDoesNotPromoteUnknownReceiptOrSelectors(t *testing.T) {
	for _, flag := range []string{"--verify-unslop-update", "--prepare-unslop-update", "--read-unslop-evaluation-packet", "--inspect-unslop-evaluation-origin"} {
		var out bytes.Buffer
		if code := runNormalUnslopPurpose([]string{flag}, &out); code != 1 || !json.Valid(out.Bytes()) {
			t.Fatal("unknown native dependency granted", flag, code, out.String())
		}
	}
}

func TestUnslopNativeBuildUsesActualProcessAndDrainedEvidence(t *testing.T) {
	t.Setenv("UNSLOP_TEST_COMPILER", "1")
	called := 0
	s := &unslopNativeSession{sealed: true, run: func(ctx context.Context) askChildResult {
		called++
		ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{})
		return askOwnedChild(ctx, os.Args[0], "-test.run=^TestUnslopNativeCompilerChild$")
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := s.Build(ctx)
	if e != nil || !r.Started || !r.Assigned || !r.WaitVerified || !r.Reaped || r.TimedOut || r.Exit != 0 || r.NativeProcess == nil || r.ToolCallID != nil || called != 1 || r.CargoJSONSHA != digest(r.CargoJSON) || !unslopCargoOutput(r.CargoJSON) {
		t.Fatal("native build evidence unbound", e, r)
	}
	h, e := syscall.OpenProcess(syscall.SYNCHRONIZE, false, r.NativeProcess.PID)
	if e == nil {
		defer syscall.CloseHandle(h)
		state, e := syscall.WaitForSingleObject(h, 0)
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			t.Fatal("native child not collected")
		}
	}
	if _, e = s.Build(ctx); e == nil || called != 1 {
		t.Fatal("same session ran producer twice")
	}
	cancelled, c := context.WithCancel(context.Background())
	c()
	s = &unslopNativeSession{sealed: true, run: s.run}
	if _, e = s.Build(cancelled); e == nil || called != 1 {
		t.Fatal("cancelled session started child")
	}
	clear(r.CargoJSON)
	clear(r.CargoStream)
}

func TestUnslopNativeCaptureFailureClearsOutput(t *testing.T) {
	for _, mode := range []string{"overflow", "incomplete", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte(`{"secret":"synthetic-sensitive-output"}`)
			s := &unslopNativeSession{sealed: true, run: func(context.Context) askChildResult {
				return askChildResult{Started: true, Assigned: true, WaitVerified: true, Reaped: true, Capture: &askChildCaptureResult{Stdout: raw, NativeProcess: &askNativeProcess{PID: 1, Creation: 1}, Complete: mode != "incomplete", Overflow: mode == "overflow"}}
			}}
			if _, e := s.Build(context.Background()); e == nil {
				t.Fatal("invalid capture qualified")
			}
			for _, b := range raw {
				if b != 0 {
					t.Fatal("failure output not cleared")
				}
			}
		})
	}
}

func TestUnslopReceiptPublicationPreservesFailureAndCollision(t *testing.T) {
	for _, mode := range []string{"normal", "cancel", "pending_collision", "final_collision", "close_failure"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			dest := filepath.Join(root, "receipt.json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			if mode == "pending_collision" {
				_ = os.WriteFile(dest+".pending", []byte("before"), 0600)
			}
			if mode == "final_collision" {
				_ = os.WriteFile(dest, []byte("before"), 0600)
			}
			var hook func() error
			if mode == "close_failure" {
				hook = func() error { return errors.New("owned close failure") }
			}
			err := unslopPublishReceiptWithHook(ctx, dest, []byte(`{"owned":true}`), hook)
			if mode == "normal" {
				if err != nil {
					t.Fatal(err)
				}
				raw, e := os.ReadFile(dest)
				if e != nil || string(raw) != `{"owned":true}` {
					t.Fatal("published wrong bytes")
				}
				return
			}
			if err == nil {
				t.Fatal("failed publication accepted")
			}
			raw, e := os.ReadFile(dest)
			if mode == "final_collision" {
				if e != nil || string(raw) != "before" {
					t.Fatal("existing receipt overwritten")
				}
			} else if !os.IsNotExist(e) {
				t.Fatal("failed receipt published")
			}
		})
	}
}

func TestUnslopCargoAliasIsConfinedToProducedPair(t *testing.T) {
	valid := unslopCaptureRoot + "/cargo/release/deps/godd_unslop-0123456789abcdef.exe"
	for _, name := range []string{valid, valid + ".other", unslopCaptureRoot + "/cargo/release/godd_unslop-0123456789abcdef.exe", unslopCaptureRoot + "/cargo/release/deps/other.exe"} {
		_, ok := unslopCargoAlias([]string{unslopCaptureOutput, name})
		if ok != (name == valid) {
			t.Fatal("alias boundary", name, ok)
		}
	}
}

func TestUnslopCargoStreamingBindsFullDrainToExactArtifact(t *testing.T) {
	for _, fault := range []string{"normal", "byte_chunks", "large_unrelated_output", "missing_finish", "failed_finish", "wrong_output", "fresh", "duplicate", "truncated", "oversize_line", "oversize_stream", "after_finish"} {
		t.Run(fault, func(t *testing.T) {
			fixture := syntheticUnslopCapture().build.CargoJSON
			switch fault {
			case "large_unrelated_output":
				fixture = append([]byte(`{"reason":"compiler-message","message":"`+strings.Repeat("x", 80<<10)+`"}`+"\n"), fixture...)
			case "missing_finish":
				fixture = fixture[:strings.Index(string(fixture), `{"reason":"build-finished"`)]
			case "failed_finish":
				fixture = []byte(strings.ReplaceAll(string(fixture), `"success":true`, `"success":false`))
			case "wrong_output":
				fixture = []byte(strings.ReplaceAll(string(fixture), unslopCaptureOutput, unslopCaptureOutput+".untrusted"))
			case "fresh":
				fixture = []byte(strings.ReplaceAll(string(fixture), `"fresh":false`, `"fresh":true`))
			case "duplicate":
				fixture = append(fixture, fixture...)
			case "truncated":
				fixture = fixture[:len(fixture)-4]
			case "oversize_line":
				fixture = append([]byte(strings.Repeat("x", 1<<20+1)), fixture...)
			case "oversize_stream":
				fixture = []byte(strings.Repeat("{}\n", 3<<20))
			case "after_finish":
				fixture = append(fixture, []byte("{}\n")...)
			}
			stream := &unslopCargoStream{}
			chunk := 4097
			if fault == "byte_chunks" {
				chunk = 1
			}
			for start := 0; start < len(fixture); start += chunk {
				end := start + chunk
				if end > len(fixture) {
					end = len(fixture)
				}
				n, e := stream.Write(fixture[start:end])
				if n != end-start || e != nil {
					t.Fatal("stream failed to drain")
				}
			}
			projection, sha, ok := stream.finish()
			want := fault == "normal" || fault == "byte_chunks" || fault == "large_unrelated_output"
			if ok != want {
				t.Fatalf("qualified=%v want=%v", ok, want)
			}
			if want {
				if !unslopCargoOutput(projection) || sha != digest(fixture) {
					t.Fatal("projection and whole stream not bound")
				}
				var doc any
				_ = json.Unmarshal(projection, &doc)
				if strings.Contains(string(projection), strings.Repeat("x", 100)) {
					t.Fatal("compiler free text forwarded")
				}
			} else if len(projection) > 0 || sha != "" {
				t.Fatal("failed stream yielded evidence")
			}
		})
	}
}
