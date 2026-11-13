//go:build windows

package main

import (
	"bytes"
	"context"
	"io"
)

// Only compiler invocations opt in. Protocol workers keep their existing
// stderr-empty, complete-output contract; no captured text escapes this type.
type compilerDiagnosticContext struct{}

type compilerDiagnostic struct {
	Cause           string                `json:"cause"`
	RustCode        string                `json:"rust_code,omitempty"`
	Truncated       bool                  `json:"truncated"`
	ArchiveProgress *cargoArchiveProgress `json:"archive_progress,omitempty"`
	InputStage      string                `json:"input_stage,omitempty"`
	InputOSCode     uint32                `json:"input_os_code,omitempty"`
}

// Retain the end, where Cargo reports its cause, without allocating growing
// backing arrays. Always drain the pipe; truncation must not break the compiler.
type compilerCapture struct {
	data      [65536]byte
	n         int
	truncated bool
}

func (b *compilerCapture) ReadFrom(r io.Reader) (total int64, err error) {
	var scratch [8192]byte
	defer clear(scratch[:])
	empty := 0
	for {
		n, readErr := r.Read(scratch[:])
		if n > 0 {
			_, _ = b.Write(scratch[:n])
			total += int64(n)
			empty = 0
		} else {
			empty++
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
		if empty >= 100 {
			return total, io.ErrNoProgress
		}
	}
}

func (b *compilerCapture) Write(p []byte) (int, error) {
	count := len(p)
	if count >= len(b.data) {
		b.truncated = b.truncated || b.n != 0 || count > len(b.data)
		copy(b.data[:], p[count-len(b.data):])
		b.n = len(b.data)
		return count, nil
	}
	if extra := b.n + count - len(b.data); extra > 0 {
		copy(b.data[:], b.data[extra:b.n])
		b.n -= extra
		b.truncated = true
	}
	copy(b.data[b.n:], p)
	b.n += count
	return count, nil
}

func (b *compilerCapture) erase() { clear(b.data[:]); b.n = 0 }

func compilerCause(stdout, stderr []byte, truncated bool) *compilerDiagnostic {
	r := &compilerDiagnostic{Cause: "unknown", Truncated: truncated}
	contains := func(marker string) bool {
		return bytes.Contains(stdout, []byte(marker)) || bytes.Contains(stderr, []byte(marker))
	}
	for _, stream := range [][]byte{stderr, stdout} {
		for offset := 0; offset < len(stream); offset++ {
			if stream[offset] != 'e' || offset != 0 && stream[offset-1] != '\n' || !bytes.HasPrefix(stream[offset:], []byte("error[E")) {
				continue
			}
			if offset+13 <= len(stream) && stream[offset+11] == ']' && stream[offset+12] == ':' {
				code := stream[offset+6 : offset+11]
				if code[0] == 'E' && code[1] >= '0' && code[1] <= '9' && code[2] >= '0' && code[2] <= '9' && code[3] >= '0' && code[3] <= '9' && code[4] >= '0' && code[4] <= '9' {
					r.Cause, r.RustCode = "rust_compile_error", string(code)
					return r
				}
			}
		}
	}
	switch {
	case contains("(os error 206)"):
		r.Cause = "windows_path_limit"
	case contains("(os error 112)"):
		r.Cause = "disk_full"
	case contains("(os error 32)"):
		r.Cause = "sharing_violation"
	case contains("(os error 5)"):
		r.Cause = "access_denied"
	case contains("(os error 2)"):
		r.Cause = "file_unavailable"
	case contains("(os error 3)"):
		r.Cause = "path_unavailable"
	case contains("linker `link.exe` not found"):
		r.Cause = "linker_unavailable"
	case contains("linking with `link.exe` failed"):
		r.Cause = "linker_failed"
	case contains("sccache: error"):
		r.Cause = "compiler_cache_failed"
	case contains("failed to run custom build command"):
		r.Cause = "dependency_build_failed"
	case contains("could not compile"):
		r.Cause = "compilation_failed"
	}
	return r
}

func compilerContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, compilerDiagnosticContext{}, true)
}
