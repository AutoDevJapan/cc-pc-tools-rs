//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestCompilerCompletionAggregatesCleanupBeforeSingleFinalRecord(t *testing.T) {
	for _, fault := range []string{"normal", "snapshot", "config", "both", "compile_failure"} {
		t.Run(fault, func(t *testing.T) {
			var calls []string
			c := softwareWorkerCompletion{
				snapshot: func() error {
					calls = append(calls, "snapshot")
					if fault == "snapshot" || fault == "both" {
						return errors.New("owned snapshot failure")
					}
					return nil
				},
				config: func() error {
					calls = append(calls, "config")
					if fault == "config" || fault == "both" {
						return errors.New("owned config failure")
					}
					return nil
				},
			}
			r := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: mp4Source, Reason: "producer_acknowledged", TargetCreated: true, TargetState: "created_preserved", CompilerDiagnostic: &compilerDiagnostic{Cause: "rust_compile_error", RustCode: "E0308"}}
			exit := 0
			if fault == "compile_failure" {
				r.Reason, exit = "producer_failed_or_uncollected", 1
			}
			var out bytes.Buffer
			c.finish(&out, &r, &exit, softwareBuildPlan{Source: mp4Source})
			var got askArtifactResult
			d := json.NewDecoder(&out)
			if d.Decode(&got) != nil || d.Decode(new(any)) != io.EOF || got.CompilerDiagnostic == nil || got.CompilerDiagnostic.RustCode != "E0308" || got.SnapshotCleanup == nil {
				t.Fatal("missing or multiple final records")
			}
			if len(calls) != 2 || calls[0] != "snapshot" || calls[1] != "config" {
				t.Fatal("cleanup was not exhausted")
			}
			c.close(&r)
			if len(calls) != 2 {
				t.Fatal("cleanup repeated after failure")
			}
			if fault == "normal" {
				if exit != 0 || !*got.SnapshotCleanup {
					t.Fatal("normal completion changed")
				}
			} else if exit != 1 || got.Qualified {
				t.Fatal("failed completion became success")
			}
			if fault == "both" && got.Reason != "multiple_compiler_cleanup_failed_preserved" {
				t.Fatal("combined failure lost")
			}
		})
	}
}

func TestCompilerCompletionNativeChild(t *testing.T) {
	if os.Getenv("OWNED_COMPILER_COMPLETION") != "1" {
		return
	}
	r := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: mp4Source, Reason: "producer_failed_or_uncollected", TargetCreated: true, TargetState: "created_preserved", CompilerDiagnostic: &compilerDiagnostic{Cause: "rust_compile_error", RustCode: "E0308"}}
	c := softwareWorkerCompletion{snapshot: func() error { return errors.New("owned snapshot") }, config: func() error { return errors.New("owned config") }}
	exit := 1
	c.finish(os.Stdout, &r, &exit, softwareBuildPlan{Source: mp4Source})
	os.Exit(exit)
}

func TestCompilerCompletionFailureSurvivesActualSupervisor(t *testing.T) {
	t.Setenv("OWNED_COMPILER_COMPLETION", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ready := 0
	r := askOwnedChildWithHandshake(ctx, os.Args[0], func([]byte) bool { ready++; return false }, "-test.run=^TestCompilerCompletionNativeChild$")
	defer clear(r.Raw)
	var got askArtifactResult
	if !r.Started || !r.Assigned || !r.WaitVerified || !r.Reaped || r.TimedOut || r.Exit != 1 || json.Unmarshal(r.Raw, &got) != nil || got.CompilerDiagnostic == nil || got.CompilerDiagnostic.RustCode != "E0308" || got.Reason != "multiple_compiler_cleanup_failed_preserved" || !got.TargetCreated || got.SnapshotCleanup == nil || *got.SnapshotCleanup {
		t.Fatal("final failure lost at actual supervisor boundary", ready)
	}
}

type compilerFinalWriterFailure struct{}

func (compilerFinalWriterFailure) Write([]byte) (int, error) {
	return 0, errors.New("owned output failure")
}

func TestCompilerCompletionOutputFailureIsNotSuccess(t *testing.T) {
	r := askArtifactResult{Reason: "producer_acknowledged"}
	c := softwareWorkerCompletion{}
	exit := 0
	c.finish(compilerFinalWriterFailure{}, &r, &exit, softwareBuildPlan{Source: mp4Source})
	if exit != 1 {
		t.Fatal("failed final output succeeded")
	}
}

func TestCompilerCompletionWithoutCallbacksRetainsFirstCloseResult(t *testing.T) {
	r := askArtifactResult{}
	c := softwareWorkerCompletion{}
	if !c.close(&r) || !c.close(&r) || r.SnapshotCleanup != nil {
		t.Fatal("empty completion changed its first close result")
	}
}
