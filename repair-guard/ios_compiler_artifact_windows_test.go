package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestIOSCompilerCLIEntryRejectsMixedFlagsWithoutProvider(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runGuardCLI([]string{"--verify-ios-compiler-artifact", "--guard-path", "C:/unopened"}, &out, &stderr, func(int) object { t.Fatal("unrelated verifier"); return nil })
	var v iosCompilerResult
	if code != 2 || stderr.Len() != 0 || json.Unmarshal(out.Bytes(), &v) != nil || v.Stage != "invalid_arguments" {
		t.Fatal("purpose CLI not connected before ordinary flag parser", code, stderr.Len(), out.Len())
	}
}

func TestIOSCompilerProviderValidatesEveryFixedAPIObservation(t *testing.T) {
	for mutate := -1; mutate < len(iosCompilerPins()); mutate++ {
		calls := 0
		err := iosCompilerProvider(context.Background(), func(_ context.Context, exe string, args ...string) ([]byte, error) {
			pins := iosCompilerPins()
			if exe != official3894Gate || len(args) != 6 || args[0] != "api" || args[1] != "--hostname" || args[2] != "github.com" || args[3] != pins[calls].endpoint || args[4] != "--jq" || args[5] != pins[calls].query {
				t.Fatal("unexpected provider operation")
			}
			row := pins[calls].expected
			if calls == mutate {
				row = object{"unqualified": "private token must not leave this boundary"}
			}
			calls++
			return json.Marshal(row)
		})
		if (err == nil) != (mutate == -1) || (mutate != -1 && calls != mutate+1) {
			t.Fatal("provider pin/order failure", mutate, calls, err)
		}
	}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if iosCompilerProvider(ctx, func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }) == nil || calls != 0 {
		t.Fatal("cancelled provider made a request")
	}
}

func TestIOSCompilerPlatformSealHasDedicatedDomainAndRejectsTampering(t *testing.T) {
	payload := []byte(`{"domain":"original-ios-compiler-abi-read-v1","source":"synthetic"}`)
	sealed, err := iosCompilerDPAPI(payload, true)
	if err != nil {
		t.Fatal("platform issuer", err)
	}
	defer clear(sealed)
	plain, err := iosCompilerDPAPI(sealed, false)
	defer clear(plain)
	if err != nil || !bytes.Equal(plain, payload) {
		t.Fatal("platform seal roundtrip")
	}
	sealed[len(sealed)/2] ^= 1
	if got, err := iosCompilerDPAPI(sealed, false); err == nil || len(got) != 0 {
		clear(got)
		t.Fatal("tampered issuer accepted")
	}
	if got, ok := official3894Seal(payload, true); !ok {
		t.Fatal("control platform issuer unavailable")
	} else {
		defer clear(got)
		if raw, err := iosCompilerDPAPI(got, false); err == nil {
			clear(raw)
			t.Fatal("other purpose issuer accepted")
		}
	}
}

type iosCompilerShortWriter struct{}

func (iosCompilerShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestIOSCompilerCLIOutputAndUnqualifiedBoundaries(t *testing.T) {
	for _, args := range [][]string{nil, {"--verify-ios-compiler-artifact", "other"}, {"--consume-ios-compiler-artifact=path"}} {
		a, _ := iosCompilerFixture(t, "")
		var out bytes.Buffer
		if runIOSCompilerCLI(args, &out, a) != 2 || a.pins != 0 || a.seals != 0 {
			t.Fatal("invalid CLI performed work")
		}
	}
	a, _ := iosCompilerFixture(t, "")
	a.providerErr = errIOSCompiler
	var out bytes.Buffer
	if runIOSCompilerCLI([]string{"--consume-ios-compiler-artifact"}, &out, a) != 1 || a.pins != 0 || bytes.Contains(out.Bytes(), []byte("synthetic private")) {
		t.Fatal("provider failure consumed original")
	}
	// A failing metadata writer cannot turn a refused operation into success.
	if runIOSCompilerCLI([]string{"--verify-ios-compiler-artifact"}, iosCompilerShortWriter{}, a) != 1 {
		t.Fatal("short output reported success")
	}
}

func TestIOSCompilerIdentityOnlyIgnoresReadAccessTime(t *testing.T) {
	a := syscall.ByHandleFileInformation{VolumeSerialNumber: 1, FileIndexLow: 2, FileSizeLow: 3, NumberOfLinks: 1}
	b := a
	b.LastAccessTime.LowDateTime = 4
	if !iosCompilerSameFileInfo(a, b) {
		t.Fatal("read access time changed identity")
	}
	for _, mutate := range []func(*syscall.ByHandleFileInformation){
		func(i *syscall.ByHandleFileInformation) { i.FileIndexLow++ },
		func(i *syscall.ByHandleFileInformation) { i.FileSizeLow++ },
		func(i *syscall.ByHandleFileInformation) { i.LastWriteTime.LowDateTime++ },
		func(i *syscall.ByHandleFileInformation) { i.CreationTime.LowDateTime++ },
		func(i *syscall.ByHandleFileInformation) { i.NumberOfLinks++ },
		func(i *syscall.ByHandleFileInformation) { i.FileAttributes++ },
	} {
		b = a
		mutate(&b)
		if iosCompilerSameFileInfo(a, b) {
			t.Fatal("non access field ignored")
		}
	}
}

type iosCompilerObservingWriter struct {
	data  []byte
	fail  bool
	calls int
}

func (w *iosCompilerObservingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	w.data = append(w.data, p...)
	return len(p), nil
}
func TestIOSCompilerOutputScratchAndControllerFailureAreErased(t *testing.T) {
	for _, fail := range []bool{false, true} {
		scratch := make([]byte, 4096)
		w := &iosCompilerObservingWriter{fail: fail}
		raw := bytes.Repeat([]byte("synthetic-private"), 1000)
		err := iosCompilerWriteBase64(w, raw, scratch)
		if (err != nil) != fail || !bytes.Equal(scratch, make([]byte, len(scratch))) {
			t.Fatal("encoded scratch retained")
		}
		if !fail {
			got, err := base64.StdEncoding.DecodeString(string(w.data))
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("streaming output changed bytes")
			}
			clear(got)
		}
		clear(w.data)
		clear(raw)
	}
	for _, fail := range []bool{false, true} {
		a, digest := iosCompilerFixture(t, "")
		w := &iosCompilerObservingWriter{fail: fail}
		r := iosCompilerRead(context.Background(), a, digest, func(m iosCompilerManifest, files map[string][]byte) error { return iosCompilerWriteFiles(w, m, files) })
		if r.Consumed == fail || !r.CleanupComplete || !a.snapshot.closed {
			t.Fatal("controller output/cleanup result", r)
		}
		for _, data := range a.snapshot.files {
			if !bytes.Equal(data, make([]byte, len(data))) {
				t.Fatal("output failure retained input")
			}
		}
		clear(w.data)
	}
}

func TestIOSCompilerFullCLIConsumerCommitsOnlyAfterCleanup(t *testing.T) {
	for _, mode := range []string{"verify", "consume", "cleanup-fail", "short-output"} {
		a, digest := iosCompilerFixture(t, "")
		if mode == "cleanup-fail" {
			a.snapshot.closeErr = errIOSCompiler
		}
		var output bytes.Buffer
		var writer io.Writer = &output
		if mode == "short-output" {
			writer = iosCompilerShortWriter{}
		}
		args := []string{"--consume-ios-compiler-artifact"}
		if mode == "verify" {
			args[0] = "--verify-ios-compiler-artifact"
		}
		code := runIOSCompilerCLIWithDigest(args, writer, a, digest)
		wantFailure := mode == "cleanup-fail" || mode == "short-output"
		if (code != 0) != wantFailure || !a.snapshot.closed {
			t.Fatal("full CLI status", mode, code)
		}
		if mode == "short-output" {
			continue
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &v) != nil {
			t.Fatal("full CLI returned invalid JSON")
		}
		if mode == "verify" {
			if _, ok := v["files"]; ok {
				t.Fatal("verify emitted contents")
			}
			continue
		}
		var files map[string][]byte
		if json.Unmarshal(v["files"], &files) != nil || len(files) != 11 {
			t.Fatal("read-only output missing files")
		}
		for name, data := range files {
			if string(data) != "synthetic private "+name {
				t.Fatal("CLI changed verified bytes")
			}
			clear(data)
		}
		var metadata iosCompilerResult
		if json.Unmarshal(v["metadata"], &metadata) != nil || metadata.CleanupComplete == (mode == "cleanup-fail") {
			t.Fatal("cleanup not surfaced")
		}
		clear(output.Bytes())
	}
}

func TestIOSCompilerWindowsOwnedSameHandleAndMutationExclusion(t *testing.T) {
	root := os.Getenv("IOS_COMPILER_OWNED_FIXTURE")
	if root == "" {
		t.Skip("explicit normally owned fixture required")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("fixture root must be absolute")
	}
	for _, name := range []string{"artifact", "artifact.zip"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal("fixture unavailable")
		}
	}
	output := filepath.Join(root, "artifact")
	if err := os.Mkdir(output, 0700); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	a, digest := iosCompilerFixture(t, "")
	archive := filepath.Join(root, "artifact.zip")
	if err := os.WriteFile(archive, a.snapshot.archive, 0600); err != nil {
		t.Fatal(err)
	}
	for name, data := range a.snapshot.files {
		if err := os.WriteFile(filepath.Join(output, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := iosCompilerPinAt(archive, output)
	if err != nil {
		fs := skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}
		defer fs.close()
		_, dirOK := fs.directory(output)
		t.Logf("pin diagnostic: blocked=%t alias=%t directory=%t", fs.blocked(archive), ownerWindowsAlias(archive), dirOK)
		t.Fatal("native pin", err)
	}
	defer s.Close()
	raw, id := s.Archive()
	if id == "" || iosCompilerHash(raw) != digest {
		t.Fatal("native archive mismatch")
	}
	files := map[string][]byte{}
	for _, name := range iosCompilerNames {
		data, id, err := s.File(name)
		if err != nil || id == "" {
			t.Fatal("native file pin", name, err)
		}
		files[name] = data
	}
	if !iosCompilerMatchArchive(raw, digest, files) || s.Recheck() != nil {
		t.Fatal("native same bytes validation")
	}
	if f, err := os.OpenFile(archive, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Fatal("held archive allowed writer")
	}
	if err := os.Rename(filepath.Join(output, "input.swift"), filepath.Join(output, "foreign.swift")); err == nil {
		t.Fatal("held original allowed rename")
	}
	if err := s.Close(); err != nil {
		t.Fatal("native close", err)
	}
	if f, err := os.OpenFile(archive, os.O_WRONLY, 0); err != nil {
		t.Fatal("native handle leaked", err)
	} else {
		f.Close()
	}
	for _, data := range files {
		if !bytes.Equal(data, make([]byte, len(data))) {
			t.Fatal("native snapshot buffer not erased")
		}
	}
}
