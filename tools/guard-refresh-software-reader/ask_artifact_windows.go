//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const askBuildSource = "3f04e69df159bb4f0df1fe80af3b92c611b9f30a"
const askBuildProofPath = "D:/CC/Tools/codex-jobs/fourbros4431-ask-artifact-3f04-source-proof.json"
const askBuildProofSHA = "825de12851246019bf427a26c5f05da0b9f7bf983e3674514ffc17008dadc0d2"
const askBuildOwnedTarget = updateSourceRoot + "/codex-owned-b30691d5cc845d615ea1be06bb0d3de3/cargo-final-3f04"
const askBuildOutput = askBuildOwnedTarget + "/release/discord_ask.exe"
const askBuildAlias = askBuildOwnedTarget + "/release/deps/discord_ask.exe"
const askBuildExport = updateSourceRoot + "/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-privacy-3f04.exe"
const askBuildReceipt = "D:/CC/Tools/codex-jobs/fourbros4431-ask-artifact-producer-receipt.json"

type askBuildEvidence struct {
	Head    string
	Records int
	Inputs  []buildSourceRecord
}
type askArtifactResult struct {
	Qualified         bool              `json:"qualified"`
	Domain            string            `json:"domain"`
	SHA               string            `json:"sha256,omitempty"`
	Bytes             int               `json:"bytes,omitempty"`
	Source            string            `json:"source"`
	ExportCreated     bool              `json:"export_created"`
	Identity          *softwareIdentity `json:"identity,omitempty"`
	DataAccessGranted bool              `json:"data_access_granted"`
	UpdatePerformed   bool              `json:"update_performed"`
	Reason            string            `json:"reason"`
	ProducerExit      int               `json:"producer_exit"`
	ProducerReaped    bool              `json:"producer_reaped"`
	ProducerElapsedMS int64             `json:"producer_elapsed_ms"`
	OutputIdentity    *softwareIdentity `json:"output_identity,omitempty"`
	TargetCreated     bool              `json:"target_created"`
	TargetState       string            `json:"target_state"`
}

// This purpose recognizes the two names created by the recorded Cargo build.
// It does not change ordinary hardlink rejection or qualify a data pathname.
func askBuildSourceMatches(ctx context.Context, raw []byte, git func(...string) ([]byte, bool)) bool {
	if digest(raw) != askBuildProofSHA {
		return false
	}
	var evidence askBuildEvidence
	if json.Unmarshal(raw, &evidence) != nil || evidence.Head != askBuildSource || evidence.Records != 207 || len(evidence.Inputs) != 207 {
		return false
	}
	// Reuse the exact tree/archive/blob/digest validator before opening output.
	marker := []byte("fixed software producer input check")
	proof := askSoftwareProof{Head: askBuildSource, Candidate: askBuildOutput, SHA: digest(marker), Bytes: len(marker), Sources: evidence.Inputs}
	encoded, e := json.Marshal(proof)
	if e != nil {
		return false
	}
	plan := askSoftwarePlan{Candidate: askBuildOutput, Source: askBuildSource, SHA: digest(marker), ProofSHA: digest(encoded), Bytes: len(marker)}
	return askCandidateMatchesContext(ctx, marker, encoded, plan, git)
}

func softwareLinks(path string) ([]string, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	k := syscall.NewLazyDLL("kernel32.dll")
	h, _, err := k.NewProc("FindFirstFileNameW").Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
	if h == ^uintptr(0) {
		return nil, err
	}
	defer k.NewProc("FindClose").Call(h)
	result := []string{}
	for {
		name := syscall.UTF16ToString(buf)
		result = append(result, filepath.Clean(filepath.VolumeName(path)+name))
		n = uint32(len(buf))
		ok, _, err := k.NewProc("FindNextFileNameW").Call(h, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
		if ok == 0 {
			if err != syscall.Errno(38) {
				return nil, err
			}
			break
		}
		if len(result) >= 3 {
			return nil, errUpdateObject
		}
	}
	return result, nil
}

func exactTwoSoftwareLinks(names []string, a, b string) bool {
	if len(names) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range names {
		if !sameCleanPath(p, a) && !sameCleanPath(p, b) {
			return false
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// All known alias handles and plain parents are held before any content read.
// No alias is renamed/deleted to make the generic guard pass.
func withPinnedCargoOutput(a, b string, expected softwareIdentity, consume func([]byte, softwareIdentity) bool) bool {
	var held []syscall.Handle
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			syscall.CloseHandle(held[i])
		}
	}()
	for _, path := range []string{a, b} {
		var parents []string
		for p := filepath.Dir(path); ; p = filepath.Dir(p) {
			parents = append(parents, p)
			if filepath.Dir(p) == p {
				break
			}
		}
		for i := len(parents) - 1; i >= 0; i-- {
			h, ok := openPlain(parents[i], true)
			if !ok {
				return false
			}
			held = append(held, h)
		}
	}
	var id softwareIdentity
	var aliases []syscall.Handle
	for _, path := range []string{a, b} {
		p, e := syscall.UTF16PtrFromString(path)
		if e != nil {
			return false
		}
		h, e := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
		if e != nil {
			return false
		}
		held = append(held, h)
		aliases = append(aliases, h)
		var info syscall.ByHandleFileInformation
		actual, ok := handlePath(h)
		if !ok || !sameCleanPath(actual, path) || syscall.GetFileInformationByHandle(h, &info) != nil || info.NumberOfLinks != 2 || info.FileAttributes&0x410 != 0 {
			return false
		}
		got := softwareIdentity{Volume: fmtUint(uint64(info.VolumeSerialNumber)), FileID: fmtUint(uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)), Created: fmtUint(uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime))}
		if id.FileID != "" && id != got {
			return false
		}
		id = got
	}
	if expected.FileID != "" && (id.Volume != expected.Volume || id.FileID != expected.FileID) {
		return false
	}
	names, e := softwareLinks(a)
	if e != nil || !exactTwoSoftwareLinks(names, a, b) {
		return false
	}
	// ReadFile on the already pinned first alias; no pathname is reopened.
	raw := make([]byte, 0, 16<<20)
	scratch := make([]byte, 64<<10)
	defer clear(scratch)
	var n uint32
	for {
		e = syscall.ReadFile(aliases[0], scratch, &n, nil)
		if e != nil {
			clear(raw)
			return false
		}
		if n == 0 {
			break
		}
		if len(raw)+int(n) > 32<<20 {
			clear(raw)
			return false
		}
		raw = append(raw, scratch[:n]...)
	}
	defer clear(raw)
	names, e = softwareLinks(a)
	if e != nil || !exactTwoSoftwareLinks(names, a, b) {
		return false
	}
	return consume(raw, id)
}

func fmtUint(n uint64) string { return strconv.FormatUint(n, 10) }

func createAskArtifactCopy(path string, raw []byte) (softwareIdentity, error) {
	var parents []syscall.Handle
	defer func() {
		for i := len(parents) - 1; i >= 0; i-- {
			syscall.CloseHandle(parents[i])
		}
	}()
	var names []string
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		names = append(names, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(names) - 1; i >= 0; i-- {
		h, ok := openPlain(names[i], true)
		if !ok {
			return softwareIdentity{}, errUpdateObject
		}
		parents = append(parents, h)
	}
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return softwareIdentity{}, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.CREATE_NEW, 0x00200000, 0)
	if e != nil {
		return softwareIdentity{}, e
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	id, e := fileSoftwareIdentity(f)
	if e != nil {
		return softwareIdentity{FileID: "unknown_created"}, e
	}
	actual, ok := handlePath(h)
	if !ok || !sameCleanPath(path, actual) {
		return id, errUpdateObject
	}
	n, e := f.Write(raw)
	if e != nil || n != len(raw) || f.Sync() != nil {
		return id, errUpdateObject
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return id, e
	}
	check := make([]byte, len(raw)+1)
	defer clear(check)
	n, e = io.ReadFull(f, check)
	if e != io.EOF && e != io.ErrUnexpectedEOF {
		return id, errUpdateObject
	}
	if n != len(raw) || digest(check[:n]) != digest(raw) {
		return id, errUpdateObject
	}
	return id, nil
}

func runAskBuildArtifact(args []string, out io.Writer) int {
	if len(args) != 1 || args[0] != "--export-ask-cargo-output" {
		return 2
	}
	r := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: askBuildSource, Reason: "unqualified", TargetState: "unknown"}
	exe, e := os.Executable()
	if e != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	readyAccepted := false
	child := askOwnedChildWithHandshake(ctx, exe, func(payload []byte) bool {
		if ctx.Err() != nil {
			return false
		}
		var p askArtifactResult
		if json.Unmarshal(payload, &p) != nil || p.Domain != r.Domain || p.Source != askBuildSource {
			return false
		}
		r = p
		if p.Reason != "producer_output_held" || p.OutputIdentity == nil || p.SHA == "" || p.Bytes <= 0 || !p.ProducerReaped || p.ProducerExit != 0 {
			return false
		}
		readyAccepted = withPinnedCargoOutput(askBuildOutput, askBuildAlias, *p.OutputIdentity, func(raw []byte, id softwareIdentity) bool {
			if ctx.Err() != nil || digest(raw) != p.SHA || len(raw) != p.Bytes || id != *p.OutputIdentity {
				return false
			}
			made, e := createAskArtifactCopy(askBuildExport, raw)
			if made.FileID != "" {
				r.ExportCreated = true
				r.Identity = &made
				r.Reason = "incomplete_export_preserved"
			}
			if e != nil || ctx.Err() != nil {
				return false
			}
			r.Reason = "export_awaiting_producer_exit"
			return true
		})
		return readyAccepted
	}, "--ask-build-worker")
	if r.TargetState == "unknown" && len(child.Raw) > 0 {
		var failed askArtifactResult
		if json.Unmarshal(child.Raw, &failed) == nil && failed.Domain == r.Domain && failed.Source == r.Source && !failed.Qualified && failed.TargetState != "" {
			r = failed
		}
	}
	clear(child.Raw)
	if !readyAccepted || !child.Started || !child.Assigned || !child.WaitVerified || !child.Reaped || child.TimedOut || child.Exit != 0 {
		r.Qualified = false
		if r.Reason == "export_awaiting_producer_exit" {
			r.Reason = "export_preserved_producer_exit_unknown"
		}
	} else if ctx.Err() == nil {
		// A successful receipt is published only after the producer ACK and
		// its verified termination, while the exported generation is pinned.
		exported, e := openUpdateObject(askBuildExport)
		if e == nil {
			defer exported.Close()
			if ctx.Err() == nil && r.Identity != nil && exported.ID() == *r.Identity && len(exported.Raw()) == r.Bytes && digest(exported.Raw()) == r.SHA {
				r.Qualified = true
				r.Reason = "fixed_software_exported"
				if writeAskBuildReceiptContext(ctx, askBuildReceipt, r, nil) != nil {
					r.Qualified = false
					r.Reason = "export_preserved_receipt_failed"
				}
			}
		}
	}
	if r.ExportCreated && !r.Qualified && r.Reason == "export_awaiting_producer_exit" {
		r.Reason = "export_preserved_receipt_unavailable"
	}
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified {
		return 1
	}
	return 0
}

func runAskBuildWorker(out io.Writer) int {
	r := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: askBuildSource, Reason: "unqualified", TargetState: "not_created"}
	defer func() {
		if r.Reason != "producer_acknowledged" {
			json.NewEncoder(out).Encode(r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	proof, e := openUpdateObject(askBuildProofPath)
	if e == nil {
		defer proof.Close()
		git := func(args ...string) ([]byte, bool) {
			return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
		}
		if askBuildSourceMatches(ctx, proof.Raw(), git) {
			var evidence askBuildEvidence
			if json.Unmarshal(proof.Raw(), &evidence) != nil {
				return 1
			}
			var sources []updateObject
			defer func() {
				for _, o := range sources {
					o.Close()
				}
			}()
			for _, s := range evidence.Inputs {
				o, err := openUpdateObject(filepath.Join(updateSourceRoot, filepath.FromSlash(s.Path)))
				if err != nil {
					return 1
				}
				sources = append(sources, o)
				if digest(o.Raw()) != s.SHA {
					return 1
				}
			}
			head, ok := git("rev-parse", "HEAD")
			if !ok || strings.TrimSpace(string(head)) != askBuildSource {
				return 1
			}
			// The final source differs from the old unqualified artifact. Own
			// this necessary producer invocation and retain its exact source
			// buffers and parent names through output collection/export.
			// A fresh isolated target excludes preexisting Cargo Fresh outputs
			// and keeps normal target writers separate after cargo_gate releases
			// its build slot. Do not adopt the old unqualified two-link image.
			if err := os.Mkdir(askBuildOwnedTarget, 0700); err != nil {
				r.Reason = "isolated_target_collision_or_unavailable"
				r.TargetState = "preexisting_or_unknown"
				return 1
			}
			r.TargetCreated = true
			r.TargetState = "created_preserved"
			// Keep the directory's deletion namespace pinned through export.
			dir, ok := openPlain(askBuildOwnedTarget, true)
			if !ok {
				return 1
			}
			defer syscall.CloseHandle(dir)
			if os.Setenv("CODEX_JOBS_REAL_CARGO", "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe") != nil {
				return 1
			}
			buildCtx, buildCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			started := time.Now()
			built := askOwnedChild(buildCtx, "D:/CC/Tools/codex-jobs/cargo_gate.exe", "build", "--locked", "--release", "--manifest-path", updateSourceRoot+"/Cargo.toml", "--target-dir", askBuildOwnedTarget, "-p", "cc-ask", "--bin", "discord_ask")
			buildCancel()
			clear(built.Raw)
			r.ProducerExit = built.Exit
			r.ProducerReaped = built.Reaped
			r.ProducerElapsedMS = time.Since(started).Milliseconds()
			if !built.Started || !built.Assigned || !built.WaitVerified || !built.Reaped || built.TimedOut || built.Exit != 0 {
				r.Reason = "producer_failed_or_uncollected"
				return 1
			}
			acknowledged := withPinnedCargoOutput(askBuildOutput, askBuildAlias, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
				if len(raw) < 1 || len(raw) > 32<<20 {
					return false
				}
				// Pinned input bytes and namespace stayed held throughout build;
				// compare them again at the same consumption boundary.
				for i, o := range sources {
					if digest(o.Raw()) != evidence.Inputs[i].SHA {
						return false
					}
				}
				r.Bytes = len(raw)
				r.SHA = digest(raw)
				r.OutputIdentity = &id
				r.Reason = "producer_output_held"
				if json.NewEncoder(out).Encode(r) != nil {
					return false
				}
				// Parent completion is acknowledged while both aliases and all
				// source handles remain held. No EOF/exit implies acceptance.
				line, e := bufio.NewReader(io.LimitReader(os.Stdin, 32)).ReadString('\n')
				if e != nil || line != "ACCEPT\n" {
					r.Reason = "producer_rejected_or_ack_unknown"
					return false
				}
				r.Reason = "producer_acknowledged"
				return true
			})
			if acknowledged {
				return 0
			}
		}
	}
	return 1
}

func writeAskBuildReceipt(path string, r askArtifactResult) error {
	return writeAskBuildReceiptContext(context.Background(), path, r, nil)
}

// Test hooks inject faults at the actual I/O boundaries, not in a parallel
// state model. No successful final name exists before Flush/Close succeed.
func writeAskBuildReceiptContext(ctx context.Context, path string, r askArtifactResult, hook func(string) error) error {
	step := func(name string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if hook != nil {
			return hook(name)
		}
		return nil
	}
	value := struct {
		Result      askArtifactResult `json:"result"`
		ProofSHA    string            `json:"source_proof_sha256"`
		InputCount  int               `json:"source_inputs"`
		BuildTarget string            `json:"isolated_build_target"`
		Export      string            `json:"export"`
	}{r, askBuildProofSHA, 207, askBuildOwnedTarget, askBuildExport}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if e = step("before_create"); e != nil {
		return e
	}
	f, e := os.OpenFile(path+".pending", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
	}()
	n, e := f.Write(raw)
	if e != nil || n != len(raw) {
		return errUpdateObject
	}
	if e = step("before_sync"); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = step("after_sync"); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	closed = true
	if e = step("before_publish"); e != nil {
		return e
	}
	from, e := syscall.UTF16PtrFromString(path + ".pending")
	if e != nil {
		return e
	}
	to, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 8)
	if ok == 0 {
		return err
	}
	return nil
}
