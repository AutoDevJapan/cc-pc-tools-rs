package main

// This is a directory-purpose query for one normally created receiver. It is
// independent of SDK receipt qualification/consumption and never opens an SDK
// journal, payload, credential or runner. No caller supplies evidence or a store.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const ci3888RuntimeID = "540085cc356bbd10a9fc49a191dfcd1b"
const ci3888RuntimeRoot = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-" + ci3888RuntimeID
const ci3888RuntimeProducer = "20261027-220957-todo-3888"
const ci3888RuntimeCreationSHA = "1011174992c9571b548fbb2112cbca31bf2328afd8f11a0915790e1445ea9b80"
const ci3888RuntimeWorkspace = "D:/CC/Work/local-ai-console-3339-wt/todo-3888-local-ai-console-windows-ci-pc"
const ci3888RuntimeCreationOutput = ci3888RuntimeWorkspace + "/.ci-work/runtime-create-220957.stdout.json"
const ci3888RuntimeIssuerRecord = "D:/CC/Tools/safe-delete/receipts-go/" + ci3888RuntimeID + ".json"
const ci3888RuntimeFlag = "--verify-ci3888-runtime"

func ci3888RuntimeFlagPresent(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimLeft(arg, "-")
		if arg == "verify-ci3888-runtime" || strings.HasPrefix(arg, "verify-ci3888-runtime=") {
			return true
		}
	}
	return false
}

func ci3888RuntimePath(path string) bool {
	return filepath.IsAbs(path) && !ownerWindowsAlias(path) && same(path, ci3888RuntimeRoot)
}

func runCI3888RuntimeCLI(args []string, out io.Writer, verify func(context.Context, string) error) int {
	result := object{"purpose_verified": false, "access_allowed": false, "child_data_access": false,
		"sdk_qualified": false, "sdk_consumed": false, "launch_performed": false, "original_registry_recovered": false}
	if len(args) != 2 || args[0] != ci3888RuntimeFlag || !ci3888RuntimePath(args[1]) {
		result["reason"] = "arguments"
		_ = json.NewEncoder(out).Encode(result)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := verify(ctx, args[1]); err != nil {
		// Only fixed internal stage names leave this metadata boundary.
		result["reason"] = "purpose_or_io"
		var stage official3894Error
		if errors.As(err, &stage) {
			result["reason"] = string(stage)
		}
		_ = json.NewEncoder(out).Encode(result)
		return 1
	}
	result["purpose_verified"], result["directory_location_verified"] = true, true
	result["owned_id"], result["purpose"] = ci3888RuntimeID, "official-ci3387-public-runtime"
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}

// The original create stdout was observed independently of markers and caller
// JSON. The normal current issuer record must have those exact same bytes.
// uint64 fields are decoded directly, never through floating point objects.
func ci3888RuntimeCreation(raw []byte) (official3894Owned, bool) {
	var r official3894Owned
	if hashBytes(raw) != ci3888RuntimeCreationSHA || !skillsStrict(raw, &r) {
		return r, false
	}
	return r, ci3888RuntimeCreationFields(r)
}

func ci3888RuntimeCreationFields(r official3894Owned) bool {
	if r.Version != 3 || r.ID != ci3888RuntimeID || r.State != "owned" ||
		!same(r.Path, ci3888RuntimeRoot) || !same(r.Root, filepath.Dir(ci3888RuntimeRoot)) ||
		r.Project != "todo-3888" || r.Content != "official-ci3387-public-runtime" ||
		r.ProducerJob != ci3888RuntimeProducer || r.ProducerTodo != 3888 || r.ProducerKind != "scheduler" ||
		r.Volume != 2229119083 || r.FileID != 14918173766427707 || r.Birth != 134375814346352521 ||
		!official3894Hex(r.Integrity, 64) || len(r.Ancestors) == 0 {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, r.Created)
	if err != nil || created.Before(time.Date(2026, 10, 27, 13, 9, 57, 0, time.UTC)) {
		return false
	}
	for i, ancestor := range r.Ancestors {
		expected := filepath.Dir(ci3888RuntimeRoot)
		if i > 0 {
			expected = filepath.Dir(r.Ancestors[i-1].Path)
			if same(expected, r.Ancestors[i-1].Path) {
				return false
			}
		}
		if !same(ancestor.Path, expected) || ancestor.Volume != r.Volume || ancestor.FileID == 0 || ancestor.Birth == 0 {
			return false
		}
	}
	first, last := r.Ancestors[0], r.Ancestors[len(r.Ancestors)-1]
	return first.Volume == 2229119083 && first.FileID == 562949953422078 && first.Birth == 134072793159133344 && same(last.Path, filepath.Dir(last.Path))
}

func ci3888RuntimeJobBound(row skillsTask, job object, id, common string) bool {
	owner := obj(job["work_owner"])
	return skillsHasJob(row, id) && job["id"] == id && skillsJobTask(job, 3888) &&
		job["project"] == "localai-web-rs" && job["task_type"] == "implementation" &&
		job["scope_source"] == "launcher" && official3894IndependentJob(job) &&
		same(str(job["cwd"]), ci3888RuntimeWorkspace) && len(owner) == 4 &&
		strings.EqualFold(str(owner["repository"]), "autodevjapan/local-ai-console") && common != "" &&
		same(str(owner["git_common"]), common) && same(str(owner["worktree"]), row.Scheduler.Worktree) && owner["branch"] == row.Scheduler.Branch
}

func ci3888RuntimeActorBound(reg skillsRegistry, caller string, job, producer object, common string) bool {
	m := currentSubworkJobName.FindStringSubmatch(caller)
	row := reg.Tasks[3888]
	return len(m) == 3 && m[2] == "3888" && official3894TaskLineage(reg) &&
		row.ID == 3888 && row.Project == "localai-web-rs" && (row.Status == "doing" || row.Status == "todo") &&
		same(row.Scheduler.Worktree, ci3888RuntimeWorkspace) && strings.HasPrefix(row.Scheduler.Branch, "scheduler/todo-3888-") &&
		row.Scheduler.Job == caller && row.Scheduler.Key != "" && skillsLatest(row, caller) &&
		ci3888RuntimeJobBound(row, job, caller, common) && job["status"] == "running" && job["launch_key"] == row.Scheduler.Key &&
		ci3888RuntimeJobBound(row, producer, ci3888RuntimeProducer, common) &&
		(producer["status"] == "running" || producer["status"] == "succeeded")
}

func ci3888RuntimePhysical(r official3894Owned, identity func(string) (audio2906Metadata, error)) bool {
	check := func(path string, volume uint32, id, birth uint64, owned bool) bool {
		m, err := identity(path)
		return err == nil && m.Volume == volume && m.ID == id && uint64(m.Created) == birth &&
			(!owned || m.Owner == civ6Owner) && m.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 && m.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
	}
	if !check(r.Path, r.Volume, r.FileID, r.Birth, true) {
		return false
	}
	for i, ancestor := range r.Ancestors {
		// The normal create record pins ancestor identities, not their owners.
		// Only this user-owned root and its Temp parent have an owner claim;
		// Windows' C:/Users and volume root normally have system owners.
		if !check(ancestor.Path, ancestor.Volume, ancestor.FileID, ancestor.Birth, i == 0) {
			return false
		}
	}
	return true
}

// Dependency seams keep tests away from the actual issuer and SDK. Production
// binds every function below to normal native metadata/source APIs only.
type ci3888RuntimeAccess struct {
	registry  func(context.Context) (skillsRegistry, bool)
	job       func(string) (object, bool)
	read      func(string, int64) ([]byte, bool)
	directory func(string) bool
	identity  func(string) (audio2906Metadata, error)
	common    func(string) (string, bool)
	source    func(string, string) error
	live      func(object) bool
	github    func(context.Context) error
	merged    func(context.Context) error
	blocked   func(string) bool
	creation  func([]byte) (official3894Owned, bool)
}

type ci3888RuntimeActorSnapshot struct {
	root, workspace, branch, common, key, origins, actorSHA string
}

func ci3888RuntimeCurrent(ctx context.Context, caller string, a ci3888RuntimeAccess) (ci3888RuntimeActorSnapshot, error) {
	var result ci3888RuntimeActorSnapshot
	m := currentSubworkJobName.FindStringSubmatch(caller)
	if len(m) != 3 || m[2] != "3888" {
		return result, official3894Error("runtime_native_provider_required")
	}
	reg, ok := a.registry(ctx)
	if !ok {
		return result, official3894Error("runtime_task_metadata_unavailable")
	}
	row := reg.Tasks[3888]
	common, ok := a.common(row.Scheduler.Worktree)
	actor, actorOK := a.job(caller)
	producer, producerOK := a.job(ci3888RuntimeProducer)
	if !ok || !actorOK || !producerOK || !ci3888RuntimeActorBound(reg, caller, actor, producer, common) || !a.live(actor) {
		return result, official3894Error("runtime_native_provider_required")
	}
	origins, ok := official3894OriginalLineage(reg, a.job)
	if !ok {
		return result, official3894Error("runtime_original_lineage_required")
	}
	raw, err := json.Marshal(actor)
	if err != nil || ctx.Err() != nil {
		return result, official3894Error("runtime_current_evidence_changed")
	}
	return ci3888RuntimeActorSnapshot{reg.Root, row.Scheduler.Worktree, row.Scheduler.Branch, common, row.Scheduler.Key, origins, hashBytes(raw)}, nil
}

func ci3888RuntimeVerifyWith(ctx context.Context, path, caller string, a ci3888RuntimeAccess) error {
	deny := func(reason string) error { return official3894Error("runtime_" + reason) }
	if !ci3888RuntimePath(path) {
		return deny("location_required")
	}
	if a.blocked(path) {
		return deny("location_protection_unverified")
	}
	actor, err := ci3888RuntimeCurrent(ctx, caller, a)
	if err != nil {
		return err
	}
	if err := a.source(actor.workspace, actor.branch); err != nil {
		return err
	}
	if err := a.merged(ctx); err != nil {
		return err
	}
	stdout, ok := a.read(ci3888RuntimeCreationOutput, 8192)
	if !ok {
		return deny("creation_output_unavailable")
	}
	r, ok := a.creation(stdout)
	if !ok {
		return deny("independent_creation_required")
	}
	issuer, ok := a.read(ci3888RuntimeIssuerRecord, 8192)
	if !ok || string(issuer) != string(stdout) {
		return deny("current_ownership_required")
	}
	// Every ancestor is held without delete sharing before observing identity.
	if !a.directory(r.Path) || !ci3888RuntimePhysical(r, a.identity) {
		return deny("physical_identity_required")
	}
	for _, ancestor := range r.Ancestors {
		if a.blocked(ancestor.Path) || !a.directory(ancestor.Path) {
			return deny("physical_ancestor_required")
		}
	}
	if err := a.github(ctx); err != nil {
		return deny("original_github_scope_required")
	}
	// File snapshots are closed before external calls. Reacquire the current
	// assignment, stops, job projections and ownership bytes; keeping ordinary
	// metadata files open here would prevent normal atomic progress writes.
	if err := a.source(actor.workspace, actor.branch); err != nil {
		return err
	}
	fresh, err := ci3888RuntimeCurrent(ctx, caller, a)
	if err != nil {
		return err
	}
	if fresh != actor {
		return deny("current_evidence_changed")
	}
	currentOutput, outputOK := a.read(ci3888RuntimeCreationOutput, 8192)
	currentIssuer, issuerOK := a.read(ci3888RuntimeIssuerRecord, 8192)
	if !outputOK || !issuerOK || !bytes.Equal(currentOutput, stdout) || !bytes.Equal(currentIssuer, issuer) {
		return deny("current_ownership_changed")
	}
	if ctx.Err() != nil || a.blocked(path) || !ci3888RuntimePhysical(r, a.identity) {
		return deny("current_evidence_changed")
	}
	return nil
}

func ci3888RuntimeVerify(ctx context.Context, path string) error {
	// Only physical directories are held across network calls. Normal task/job
	// metadata and the fixed public creation proof use short, closed snapshots.
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	return ci3888RuntimeVerifyWith(ctx, path, os.Getenv("CC_JOB_ID"), ci3888RuntimeAccess{
		registry: official3894Registry, job: ci3888RuntimeReadJob, read: ci3888RuntimeRead,
		directory: func(path string) bool { _, ok := s.directory(path); return ok },
		identity:  func(path string) (audio2906Metadata, error) { return resourceIdentity(path, true) },
		common:    currentGitCommonDir, source: ci3888RuntimeSource,
		live: official3894Descendant, github: official3894GitHub, blocked: skillsBlocked, creation: ci3888RuntimeCreation,
		merged: func(ctx context.Context) error {
			head, ok := official3894BuildHead()
			if !ok {
				return official3894Error("runtime_clean_reader_build_required")
			}
			if outcome := official3894MergeLookup(ctx, head, official3894Command); outcome != official3894MergeConfirmed {
				return official3894Error("runtime_reader_merge_" + outcome.reason())
			}
			return nil
		},
	})
}

func ci3888RuntimeSource(root, branch string) error {
	verdict, expired := ownerRegisteredSourceStatus(root)
	if expired {
		return official3894Error("runtime_provider_source_deadline")
	}
	if !verdict.AccessAllowed {
		return official3894Error("runtime_provider_source_unverified")
	}
	if !official3894ProviderSource(root, branch) {
		return official3894Error("runtime_provider_source_binding_required")
	}
	return nil
}

func ci3888RuntimeRead(path string, limit int64) ([]byte, bool) {
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	return s.read(path, limit)
}

func ci3888RuntimeReadJob(id string) (object, bool) {
	return ci3888RuntimeJobFile(filepath.Join(skillsJobsRoot, id, "job.json"), id)
}

func ci3888RuntimeJobFile(path, id string) (object, bool) {
	if !currentSubworkJobName.MatchString(id) {
		return nil, false
	}
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	f, ok := s.open(path, 4<<20)
	if !ok {
		return nil, false
	}
	return official3894JobProjection(io.LimitReader(f, 4<<20+1), id)
}

func ci3888RuntimePathAllowed(path string) bool {
	if !ci3888RuntimePath(path) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return ci3888RuntimeVerify(ctx, path) == nil
}
