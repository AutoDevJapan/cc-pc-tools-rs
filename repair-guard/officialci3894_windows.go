package main

// This constructor has no path/hash/owner input and never changes classification.
// It binds one newly issued public-software receipt to the original CI purpose.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

const official3894Gate = "D:/CC/Tools/codex-jobs/gh_gate.exe"
const official3894Store = "D:/CC/Tools/client-records/official-ci3387.creation-v1"
const official3894Domain = "official-ci3387-ordinary-implementation-3894-v1"
const official3894Safe = "D:/CC/Tools/safe-delete/safe-delete.exe"
const official3894Temp = "C:/Users/makhmeto/AppData/Local/Temp"

type official3894Source struct {
	Head, Registration, CreatorJob, Workspace, Branch, SpecSHA string
}

type official3894Owned struct {
	Version      int    `json:"version"`
	ID           string `json:"id"`
	Root         string `json:"root"`
	Path         string `json:"path"`
	Volume       uint32 `json:"volume"`
	FileID       uint64 `json:"file_id"`
	Birth        uint64 `json:"birth"`
	Created      string `json:"created_at"`
	Project      string `json:"project"`
	Content      string `json:"content"`
	ProducerJob  string `json:"producer_job"`
	ProducerTodo int    `json:"producer_todo"`
	ProducerKind string `json:"producer_kind"`
	Ancestors    []struct {
		Path   string `json:"path"`
		Volume uint32 `json:"volume"`
		FileID uint64 `json:"file_id"`
		Birth  uint64 `json:"birth"`
	} `json:"ancestors"`
	Integrity string `json:"integrity"`
	State     string `json:"state"`
}

func official3894Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func official3894Hex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return len(s) == n && e == nil && hex.EncodeToString(b) == s
}

func official3894Flag(args []string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		for _, f := range []string{"prepare-official-ci3387", "verify-official-ci3387", "consume-official-ci3387"} {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func official3894Parse(args []string) (string, string, bool) {
	if len(args) == 1 && args[0] == "--prepare-official-ci3387" {
		return "prepare", "", true
	}
	if len(args) == 2 && official3894Hex(args[1], 32) {
		switch args[0] {
		case "--verify-official-ci3387":
			return "verify", args[1], true
		case "--consume-official-ci3387":
			return "consume", args[1], true
		}
	}
	return "", "", false
}

func official3894Failure(reason string) object {
	return object{"purpose_verified": false, "reason": reason, "access_allowed": false, "original_registry_recovered": false, "launch_performed": false}
}

type official3894Error string

func (e official3894Error) Error() string { return string(e) }

func runOfficial3894CLI(args []string, out io.Writer, operation func(context.Context, string, string) (object, error)) int {
	op, id, ok := official3894Parse(args)
	if !ok {
		_ = json.NewEncoder(out).Encode(official3894Failure("arguments"))
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, e := operation(ctx, op, id)
	if e != nil {
		// Preserve bounded, non-secret ownership/state metadata, never public file
		// bodies from a callback whose durable terminal save did not succeed.
		if r == nil {
			r = official3894Failure("purpose_or_io")
		}
		var boundary official3894Error
		if errors.As(e, &boundary) {
			r["reason"] = string(boundary)
		} else if errors.Is(e, context.DeadlineExceeded) {
			r["reason"] = "deadline"
		}
		r["purpose_verified"] = false
		delete(r, "files")
		_ = json.NewEncoder(out).Encode(r)
		return 1
	}
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	return 0
}

// Explicit environment projection: do not read all environment variables and
// never let GH_TOKEN/GITHUB_TOKEN select an API-key credential in this path.
func official3894Env() []string {
	return official3894EnvWith(os.LookupEnv)
}
func official3894EnvWith(lookup func(string) (string, bool)) []string {
	var out []string
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "COMSPEC", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "APPDATA", "CC_JOB_ID"} {
		if value, ok := lookup(key); ok {
			out = append(out, key+"="+value)
		}
	}
	// TEMP is part of the target boundary, not a caller-selected root override.
	out = append(out, "TEMP="+official3894Temp, "TMP="+official3894Temp)
	return out
}

func official3894Command(ctx context.Context, file string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Env = official3894Env()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	// Every endpoint is a bounded metadata projection, and safe-delete create
	// emits only its one new receipt. A child cannot force unbounded capture.
	var b official3894Buffer
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	e := cmd.Run()
	if e != nil || b.overflow {
		return b.Bytes(), oc.ErrDenied
	}
	return b.Bytes(), nil
}

type official3894Buffer struct {
	bytes.Buffer
	overflow bool
}

func (b *official3894Buffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		b.overflow = true
		return 0, oc.ErrDenied
	}
	return b.Buffer.Write(p)
}

type official3894Pin struct {
	endpoint, query string
	expected        object
}

func official3894Pins() []official3894Pin {
	b := oc.FixedBinding("", "", "", "")
	a := oc.OfficialAsset()
	return []official3894Pin{
		{"repos/AutoDevJapan/cc-pc-tools-rs", "{id,full_name,private,owner:.owner.login}", object{"id": int64(1406818713), "full_name": "AutoDevJapan/cc-pc-tools-rs", "private": false, "owner": "AutoDevJapan"}},
		{"repos/AutoDevJapan/local-ai-console", "{id,full_name,private,owner:.owner.login}", object{"id": b.RepoID, "full_name": "AutoDevJapan/local-ai-console", "private": true, "owner": b.Owner}},
		{"repos/AutoDevJapan/local-ai-console/pulls/17", "{number,state,merged,head:.head.sha,repo_id:.head.repo.id}", object{"number": 17, "state": "open", "merged": false, "head": b.Head, "repo_id": b.RepoID}},
		{"repos/AutoDevJapan/local-ai-console/git/commits/" + b.Head, "{sha,tree:.tree.sha}", object{"sha": b.Head, "tree": b.Tree}},
		{"repos/AutoDevJapan/local-ai-console/actions/runs/40228177555", "{id,run_attempt,head_sha,status,conclusion,repository_id:.repository.id,workflow_id,event,path}", object{"id": b.RunID, "run_attempt": b.Attempt, "head_sha": b.Head, "status": "queued", "conclusion": nil, "repository_id": b.RepoID, "workflow_id": 392978784, "event": "pull_request", "path": ".github/workflows/windows.yml"}},
		{"repos/AutoDevJapan/local-ai-console/actions/workflows/392978784/runs?event=pull_request&per_page=100", "[.workflow_runs[]|select(any(.pull_requests[]?;.number==17))][0]|{id,head_sha,run_attempt}", object{"id": b.RunID, "head_sha": b.Head, "run_attempt": b.Attempt}},
		{"repos/AutoDevJapan/local-ai-console/actions/jobs/121273999986", "{id,run_id,head_sha,status,conclusion,labels}", object{"id": b.JobID, "run_id": b.RunID, "head_sha": b.Head, "status": "queued", "conclusion": nil, "labels": []string{"self-hosted", "Windows", "X64", "todo3339"}}},
		{"repos/actions/runner", "{full_name,private,owner:.owner.login}", object{"full_name": "actions/runner", "private": false, "owner": "actions"}},
		{"repos/actions/runner/releases/tags/v2.338.0", "{tag_name,draft,prerelease,assets:[.assets[]|select(.id==615677454)|{id,name,size,digest,state,browser_download_url}]}", object{"tag_name": a.Tag, "draft": false, "prerelease": false, "assets": []object{{"id": a.ID, "name": a.Name, "size": a.Size, "digest": "sha256:" + a.SHA256, "state": "uploaded", "browser_download_url": a.URL}}}},
	}
}

func official3894Match(raw []byte, want object) bool {
	got, ok := conversationJSON(raw)
	if !ok {
		return false
	}
	b, e := json.Marshal(got)
	w, e2 := json.Marshal(want)
	return e == nil && e2 == nil && bytes.Equal(b, w)
}

func official3894GitHub(ctx context.Context) error {
	for i, p := range official3894Pins() {
		b, e := official3894Command(ctx, official3894Gate, "api", "--hostname", "github.com", p.endpoint, "--jq", p.query)
		if e != nil || !official3894Match(b, p.expected) {
			return official3894Error("github_metadata_pin_" + strconv.Itoa(i+1))
		}
	}
	return nil
}

func official3894Birth(pid uint32) (uint64, bool) {
	h, e := syscall.OpenProcess(0x1000|0x100000, false, pid)
	if e != nil {
		return 0, false
	}
	defer syscall.CloseHandle(h)
	var c, x, k, u syscall.Filetime
	var code uint32
	if syscall.GetProcessTimes(h, &c, &x, &k, &u) != nil || syscall.GetExitCodeProcess(h, &code) != nil || code != 259 {
		return 0, false
	}
	return uint64(c.HighDateTime)<<32 | uint64(c.LowDateTime), true
}

func official3894Descendant(job object) bool {
	want, birth := conversationNumber(job["codex_pid"]), conversationNumber(job["codex_pid_created"])
	if want == 0 || want > 0xffffffff || birth == 0 {
		return false
	}
	actual, ok := official3894Birth(uint32(want))
	if !ok || actual != birth {
		return false
	}
	h, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	parents := map[uint32]uint32{}
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafe.Sizeof(p))
	for e = syscall.Process32First(h, &p); e == nil; e = syscall.Process32Next(h, &p) {
		parents[p.ProcessID] = p.ParentProcessID
	}
	pid := uint32(os.Getpid())
	seen := map[uint32]bool{}
	for i := 0; i < 64 && pid != 0 && !seen[pid]; i++ {
		if pid == uint32(want) {
			return true
		}
		seen[pid] = true
		pid = parents[pid]
	}
	return false
}

func official3894BuildHead() (string, bool) {
	b, ok := debug.ReadBuildInfo()
	if !ok || b.Path != skillsGuardModule {
		return "", false
	}
	settings := map[string]string{}
	for _, s := range b.Settings {
		settings[s.Key] = s.Value
	}
	head := settings["vcs.revision"]
	return head, official3894Hex(head, 40) && settings["vcs.modified"] == "false" && settings["GOOS"] == "windows" && settings["GOARCH"] == "amd64"
}

// Only the normal metadata reader projects these three existing tasks. It
// neither creates a registry nor returns notes, original prompts or credentials.
func official3894SourceCheck(ctx context.Context, saved *official3894Source, consume bool) (official3894Source, error) {
	var result official3894Source
	caller := os.Getenv("CC_JOB_ID")
	m := currentSubworkJobName.FindStringSubmatch(caller)
	if len(m) != 3 || (m[2] != "3894" && m[2] != "3888") || (consume && m[2] != "3888") {
		return result, official3894Error("registered_actor_required")
	}
	reg, ok := official3894Registry(ctx)
	if !ok {
		return result, official3894Error("normal_task_metadata_unavailable")
	}
	parent, provider, creator := reg.Tasks[3387], reg.Tasks[3888], reg.Tasks[3894]
	b := oc.FixedBinding("", "", "", "")
	if parent.Project != "cc-pc-tools-rs" || creator.Project != parent.Project || provider.Project != "localai-web-rs" ||
		!skillsHasJob(parent, b.ParentJob) || !skillsHasJob(provider, b.OriginalProviderJob) || skillsHeld(parent) || skillsHeld(provider) || skillsHeld(creator) || parent.Status == "done" || provider.Status == "done" || parent.Status == "block" || provider.Status == "block" || creator.Status == "block" {
		return result, official3894Error("original_task_lineage_or_stop")
	}
	actorID, _ := strconv.Atoi(m[2])
	row := reg.Tasks[actorID]
	if (row.Status != "doing" && row.Status != "todo") || row.Scheduler.Job != caller || !skillsLatest(row, caller) || !skillsHasJob(row, caller) {
		return result, official3894Error("current_assignment_required")
	}
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	job, ok := skillsReadJob(&s, caller)
	if !ok || !skillsJobTask(job, actorID) || job["status"] != "running" || !official3894Descendant(job) || !same(str(job["cwd"]), row.Scheduler.Worktree) || job["launch_key"] != row.Scheduler.Key {
		return result, official3894Error("native_actor_identity_required")
	}
	if saved == nil && actorID != 3894 {
		return result, oc.ErrDenied
	}
	creatorJob := caller
	if saved != nil {
		creatorJob = saved.CreatorJob
	}
	if !skillsHasJob(creator, creatorJob) || !strings.HasPrefix(creator.Scheduler.Branch, "scheduler/todo-3894-") || !within(roots[3]+"-wt", creator.Scheduler.Worktree) {
		return result, oc.ErrDenied
	}
	if actorID == 3888 && (!ownerRegisteredSource(row.Scheduler.Worktree).AccessAllowed || !sourceOriginVerified(row.Scheduler.Worktree, "autodevjapan/local-ai-console") || !skillsActorSource(row.Scheduler.Worktree, row.Scheduler.Branch)) {
		return result, oc.ErrDenied
	}
	if !within(currentTodoSpecsRoot, creator.Spec) || !strings.HasPrefix(filepath.Base(creator.Spec), "3894-") || !currentSubworkSpecName.MatchString(filepath.Base(creator.Spec)) {
		return result, oc.ErrDenied
	}
	spec, ok := s.read(creator.Spec, 1<<20)
	if !ok {
		return result, oc.ErrDenied
	}
	specSHA := official3894Hash(spec)
	// Source registration qualifies only the actual source. The SDK anchor is
	// never sent to this function or made into a Git source.
	if !ownerRegisteredSource(creator.Scheduler.Worktree).AccessAllowed || !sourceVerified(creator.Scheduler.Worktree) {
		return result, official3894Error("normal_registered_source_required")
	}
	head, ok := official3894BuildHead()
	if !ok {
		return result, official3894Error("clean_windows_source_build_required")
	}
	for _, path := range []string{"repair-guard", "tools/civ6-ci-contract"} {
		if _, e := permanentGitAt(creator.Scheduler.Worktree, "diff", "--quiet", "--no-ext-diff", "--no-textconv", head, "--", path); e != nil {
			return result, official3894Error("current_source_inputs_changed")
		}
	}
	merged, e := official3894Command(ctx, official3894Gate, "api", "repos/AutoDevJapan/cc-pc-tools-rs/compare/"+head+"...main", "--jq", ".status")
	if e != nil || (strings.TrimSpace(string(merged)) != "ahead" && strings.TrimSpace(string(merged)) != "identical") {
		return result, official3894Error("normal_source_merge_required")
	}
	// Only immutable registration fields enter the binding. Normal progress,
	// waiting and new provider jobs cannot silently replace the original purpose.
	proof, _ := json.Marshal(struct{ Root, Spec, SpecSHA, Workspace, Branch, ParentJob, ProviderJob string }{reg.Root, creator.Spec, specSHA, creator.Scheduler.Worktree, creator.Scheduler.Branch, b.ParentJob, b.OriginalProviderJob})
	result = official3894Source{head, official3894Hash(proof), creatorJob, creator.Scheduler.Worktree, creator.Scheduler.Branch, specSHA}
	if saved != nil && result != *saved {
		return result, oc.ErrDenied
	}
	return result, ctx.Err()
}

func official3894Registry(ctx context.Context) (skillsRegistry, bool) {
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	// The ordinary store is already close to the older reader's 64 MiB limit.
	// Stream its existing metadata projection; retain only these three rows,
	// never notes, private prompts, unrelated project names or a replacement DB.
	f, ok := s.open(productStore, 512<<20)
	if !ok {
		return skillsRegistry{}, false
	}
	r, ok := skillsRegistryProjection(ownerDeadlineReader{ctx: ctx, input: io.LimitReader(f, 512<<20+1)}, []int{3387, 3888, 3894})
	return r, ok && ctx.Err() == nil
}

func official3894Download(ctx context.Context) ([]byte, error) {
	a := oc.OfficialAsset()
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if e != nil {
		return nil, e
	}
	request.Header.Set("User-Agent", "cc-pc-tools-rs-fixed-official-software")
	client := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil || (r.URL.Host != "github.com" && r.URL.Host != "release-assets.githubusercontent.com" && r.URL.Host != "objects.githubusercontent.com") {
			return oc.ErrDenied
		}
		return nil
	}}
	response, e := client.Do(request)
	if e != nil {
		return nil, oc.ErrDenied
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || (response.ContentLength >= 0 && response.ContentLength != a.Size) {
		return nil, oc.ErrDenied
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, a.Size+1))
	if e != nil || int64(len(b)) != a.Size || official3894Hash(b) != a.SHA256 {
		return nil, oc.ErrDenied
	}
	return b, nil
}

type official3894Archive []byte

func (a official3894Archive) Open(context.Context, oc.Asset) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a)), nil
}

func official3894OwnedCheck(raw []byte, source official3894Source, since time.Time) (official3894Owned, error) {
	var r official3894Owned
	if !skillsStrict(raw, &r) || r.Version != 3 || !official3894Hex(r.ID, 32) || !official3894Hex(r.Integrity, 64) || r.State != "owned" || r.Project != "todo-3894" || r.Content != "official-ci3387-sdk" || r.ProducerKind != "scheduler" || r.ProducerTodo != 3894 || r.ProducerJob != source.CreatorJob ||
		!filepath.IsAbs(r.Root) || !same(r.Path, filepath.Join(r.Root, "codex-owned-"+r.ID)) || !same(r.Root, official3894Temp) || r.Volume == 0 || r.FileID == 0 || r.Birth == 0 || len(r.Ancestors) == 0 {
		return r, oc.ErrDenied
	}
	created, e := time.Parse(time.RFC3339Nano, r.Created)
	if e != nil || created.Before(since) || created.After(time.Now()) {
		return r, oc.ErrDenied
	}
	return r, nil
}

type official3894Authority struct {
	source         official3894Source
	owned          official3894Owned
	raw            []byte
	issuerIdentity oc.Identity
	started        time.Time
	fs             *oc.NativeFS
	consume        bool
}

func (a official3894Authority) Observe(ctx context.Context) (oc.Evidence, error) {
	var out oc.Evidence
	observed := time.Now()
	if _, e := official3894OwnedCheck(a.raw, a.source, a.started); e != nil {
		return out, e
	}
	if a.fs.ParentIdentity() != (oc.Identity{Volume: a.owned.Volume, FileID: a.owned.FileID, Birth: a.owned.Birth}) {
		return out, oc.ErrDenied
	}
	if _, e := official3894SourceCheck(ctx, &a.source, a.consume); e != nil {
		return out, e
	}
	if e := official3894GitHub(ctx); e != nil {
		return out, e
	}
	// Authenticate the saved direct issuer output before this read; compare the
	// original issuer's current record, never a caller-provided receipt checksum.
	s := skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}
	defer s.close()
	path := filepath.Join("D:/CC/Tools/safe-delete/receipts-go", a.owned.ID+".json")
	b, ok := s.read(path, 8192)
	if !ok || !bytes.Equal(b, a.raw) {
		return out, oc.ErrDenied
	}
	identity, e := official3894Identity(s.files[len(s.files)-1])
	if e != nil || identity != a.issuerIdentity || identity.FileID == 0 {
		return out, oc.ErrDenied
	}
	binding := oc.FixedBinding(a.source.Head, a.source.Registration, official3894Hash(a.raw), a.source.CreatorJob)
	out = oc.Evidence{Parent: a.fs.ParentIdentity(), FreshOwnedAnchor: true, Binding: binding, Asset: oc.OfficialAsset(), Observed: observed, SourceVerified: true, JobVerified: true, Active: true, OfficialPublicAsset: true}
	return out, nil
}

func official3894Summary(t official3894Ticket) object {
	r := official3894Failure("purpose_or_io")
	if t.CreatorPhase != "" {
		r["creation_state"] = t.CreatorPhase
	}
	if len(t.IssuerOutput) > 0 {
		r["original_creator_output_base64"] = t.IssuerOutput
		r["original_creator_output_sha256"] = official3894Hash(t.IssuerOutput)
	}
	if len(t.Owned) > 0 {
		var owned official3894Owned
		if skillsStrict(t.Owned, &owned) {
			r["owned_id"] = owned.ID
			r["original_creation_receipt_base64"] = t.Owned
			r["original_creation_receipt_sha256"] = official3894Hash(t.Owned)
			r["original_creation_receipt_identity"] = t.IssuerIdentity
		}
	}
	if t.Receipt != nil {
		r["receipt_id"] = t.Receipt.ID
		r["state"] = t.Receipt.State
		r["source_head"] = t.Source.Head
		r["source_registration_sha256"] = t.Source.Registration
		r["archive_bytes"] = t.Receipt.Asset.Size
		r["archive_sha256"] = t.Receipt.Asset.SHA256
		r["root_identity"] = t.Receipt.Root
		r["parent_identity"] = t.Receipt.Parent
		var owned official3894Owned
		if skillsStrict(t.Owned, &owned) {
			r["sdk_path"] = filepath.Join(owned.Path, t.Receipt.ID)
		}
		r["manifest_entries"] = len(t.Receipt.Files)
	}
	return r
}

func official3894Operation(ctx context.Context, op, id string) (object, error) {
	// These are purpose-specific native metadata records. No normal data path
	// classification is modified by opening this fixed journal.
	store, e := official3894OpenStore(official3894Store, op == "prepare")
	if e != nil {
		return official3894Failure("creation_store_busy_invalid_or_already_reserved"), e
	}
	defer store.close()
	if op == "prepare" {
		source, e := official3894SourceCheck(ctx, nil, false)
		if e != nil {
			return nil, e
		}
		if e = official3894GitHub(ctx); e != nil {
			return nil, e
		}
		issuer, e := official3894PinIssuer()
		if e != nil {
			return nil, e
		}
		defer issuer.close()
		archive, e := official3894Download(ctx)
		if e != nil {
			return nil, e
		}
		start := time.Now().UTC()
		if e = store.start(official3894Ticket{Domain: official3894Domain, Source: source, Started: start, CreatorPhase: "create_reserved"}); e != nil {
			return nil, e
		}
		// The constructor itself invokes the normal issuer. No existing ID/root
		// is accepted, and the ticket reservation prevents a second SDK attempt.
		raw, e := official3894Command(ctx, official3894Safe, "create", "--project", "todo-3894", "--content", "official-ci3387-sdk", "--root", official3894Temp)
		attempt := store.ticket
		attempt.IssuerOutput = append([]byte(nil), raw...)
		attempt.CreatorPhase = "issuer_returned"
		if e != nil {
			attempt.CreatorPhase = "issuer_failed_or_interrupted"
		}
		if saved := store.append(attempt); saved != nil {
			return official3894Summary(attempt), saved
		}
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		owned, e := official3894OwnedCheck(raw, source, start)
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		issuerReceipt := skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}
		original, ok := issuerReceipt.read(filepath.Join("D:/CC/Tools/safe-delete/receipts-go", owned.ID+".json"), 8192)
		var issuerIdentity oc.Identity
		if ok && bytes.Equal(original, raw) {
			issuerIdentity, e = official3894Identity(issuerReceipt.files[len(issuerReceipt.files)-1])
		} else {
			e = oc.ErrDenied
		}
		issuerReceipt.close()
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		next := store.ticket
		next.Owned = append([]byte(nil), raw...)
		next.IssuerIdentity = issuerIdentity
		next.CreatorPhase = "ownership_recorded"
		if e = store.append(next); e != nil {
			return official3894Summary(next), e
		}
		fs, e := oc.NewNativeFS(filepath.Clean(owned.Path), oc.Identity{Volume: owned.Volume, FileID: owned.FileID, Birth: owned.Birth})
		if e != nil {
			return official3894Summary(next), e
		}
		if e = fs.EmptyParent(); e != nil {
			return official3894Summary(next), e
		}
		a := official3894Authority{source: source, owned: owned, raw: raw, issuerIdentity: issuerIdentity, started: start, fs: fs}
		engine, e := oc.New(oc.FixedBinding(source.Head, source.Registration, official3894Hash(raw), source.CreatorJob), a, official3894Archive(archive), store, fs)
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		r, e := engine.Construct(ctx)
		result := official3894Summary(store.ticket)
		if e != nil {
			return result, e
		}
		if e = engine.Resume(ctx, r.ID); e != nil {
			return result, e
		}
		result["purpose_verified"] = true
		delete(result, "reason")
		return result, nil
	}
	t := store.ticket
	if t.Receipt == nil || t.Receipt.ID != id || t.Domain != official3894Domain {
		return nil, oc.ErrDenied
	}
	var owned official3894Owned
	if !skillsStrict(t.Owned, &owned) {
		return nil, oc.ErrDenied
	}
	fs, e := oc.NewNativeFS(filepath.Clean(owned.Path), t.Receipt.Parent)
	if e != nil {
		return official3894Summary(t), e
	}
	a := official3894Authority{source: t.Source, owned: owned, raw: t.Owned, issuerIdentity: t.IssuerIdentity, started: t.Started, fs: fs, consume: op == "consume"}
	engine, e := oc.New(oc.FixedBinding(t.Source.Head, t.Source.Registration, official3894Hash(t.Owned), t.Source.CreatorJob), a, official3894Archive(nil), store, fs)
	if e != nil {
		return official3894Summary(t), e
	}
	var payloads []oc.Payload
	if op == "verify" {
		e = engine.Resume(ctx, id)
	} else if op == "consume" {
		e = engine.Consume(ctx, id, func(p []oc.Payload) error { payloads = p; return nil })
	} else {
		e = oc.ErrDenied
	}
	result := official3894Summary(store.ticket)
	if e != nil {
		return result, e
	}
	result["purpose_verified"] = true
	delete(result, "reason")
	if op == "consume" {
		result["files"] = payloads
	}
	return result, nil
}

// Pins are from the original finite publisher in tools/owned-cleanup-deploy,
// source 89f2411, not hashes supplied by a caller or learned from unknown data.
// The original software handle remains held through CreateProcess and receipt
// capture so its path cannot be replaced after this comparison.
func official3894PinIssuer() (*skillsFS, error) {
	s := &skillsFS{blocked: func(p string) bool { return !same(p, official3894Safe) || legacyMatch(p) || ownerCardPath(p) }}
	b, ok := s.read(official3894Safe, 64<<20)
	if ok {
		switch official3894Hash(b) {
		case "286a22b24412cc19138c083e09a93f4626de9058f329fad256e3f377b1e3b73b", "7e1737b6bcd7f1b015998e8594527dbab3f211b68cbe8ad3b82f6a46bca39514":
			return s, nil
		}
	}
	s.close()
	return nil, official3894Error("normal_creation_issuer_unverified")
}
