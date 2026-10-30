package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// FileIdInfo is an observation of the held object, NOT a deletion-safe identity
// across time. Even 128-bit IDs can be reused. No USN continuity is asserted.
type skillsObjectID struct {
	Volume string `json:"volume"`
	File   string `json:"file_id_128"`
}

var skillsFileInformationEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx")

func skillsHandleID(h syscall.Handle) (skillsObjectID, bool) {
	var info struct {
		Volume uint64
		File   [16]byte
	}
	ok, _, _ := skillsFileInformationEx.Call(uintptr(h), 18 /* FileIdInfo */, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 || info.File == ([16]byte{}) {
		return skillsObjectID{}, false
	}
	volume, _ := json.Marshal(info.Volume)
	return skillsObjectID{string(volume), hex.EncodeToString(info.File[:])}, true
}

type skillsProducer struct {
	SHA      string `json:"sha256"`
	Revision string `json:"revision"`
}
type skillsCreatedFile struct {
	Target string         `json:"target"`
	SHA    string         `json:"sha256"`
	Size   int            `json:"bytes"`
	ID     skillsObjectID `json:"object"`
}
type skillsCreatedDirectory struct {
	Relative string         `json:"relative"`
	ID       skillsObjectID `json:"object"`
}
type skillsCreatedEvidence struct {
	Domain         string                   `json:"domain"`
	Writer         skillsWriter             `json:"writer"`
	RequestSHA     string                   `json:"request_sha256"`
	ManifestCommit string                   `json:"manifest_commit"`
	ManifestPath   string                   `json:"manifest_path"`
	ManifestSHA    string                   `json:"manifest_sha256"`
	Review         skillsResultRequest      `json:"review"`
	Producer       skillsProducer           `json:"producer"`
	Directories    []skillsCreatedDirectory `json:"directories"`
	Files          []skillsCreatedFile      `json:"files"`
	ManifestAuthor *skillsWriter            `json:"manifest_author,omitempty"`
	DecisionSHA    string                   `json:"decision_identity_sha256,omitempty"`
}
type skillsConsumeRequest struct {
	Creator     skillsWriter `json:"creator"`
	RequestPath string       `json:"creation_request"`
	RequestSHA  string       `json:"creation_request_sha256"`
	CallID      string       `json:"native_call_id"`
	Indices     []int        `json:"indices"`
}

// Issuance and subsequent use are deliberately restricted to the exact same
// installed, merged runtime. A software update needs an explicit reviewed
// migration; it cannot accept an arbitrary producer hash supplied in a receipt.
func skillsProducerObservation(s *skillsFS) (skillsProducer, bool) {
	p, ok := skillsInstalledProducer(s)
	return p, ok && skillsMerged(p.Revision)
}

// Current trusted installed code may observe itself without granting network
// access to a readonly caller. Historical data provenance still needs its
// accepted issuing producer and the original canonical CreateNew native call.
func skillsInstalledProducer(s *skillsFS) (skillsProducer, bool) {
	path, e := os.Executable()
	if e != nil || !same(path, skillsRuntimeTarget) {
		return skillsProducer{}, false
	}
	b, ok := s.read(skillsRuntimeTarget, 64<<20)
	if !ok {
		return skillsProducer{}, false
	}
	rev, ok := skillsBuildRevision(b)
	return skillsProducer{hashBytes(b), rev}, ok
}

func skillsManifestDirectories(m skillsManifest) []string {
	seen := map[string]bool{".": true}
	for _, f := range m.Files {
		for p := filepath.ToSlash(filepath.Dir(filepath.FromSlash(f.Target))); p != "."; p = filepath.ToSlash(filepath.Dir(filepath.FromSlash(p))) {
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Called only after the full real CreateNew succeeded, with every created
// handle and physical directory still held. There is no issue-from-path API.
func skillsIssueCreated(r skillsRequest, requestSHA string, m skillsManifest, root string, s *skillsFS, producer skillsProducer) (skillsCreatedEvidence, bool) {
	e := skillsCreatedEvidence{Domain: "skills-3080-created-v2", Writer: r.Writer, RequestSHA: requestSHA,
		ManifestCommit: r.ManifestCommit, ManifestPath: r.ManifestPath, ManifestSHA: r.ManifestSHA, Producer: producer}
	if r.Review == nil || len(m.Files) == 0 || len(m.Files) > 128 || len(s.created) != len(m.Files) {
		return e, false
	}
	if m.publicV2 {
		if m.Writer != r.Writer || !skillsWriterValid(m.proposalAuthor) || m.Decision != skillsDecisionSHA {
			return e, false
		}
		a := m.proposalAuthor
		e.ManifestAuthor = &a
		e.DecisionSHA = skillsDecisionSHA
	}
	e.Review = *r.Review
	for _, relative := range skillsManifestDirectories(m) {
		path := filepath.Join(root, filepath.FromSlash(relative))
		h, held := s.dirs[strings.ToLower(filepath.Clean(path))]
		id, ok := skillsHandleID(h)
		if !held || !ok || s.blocked(path) {
			return e, false
		}
		e.Directories = append(e.Directories, skillsCreatedDirectory{relative, id})
	}
	for _, output := range m.Files {
		f, held := s.created[output.Target]
		if !held || !same(f.Name(), filepath.Join(root, filepath.FromSlash(output.Target))) {
			return e, false
		}
		h := syscall.Handle(f.Fd())
		var before, after syscall.ByHandleFileInformation
		id, ok := skillsHandleID(h)
		if !ok || syscall.GetFileInformationByHandle(h, &before) != nil || before.NumberOfLinks != 1 || before.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
			return e, false
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return e, false
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(len(output.body))+1))
		if err != nil || !bytes.Equal(b, output.body) || hashBytes(b) != output.SHA || syscall.GetFileInformationByHandle(h, &after) != nil {
			return e, false
		}
		before.LastAccessTime = after.LastAccessTime
		if before != after {
			return e, false
		}
		e.Files = append(e.Files, skillsCreatedFile{output.Target, output.SHA, len(b), id})
	}
	return e, true
}

func skillsCreatedBound(e skillsCreatedEvidence, creation skillsRequest, sha string, m skillsManifest, producer skillsProducer) bool {
	if m.publicV2 {
		if e.ManifestAuthor == nil || *e.ManifestAuthor != m.proposalAuthor || e.DecisionSHA != skillsDecisionSHA {
			return false
		}
	} else if e.ManifestAuthor != nil || e.DecisionSHA != "" {
		return false
	}
	if e.Domain != "skills-3080-created-v2" || creation.Review == nil || e.Writer != creation.Writer || e.Writer != m.Writer ||
		e.RequestSHA != sha || e.ManifestCommit != creation.ManifestCommit || e.ManifestPath != creation.ManifestPath || e.ManifestSHA != creation.ManifestSHA ||
		e.Review != *creation.Review || e.Producer != producer || !skillsDigest(producer.SHA) || !civ6Commit.MatchString(producer.Revision) || len(e.Files) != len(m.Files) || len(m.Files) == 0 || len(m.Files) > 128 {
		return false
	}
	dirs := skillsManifestDirectories(m)
	if len(dirs) != len(e.Directories) {
		return false
	}
	validID := func(id skillsObjectID) bool {
		b, err := hex.DecodeString(id.File)
		var volume uint64
		return err == nil && len(b) == 16 && id.File != strings.Repeat("0", 32) && json.Unmarshal([]byte(id.Volume), &volume) == nil
	}
	for i, d := range e.Directories {
		if d.Relative != dirs[i] || !validID(d.ID) {
			return false
		}
	}
	for i, f := range e.Files {
		out := m.Files[i]
		if f.Target != out.Target || f.SHA != out.SHA || f.Size != len(out.body) || !validID(f.ID) {
			return false
		}
	}
	return true
}

// All requested bytes are validated before any are returned. The returned
// strings are made from the actual current read buffers, never manifest copies.
func skillsConsumeCurrent(m skillsManifest, e skillsCreatedEvidence, root string, s *skillsFS, indices []int) (object, string) {
	if len(indices) < 1 || len(indices) > 32 {
		return nil, "request"
	}
	seen := map[int]bool{}
	total := 0
	for _, i := range indices {
		if i < 0 || i >= len(m.Files) || seen[i] {
			return nil, "request"
		}
		seen[i] = true
		total += len(m.Files[i].body)
	}
	if total > 64<<10 {
		return nil, "request"
	}
	// Check protection for the entire proven namespace before opening leaves.
	for _, d := range e.Directories {
		path := filepath.Join(root, filepath.FromSlash(d.Relative))
		if s.blocked(path) {
			return nil, "protection"
		}
	}
	for _, i := range indices {
		if s.blocked(filepath.Join(root, filepath.FromSlash(m.Files[i].Target))) {
			return nil, "protection"
		}
	}
	for _, d := range e.Directories {
		h, ok := s.directory(filepath.Join(root, filepath.FromSlash(d.Relative)))
		id, valid := skillsHandleID(h)
		if !ok || !valid || id != d.ID {
			return nil, "current_identity"
		}
	}
	files := []object{}
	for _, i := range indices {
		out := m.Files[i]
		path := filepath.Join(root, filepath.FromSlash(out.Target))
		// Observe the ID before reading a body: a substituted old/unknown
		// object must not be read just to discover that it is a different ID.
		f, ok := s.open(path, int64(len(out.body)))
		if !ok {
			return nil, "current_identity"
		}
		h := syscall.Handle(f.Fd())
		id, valid := skillsHandleID(h)
		if !valid || id != e.Files[i].ID {
			return nil, "current_identity"
		}
		var before, after syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h, &before) != nil || before.FileSizeHigh != 0 || int(before.FileSizeLow) != len(out.body) {
			return nil, "current_bytes"
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(len(out.body))+1))
		if err != nil || syscall.GetFileInformationByHandle(h, &after) != nil {
			return nil, "current_bytes"
		}
		before.LastAccessTime = after.LastAccessTime
		if before != after || s.blocked(path) {
			return nil, "current_identity"
		}
		if !bytes.Equal(b, out.body) || hashBytes(b) != out.SHA {
			return nil, "current_bytes"
		}
		files = append(files, object{"index": i, "sha256": hashBytes(b), "bytes": len(b), "content_utf8": string(b), "current_object": id})
	}
	return object{"qualified": true, "domain": "skills-3080-consumed-v1", "operation": "consume_current_public_bytes",
		"files": files, "original_writer": e.Writer, "producer": e.Producer, "same_read_buffer_consumed": true,
		"creation_id_matches_current": true, "historical_physical_continuity_proven": false, "current_os_writer": "unproved",
		"general_access_granted": false, "original_registry_recovered": false}, ""
}

func skillsConsumeCreatedCurrent(m skillsManifest, creation skillsRequest, requestSHA string, evidence skillsCreatedEvidence, root string, s *skillsFS, indices []int, current skillsProducer) (object, string) {
	// The native issuer and the current consuming runtime are distinct. Keep
	// their exact accepted identities rather than rewriting the old issuance.
	// This is reached only after skillsReadCreationNative, never from a caller
	// supplied receipt; the complete manifest/notice/review checks stay above it.
	if !skillsCreationHeaderBound(evidence, creation, requestSHA, current) || !skillsCreatedBound(evidence, creation, requestSHA, m, evidence.Producer) {
		return nil, "creation_evidence"
	}
	return skillsConsumeAcceptedCurrent(m, evidence, root, s, indices, current)
}

func skillsConsumeOperation(r skillsRequest, reg skillsRegistry, s *skillsFS) object {
	fail := func(reason string, stages ...string) object {
		out := object{"qualified": false, "reason": reason}
		if len(stages) == 1 && stages[0] != "" {
			out["failure_stage"] = stages[0]
		}
		return out
	}
	c := r.Consume
	if c == nil || r.Writer.Task != 3080 || r.Runtime != nil || r.Result != nil || r.Review == nil || !skillsResultID(*r.Review) ||
		!skillsWriterValid(c.Creator) || !skillsDigest(c.RequestSHA) || !filepath.IsAbs(c.RequestPath) || !within(c.Creator.Workspace, c.RequestPath) ||
		strings.ContainsAny(c.RequestPath, "'\r\n") {
		return fail("request")
	}
	creator, ok := skillsReadJob(s, c.Creator.Job)
	if !ok || !skillsCreationAuthorBound(c.Creator, r.Writer, reg, creator) {
		return fail("writer")
	}
	creationRaw, ok := s.read(c.RequestPath, 1<<20)
	var creation skillsRequest
	if !ok || hashBytes(creationRaw) != c.RequestSHA || !skillsStrict(creationRaw, &creation) || creation.Domain != "skills-3080-operation-v1" ||
		creation.Writer != c.Creator || creation.Consume != nil || creation.Result != nil || creation.Runtime != nil || creation.Review == nil ||
		*creation.Review != *r.Review || creation.ManifestCommit != r.ManifestCommit || creation.ManifestPath != r.ManifestPath || creation.ManifestSHA != r.ManifestSHA {
		return fail("creation_evidence")
	}
	m, evidence, producer, stage := skillsCreatedProvenance(creation, *c, creator, s)
	if stage != "" {
		return fail(skillsCreatedFailureReason(stage), stage)
	}
	result, reason := skillsConsumeCreatedCurrent(m, creation, c.RequestSHA, evidence, skillsCentralRoot, s, c.Indices, producer)
	if reason != "" {
		return fail(reason, "current_snapshot")
	}
	// Owner stop/current generation can change while provenance is checked.
	fresh, ok := skillsRegistryRead([]int{3080, 3686})
	if !ok || !skillsWriterCheck(r.Writer, fresh, s, os.Getenv("CC_JOB_ID")) {
		return fail("writer", "writer_recheck")
	}
	result["manifest_sha256"] = r.ManifestSHA
	result["native_call_id"] = c.CallID
	result["runtime_producer"], result["original_producer_revalidated"] = producer, true
	return result
}
