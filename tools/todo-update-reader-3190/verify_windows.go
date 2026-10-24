package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// environment is the reader's only connection to the outside: the deployed
// guard's CLI. The reader runs no Git; it reads the repository's files itself.
// Tests replace the guard with a recorded fake and observe every read and
// directory listing.
type environment struct {
	guard         func(args ...string) (stdout []byte, exitCode int, ran bool)
	beforeRead    func(path string)
	beforeList    func(dir string)
	beforeRecheck func()
}

const guardExecutable = `D:\CC\Tools\client-records\scrub391.exe`

const (
	createNoWindow   = 0x08000000
	maxEvidenceBytes = 1 << 20
	maxInputBytes    = 16 << 20
	maxArtifactBytes = 64 << 20
	guardWorkers     = 4
)

func productionEnvironment() environment {
	return environment{guard: runGuard}
}

// exactLeaf accepts only the compiled leaf, spelled with either separator or
// any case. Parents, siblings, streams, trailing dots or spaces, device and
// extended-length prefixes and short names never compare equal.
func exactLeaf(path, target string) bool {
	if len(path) < 3 || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") ||
		strings.ContainsAny(path[2:], `:*?"<>|`) {
		return false
	}
	return strings.EqualFold(filepath.Clean(path), target)
}

// verifyUpdate qualifies the one fixed update or returns a fixed reason code.
// It never returns a partially filled qualification.
func verifyUpdate(requested string, p profile, env environment) (qualification, string) {
	if !exactLeaf(requested, p.target) {
		return qualification{}, "target_path"
	}
	s := newSession(env)
	defer s.close()

	// Hold the installed leaf, unread, so the purpose entry and the digest
	// below observe one file object that nobody can write or replace.
	target, ok := s.pin(p.target, maxArtifactBytes)
	if !ok {
		return qualification{}, "target_leaf"
	}
	if !capacityQualified(env, p) {
		return qualification{}, "baseline_qualification"
	}
	_, current, ok := s.read(target)
	if !ok {
		return qualification{}, "target_leaf"
	}
	if current != p.baselineSHA {
		return qualification{}, "baseline_mismatch"
	}

	if _, ok := s.pinDir(p.root); !ok || !guardClear(env, p.root) {
		return qualification{}, "source_root"
	}
	tree, reason := s.checkSource(p)
	if reason != "" {
		return qualification{}, reason
	}

	data, reason := s.evidence(filepath.Join(p.root, p.receiptName), p.receiptSHA, maxEvidenceBytes, "receipt")
	if reason != "" {
		return qualification{}, reason
	}
	var receipt buildReceipt
	if !decodeSingle(data, &receipt, true) || !receiptMatches(receipt, p) {
		return qualification{}, "receipt"
	}

	data, reason = s.evidence(filepath.Join(p.root, p.logName), p.logSHA, maxEvidenceBytes, "log")
	if reason != "" {
		return qualification{}, reason
	}
	logCrates, ok := parseBuildLog(data, p.root)
	if !ok {
		return qualification{}, "log"
	}

	data, reason = s.evidence(filepath.Join(p.root, filepath.FromSlash(p.depInfoName)), p.depInfoSHA, maxEvidenceBytes, "dep_info")
	if reason != "" {
		return qualification{}, reason
	}
	artifact := filepath.Join(p.root, filepath.FromSlash(strings.TrimSuffix(p.depInfoName, path.Ext(p.depInfoName))+".exe"))
	deps, ok := parseDepInfo(data, p.root, artifact)
	if !ok {
		return qualification{}, "dep_info"
	}

	manifests, reason := s.inputs(receipt.Inputs, tree, p)
	if reason != "" {
		return qualification{}, reason
	}
	crates, ok := closureComplete(receipt.Inputs, deps, logCrates, tree, manifests, p)
	if !ok {
		return qualification{}, "closure"
	}

	candidate, ok := s.pin(filepath.Join(p.root, p.candidateName), maxArtifactBytes)
	if !ok {
		return qualification{}, "candidate"
	}
	if !guardClear(env, candidate.path) {
		return qualification{}, "guard_classification"
	}
	if _, sum, ok := s.read(candidate); !ok || candidate.size != p.candidateBytes || sum != p.candidateSHA {
		return qualification{}, "candidate"
	}

	if env.beforeRecheck != nil {
		env.beforeRecheck()
	}
	if !s.recheck() {
		return qualification{}, "recheck"
	}
	return qualification{
		Qualified: true, Domain: qualificationDomain, TodoID: qualificationTodo, VerifierTodoID: verifierTodo,
		TargetKind: qualificationKind, Target: filepath.ToSlash(p.target),
		CurrentSHA256: current, NewSHA256: p.candidateSHA, NewBytes: p.candidateBytes,
		SourceRepository: receipt.Repository, SourceRoot: filepath.ToSlash(p.root), SourceHead: p.head, SourceTree: p.tree,
		BuildReceiptSHA256: p.receiptSHA, BuildJob: receipt.Job, BuildCommand: slices.Clone(receipt.Command),
		BuildLogSHA256: p.logSHA, DepInfoSHA256: p.depInfoSHA,
		InputsVerified: len(receipt.Inputs), DepInfoEntries: len(deps), LocalCrates: crates,
		BaselineQualifiedBy: p.capacity.domain, BaselineSourceReceiptSHA256: p.capacity.receiptSHA,
	}, ""
}

// evidence pins one build record, has the guard classify it, and returns the
// exact bytes whose digest matched the compiled pin.
func (s *session) evidence(name, pin string, limit int64, reason string) ([]byte, string) {
	f, ok := s.pin(name, limit)
	if !ok {
		return nil, reason
	}
	if !guardClear(s.env, name) {
		return nil, "guard_classification"
	}
	data, sum, ok := s.read(f)
	if !ok || sum != pin {
		return nil, reason
	}
	return data, ""
}

// inputs pins every receipt input, has the guard classify all of them, then
// hashes each pinned file and ties its bytes to the fixed tree's blob. Nothing
// else reads these files. Manifest bytes are kept for the closure check.
func (s *session) inputs(inputs []receiptInput, tree map[string]treeEntry, p profile) (map[string][]byte, string) {
	files := make([]*pinnedFile, len(inputs))
	for i, in := range inputs {
		entry, ok := tree[in.Path]
		if !ok || entry.kind != "blob" || (entry.mode != "100644" && entry.mode != "100755") || entry.object != in.Blob {
			return nil, "input"
		}
		if files[i], ok = s.pin(filepath.Join(p.root, filepath.FromSlash(in.Path)), maxInputBytes); !ok {
			return nil, "input"
		}
	}
	if !s.classifyAll(files) {
		return nil, "guard_classification"
	}
	manifests := map[string][]byte{}
	for i, in := range inputs {
		data, sum, ok := s.read(files[i])
		if !ok || sum != in.SHA256 || !checkedOut(data, in.Blob) {
			return nil, "input"
		}
		if path.Base(in.Path) == "Cargo.toml" {
			manifests[in.Path] = data
		}
	}
	return manifests, ""
}

// classifyAll has the guard classify every pinned file.
func (s *session) classifyAll(files []*pinnedFile) bool {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	return s.classifyPaths(paths)
}

// classifyPaths has the guard classify every path, guardWorkers at a time.
func (s *session) classifyPaths(paths []string) bool {
	var failed atomic.Bool
	var wg sync.WaitGroup
	next := make(chan string)
	for range guardWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range next {
				if !failed.Load() && !guardClear(s.env, name) {
					failed.Store(true)
				}
			}
		}()
	}
	for _, path := range paths {
		next <- path
	}
	close(next)
	wg.Wait()
	return !failed.Load()
}

type guardVerdict struct {
	OK                       *bool     `json:"ok"`
	AccessAllowed            *bool     `json:"access_allowed"`
	ClientResource           *bool     `json:"client_resource"`
	ClassificationUnverified *bool     `json:"classification_unverified"`
	ProtectionStatus         *string   `json:"protection_status"`
	ProtectionCategories     *[]string `json:"protection_categories"`
}

// guardClear asks the deployed guard's general classifier about one path and
// accepts only an explicit clear verdict with no protection category.
func guardClear(env environment, name string) bool {
	out, code, ran := env.guard("--guard-path", name)
	var v guardVerdict
	if !ran || code != 0 || !decodeSingle(out, &v, false) {
		return false
	}
	return v.OK != nil && *v.OK && v.AccessAllowed != nil && *v.AccessAllowed &&
		v.ClientResource != nil && !*v.ClientResource &&
		v.ClassificationUnverified != nil && !*v.ClassificationUnverified &&
		v.ProtectionStatus != nil && *v.ProtectionStatus == "clear" &&
		v.ProtectionCategories != nil && len(*v.ProtectionCategories) == 0
}

// capacityQualified requires the existing ToDo3085 purpose entry to return
// exactly its known qualification for the installed baseline, nothing more.
func capacityQualified(env environment, p profile) bool {
	out, code, ran := env.guard("--verify-capacity-runtime-update", p.target)
	var report map[string]any
	if !ran || code != 0 || !decodeSingle(out, &report, false) {
		return false
	}
	want := map[string]any{
		"qualified": true, "domain": p.capacity.domain,
		"todo_id": json.Number(strconv.FormatInt(p.capacity.todoID, 10)), "target_kind": p.capacity.targetKind,
		"old_sha256": p.capacity.oldSHA, "new_sha256": p.baselineSHA, "source_receipt_sha256": p.capacity.receiptSHA,
		"data_access_granted": false, "original_registry_recovered": false,
	}
	if len(report) != len(want) {
		return false
	}
	for name, value := range want {
		if report[name] != value {
			return false
		}
	}
	return true
}

// checkSource reads the fixed worktree's Git metadata (readSource) and then
// the objects it needs (objectStore), both by the reader itself: the fixed
// commit's tree, its ancestry from the main commit and the tree's listing. No
// Git runs, so no configuration, alternate store or working file is read on
// the reader's behalf. The guard classifies files one by one, and only the
// pinned inputs are compared with the tree, by the reader itself.
func (s *session) checkSource(p profile) (map[string]treeEntry, string) {
	src, mainID, reason := s.readSource(p)
	if reason != "" {
		return nil, reason
	}
	store, reason := s.openObjects(src, p.limits)
	if reason != "" {
		return nil, reason
	}
	defer store.close()
	if tree, _, ok := store.commit(p.head); !ok || tree != p.tree {
		return nil, store.failure()
	}
	if !store.reaches(mainID, p.head) {
		return nil, store.failure()
	}
	tree, ok := store.listTree(p.tree)
	if !ok {
		return nil, store.failure()
	}
	return tree, ""
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// childEnvironment removes inherited GIT_* overrides from the guard's
// environment.
func childEnvironment(parent []string) []string {
	out := make([]string, 0, len(parent)+3)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
}

func hiddenProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.WaitDelay = time.Second
	cmd.Env = childEnvironment(os.Environ())
	cmd.Stderr = io.Discard
}

// runGuard executes the deployed guard by its fixed path. The executable must
// be a plain file; its classification is the PC's protection authority.
func runGuard(args ...string) ([]byte, int, bool) {
	if info, ok := lstatPlain(guardExecutable); !ok || !info.Mode().IsRegular() {
		return nil, -1, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, guardExecutable, args...)
	hiddenProcess(cmd)
	out := &limitedBuffer{limit: 1 << 20}
	cmd.Stdout = out
	err := cmd.Run()
	if out.overflow {
		return nil, -1, false
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out.Bytes(), exit.ExitCode(), true
		}
		return nil, -1, false
	}
	return out.Bytes(), 0, true
}
