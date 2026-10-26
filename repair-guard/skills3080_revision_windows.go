package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const skillsRevisionTarget = "design-reference-evidence/references/sources.json"
const skillsRevisionHistory = "design-reference-evidence/references/history/sources-before-revision1.json"
const skillsRevisionPending = "design-reference-evidence/references/sources-revision1.pending.json"
const skillsRevisionOldSHA = "50a6aa009b0cc2d84147d300f7b1cdca5abf9b6003e36c4e004c67240ac439a7"
const skillsRevisionNewSHA = "e0607f086fd394bc0399c99e4101cef48e56c509272741cde81dba029778d8f4"
const skillsRevisionManifestSHA = "3d27a7072716df74d1edab8eb3187afba9c8a7f6a315260e4ed8fc54e04d9a91"
const skillsRevisionCreationSHA = "50a697af5013beb3b4f9e45087a5915e2593333ab60512a8668ce44a22f31166"
const skillsRevisionCreationCall = "call_c8e903093277429c87d06622d240f64f"

// Three finite operations: stage new bytes, apply two no-replace renames, read
// the resulting snapshot. Only original canonical native calls issue authority.
// A caller cannot submit an ID, receipt, root, backup or replacement body.
type skillsRevisionRequest struct {
	Creation skillsConsumeRequest  `json:"creation"`
	Prepared *skillsConsumeRequest `json:"prepared,omitempty"`
	Applied  *skillsConsumeRequest `json:"applied,omitempty"`
	Indices  []int                 `json:"indices,omitempty"`
}

type skillsRevisionEvidence struct {
	Domain         string                       `json:"domain"`
	Writer         skillsWriter                 `json:"writer"`
	RequestSHA     string                       `json:"request_sha256"`
	ManifestCommit string                       `json:"manifest_commit"`
	ManifestPath   string                       `json:"manifest_path"`
	ManifestSHA    string                       `json:"manifest_sha256"`
	CreationSHA    string                       `json:"creation_request_sha256"`
	CreationCall   string                       `json:"creation_native_call_id"`
	Review         skillsResultRequest          `json:"review"`
	Producer       skillsProducer               `json:"producer"`
	Original       skillsCreatedFile            `json:"original"`
	Pending        skillsCreatedFile            `json:"pending"`
	History        skillsCreatedDirectory       `json:"history_directory"`
	Recovery       *skillsRevisionRecoveryProof `json:"native_recovery,omitempty"`
}

func skillsRevisionManifestShape(old, next skillsManifest) bool {
	if len(old.Files) != 52 || len(next.Files) != 53 || old.Writer != next.Writer || old.Decision != next.Decision ||
		skillsProjectionSHA(old.Sources) != skillsProjectionSHA(next.Sources) ||
		old.Files[27].Target != skillsRevisionTarget || old.Files[27].SHA != skillsRevisionOldSHA || len(old.Files[27].body) != 8896 ||
		next.Files[27].Target != skillsRevisionTarget || next.Files[27].SHA != skillsRevisionNewSHA || len(next.Files[27].body) != 10097 ||
		next.Files[52].Target != skillsRevisionHistory || !bytes.Equal(next.Files[52].body, old.Files[27].body) || next.Files[52].SHA != old.Files[27].SHA {
		return false
	}
	for i := range old.Files {
		if i != 27 && (old.Files[i].Target != next.Files[i].Target || old.Files[i].SHA != next.Files[i].SHA || !bytes.Equal(old.Files[i].body, next.Files[i].body)) {
			return false
		}
	}
	return true
}

func skillsRevisionCreation(c skillsConsumeRequest, r skillsRequest) bool {
	return c.Creator.Job == "20261026-090101-todo-3080" && skillsWriterValid(c.Creator) && c.RequestSHA == skillsRevisionCreationSHA && c.CallID == skillsRevisionCreationCall &&
		filepath.IsAbs(c.RequestPath) && within(c.Creator.Workspace, c.RequestPath) && len(c.Indices) == 0 && r.ManifestSHA == skillsRevisionManifestSHA &&
		r.ManifestPath == "docs/skills-3080-operations/revise-design-reference1-proposed090101.json"
}

// Mutation-capable opens still deny write/delete sharing. ID comparison occurs
// before any content read; DELETE access never includes write-existing access.
func skillsRevisionOpen(s *skillsFS, path string, expected skillsCreatedFile, rename bool) (*os.File, string) {
	if s.blocked(path) || ownerWindowsAlias(path) {
		return nil, "protection"
	}
	ph, ok := s.directory(filepath.Dir(path))
	if !ok {
		return nil, "current_identity"
	}
	h, ok := skillsOpenAtAccess(ph, filepath.Base(path), false, false, false, rename)
	if !ok {
		return nil, "current_identity"
	}
	f := os.NewFile(uintptr(h), path)
	s.files = append(s.files, f)
	id, ok := skillsHandleID(h)
	actual, err := ownerHandlePath(h)
	if !ok || id != expected.ID || err != nil || !same(actual, path) {
		return nil, "current_identity"
	}
	var before, after syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &before) != nil || before.FileSizeHigh != 0 || int(before.FileSizeLow) != expected.Size {
		return nil, "current_bytes"
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(expected.Size)+1))
	if err != nil || len(b) != expected.Size || hashBytes(b) != expected.SHA || syscall.GetFileInformationByHandle(h, &after) != nil {
		return nil, "current_bytes"
	}
	before.LastAccessTime = after.LastAccessTime
	if before != after || s.blocked(path) {
		return nil, "current_identity"
	}
	return f, ""
}

func skillsRevisionDirectories(e skillsCreatedEvidence, root string, s *skillsFS) string {
	for _, d := range e.Directories {
		p := filepath.Join(root, filepath.FromSlash(d.Relative))
		if s.blocked(p) {
			return "protection"
		}
		h, ok := s.directory(p)
		id, valid := skillsHandleID(h)
		if !ok || !valid || id != d.ID {
			return "current_identity"
		}
	}
	return ""
}

// Use an already held source object and held destination directory. Class 10
// has ReplaceIfExists=false: an earlier foreign creation is always a collision.
// https://learn.microsoft.com/windows-hardware/drivers/ddi/ntifs/nf-ntifs-ntsetinformationfile
var skillsNtSetInformationFile = syscall.NewLazyDLL("ntdll.dll").NewProc("NtSetInformationFile")

func skillsRevisionRename(f *os.File, parent syscall.Handle, name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return false
	}
	u, err := syscall.UTF16FromString(name)
	if err != nil {
		return false
	}
	type header struct {
		Replace uint32
		Root    syscall.Handle
		Length  uint32
		Name    [1]uint16
	}
	n := int(unsafe.Offsetof(header{}.Name)) + 2*(len(u)-1)
	if n < int(unsafe.Sizeof(header{})) {
		n = int(unsafe.Sizeof(header{}))
	}
	buf := make([]byte, n)
	h := (*header)(unsafe.Pointer(&buf[0]))
	h.Root, h.Length = parent, uint32(2*(len(u)-1))
	copy(unsafe.Slice(&h.Name[0], len(u)-1), u[:len(u)-1])
	var status skillsIOStatus
	code, _, _ := skillsNtSetInformationFile.Call(f.Fd(), uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 10)
	return code == 0
}

func skillsRevisionAbsent(p string) bool { _, err := os.Lstat(p); return os.IsNotExist(err) }

// This internal core has no caller-selected output paths. Prepare changes no
// old object. Interrupted/unissued staging is held without touching the old
// version; only a canonical prepared receipt can resume an issued transaction.
func skillsRevisionPrepare(old skillsManifest, original skillsCreatedEvidence, root string, next skillsManifest, s *skillsFS) (skillsRevisionEvidence, string) {
	var e skillsRevisionEvidence
	if len(old.Files) != 52 || len(next.Files) != 53 || len(original.Files) != 52 {
		return e, "manifest"
	}
	if reason := skillsRevisionDirectories(original, root, s); reason != "" {
		return e, reason
	}
	for _, rel := range []string{skillsRevisionTarget, skillsRevisionPending, skillsRevisionHistory, filepath.ToSlash(filepath.Dir(skillsRevisionHistory))} {
		if s.blocked(filepath.Join(root, filepath.FromSlash(rel))) {
			return e, "protection"
		}
	}
	if _, reason := skillsRevisionOpen(s, filepath.Join(root, filepath.FromSlash(skillsRevisionTarget)), original.Files[27], false); reason != "" {
		return e, reason
	}
	history := filepath.Join(root, filepath.FromSlash(filepath.Dir(skillsRevisionHistory)))
	pending := filepath.Join(root, filepath.FromSlash(skillsRevisionPending))
	if !skillsRevisionAbsent(history) || !skillsRevisionAbsent(pending) {
		return e, "existing_target"
	}
	ph := s.dirs[strings.ToLower(filepath.Dir(history))]
	h, ok := skillsOpenAt(ph, filepath.Base(history), true, true)
	if !ok {
		return e, "existing_target"
	}
	s.dirs[strings.ToLower(history)] = h
	id, ok := skillsHandleID(h)
	if !ok {
		return e, "creation_evidence"
	}
	e.History = skillsCreatedDirectory{filepath.ToSlash(filepath.Dir(skillsRevisionHistory)), id}
	h, ok = skillsOpenAt(ph, filepath.Base(pending), false, true)
	if !ok {
		return e, "existing_target"
	}
	f := os.NewFile(uintptr(h), pending)
	s.files = append(s.files, f)
	body := next.Files[27].body
	n, err := f.Write(body)
	if err != nil || n != len(body) || f.Sync() != nil {
		return e, "create_failed"
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return e, "create_failed"
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(len(body))+1))
	id, ok = skillsHandleID(h)
	if err != nil || !ok || !bytes.Equal(b, body) {
		return e, "create_failed"
	}
	e.Original, e.Pending = original.Files[27], skillsCreatedFile{skillsRevisionPending, hashBytes(b), len(b), id}
	return e, ""
}

// The checkpoint callback is a test seam only. It simulates process loss or a
// newly observed owner stop at each actual rename boundary, never request data.
func skillsRevisionApply(e skillsRevisionEvidence, original skillsCreatedEvidence, root string, s *skillsFS, checkpoint func(string) bool) (int, string) {
	if reason := skillsRevisionDirectories(original, root, s); reason != "" {
		return 0, reason
	}
	historyDir := filepath.Join(root, filepath.FromSlash(e.History.Relative))
	if s.blocked(historyDir) {
		return 0, "protection"
	}
	hh, ok := s.directory(historyDir)
	id, valid := skillsHandleID(hh)
	if !ok || !valid || id != e.History.ID {
		return 0, "current_identity"
	}
	source, history, pending := filepath.Join(root, filepath.FromSlash(skillsRevisionTarget)), filepath.Join(root, filepath.FromSlash(skillsRevisionHistory)), filepath.Join(root, filepath.FromSlash(skillsRevisionPending))
	for _, p := range []string{source, history, pending} {
		if s.blocked(p) || ownerWindowsAlias(p) {
			return 0, "protection"
		}
	}
	changes := 0
	if skillsRevisionAbsent(history) {
		old, reason := skillsRevisionOpen(s, source, e.Original, true)
		if reason != "" {
			return 0, reason
		}
		if _, reason = skillsRevisionOpen(s, pending, e.Pending, true); reason != "" {
			return 0, reason
		}
		// The pending handle is retained; a second open is unnecessary below.
		if !checkpoint("before_archive") {
			return 0, "writer"
		}
		if !skillsRevisionRename(old, hh, filepath.Base(history)) {
			return 0, "existing_target"
		}
		changes++
		if !checkpoint("after_archive") {
			return changes, "writer"
		}
	} else {
		if _, reason := skillsRevisionOpen(s, history, e.Original, false); reason != "" {
			return 0, reason
		}
	}
	if !skillsRevisionAbsent(source) {
		if !skillsRevisionAbsent(pending) {
			return changes, "existing_target"
		}
		if _, reason := skillsRevisionOpen(s, source, e.Pending, false); reason != "" {
			return changes, reason
		}
		return changes, "" // Recovered after publication; never rename twice.
	}
	var staged *os.File
	for _, f := range s.files {
		if same(f.Name(), pending) {
			staged = f
			break
		}
	}
	if staged == nil {
		var reason string
		staged, reason = skillsRevisionOpen(s, pending, e.Pending, true)
		if reason != "" {
			return changes, reason
		}
	}
	if !checkpoint("before_publish") {
		return changes, "writer"
	}
	ph := s.dirs[strings.ToLower(filepath.Dir(source))]
	if !skillsRevisionRename(staged, ph, filepath.Base(source)) {
		return changes, "existing_target"
	}
	changes++
	if !checkpoint("after_publish") {
		return changes, "writer"
	}
	return changes, ""
}

func skillsRevisionSnapshot(original skillsCreatedEvidence, e skillsRevisionEvidence, next skillsManifest) skillsCreatedEvidence {
	out := original
	out.Files = append([]skillsCreatedFile(nil), original.Files...)
	out.Files[27] = e.Pending
	out.Files[27].Target = skillsRevisionTarget
	history := e.Original
	history.Target = skillsRevisionHistory
	out.Files = append(out.Files, history)
	out.Directories = append(append([]skillsCreatedDirectory(nil), original.Directories...), e.History)
	sort.Slice(out.Directories, func(i, j int) bool { return out.Directories[i].Relative < out.Directories[j].Relative })
	out.ManifestSHA = skillsRevisionManifestSHA
	return out
}

func skillsRevisionEvidenceBound(e skillsRevisionEvidence, r skillsRequest, original skillsCreatedEvidence, current skillsProducer) bool {
	return r.Revision != nil && r.Review != nil && e.Domain == "skills-3080-own-revision1-v1" && skillsWriterValid(e.Writer) && skillsDigest(e.RequestSHA) &&
		e.ManifestCommit == r.ManifestCommit && e.ManifestPath == r.ManifestPath && e.ManifestSHA == r.ManifestSHA && e.Review == *r.Review &&
		e.CreationSHA == skillsRevisionCreationSHA && e.CreationCall == skillsRevisionCreationCall && skillsAcceptedProducer(e.Producer, current) &&
		len(original.Files) == 52 && e.Original == original.Files[27] && e.Pending.Target == skillsRevisionPending && e.Pending.SHA == skillsRevisionNewSHA && e.Pending.Size == 10097 &&
		e.Pending.ID != e.Original.ID && e.Pending.ID.File != "" && e.History.Relative == filepath.ToSlash(filepath.Dir(skillsRevisionHistory)) && e.History.ID.File != "" &&
		(e.Recovery == nil || *e.Recovery == skillsRevisionRecoveryAudit())
}

func skillsRevisionOperation(op, path string, raw []byte, r skillsRequest, reg skillsRegistry, s *skillsFS) object {
	fail := func(reason string) object { return object{"qualified": false, "reason": reason} }
	c := r.Revision
	if c == nil || r.Writer.Task != 3080 || r.Consume != nil || r.Runtime != nil || r.Result != nil || r.Review == nil || r.Review.Task != 3931 || !skillsResultID(*r.Review) ||
		!skillsRevisionCreation(c.Creation, r) {
		return fail("request")
	}
	creator, ok := skillsReadJob(s, c.Creation.Creator.Job)
	if !ok || !skillsCreationAuthorBound(c.Creation.Creator, r.Writer, reg, creator) {
		return fail("writer")
	}
	creationRaw, ok := s.read(c.Creation.RequestPath, 1<<20)
	var creation skillsRequest
	if !ok || hashBytes(creationRaw) != c.Creation.RequestSHA || !skillsStrict(creationRaw, &creation) || creation.Writer != c.Creation.Creator ||
		creation.Revision != nil || creation.Consume != nil || creation.Runtime != nil || creation.Result != nil || creation.ManifestSHA != "fc92df3fdb1338c8de904691161eeccacf8c31256dd5e5fd7a5a4eb9fad48f85" {
		return fail("creation_evidence")
	}
	old, original, current, stage := skillsCreatedProvenance(creation, c.Creation, creator, s)
	if stage != "" {
		return object{"qualified": false, "reason": skillsCreatedFailureReason(stage), "failure_stage": stage}
	}
	nextRaw, ok := skillsManifestBytes(r)
	if !ok {
		return fail("manifest")
	}
	next, ok := skillsParseManifest(nextRaw)
	if !ok || !skillsRevisionManifestShape(old, next) || !skillsSourcesBound(next, s) {
		return fail("manifest")
	}
	reviewReg := reg
	// skillsReceive independently proves the original author, not this repair
	// actor. The accepted whole-content review is bound to the new manifest.
	review, reason := skillsReceive(*r.Review, reviewReg, s, true)
	writer, valid := skillsReadJob(s, r.Writer.Job)
	if reason != "" || !valid || !skillsReviewAccepted(review, r.ManifestSHA, str(writer["model"])) {
		return fail("review")
	}
	live := func(string) bool {
		fresh, ok := skillsRegistryRead([]int{3080, 3686})
		return ok && skillsWriterCheck(r.Writer, fresh, s, os.Getenv("CC_JOB_ID"))
	}
	if !live("") {
		return fail("writer")
	}
	result := object{"qualified": true, "domain": "skills-3080-purpose-v1", "general_access_granted": false, "original_registry_recovered": false, "request_sha256": hashBytes(raw), "manifest_sha256": r.ManifestSHA}
	if op == "qualify-skills3080-revision" {
		if c.Applied != nil || len(c.Indices) != 0 {
			return fail("request")
		}
		if c.Prepared != nil {
			e, reason := skillsReadRevisionReceipt(s, *c.Prepared, "prepare-skills3080-revision", r, reg, original, current)
			if reason != "" {
				return fail(reason)
			}
			if e.Recovery != nil {
				result["native_recovery"] = e.Recovery
			}
			result["operation"], result["apply_native_invocation"] = "qualify_own_revision1", skillsRevisionInvocation("apply-skills3080-revision", path, r.Writer.Workspace)
			return result
		}
		if reason := skillsRevisionDirectories(original, skillsCentralRoot, s); reason != "" {
			return fail(reason)
		}
		if _, reason := skillsRevisionOpen(s, filepath.Join(skillsCentralRoot, filepath.FromSlash(skillsRevisionTarget)), original.Files[27], false); reason != "" {
			return fail(reason)
		}
		for _, rel := range []string{skillsRevisionPending, filepath.ToSlash(filepath.Dir(skillsRevisionHistory))} {
			p := filepath.Join(skillsCentralRoot, filepath.FromSlash(rel))
			if s.blocked(p) {
				return fail("protection")
			}
			if !skillsRevisionAbsent(p) {
				return fail("existing_target")
			}
		}
		result["operation"], result["prepare_native_invocation"] = "qualify_own_revision1", skillsRevisionInvocation("prepare-skills3080-revision", path, r.Writer.Workspace)
		return result
	}
	var evidence skillsRevisionEvidence
	if op == "prepare-skills3080-revision" {
		if c.Prepared != nil || c.Applied != nil || len(c.Indices) != 0 {
			return fail("request")
		}
		if !skillsRevisionPrepareNative(s, r.Writer, path) {
			return fail("native_invocation")
		}
		evidence, reason = skillsRevisionPrepare(old, original, skillsCentralRoot, next, s)
		if reason != "" {
			return object{"qualified": false, "reason": reason, "created_files": 0, "partial_operation_possible": true}
		}
		if !live("") {
			return fail("writer")
		}
	} else {
		if c.Prepared == nil {
			return fail("request")
		}
		evidence, reason = skillsReadRevisionReceipt(s, *c.Prepared, "prepare-skills3080-revision", r, reg, original, current)
		if reason != "" {
			return fail(reason)
		}
		if op == "apply-skills3080-revision" {
			if c.Applied != nil || len(c.Indices) != 0 {
				return fail("request")
			}
			changes, reason := skillsRevisionApply(evidence, original, skillsCentralRoot, s, live)
			if reason != "" {
				return object{"qualified": false, "reason": reason, "created_files": 0, "partial_operation_possible": changes > 0}
			}
			result["renames_performed"] = changes
			result["already_applied"] = changes == 0
		} else if op == "consume-skills3080-revision" {
			if c.Applied == nil {
				return fail("request")
			}
			applied, reason := skillsReadRevisionReceipt(s, *c.Applied, "apply-skills3080-revision", r, reg, original, current)
			if reason != "" || applied.Pending != evidence.Pending || applied.History != evidence.History || !skillsRevisionRecoveryEqual(applied.Recovery, evidence.Recovery) {
				return fail("creation_evidence")
			}
			result, reason := skillsConsumeCurrent(next, skillsRevisionSnapshot(original, applied, next), skillsCentralRoot, s, c.Indices)
			if reason != "" {
				return fail(reason)
			}
			if !live("") {
				return fail("writer")
			}
			result["revision_native_call_id"], result["manifest_sha256"], result["revision_producer"] = c.Applied.CallID, r.ManifestSHA, applied.Producer
			if applied.Recovery != nil {
				result["native_recovery"] = applied.Recovery
			}
			return result
		} else {
			return fail("request")
		}
	}
	evidence.Domain, evidence.Writer, evidence.RequestSHA = "skills-3080-own-revision1-v1", r.Writer, hashBytes(raw)
	evidence.ManifestCommit, evidence.ManifestPath, evidence.ManifestSHA = r.ManifestCommit, r.ManifestPath, r.ManifestSHA
	evidence.CreationSHA, evidence.CreationCall, evidence.Review, evidence.Producer = c.Creation.RequestSHA, c.Creation.CallID, *r.Review, current
	result["operation"], result["revision_evidence"] = op, evidence
	return result
}
