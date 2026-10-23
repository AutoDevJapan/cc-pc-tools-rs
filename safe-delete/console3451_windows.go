//go:build windows

package main

// One normally created Console acceptance tree. This is preservation, never a
// deletion grant: the original producer receipt, Git data and every object move
// together through quarantinePreserved's existing same-volume verification.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const console3451ID = "8810a8e6743a392c14257036d9e13619"
const console3451Root = `C:\Users\makhmeto\AppData\Local\Temp\codex-owned-` + console3451ID
const console3451Git = "runner338/_work/local-ai-console/local-ai-console/.git"
const console3451Proof = "C:/Users/makhmeto/Documents/Codex/2026-10-23/work-management/work/monitor-retirement-official.json"

// Filled from the completed official Actions/merge observation, after capture.
const console3451ProofSHA = "dd01aef973233895fb9b0b5736d3ff6bc4f9ca4ce09927e504dc34383b9375f8"
const console3451RunSHA = "751872604de4cf3cb52cf79340787c29f8480cb3b6a061e7c8e9429480d97eb3"
const console3451JobSHA = "cfcb249d6b287bca1c534f88e225bf7e640a7f2698ff1cdc226ad1c5bd7b8fd0"
const console3451PRSHA = "f603c7d12aa95bcef058ffd6a2dbf44b9f46a7e89ea5c22b1a5bc51d038e45b4"
const console3451MainSHA = "3da23be7ef72cc6d56c5afe6f870b80f033539e2c67769b417f3b992b5e4f390"
const console3451Candidate = "C:/Users/makhmeto/Documents/Codex/2026-10-23/work-management/outputs/console-monitor-release-3437/localai-web-rs.exe"
const console3451Installed = "D:/CC/Tools/localai-web-rs/target/release/localai-web-rs.exe"

type console3451Acceptance struct {
	Version      int    `json:"version"`
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Head, Tree   string
	Run          struct {
		ID                        int64
		WorkflowID                int64 `json:"workflow_id"`
		Path                      string
		Head                      string `json:"head_sha"`
		Status, Conclusion, Event string
		Repository                struct{ ID int64 }
		HeadRepository            struct{ ID int64 } `json:"head_repository"`
	} `json:"run"`
	Job struct {
		ID                       int64
		RunID                    int64  `json:"run_id"`
		Head                     string `json:"head_sha"`
		Status, Conclusion, Name string
	} `json:"job"`
	PR struct {
		Merged bool
		Merge  string `json:"merge_commit_sha"`
		Head   struct {
			SHA  string
			Repo struct{ ID int64 }
		}
		Base struct {
			Ref  string
			Repo struct{ ID int64 }
		}
	} `json:"pr"`
	Main struct {
		Commit struct {
			SHA    string
			Commit struct{ Tree struct{ SHA string } }
		}
	} `json:"main"`
	Installed struct {
		BinaryInstalled  bool `json:"binary_installed"`
		RestartRequested bool `json:"restart_requested"`
		Source, Tree     string
		BinarySHA        string `json:"binary_sha256"`
	} `json:"installed"`
}

func console3451AcceptanceValid(p console3451Acceptance) bool {
	return p.Version == 1 && p.Repository == "AutoDevJapan/local-ai-console" && p.RepositoryID == 1429803672 && len(p.Head) == 40 && len(p.Tree) == 40 &&
		p.Run.ID > 0 && p.Run.WorkflowID == 392978784 && (p.Run.Path == ".github/workflows/windows.yml" || strings.HasPrefix(p.Run.Path, ".github/workflows/windows.yml@refs/pull/")) && p.Run.Head == p.Head && p.Run.Status == "completed" && p.Run.Conclusion == "success" && p.Run.Event == "pull_request" && p.Run.Repository.ID == p.RepositoryID && p.Run.HeadRepository.ID == p.RepositoryID &&
		p.Job.ID > 0 && p.Job.RunID == p.Run.ID && p.Job.Head == p.Head && p.Job.Status == "completed" && p.Job.Conclusion == "success" && p.Job.Name == "acceptance" &&
		p.PR.Merged && console3451SHA(p.PR.Merge, 40) && p.PR.Base.Ref == "main" && p.PR.Head.SHA == p.Head && p.PR.Head.Repo.ID == p.RepositoryID && p.PR.Base.Repo.ID == p.RepositoryID && p.Main.Commit.SHA == p.PR.Merge && p.Main.Commit.Commit.Tree.SHA == p.Tree &&
		p.Installed.BinaryInstalled && p.Installed.RestartRequested && p.Installed.Source == p.Head && p.Installed.Tree == p.Tree
}
func console3451SHA(value string, size int) bool {
	decoded, e := hex.DecodeString(value)
	return e == nil && len(value) == size && len(decoded) == size/2
}
func console3451Pinned(path, expected string, limit int64) ([]byte, error) {
	if !console3451SHA(expected, 64) {
		return nil, fail("CONSOLE_ACCEPTANCE_UNPINNED")
	}
	if _, e := checked(path); e != nil {
		return nil, e
	}
	before, e := fileIdentity(path)
	if e != nil || before.NumberOfLinks != 1 || before.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, fail("CONSOLE_ACCEPTANCE_INVALID")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fail("CONSOLE_ACCEPTANCE_UNAVAILABLE")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	f.Close()
	after, ae := fileIdentity(path)
	hash := sha256.Sum256(b)
	if e != nil || ae != nil || int64(len(b)) > limit || geminiMetadata(before) != geminiMetadata(after) || hex.EncodeToString(hash[:]) != expected {
		return nil, fail("CONSOLE_ACCEPTANCE_INVALID")
	}
	return b, nil
}
func console3451Receipt(r receipt) bool {
	return r.Version == 2 && r.ID == console3451ID && same(r.Path, console3451Root) && same(r.Root, filepath.Dir(console3451Root)) &&
		r.Volume == 2229119083 && r.FileID == 20547673302021654 && r.Created == "2026-10-23T21:55:15.4048148+09:00" &&
		r.Project == "localai-console" && r.Content == "ui3450-ci" && r.Integrity == "117f29851c129d28f1fcd162a6835aa83887708ce03e488e5dc45af9e6ac6ce3" && r.State == "owned" && r.Destination == ""
}
func console3451ReadProof() (console3451Acceptance, error) {
	var proof console3451Acceptance
	if len(console3451ProofSHA) != 64 {
		return proof, fail("CONSOLE_ACCEPTANCE_UNPINNED")
	}
	if _, e := checked(console3451Proof); e != nil {
		return proof, e
	}
	before, e := fileIdentity(console3451Proof)
	if e != nil || before.NumberOfLinks != 1 || before.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return proof, fail("CONSOLE_ACCEPTANCE_INVALID")
	}
	file, e := os.Open(console3451Proof)
	if e != nil {
		return proof, fail("CONSOLE_ACCEPTANCE_UNAVAILABLE")
	}
	bytes, e := io.ReadAll(io.LimitReader(file, 256*1024+1))
	file.Close()
	after, ae := fileIdentity(console3451Proof)
	hash := sha256.Sum256(bytes)
	if e != nil || ae != nil || len(bytes) > 256*1024 || geminiMetadata(before) != geminiMetadata(after) || hex.EncodeToString(hash[:]) != console3451ProofSHA || json.Unmarshal(bytes, &proof) != nil {
		return proof, fail("CONSOLE_ACCEPTANCE_INVALID")
	}
	// Freeze the original GET responses separately; never accept a summary as
	// the official run/job/PR/main observation.
	raw := map[string][]byte{}
	for _, part := range []struct{ name, hash string }{{"run", console3451RunSHA}, {"job", console3451JobSHA}, {"pr", console3451PRSHA}, {"main", console3451MainSHA}} {
		b, e := console3451Pinned(strings.TrimSuffix(console3451Proof, ".json")+"-"+part.name+".json", part.hash, 256<<10)
		if e != nil {
			return proof, fail("CONSOLE_OFFICIAL_RESPONSE_INVALID")
		}
		raw[part.name] = b
	}
	proof, e = console3451ApplyOfficial(proof, raw)
	if e != nil {
		return proof, e
	}
	if !console3451AcceptanceValid(proof) || !console3451SHA(proof.Installed.BinarySHA, 64) {
		return proof, fail("CONSOLE_ACCEPTANCE_INVALID")
	}
	for _, path := range []string{console3451Candidate, console3451Installed} {
		if _, e := console3451Pinned(path, proof.Installed.BinarySHA, 128<<20); e != nil {
			return proof, fail("CONSOLE_DISTRIBUTION_CHANGED")
		}
	}
	return proof, nil
}
func console3451ApplyOfficial(summary console3451Acceptance, raw map[string][]byte) (console3451Acceptance, error) {
	// Every official field starts at zero. A missing field in a frozen API
	// response must never inherit a claimed value from the local summary.
	official := console3451Acceptance{Version: summary.Version, Repository: summary.Repository, RepositoryID: summary.RepositoryID, Head: summary.Head, Tree: summary.Tree, Installed: summary.Installed}
	for _, part := range []struct {
		name   string
		target any
	}{{"run", &official.Run}, {"pr", &official.PR}, {"main", &official.Main}} {
		if json.Unmarshal(raw[part.name], part.target) != nil {
			return official, fail("CONSOLE_OFFICIAL_RESPONSE_INVALID")
		}
	}
	var jobs struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if json.Unmarshal(raw["job"], &jobs) != nil || len(jobs.Jobs) != 1 || json.Unmarshal(jobs.Jobs[0], &official.Job) != nil {
		return official, fail("CONSOLE_OFFICIAL_RESPONSE_INVALID")
	}
	if !console3451AcceptanceValid(official) {
		return official, fail("CONSOLE_OFFICIAL_RESPONSE_INVALID")
	}
	return official, nil
}
func console3451GitSaved(root string, proof console3451Acceptance) error {
	repo := filepath.Join(root, filepath.FromSlash(filepath.Dir(console3451Git)))
	read := func(args ...string) (string, error) {
		common := []string{"--no-optional-locks", "-C", repo, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL"}
		b, e := runHidden(10*time.Second, "git", append(common, args...)...)
		return strings.TrimSpace(string(b)), e
	}
	origin, e := read("remote", "get-url", "origin")
	if e != nil || (origin != "https://github.com/AutoDevJapan/local-ai-console" && origin != "https://github.com/AutoDevJapan/local-ai-console.git") {
		return fail("CONSOLE_GIT_ORIGIN_MISMATCH")
	}
	head, e := read("rev-parse", "HEAD")
	if e != nil || head != proof.Head {
		return fail("CONSOLE_GIT_HEAD_MISMATCH")
	}
	tree, e := read("rev-parse", "HEAD^{tree}")
	if e != nil || tree != proof.Tree {
		return fail("CONSOLE_GIT_TREE_MISMATCH")
	}
	changes, e := read("diff", "--name-only", "HEAD")
	if e != nil || changes != "" {
		return fail("CONSOLE_UNSAVED_SOURCE")
	}
	stash, e := read("stash", "list")
	if e != nil || stash != "" {
		return fail("CONSOLE_UNSAVED_SOURCE")
	}
	untracked, e := read("ls-files", "--others", "--exclude-standard")
	if e != nil {
		return fail("CONSOLE_GIT_UNVERIFIED")
	}
	for _, line := range strings.Split(untracked, "\n") {
		if line != "" && !strings.HasPrefix(line, "artifacts/") {
			return fail("CONSOLE_UNSAVED_SOURCE")
		}
	}
	// actions/checkout must have removed its temporary credential header.
	headers, headerErr := read("config", "--local", "--get-regexp", `^http\..*\.extraheader$`)
	var missing *exec.ExitError
	if headers != "" || (headerErr != nil && !(errors.As(headerErr, &missing) && missing.ExitCode() == 1)) {
		return fail("CONSOLE_RUNNER_NOT_COLLECTED")
	}
	return nil
}
func console3451Idle(root string, names map[string]bool) error {
	snapshot, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return fail("CONSOLE_PROCESSES_UNVERIFIED")
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	query := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	for e = syscall.Process32First(snapshot, &entry); e == nil; e = syscall.Process32Next(snapshot, &entry) {
		if entry.ProcessID == 0 {
			continue
		}
		handle, err := syscall.OpenProcess(0x1000, false, entry.ProcessID)
		if err != nil {
			if names[strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:]))] {
				return fail("CONSOLE_PROCESS_IDENTITY_UNVERIFIED")
			}
			continue
		}
		var image [32768]uint16
		length := uint32(len(image))
		ok, _, _ := query.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&image[0])), uintptr(unsafe.Pointer(&length)))
		syscall.CloseHandle(handle)
		if ok == 0 {
			if names[strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:]))] {
				return fail("CONSOLE_PROCESS_IDENTITY_UNVERIFIED")
			}
			continue
		}
		path := syscall.UTF16ToString(image[:length])
		rel, re := filepath.Rel(root, path)
		if re == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fail("CONSOLE_RESOURCE_IN_USE")
		}
	}
	if e != syscall.ERROR_NO_MORE_FILES {
		return fail("CONSOLE_PROCESSES_UNVERIFIED")
	}
	return nil
}
func (c config) console3451Recovery(id string) (*ci3174Scope, error) {
	if id != console3451ID || !same(c.Store, "D:/CC/Tools/safe-delete/receipts-go") || !same(c.Quarantine, `C:\Users\makhmeto\Downloads\削除希望`) {
		return nil, fail("CONSOLE_SCOPE_MISMATCH")
	}
	r, e := c.load(id, true)
	if e != nil {
		return nil, e
	}
	if !console3451Receipt(r) {
		return nil, fail("CONSOLE_SCOPE_MISMATCH")
	}
	proof, e := console3451ReadProof()
	if e != nil {
		return nil, e
	}
	current, e := ci3174Identity(r.Path)
	if e != nil {
		return nil, e
	}
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	user, e := token.GetTokenUser()
	token.Close()
	if e != nil {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	owner, e := user.User.Sid.String()
	if e != nil || current.Owner != owner {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	s := &ci3174Scope{maxEntries: 10000, maxBytes: 16 << 30, identity: ci3174Identity, volume: r.Volume, rootID: r.FileID, rootBirth: ci3174Birth(current), captureCollected: true, internalLinks: true, preservedGit: console3451Git, createdOwner: owner}
	before, _, e := s.snapshot(r.Path)
	if e != nil {
		return nil, e
	}
	names := map[string]bool{}
	for name, m := range before {
		if m.Attributes&0x400 != 0 {
			return nil, fail("LINK_OR_REPARSE_POINT")
		}
		leaf := strings.ToLower(filepath.Base(name))
		if leaf == ".runner" || strings.HasPrefix(leaf, ".credentials") {
			return nil, fail("CONSOLE_RUNNER_NOT_COLLECTED")
		}
		if strings.HasSuffix(leaf, ".exe") {
			names[leaf] = true
		}
	}
	s.captureCollected = false
	s.baseline = before
	s.opaque = map[string]ci3174Entry{}
	p, _ := c.recordPath(id)
	original, e := ci3250Original(p)
	if e != nil {
		return nil, e
	}
	s.originalReceipt = func() ([]byte, error) {
		current, e := ci3250Original(p)
		if e != nil || string(current) != string(original) {
			return nil, fail("CONSOLE_RECEIPT_CHANGED")
		}
		return current, nil
	}
	s.settled = func() error {
		if _, e := s.originalReceipt(); e != nil {
			return e
		}
		if _, e := console3451ReadProof(); e != nil {
			return e
		}
		if e := console3451GitSaved(r.Path, proof); e != nil {
			return e
		}
		if e := console3451Idle(r.Path, names); e != nil {
			return e
		}
		first, _, e := s.snapshot(r.Path)
		if e != nil {
			return e
		}
		time.Sleep(300 * time.Millisecond)
		second, _, e := s.snapshot(r.Path)
		if e != nil || !reflect.DeepEqual(first, second) {
			return fail("CONSOLE_RESOURCE_CHANGING")
		}
		return nil
	}
	if e = s.settled(); e != nil {
		return nil, e
	}
	return s, nil
}
