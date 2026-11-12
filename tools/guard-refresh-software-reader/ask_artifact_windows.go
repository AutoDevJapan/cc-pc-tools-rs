//go:build windows

package main

import (
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

const askBuildSource = "d5f2d73327bcc39d565c2ce0917b0bdb614c2333"
const askBuildProofPath = "D:/CC/Tools/codex-jobs/fourbros4431-ask-artifact-d5f2-source-proof-v2.json"
const askBuildProofSHA = "a99a2e11e8dbeb766284237cbfdb3d49c016b972ac76eea8e71cead22c7c2662"
const askBuildOutput = updateSourceRoot + "/target/release/discord_ask.exe"
const askBuildAlias = updateSourceRoot + "/target/release/deps/discord_ask.exe"
const askBuildExport = updateSourceRoot + "/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-privacy-d5f2.exe"

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
	r := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: askBuildSource, Reason: "unqualified"}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	proof, e := openUpdateObject(askBuildProofPath)
	if e == nil {
		defer proof.Close()
		git := func(args ...string) ([]byte, bool) {
			return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
		}
		if askBuildSourceMatches(ctx, proof.Raw(), git) {
			withPinnedCargoOutput(askBuildOutput, askBuildAlias, softwareIdentity{Volume: "2229119083", FileID: "1970324853903258"}, func(raw []byte, id softwareIdentity) bool {
				if len(raw) != 13400576 || ctx.Err() != nil {
					return false
				}
				made, e := createAskArtifactCopy(askBuildExport, raw)
				if made.FileID != "" {
					r.ExportCreated = true
					r.Identity = &made
					r.Reason = "incomplete_export_preserved"
				}
				if e != nil {
					return false
				}
				r.Qualified = true
				r.Reason = "fixed_software_exported"
				r.ExportCreated = true
				r.Bytes = len(raw)
				r.SHA = digest(raw)
				_ = id
				return true
			})
		}
	}
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified {
		return 1
	}
	return 0
}
