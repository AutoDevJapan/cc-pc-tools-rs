//go:build windows

package main

// This bridge issues a finite plan to the existing Rust cleaner. It never
// removes files. Producer events are independent of mutable receipt files.
import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const retirementStore = "D:/CC/Tools/safe-delete/receipts-go"
const retirementTool = "D:/CC/Tools/safe-delete/safe-delete.exe"

type retiredIdentity struct {
	Volume     uint32 `json:"volume"`
	ID         uint64 `json:"file_id"`
	Birth      uint64 `json:"birth"`
	Written    uint64 `json:"written"`
	Size       uint64 `json:"size"`
	Attributes uint32 `json:"attributes"`
	Links      uint32 `json:"links"`
}
type retiredAncestor struct {
	Path   string `json:"path"`
	Volume uint32 `json:"volume"`
	ID     uint64 `json:"file_id"`
	Birth  uint64 `json:"birth"`
}
type retiredEntry struct {
	Relative string `json:"relative"`
	retiredIdentity
}
type retirementJob struct {
	ID       string `json:"id"`
	Todo     uint64 `json:"todo_id"`
	CWD      string `json:"cwd"`
	Status   string `json:"status"`
	PID      uint32 `json:"pid"`
	CodexPID uint32 `json:"codex_pid"`
}
type retirementTodo struct {
	ID          uint64   `json:"id"`
	Status      string   `json:"status"`
	Jobs        []string `json:"jobs"`
	Note        string   `json:"note"`
	Spec        string   `json:"spec"`
	WaitOn      []uint64 `json:"wait_on"`
	NoteHistory []struct {
		Note string `json:"note"`
	} `json:"note_history"`
}
type retiredRelease struct {
	Schema           int            `json:"schema"`
	ID               string         `json:"id"`
	CreateJob        string         `json:"create_job"`
	ReleaseJob       string         `json:"release_job"`
	MoveJob          string         `json:"move_job,omitempty"`
	Todo             uint64         `json:"todo_id"`
	Original         identityRecord `json:"original"`
	NoteSHA          string         `json:"note_sha256"`
	SpecSHA          string         `json:"spec_sha256"`
	SourceHead       string         `json:"source_head"`
	SourceRoot       string         `json:"source_root"`
	ResourceFinished bool           `json:"resource_finished"`
	Unneeded         bool           `json:"unneeded"`
	Retention        bool           `json:"retention"`
	Dependencies     bool           `json:"dependencies"`
	Reproducible     bool           `json:"reproducible"`
	Entries          []retiredEntry `json:"entries"`
}
type retiredCandidate struct {
	Action    string         `json:"action"`
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	Bytes     uint64         `json:"bytes"`
	Entries   []retiredEntry `json:"entries"`
	record    receipt
	release   retiredRelease
	ancestors []retiredAncestor
}
type retirementRuntime struct {
	Jobs, Todo, Tool string
	Guard            func(string) error
	Actor            func() (retirementJob, error)
	SavedSource      func(string) (string, error)
	Until            time.Time
}

func (c config) retirementRuntime() retirementRuntime {
	if c.Retirement != nil {
		return *c.Retirement
	}
	return retirementRuntime{Jobs: "D:/CC/Tools/codex-jobs/jobs", Todo: "D:/CC/Tools/todo/todo.json", Tool: retirementTool, Guard: retirementGuard, SavedSource: retirementSavedSource, Until: time.Now().Add(90 * time.Second)}
}
func retiredSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func retiredMeta(i syscall.ByHandleFileInformation) retiredIdentity {
	return retiredIdentity{Volume: i.VolumeSerialNumber, ID: fileID(i), Birth: uint64(i.CreationTime.HighDateTime)<<32 | uint64(i.CreationTime.LowDateTime), Written: uint64(i.LastWriteTime.HighDateTime)<<32 | uint64(i.LastWriteTime.LowDateTime), Size: uint64(i.FileSizeHigh)<<32 | uint64(i.FileSizeLow), Attributes: i.FileAttributes, Links: i.NumberOfLinks}
}
func retiredAncestors(path string) ([]retiredAncestor, error) {
	if !filepath.IsAbs(path) {
		return nil, fail("INVALID_PATH")
	}
	p, e := checked(path)
	if e != nil {
		return nil, e
	}
	var result []retiredAncestor
	for {
		i, e := fileIdentity(p)
		if e != nil {
			return nil, e
		}
		if i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return nil, fail("ANCESTOR_NOT_DIRECTORY")
		}
		m := retiredMeta(i)
		result = append(result, retiredAncestor{p, m.Volume, m.ID, m.Birth})
		parent := filepath.Dir(p)
		if same(p, parent) {
			break
		}
		p = parent
	}
	return result, nil
}

// Reject duplicate keys and trailing JSON before typed decoding. A checksum of
// a caller-edited JSON object is not an original receipt.
func retiredUniqueJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return fail("INVALID_PROOF_JSON")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return fail("INVALID_PROOF_JSON")
				}
				seen[key] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case '[':
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		default:
			return fail("INVALID_PROOF_JSON")
		}
	}
	if e := value(0); e != nil {
		return fail("INVALID_PROOF_JSON")
	}
	if _, e := d.Token(); e != io.EOF {
		return fail("INVALID_PROOF_JSON")
	}
	return nil
}
func retiredRead(path string, limit int64, target any, strict bool) error {
	if _, e := checked(path); e != nil {
		return e
	}
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return fail("PROOF_READ_FAILED")
	}
	// Deny writes/rename to the actual opened record, then decode those bytes.
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return fail("PROOF_READ_FAILED")
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &i) != nil || i.NumberOfLinks != 1 || i.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 || uint64(i.FileSizeHigh)<<32|uint64(i.FileSizeLow) > uint64(limit) {
		return fail("INVALID_PROOF_FILE")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return fail("PROOF_READ_FAILED")
	}
	if e = retiredUniqueJSON(b); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if strict {
		d.DisallowUnknownFields()
	}
	if d.Decode(target) != nil {
		return fail("INVALID_PROOF_JSON")
	}
	return nil
}

var retirementJobID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (rt retirementRuntime) job(id string) (retirementJob, error) {
	var j retirementJob
	if !retirementJobID.MatchString(id) {
		return j, fail("PRODUCER_JOB_UNVERIFIED")
	}
	if e := retiredRead(filepath.Join(rt.Jobs, id, "job.json"), 1<<20, &j, false); e != nil {
		return j, e
	}
	if j.ID != id || j.Todo == 0 || !filepath.IsAbs(j.CWD) {
		return j, fail("PRODUCER_JOB_UNVERIFIED")
	}
	return j, nil
}
func (rt retirementRuntime) todo(id uint64) (retirementTodo, error) {
	var db struct {
		Items []json.RawMessage `json:"items"`
	}
	if e := retiredRead(rt.Todo, 64<<20, &db, false); e != nil {
		return retirementTodo{}, e
	}
	var found *retirementTodo
	for _, raw := range db.Items {
		var key struct {
			ID uint64 `json:"id"`
		}
		if json.Unmarshal(raw, &key) != nil {
			continue
		}
		if key.ID == id {
			if found != nil {
				return retirementTodo{}, fail("PRODUCER_TODO_UNVERIFIED")
			}
			var selected retirementTodo
			if json.Unmarshal(raw, &selected) != nil {
				return retirementTodo{}, fail("PRODUCER_TODO_UNVERIFIED")
			}
			found = &selected
		}
	}
	if found == nil {
		return retirementTodo{}, fail("PRODUCER_TODO_UNVERIFIED")
	}
	return *found, nil
}
func retiredHas(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (rt retirementRuntime) actor() (retirementJob, error) {
	if rt.Actor != nil {
		return rt.Actor()
	}
	j, e := rt.job(os.Getenv("CC_JOB_ID"))
	if e != nil {
		return j, e
	}
	if j.Status != "running" || !retirementProcessAncestor(j.CodexPID) {
		return j, fail("PRODUCER_ACTOR_UNVERIFIED")
	}
	t, e := rt.todo(j.Todo)
	if e != nil || !retiredHas(t.Jobs, j.ID) {
		return j, fail("PRODUCER_ACTOR_UNVERIFIED")
	}
	return j, nil
}
func retirementProcessAncestor(want uint32) bool {
	if want == 0 {
		return false
	}
	h, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	parents := map[uint32]uint32{}
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafeProcessEntrySize())
	for e = syscall.Process32First(h, &p); e == nil; e = syscall.Process32Next(h, &p) {
		parents[p.ProcessID] = p.ParentProcessID
	}
	pid := uint32(os.Getpid())
	for n := 0; n < 64 && pid != 0; n++ {
		if pid == want {
			return true
		}
		next := parents[pid]
		if next == pid {
			return false
		}
		pid = next
	}
	return false
}
func (c config) captureProducer(r *receipt) {
	if c.Retirement == nil && !same(c.Store, retirementStore) {
		return
	}
	rt := c.retirementRuntime()
	j, e := rt.actor()
	if e != nil {
		return
	}
	a, e := retiredAncestors(r.Root)
	if e != nil {
		return
	}
	r.Version = 3
	if info, e := fileIdentity(r.Path); e == nil {
		r.Birth = retiredMeta(info).Birth
	} else {
		return
	}
	r.ProducerJob = j.ID
	r.ProducerTodo = j.Todo
	r.Ancestors = a
}
func retirementGuard(path string) error {
	b, e := runHidden(10*time.Second, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	if e != nil || len(b) > 4096 {
		return fail("GUARD_UNAVAILABLE")
	}
	var r struct {
		OK, Allowed, Client, Unverified *bool
		Status                          string
	}
	var v map[string]json.RawMessage
	if retiredUniqueJSON(b) != nil || json.Unmarshal(b, &v) != nil {
		return fail("GUARD_UNAVAILABLE")
	}
	json.Unmarshal(v["ok"], &r.OK)
	json.Unmarshal(v["access_allowed"], &r.Allowed)
	json.Unmarshal(v["client_resource"], &r.Client)
	json.Unmarshal(v["classification_unverified"], &r.Unverified)
	json.Unmarshal(v["protection_status"], &r.Status)
	if r.OK == nil || !*r.OK || r.Allowed == nil || !*r.Allowed || r.Client == nil || *r.Client || r.Unverified == nil || *r.Unverified || r.Status != "clear" {
		return fail("GUARD_RETAINED")
	}
	return nil
}
func retirementSavedSource(root string) (string, error) {
	if _, e := checked(root); e != nil {
		return "", e
	}
	status, e := runHidden(10*time.Second, "git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all")
	if e != nil || len(bytes.TrimSpace(status)) != 0 {
		return "", fail("SOURCE_NOT_SAVED")
	}
	head, e := runHidden(10*time.Second, "git", "-C", root, "rev-parse", "HEAD")
	if e != nil {
		return "", fail("SOURCE_NOT_SAVED")
	}
	if _, e = runHidden(10*time.Second, "git", "-C", root, "merge-base", "--is-ancestor", "HEAD", "@{upstream}"); e != nil {
		return "", fail("SOURCE_NOT_PUSHED")
	}
	return strings.TrimSpace(string(head)), nil
}

// Only a single direct invocation is evidence. Reject echo, compound scripts,
// async sessions, failed executions and output embedded inside quoted JSON.
func retiredDirect(command, tool, action string) bool {
	command = strings.ReplaceAll(command, `\"'`, "'")
	if p := strings.Index(command, " -Command '"); p >= 0 && strings.HasSuffix(command, "'") {
		command = command[p+11 : len(command)-1]
	}
	command = strings.TrimSpace(command)
	if strings.ContainsAny(command, ";`\r\n|<>$(){}") || strings.Contains(command, "&&") {
		return false
	}
	command = strings.TrimSpace(strings.TrimPrefix(command, "& "))
	var parts []string
	for len(command) > 0 {
		command = strings.TrimLeft(command, " \t")
		if command == "" {
			break
		}
		q := byte(0)
		if command[0] == '\'' || command[0] == '"' {
			q = command[0]
			command = command[1:]
		}
		n := 0
		for n < len(command) && ((q != 0 && command[n] != q) || (q == 0 && command[n] != ' ' && command[n] != '\t')) {
			n++
		}
		if q != 0 && n == len(command) {
			return false
		}
		parts = append(parts, command[:n])
		if q != 0 {
			n++
		}
		command = command[n:]
	}
	if len(parts) < 2 || !same(parts[0], tool) || parts[1] != action {
		return false
	}
	allowed := map[string]bool{"--id": true, "--root": true, "--project": true, "--content": true, "--reason": true, "--producer-job": true, "--move-job": true}
	for i := 2; i < len(parts); i++ {
		if parts[i] == "--disposable" {
			continue
		}
		if !allowed[parts[i]] || i+1 == len(parts) {
			return false
		}
		i++
	}
	return true
}
func (rt retirementRuntime) event(job, action string, match func([]byte) bool) error {
	if !retirementJobID.MatchString(job) {
		return fail("ORIGINAL_EVENT_UNVERIFIED")
	}
	path := filepath.Join(rt.Jobs, job, "events.jsonl")
	if _, e := checked(path); e != nil {
		return e
	}
	i, e := fileIdentity(path)
	if e != nil || i.NumberOfLinks != 1 || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || uint64(i.FileSizeHigh)<<32|uint64(i.FileSizeLow) > 64<<20 {
		return fail("ORIGINAL_EVENT_UNVERIFIED")
	}
	f, e := os.Open(path)
	if e != nil {
		return fail("ORIGINAL_EVENT_UNVERIFIED")
	}
	defer f.Close()
	s := bufio.NewScanner(io.LimitReader(f, 64<<20+1))
	s.Buffer(make([]byte, 64<<10), 4<<20)
	count := 0
	for s.Scan() {
		if time.Now().After(rt.Until) {
			return fail("RETIREMENT_DEADLINE")
		}
		var v struct {
			Type string `json:"type"`
			Item struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Command string `json:"command"`
				Output  string `json:"aggregated_output"`
				Exit    *int   `json:"exit_code"`
			} `json:"item"`
		}
		if json.Unmarshal(s.Bytes(), &v) != nil {
			return fail("ORIGINAL_EVENT_UNVERIFIED")
		}
		if v.Type != "item.completed" || v.Item.Type != "command_execution" || v.Item.Status != "completed" || v.Item.Exit == nil || *v.Item.Exit != 0 || !retiredDirect(v.Item.Command, rt.Tool, action) {
			continue
		}
		out := []byte(strings.TrimSpace(v.Item.Output))
		if retiredUniqueJSON(out) == nil && match(out) {
			count++
		}
	}
	if s.Err() != nil || count != 1 {
		return fail("ORIGINAL_EVENT_UNVERIFIED")
	}
	return nil
}
func (c config) retiredReceipt(id string) (receipt, error) {
	var r receipt
	p, e := c.recordPath(id)
	if e != nil {
		return r, e
	}
	if e = retiredRead(p, 8192, &r, true); e != nil {
		return r, e
	}
	if (r.Version != 2 && r.Version != 3) || r.ID != id || digest(r) != r.Integrity || !label(r.Project) || !label(r.Content) || !filepath.IsAbs(r.Root) || !same(r.Path, filepath.Join(r.Root, "codex-owned-"+id)) {
		return r, fail("RECEIPT_INTEGRITY_MISMATCH")
	}
	return r, nil
}
func (c config) original(r receipt, job string) (retirementJob, error) {
	rt := c.retirementRuntime()
	j, e := rt.job(job)
	if e != nil {
		return j, fail("ORIGINAL_CREATE_UNVERIFIED")
	}
	t, e := rt.todo(j.Todo)
	if e != nil || !retiredHas(t.Jobs, job) {
		return j, fail("ORIGINAL_CREATE_UNVERIFIED")
	}
	if r.Version == 3 && (r.ProducerJob != job || r.ProducerTodo != j.Todo || len(r.Ancestors) == 0) {
		return j, fail("ORIGINAL_CREATE_UNVERIFIED")
	}
	e = rt.event(job, "create", func(b []byte) bool {
		var v receipt
		return json.Unmarshal(b, &v) == nil && v.State == "owned" && v.Destination == "" && v.MoveJob == "" && reflect.DeepEqual(v.identityRecord, r.identityRecord) && v.Integrity == r.Integrity
	})
	if e != nil {
		return j, fail("ORIGINAL_CREATE_UNVERIFIED")
	}
	return j, nil
}
func retiredSourceName(name string) bool {
	n := strings.ToLower(name)
	if protected(n) {
		return true
	}
	switch n {
	case "src", "client-projects.txt", "cargo.toml", "go.mod", "go.sum", "package.json", "pyproject.toml", "requirements.txt":
		return true
	}
	for _, s := range []string{"2467", "recovery", "復旧", "原設定", "原記録", "ledger", "protection", "verification", "test-result", "test_result", "junit", "coverage", "sarif", ".private."} {
		if strings.Contains(n, s) {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(n)) {
	case ".rs", ".go", ".c", ".cpp", ".h", ".py", ".ts", ".tsx", ".jsx", ".log", ".jsonl", ".sarif", ".trx", ".tap":
		return true
	}
	return false
}
func (c config) retiredSnapshot(root string) ([]retiredEntry, uint64, error) {
	rt := c.retirementRuntime()
	if _, e := checked(root); e != nil {
		return nil, 0, e
	}
	base, e := fileIdentity(root)
	if e != nil {
		return nil, 0, e
	}
	pending := []string{root}
	var result []retiredEntry
	var total uint64
	for len(pending) > 0 {
		if time.Now().After(rt.Until) {
			return result, total, fail("RETIREMENT_DEADLINE")
		}
		if len(result) > c.MaxEntries {
			return result, total, fail("ENTRY_LIMIT_EXCEEDED")
		}
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		rel, e := filepath.Rel(root, p)
		if e != nil || len(strings.Split(rel, string(os.PathSeparator))) > 64 {
			return result, total, fail("ENTRY_LIMIT_EXCEEDED")
		}
		if rel != "." && retiredSourceName(filepath.Base(p)) {
			return result, total, fail("SOURCE_OR_RECOVERY_RETAINED")
		}
		i, e := fileIdentity(p)
		if e != nil {
			return result, total, e
		}
		m := retiredMeta(i)
		if m.Volume != base.VolumeSerialNumber || m.Links != 1 {
			return result, total, fail("HARD_LINK_OR_VOLUME_CHANGED")
		}
		if m.Attributes&3 != 0 {
			return result, total, fail("PROTECTED_ATTRIBUTES_RETAINED")
		}
		result = append(result, retiredEntry{filepath.ToSlash(rel), m})
		if m.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			if m.Size > uint64(^uint64(0)>>1)-total {
				return result, total, fail("BYTE_LIMIT_EXCEEDED")
			}
			total += m.Size
			continue
		}
		d, e := os.Open(p)
		if e != nil {
			return result, total, fail("CONTENT_METADATA_FAILED")
		}
		for {
			xs, re := d.ReadDir(64)
			for _, x := range xs {
				pending = append(pending, filepath.Join(p, x.Name()))
				if len(pending)+len(result) > c.MaxEntries+1 {
					d.Close()
					return result, total, fail("ENTRY_LIMIT_EXCEEDED")
				}
			}
			if re != nil {
				d.Close()
				if re != io.EOF {
					return result, total, fail("CONTENT_METADATA_FAILED")
				}
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Relative < result[j].Relative })
	return result, total, nil
}
func retiredAbsent(path string) bool { _, e := os.Lstat(path); return os.IsNotExist(e) }
func (rt retirementRuntime) liveState(r receipt, rel retiredRelease) error {
	t, e := rt.todo(rel.Todo)
	if e != nil {
		return e
	}
	noteMatches := retiredSHA([]byte(t.Note)) == rel.NoteSHA
	if !noteMatches && t.Status == "done" && t.Note == "割り当てたジョブが完了しました。" && len(t.NoteHistory) > 0 {
		noteMatches = retiredSHA([]byte(t.NoteHistory[len(t.NoteHistory)-1].Note)) == rel.NoteSHA
	}
	if t.Status == "blocked" || t.Status == "block" || t.Status == "owner_wait" || len(t.WaitOn) > 0 || !noteMatches {
		return fail("RETENTION_OR_TODO_CHANGED")
	}
	// Specifications are plain text; bounded reading is separate from JSON.
	b, e := retiredText(t.Spec, 8<<20)
	if e != nil || retiredSHA(b) != rel.SpecSHA {
		return fail("RETENTION_OR_SPEC_CHANGED")
	}
	if !retiredHas(t.Jobs, rel.CreateJob) || !retiredHas(t.Jobs, rel.ReleaseJob) {
		return fail("PRODUCER_TODO_UNVERIFIED")
	}
	if len(t.Jobs) > 1000 {
		return fail("JOB_HISTORY_LIMIT")
	}
	for _, id := range t.Jobs {
		j, e := rt.job(id)
		if e != nil {
			return e
		}
		if j.Todo != rel.Todo {
			return fail("PRODUCER_TODO_UNVERIFIED")
		}
		switch j.Status {
		case "succeeded", "failed", "cancelled", "canceled", "done":
			continue
		case "running", "queued", "starting", "cancelling", "canceling":
			if retiredInside(r.Path, j.CWD) || r.Destination != "" && retiredInside(r.Destination, j.CWD) {
				return fail("RESOURCE_STILL_ACTIVE")
			}
		default:
			return fail("JOB_STATE_UNVERIFIED")
		}
	}
	head, e := rt.SavedSource(rel.SourceRoot)
	if e != nil {
		return e
	}
	if head != rel.SourceHead {
		return fail("SOURCE_CHANGED")
	}
	return nil
}
func retiredInside(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
func retiredText(path string, limit int64) ([]byte, error) {
	if _, e := checked(path); e != nil {
		return nil, e
	}
	i, e := fileIdentity(path)
	if e != nil || i.NumberOfLinks != 1 || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, fail("SOURCE_PROOF_UNVERIFIED")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	after, ae := fileIdentity(path)
	if e != nil || ae != nil || int64(len(b)) > limit || retiredMeta(i) != retiredMeta(after) {
		return nil, fail("SOURCE_PROOF_CHANGED")
	}
	return b, nil
}
func (c config) retirementPath(id, suffix string) (string, error) {
	if _, e := c.recordPath(id); e != nil {
		return "", e
	}
	return checked(filepath.Join(filepath.Dir(c.Store), "retirements-go", id+suffix))
}
func retiredWriteNew(path string, v any) error {
	if _, e := checked(path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return fail("RETIREMENT_WRITE_FAILED")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fail("RETIREMENT_RECORD_EXISTS")
	}
	e = json.NewEncoder(f).Encode(v)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return fail("RETIREMENT_WRITE_FAILED")
	}
	return nil
}
func (c config) retirementLock(id string) (func(), error) {
	p, e := c.retirementPath(id, ".lock")
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return nil, fail("RETIREMENT_LOCK_FAILED")
	}
	u, _ := syscall.UTF16PtrFromString(p)
	h, e := syscall.CreateFile(u, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, fail("RETIREMENT_BUSY")
	}
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &i) != nil || i.NumberOfLinks != 1 || i.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		syscall.CloseHandle(h)
		return nil, fail("RETIREMENT_LOCK_FAILED")
	}
	return func() { syscall.CloseHandle(h) }, nil
}
func (c config) indexRetirement(id, dest string) error {
	if _, e := c.recordPath(id); e != nil {
		return e
	}
	key := retiredSHA([]byte(strings.ToLower(filepath.Clean(dest))))
	p := filepath.Join(filepath.Dir(c.Store), "retirements-go", "dest-"+key+".json")
	v := struct {
		ID string `json:"id"`
	}{id}
	if !retiredAbsent(p) {
		var old struct {
			ID string `json:"id"`
		}
		if e := retiredRead(p, 1024, &old, true); e != nil || old != v {
			return fail("RETIREMENT_INDEX_CONFLICT")
		}
		return nil
	}
	return retiredWriteNew(p, v)
}
func (c config) retirementID(dest string) (string, error) {
	if !filepath.IsAbs(dest) {
		return "", fail("INVALID_PATH")
	}
	key := retiredSHA([]byte(strings.ToLower(filepath.Clean(dest))))
	p := filepath.Join(filepath.Dir(c.Store), "retirements-go", "dest-"+key+".json")
	if retiredAbsent(p) {
		return "", fail("NOT_REGISTERED")
	}
	var v struct {
		ID string `json:"id"`
	}
	if e := retiredRead(p, 1024, &v, true); e != nil {
		return "", e
	}
	if _, e := c.recordPath(v.ID); e != nil {
		return "", e
	}
	return v.ID, nil
}
func (c config) releaseRetirement(id, producerJob, moveJob string, disposable bool) (retiredRelease, error) {
	var rel retiredRelease
	unlock, e := c.retirementLock(id)
	if e != nil {
		return rel, e
	}
	defer unlock()
	r, e := c.retiredReceipt(id)
	if e != nil {
		return rel, e
	}
	if r.State != "owned" && r.State != "quarantined" {
		return rel, fail("RESOURCE_NOT_RELEASABLE")
	}
	if producerJob == "" {
		producerJob = r.ProducerJob
	}
	orig, e := c.original(r, producerJob)
	if e != nil {
		return rel, e
	}
	rt := c.retirementRuntime()
	actor, e := rt.actor()
	if e != nil || actor.Todo != orig.Todo {
		return rel, fail("ORIGINAL_PRODUCER_REQUIRED")
	}
	if !disposable {
		return rel, fail("RESOURCE_DISPOSITION_REQUIRED")
	}
	for _, p := range []string{r.Root, r.Path} {
		if e = rt.Guard(p); e != nil {
			return rel, e
		}
	}
	a, e := retiredAncestors(r.Root)
	if e != nil {
		return rel, e
	}
	if r.Version == 3 && !reflect.DeepEqual(a, r.Ancestors) {
		return rel, fail("ORIGINAL_ANCESTOR_CHANGED")
	}
	t, e := rt.todo(orig.Todo)
	if e != nil {
		return rel, e
	}
	spec, e := retiredText(t.Spec, 8<<20)
	if e != nil {
		return rel, e
	}
	if e = rt.Guard(orig.CWD); e != nil {
		return rel, e
	}
	head, e := rt.SavedSource(orig.CWD)
	if e != nil {
		return rel, e
	}
	path := r.Path
	if r.State == "quarantined" {
		path = filepath.Join(r.Destination, "files")
		if !retiredAbsent(r.Path) {
			return rel, fail("ORIGINAL_PATH_REAPPEARED")
		}
		for _, p := range []string{r.Destination, path} {
			if e = rt.Guard(p); e != nil {
				return rel, e
			}
		}
	}
	items, _, e := c.retiredSnapshot(path)
	if e != nil {
		return rel, e
	}
	if len(items) == 0 || items[0].Relative != "." || items[0].Volume != r.Volume || items[0].ID != r.FileID || r.Version == 3 && (r.Birth == 0 || items[0].Birth != r.Birth) {
		return rel, fail("TARGET_IDENTITY_CHANGED")
	}
	rel = retiredRelease{Schema: 1, ID: id, CreateJob: producerJob, ReleaseJob: actor.ID, MoveJob: moveJob, Todo: orig.Todo, Original: r.identityRecord, NoteSHA: retiredSHA([]byte(t.Note)), SpecSHA: retiredSHA(spec), SourceHead: head, SourceRoot: orig.CWD, ResourceFinished: true, Unneeded: true, Reproducible: true, Entries: items}
	if e = rt.liveState(r, rel); e != nil {
		return rel, e
	}
	encoded, e := json.Marshal(rel)
	if e != nil || len(encoded) > 4<<20 {
		return rel, fail("ENTRY_LIMIT_EXCEEDED")
	}
	p, e := c.retirementPath(id, ".release.json")
	if e != nil {
		return rel, e
	}
	if e = retiredWriteNew(p, rel); e != nil {
		return rel, e
	}
	if r.State == "quarantined" {
		e = c.indexRetirement(id, r.Destination)
	}
	return rel, e
}
func (c config) retiredPlan(id string) (retiredCandidate, error) {
	var candidate retiredCandidate
	r, e := c.retiredReceipt(id)
	if e != nil {
		return candidate, e
	}
	if r.State == "quarantine_removed" && r.Destination != "" && retiredAbsent(r.Path) && retiredAbsent(filepath.Join(r.Destination, "files")) {
		return retiredCandidate{Action: "completed", ID: id}, nil
	}
	p, e := c.retirementPath(id, ".release.json")
	if e != nil {
		return candidate, e
	}
	var rel retiredRelease
	if e = retiredRead(p, 4<<20, &rel, true); e != nil {
		return candidate, fail("ORIGINAL_CREATE_UNVERIFIED")
	}
	orig, e := c.original(r, rel.CreateJob)
	if e != nil {
		return candidate, e
	}
	if rel.Schema != 1 || rel.ID != id || rel.Todo != orig.Todo || !same(rel.SourceRoot, orig.CWD) || !reflect.DeepEqual(rel.Original, r.identityRecord) || !rel.ResourceFinished || !rel.Unneeded || rel.Retention || rel.Dependencies || !rel.Reproducible {
		return candidate, fail("RESOURCE_DISPOSITION_UNVERIFIED")
	}
	rt := c.retirementRuntime()
	j, e := rt.job(rel.ReleaseJob)
	if e != nil || j.Todo != rel.Todo {
		return candidate, fail("ORIGINAL_RELEASE_UNVERIFIED")
	}
	if e = rt.event(rel.ReleaseJob, "release", func(b []byte) bool {
		var v retiredRelease
		return json.Unmarshal(b, &v) == nil && reflect.DeepEqual(v, rel)
	}); e != nil {
		return candidate, fail("ORIGINAL_RELEASE_UNVERIFIED")
	}
	moveJob := r.MoveJob
	if moveJob == "" {
		moveJob = rel.MoveJob
	}
	if r.State != "quarantined" || r.Destination == "" || moveJob == "" {
		return candidate, fail("QUARANTINE_PROVENANCE_MISSING")
	}
	if _, e = rt.job(moveJob); e != nil {
		return candidate, fail("QUARANTINE_PROVENANCE_MISSING")
	}
	if e = rt.event(moveJob, "quarantine", func(b []byte) bool {
		var v struct {
			ID          string `json:"id"`
			State       string `json:"state"`
			Destination string `json:"destination"`
		}
		return json.Unmarshal(b, &v) == nil && v.ID == id && v.State == "quarantined" && same(v.Destination, r.Destination)
	}); e != nil {
		return candidate, fail("QUARANTINE_PROVENANCE_MISSING")
	}
	if !same(filepath.VolumeName(r.Path), filepath.VolumeName(r.Destination)) || !same(filepath.Dir(r.Destination), ownedQuarantineParent(r.Path, c.Quarantine)) || filepath.Base(r.Destination) == "files" {
		return candidate, fail("QUARANTINE_BOUNDARY_MISMATCH")
	}
	for _, p := range []string{r.Root, r.Path, rel.SourceRoot, r.Destination, filepath.Join(r.Destination, "files")} {
		if e = rt.Guard(p); e != nil {
			return candidate, e
		}
	}
	if !retiredAbsent(r.Path) {
		return candidate, fail("ORIGINAL_PATH_REAPPEARED")
	}
	a, e := retiredAncestors(r.Root)
	if e != nil {
		return candidate, e
	}
	if r.Version == 3 && !reflect.DeepEqual(a, r.Ancestors) {
		return candidate, fail("ORIGINAL_ANCESTOR_CHANGED")
	}
	if e = rt.liveState(r, rel); e != nil {
		return candidate, e
	}
	payload := filepath.Join(r.Destination, "files")
	items, total, e := c.retiredSnapshot(payload)
	if e != nil {
		return candidate, e
	}
	if !reflect.DeepEqual(items, rel.Entries) {
		return candidate, fail("RESOURCE_CONTENTS_CHANGED")
	}
	anc, e := retiredAncestors(r.Destination)
	if e != nil {
		return candidate, e
	}
	return retiredCandidate{Action: "candidate", ID: id, Path: payload, Bytes: total, Entries: items, record: r, release: rel, ancestors: anc}, nil
}
func (c config) finishRetirement(p retiredCandidate) error {
	r, e := c.retiredReceipt(p.ID)
	if e != nil || !reflect.DeepEqual(r, p.record) {
		return fail("CANONICAL_RECEIPT_CHANGED")
	}
	if !retiredAbsent(r.Path) || !retiredAbsent(p.Path) {
		return fail("TARGET_STILL_EXISTS")
	}
	a, e := retiredAncestors(r.Destination)
	if e != nil || !reflect.DeepEqual(a, p.ancestors) {
		return fail("QUARANTINE_ANCESTOR_CHANGED")
	}
	var rel retiredRelease
	rp, _ := c.retirementPath(p.ID, ".release.json")
	if retiredRead(rp, 4<<20, &rel, true) != nil || !reflect.DeepEqual(rel, p.release) {
		return fail("ORIGINAL_RELEASE_CHANGED")
	}
	r.State = "quarantine_removed"
	return c.save(r, false)
}

func (c config) openRetiredPlan(id string, readOnly bool) (retiredCandidate, func(), error) {
	unlock := func() {}
	if !readOnly {
		var e error
		unlock, e = c.retirementLock(id)
		if e != nil {
			return retiredCandidate{}, func() {}, e
		}
	}
	p, e := c.retiredPlan(id)
	if e != nil {
		unlock()
		return p, func() {}, e
	}
	return p, unlock, nil
}

func retirementMain(args []string) bool {
	if len(args) == 0 || (args[0] != "release" && args[0] != "cleanup-lease" && args[0] != "inspect-retired") {
		return false
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	path := fs.String("path", "", "")
	producer := fs.String("producer-job", "", "")
	moveJob := fs.String("move-job", "", "")
	disposable := fs.Bool("disposable", false, "")
	c := defaultConfig()
	runtime := c.retirementRuntime()
	c.Retirement = &runtime
	if args[0] == "inspect-retired" {
		c.Store = retirementStore
	}
	var result any
	var err error
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		err = fail("INVALID_ARGUMENTS")
	} else if args[0] == "release" {
		result, err = c.releaseRetirement(*id, *producer, *moveJob, *disposable)
	} else {
		if *id == "" {
			*id, err = c.retirementID(*path)
		}
		if err == nil {
			var unlock func()
			var p retiredCandidate
			p, unlock, err = c.openRetiredPlan(*id, args[0] == "inspect-retired")
			if err == nil {
				defer unlock()
				if p.Action != "completed" && *path != "" && !same(filepath.Dir(p.Path), *path) {
					err = fail("QUARANTINE_BOUNDARY_MISMATCH")
				}
				if err == nil {
					if p.Action == "completed" {
						result = map[string]any{"action": "completed", "id": p.ID}
					} else if args[0] == "inspect-retired" {
						result = map[string]any{"action": "candidate", "id": p.ID, "bytes": p.Bytes, "entries": len(p.Entries), "changed": false}
					} else {
						json.NewEncoder(os.Stdout).Encode(p)
						// A finite lease dies even if the consumer crashes or keeps stdin open.
						time.AfterFunc(120*time.Second, func() { os.Exit(1) })
						line, e := bufio.NewReader(io.LimitReader(os.Stdin, 64)).ReadString('\n')
						if e != nil || line != "finish\n" {
							err = fail("CLEANUP_NOT_FINISHED")
						} else {
							err = c.finishRetirement(p)
							result = map[string]any{"state": "quarantine_removed", "id": p.ID}
						}
					}
				}
			}
		}
	}
	if err != nil {
		var e *failure
		if !errors.As(err, &e) {
			e = &failure{Code: "RETIREMENT_IO_FAILED"}
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"action": "retain", "code": e.Code})
		os.Exit(1)
	} else {
		json.NewEncoder(os.Stdout).Encode(result)
	}
	return true
}

// Kept separate so the process snapshot's ABI size always matches this build.
func unsafeProcessEntrySize() uintptr { return unsafe.Sizeof(syscall.ProcessEntry32{}) }
