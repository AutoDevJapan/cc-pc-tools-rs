//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCompilerCaptureRetainsBoundedTailAndErasesEveryByte(t *testing.T) {
	var b compilerCapture
	old := b.data[:]
	for _, input := range [][]byte{bytes.Repeat([]byte{'s'}, 35000), bytes.Repeat([]byte{'x'}, 40000), bytes.Repeat([]byte{'y'}, 70000), []byte("\nerror[E0308]: secret details\n")} {
		n, err := b.Write(input)
		if n != len(input) || err != nil {
			t.Fatal("compiler pipe not fully drained")
		}
	}
	if b.n != len(b.data) || !b.truncated || compilerCause(nil, b.data[:b.n], b.truncated).RustCode != "E0308" {
		t.Fatal("bounded tail cause lost")
	}
	b.erase()
	if b.n != 0 || !bytes.Equal(old, make([]byte, len(old))) {
		t.Fatal("compiler backing storage not erased")
	}
}

func TestCompilerDiagnosticReturnsOnlyFixedFields(t *testing.T) {
	for _, tc := range []struct{ text, cause, code string }{
		{"error[E0308]: DO_NOT_RETURN_PRIVATE_VALUE", "rust_compile_error", "E0308"},
		{"error[E12345]: secret", "unknown", ""},
		{"prefixerror[E0308]: secret", "unknown", ""},
		{"failed (os error 206)", "windows_path_limit", ""},
		{"failed (os error 5)", "access_denied", ""},
		{"failed (os error 2)", "file_unavailable", ""},
		{"failed (os error 3)", "path_unavailable", ""},
		{"failed (os error 50)", "unknown", ""},
		{"chaos error 5)", "unknown", ""},
		{"failed (os error 32)", "sharing_violation", ""},
		{"failed (os error 112)", "disk_full", ""},
		{"linker `link.exe` not found", "linker_unavailable", ""},
		{"linking with `link.exe` failed", "linker_failed", ""},
		{"sccache: error: secret", "compiler_cache_failed", ""},
		{"failed to run custom build command for secret", "dependency_build_failed", ""},
		{"error: could not compile secret", "compilation_failed", ""},
		{"DO_NOT_RETURN_PRIVATE_VALUE", "unknown", ""},
	} {
		d := compilerCause(nil, []byte(tc.text), false)
		if d.Cause != tc.cause || d.RustCode != tc.code {
			t.Fatalf("wrong bounded cause for %s", tc.cause)
		}
		raw, _ := json.Marshal(d)
		if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("DO_NOT_RETURN")) {
			t.Fatal("compiler text escaped")
		}
	}
}

func TestCompilerDiagnosticNativeChild(t *testing.T) {
	mode := os.Getenv("OWNED_NATIVE_COMPILER_DIAGNOSTIC")
	if mode == "" {
		return
	}
	if mode == "stall" {
		time.Sleep(5 * time.Second)
		return
	}
	if mode == "large" {
		fmt.Fprint(os.Stderr, strings.Repeat("DO_NOT_RETURN_PRIVATE_VALUE", 6000))
	}
	if mode != "unknown" {
		fmt.Fprintln(os.Stderr, "\nerror[E0308]: DO_NOT_RETURN_PRIVATE_VALUE")
	} else {
		fmt.Fprintln(os.Stderr, "DO_NOT_RETURN_PRIVATE_VALUE")
	}
	os.Exit(101)
}

func TestCompilerFailureNativeBoundaryClassifiesAndReapsWithoutText(t *testing.T) {
	for _, mode := range []string{"rust", "large", "unknown", "stall", "protocol"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("OWNED_NATIVE_COMPILER_DIAGNOSTIC", mode)
			budget := 2 * time.Second
			if mode == "stall" {
				budget = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			if mode != "protocol" {
				ctx = compilerContext(ctx)
			}
			r := askOwnedChild(ctx, os.Args[0], "-test.run=^TestCompilerDiagnosticNativeChild$")
			if !r.Started || !r.Assigned || !r.WaitVerified || !r.Reaped || len(r.Raw) != 0 {
				t.Fatal("failed child not collected or raw escaped")
			}
			if mode == "stall" {
				if !r.TimedOut || r.Diagnostic != nil {
					t.Fatal("timeout was reported as a compiler cause")
				}
				return
			}
			if r.Exit != 101 || r.TimedOut {
				t.Fatal("native compiler oracle not reached")
			}
			if mode == "protocol" {
				if r.Diagnostic != nil {
					t.Fatal("ordinary protocol contract changed")
				}
				return
			}
			if r.Diagnostic == nil || r.Diagnostic.Truncated != (mode == "large") {
				t.Fatal("bounded compiler result missing")
			}
			want := "rust_compile_error"
			if mode == "unknown" {
				want = "unknown"
			}
			if r.Diagnostic.Cause != want {
				t.Fatal("wrong fixed diagnostic")
			}
			raw, _ := json.Marshal(r.Diagnostic)
			if bytes.Contains(raw, []byte("DO_NOT_RETURN")) {
				t.Fatal("compiler text leaked")
			}
		})
	}
}
