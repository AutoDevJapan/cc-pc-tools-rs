package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const skillsJobsRoot = "D:/CC/Tools/codex-jobs/jobs"

type skillsSubwork struct {
	Parent    int    `json:"parent_todo_id"`
	ParentJob string `json:"origin_job_id"`
	Kind      string `json:"kind"`
	Owner     struct {
		Worktree string `json:"worktree"`
	} `json:"owner"`
}
type skillsAdmission struct {
	Key      string        `json:"key"`
	Spec     string        `json:"spec"`
	Worktree string        `json:"worktree"`
	Subwork  skillsSubwork `json:"subwork"`
}

type skillsTask struct {
	// Derived from the original canonical JSON, never caller request fields.
	assignmentKeyNull    bool
	assignmentJob        string
	assignmentGeneration uint64
	ID                   int      `json:"id"`
	Project              string   `json:"project"`
	Status               string   `json:"status"`
	Spec                 string   `json:"spec"`
	Jobs                 []string `json:"jobs"`
	OwnerStopped         bool     `json:"owner_stopped"`
	Postponed            bool     `json:"owner_postponement_recorded"`
	StopKind             string   `json:"stop_kind"`
	OwnerQuestion        any      `json:"owner_question_wait"`
	DecisionWait         any      `json:"decision_wait"`
	WaitReady            any      `json:"wait_ready"`
	Scheduler            struct {
		Job           string           `json:"job"`
		LastJob       string           `json:"last_job"`
		Worktree      string           `json:"worktree"`
		Branch        string           `json:"branch"`
		Key           string           `json:"key"`
		SubworkParent string           `json:"subwork_parent_job"`
		PendingCancel any              `json:"pending_cancel"`
		OwnerStop     any              `json:"owner_stop_job"`
		Admission     *skillsAdmission `json:"subwork_admission"`
	} `json:"scheduler"`
	Subwork skillsSubwork `json:"subwork"`
}
type skillsRegistry struct {
	Root  string
	Tasks map[int]skillsTask
	// Assigned only after the real current actor passed skillsActorCheck. The
	// caller cannot supply a registry or substitute a past author for this actor.
	writer skillsWriter
}

// Metadata projection: never retain titles, notes, history, unrelated rows or
// private department names. Only the declared self project and requested IDs.
func skillsRegistryProjection(input io.Reader, ids []int) (skillsRegistry, bool) {
	return skillsRegistryProjectionWith(input, ids, nil)
}

// A finite consumer can project stricter task metadata before struct decoding.
// Existing Skills consumers retain their original reader and result contract.
func skillsRegistryProjectionWith(input io.Reader, ids []int, project func([]byte) ([]byte, bool)) (skillsRegistry, bool) {
	out := skillsRegistry{Tasks: map[int]skillsTask{}}
	wanted := map[int]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	d := json.NewDecoder(input)
	seen := map[string]bool{}
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return out, false
	}
	for d.More() {
		k, e := d.Token()
		key, ok := k.(string)
		if e != nil || !ok || seen[key] {
			return out, false
		}
		seen[key] = true
		if project != nil && ((key != "projects" && strings.EqualFold(key, "projects")) || (key != "items" && strings.EqualFold(key, "items"))) {
			return out, false
		}
		switch key {
		case "projects":
			tok, e := d.Token()
			if e != nil || tok != json.Delim('{') {
				return out, false
			}
			keys := map[string]bool{}
			for d.More() {
				t, e := d.Token()
				name, ok := t.(string)
				if e != nil || !ok || keys[name] {
					return out, false
				}
				keys[name] = true
				if project != nil && name != "cc-pc-tools-rs" && strings.EqualFold(name, "cc-pc-tools-rs") {
					return out, false
				}
				if name == "cc-pc-tools-rs" {
					if d.Decode(&out.Root) != nil {
						return out, false
					}
				} else {
					var skip ownerDiscardJSON
					if d.Decode(&skip) != nil {
						return out, false
					}
				}
			}
			if _, e := d.Token(); e != nil {
				return out, false
			}
		case "items":
			tok, e := d.Token()
			if e != nil || tok != json.Delim('[') {
				return out, false
			}
			for d.More() {
				var raw json.RawMessage
				if d.Decode(&raw) != nil {
					return out, false
				}
				var id struct {
					ID int `json:"id"`
				}
				if project != nil {
					var ok bool
					id.ID, ok = skillsRegistryTaskID(raw)
					if !ok {
						return out, false
					}
				} else if json.Unmarshal(raw, &id) != nil {
					return out, false
				}
				if wanted[id.ID] {
					if _, exists := out.Tasks[id.ID]; exists {
						return out, false
					}
					if project != nil {
						var ok bool
						raw, ok = project(raw)
						if !ok {
							return out, false
						}
					}
					metadata, ok := conversationJSON(raw)
					if !ok {
						return out, false
					}
					var row skillsTask
					if json.Unmarshal(raw, &row) != nil || row.ID != id.ID {
						return out, false
					}
					if (row.ID == 3080 || row.ID == 3686) && !skillsProjectActorAssignment(metadata, &row) {
						return out, false
					}
					out.Tasks[id.ID] = row
				}
			}
			if _, e := d.Token(); e != nil {
				return out, false
			}
		default:
			var skip ownerDiscardJSON
			if d.Decode(&skip) != nil {
				return out, false
			}
		}
	}
	if _, e := d.Token(); e != nil {
		return out, false
	}
	if _, e := d.Token(); e != io.EOF {
		return out, false
	}
	return out, seen["projects"] && seen["items"] && same(out.Root, roots[3]) && len(out.Tasks) == len(wanted)
}

// A strict consumer selects rows by the literal canonical ID, before deciding
// which task metadata to decode. An ID alias must not hide a nominated row in
// the unrelated-row branch. Other fields are discarded without decoding them.
func skillsRegistryTaskID(raw []byte) (int, bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return 0, false
	}
	id, found := 0, false
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok {
			return 0, false
		}
		if strings.EqualFold(name, "id") {
			if name != "id" || found || d.Decode(&id) != nil || id <= 0 {
				return 0, false
			}
			found = true
		} else {
			var discard ownerDiscardJSON
			if d.Decode(&discard) != nil {
				return 0, false
			}
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') {
		return 0, false
	}
	if _, e := d.Token(); e != io.EOF {
		return 0, false
	}
	return id, found
}
func skillsRegistryRead(ids []int) (skillsRegistry, bool) {
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	b, ok := s.read(productStore, 64<<20)
	if !ok {
		return skillsRegistry{}, false
	}
	return skillsRegistryProjection(bytes.NewReader(b), ids)
}
func skillsHasJob(row skillsTask, job string) bool {
	for _, j := range row.Jobs {
		if j == job {
			return true
		}
	}
	return false
}

func skillsHeld(row skillsTask) bool {
	return row.OwnerStopped || row.Postponed || row.StopKind != "" || row.OwnerQuestion != nil || row.DecisionWait != nil || row.WaitReady != nil || row.Scheduler.PendingCancel != nil || row.Scheduler.OwnerStop != nil
}
func skillsLatest(row skillsTask, job string) bool {
	return len(row.Jobs) > 0 && row.Jobs[len(row.Jobs)-1] == job && (row.Scheduler.Job == "" || row.Scheduler.Job == job) && (row.Scheduler.LastJob == "" || row.Scheduler.Job == job || row.Scheduler.LastJob == job)
}
func skillsTaskSpec(s *skillsFS, row skillsTask, sha string) bool {
	if !skillsDigest(sha) || !within(currentTodoSpecsRoot, row.Spec) || !currentSubworkSpecName.MatchString(filepath.Base(row.Spec)) {
		return false
	}
	b, ok := s.read(row.Spec, 1<<20)
	return ok && hashBytes(b) == sha
}
func skillsReadJob(s *skillsFS, id string) (object, bool) {
	if !currentSubworkJobName.MatchString(id) {
		return nil, false
	}
	b, ok := s.read(filepath.Join(skillsJobsRoot, id, "job.json"), 4<<20)
	if !ok {
		return nil, false
	}
	m, ok := conversationJSON(b)
	return m, ok && m["id"] == id
}
func skillsJobTask(job object, id int) bool { return conversationNumber(job["todo_id"]) == uint64(id) }
func skillsWriterCheck(w skillsWriter, reg skillsRegistry, s *skillsFS, caller string) bool {
	row, ok := reg.Tasks[3080]
	if !ok || skillsHeld(row) || !skillsWriterValid(w) || w.Job != caller || row.Project != "cc-pc-tools-rs" || row.Status != "doing" || row.Scheduler.Job != w.Job || !same(row.Scheduler.Worktree, w.Workspace) || !skillsHasJob(row, w.Job) || !skillsLatest(row, w.Job) || !skillsTaskSpec(s, row, w.SpecSHA) {
		return false
	}
	job, ok := skillsReadJob(s, w.Job)
	return ok && skillsJobTask(job, 3080) && job["status"] == "running" && skillsActorLive(job) && skillsActorSource(w.Workspace, row.Scheduler.Branch) && same(str(job["cwd"]), w.Workspace) && skillsActorAssignmentBound(row, job)
}

func skillsActorLive(job object) bool {
	for _, key := range []string{"codex_pid", "pid"} {
		pid, birth := conversationNumber(job[key]), conversationNumber(job[key+"_created"])
		if pid == 0 || pid > 0xffffffff || birth == 0 {
			continue
		}
		h, e := syscall.OpenProcess(0x1000|0x100000, false, uint32(pid))
		if e != nil {
			continue
		}
		var created, exit, kernel, user syscall.Filetime
		var code uint32
		ok := syscall.GetProcessTimes(h, &created, &exit, &kernel, &user) == nil && syscall.GetExitCodeProcess(h, &code) == nil && code == 259 && (uint64(created.HighDateTime)<<32|uint64(created.LowDateTime)) == birth
		syscall.CloseHandle(h)
		if ok {
			return true
		}
	}
	return false
}
func skillsActorSource(root, branch string) bool {
	actual, ok := currentGit(root, "branch", "--show-current")
	return ok && actual == branch && sourceVerified(root)
}

// Original public task scope. These two specs predate the scope answer and are
// fixed by the original parent's preserved source, not by a result's contents.
var skillsOriginalSpecs = map[int]string{
	3674: "d16d2e80dce1f8aebc24a2651c32329bf2a6fe9d3a10494a1abdd70bae26df75",
	3669: "1db9502e1e8cd3793846368a89e4b9dcdba3674663ddf2a7659da5f79fe24d64",
}

// The owner-designated replacement of the old 3676 review is one already
// completed root review, not a new exception for arbitrary root jobs. Its exact
// saved spec, job and response were independently received by the parent.
const skillsReplacementReviewJob = "20261025-025407-todo-3709"
const skillsReplacementReviewSpec = "f019aa26622c8c2d72423945e88ebcfea492140869c905f4178d42ef1e90ae79"
const skillsReplacementReviewFinal = "9e91a000cbb8fe65530bfee0f3a961e982e462736d65a5c06ac91f0b74869a3e"

func skillsReplacementReviewBound(r skillsResultRequest, job object) bool {
	return r.Task == 3709 && r.Job == skillsReplacementReviewJob && r.SpecSHA == skillsReplacementReviewSpec &&
		skillsModel(str(job["model"])) == "gpt-6-luna" && job["effort"] == "max" &&
		job["model_origin"] == "owner_request" && job["effort_origin"] == "owner_request"
}

func skillsReplacementNativeBound(native []object) bool {
	if len(native) == 0 {
		return false
	}
	for _, observed := range native {
		if skillsModel(str(observed["model"])) != "gpt-6-luna" || observed["effort"] != "max" {
			return false
		}
	}
	return true
}

type skillsReceived struct {
	Body     []byte
	Metadata object
	Native   []object
	Spec     []byte
}

func skillsResultBinding(r skillsResultRequest, reg skillsRegistry, job object, spec []byte, forReview bool) bool {
	return skillsResultBindingWith(r, reg, job, spec, forReview, skillsOriginalSpecs)
}
func skillsResultBindingWith(r skillsResultRequest, reg skillsRegistry, job object, spec []byte, forReview bool, originals map[int]string) bool {
	if r == skillsPublicReview3850 && !skillsPublicReview3850Metadata(job) {
		return false
	}
	publicResult := skillsOriginalParentReview
	if skillsPublicReviewCandidate(r, reg, spec) || skillsConsumerResultCandidate(r, reg, spec) {
		publicResult = r
	}
	return skillsResultBindingKnown(r, reg, job, spec, forReview, originals, publicResult)
}

// The parent independently identified this exact public response before this
// repair. Parentage alone does not classify every child review as public.
var skillsOriginalParentReview = skillsResultRequest{
	Task: 3847, Job: "20261025-215831-todo-3847",
	SpecSHA:  "902cb85702fff34a93089c65504f14ba636d92070d2d7a735577618cf5af4dda",
	FinalSHA: "3a69abde4602353425769ad988a82f38d124afe56c3371a96102f284c607cbb4",
}

// Alternative identities are a unit-test seam, never request fields.
func skillsResultBindingKnown(r skillsResultRequest, reg skillsRegistry, job object, spec []byte, forReview bool, originals map[int]string, publicResult skillsResultRequest) bool {
	row, ok := reg.Tasks[r.Task]
	parent, hasParent := reg.Tasks[3080]
	if !ok || !hasParent || parent.Project != "cc-pc-tools-rs" || row.Project != parent.Project || !skillsHasJob(row, r.Job) || !skillsLatest(row, r.Job) || !skillsJobTask(job, r.Task) || job["id"] != r.Job || job["status"] != "succeeded" || job["exit_code"] != json.Number("0") || !same(str(job["cwd"]), row.Scheduler.Worktree) || hashBytes(spec) != r.SpecSHA {
		return false
	}
	if expected, ok := originals[r.Task]; ok && !forReview {
		return expected == r.SpecSHA && strings.Contains(string(spec), skillsParentRoot)
	}
	if r.Task == 3709 && !forReview {
		return skillsReplacementReviewBound(r, job)
	}
	if r.Task == 3676 && !forReview {
		return job["sandbox"] == "read-only" && strings.Contains(string(spec), "3669") && same(str(obj(job["finite_review"])["spec"]), row.Spec)
	}
	// New acceptance reviewers must be real registered child reviews, not a
	// caller-created result file or another public/private task with similar text.
	// The real public parent may receive its registered review's raw response
	// before accepting a manifest or merging. A missing acceptance marker is
	// still rejected by skillsReviewAccepted, never rewritten here.
	publicParentResult := !forReview && r == publicResult && row.Subwork.Parent == 3080 && reg.writer.Task == 3080 && skillsDigest(r.FinalSHA)
	return (forReview || publicParentResult || (row.Subwork.Parent == 3686 && strings.Contains(string(spec), "SKILLS3080_IMPLEMENTATION_REVIEW "))) && row.Subwork.Kind == "review" && (row.Subwork.Parent == 3080 || row.Subwork.Parent == 3686) && job["sandbox"] == "read-only" &&
		conversationNumber(obj(job["subwork"])["parent_todo_id"]) == uint64(row.Subwork.Parent) &&
		obj(job["subwork"])["origin_job_id"] == row.Subwork.ParentJob &&
		skillsReviewAdmissionBound(row, job) && skillsReviewParentBound(reg, row, forReview || publicParentResult, str(job["parent_job_id"])) &&
		same(str(obj(job["finite_review"])["spec"]), row.Spec)
}

func skillsReviewAdmissionBound(row skillsTask, job object) bool {
	a := row.Scheduler.Admission
	return a != nil && a.Key != "" && job["launch_key"] == a.Key && same(a.Spec, row.Spec) && same(a.Worktree, row.Scheduler.Worktree) &&
		a.Subwork == row.Subwork && job["subwork_validated"] == true && obj(job["subwork"])["kind"] == row.Subwork.Kind &&
		same(str(obj(obj(job["subwork"])["owner"])["worktree"]), row.Subwork.Owner.Worktree)
}

// A review belongs to a registered author generation, not merely to matching
// strings inside the child. A normally resumed actor may consume the same fixed
// source review from an earlier registered author; it never impersonates it.
func skillsReviewParentBound(reg skillsRegistry, row skillsTask, manifestReview bool, launch string) bool {
	parent, ok := reg.Tasks[row.Subwork.Parent]
	w := reg.writer
	consumer, consumerOK := reg.Tasks[w.Task]
	if !ok || !consumerOK || parent.ID != row.Subwork.Parent || parent.Project != "cc-pc-tools-rs" || skillsHeld(parent) ||
		(w.Task != 3080 && w.Task != 3686) || consumer.Project != parent.Project || consumer.Scheduler.Job != w.Job ||
		!same(consumer.Scheduler.Worktree, w.Workspace) || !skillsLatest(consumer, w.Job) ||
		(manifestReview && (w.Task != 3080 || parent.ID != 3080)) ||
		(!manifestReview && parent.ID != 3686) {
		return false
	}
	if (row.Scheduler.SubworkParent != "" && row.Scheduler.SubworkParent != launch) ||
		!same(row.Subwork.Owner.Worktree, parent.Scheduler.Worktree) {
		return false
	}
	origin := row.Subwork.ParentJob
	parts := currentSubworkJobName.FindStringSubmatch(origin)
	if len(parts) != 3 || parts[2] != strconv.Itoa(parent.ID) {
		return false
	}
	if launch != "" {
		parts := currentSubworkJobName.FindStringSubmatch(launch)
		if len(parts) != 3 || parts[2] != strconv.Itoa(parent.ID) {
			return false
		}
	}
	latest := parent.Scheduler.Job
	if latest == "" {
		latest = parent.Scheduler.LastJob
	}
	if latest == "" && len(parent.Jobs) > 0 {
		latest = parent.Jobs[len(parent.Jobs)-1]
	}
	if !skillsLatest(parent, latest) || (w.Task == parent.ID && latest != w.Job) {
		return false
	}
	originAt, latestAt, launchAt := -1, -1, -1
	for i, id := range parent.Jobs {
		if id == origin {
			if originAt >= 0 {
				return false
			}
			originAt = i
		}
		if id == latest {
			if latestAt >= 0 {
				return false
			}
			latestAt = i
		}
		if id == launch {
			if launchAt >= 0 {
				return false
			}
			launchAt = i
		}
	}
	// The normal scheduler can bind a queued review to a resumed real parent,
	// or remove a no-longer-live launch parent. Keep that launch generation
	// separate from the immutable author/origin and from the current consumer.
	return originAt >= 0 && latestAt >= originAt && (launch == "" || (launchAt >= originAt && launchAt <= latestAt))
}

func skillsReviewOriginBound(parent skillsTask, row skillsTask, child, origin object) bool {
	return skillsReviewAuthorBound(parent, row.Subwork.ParentJob, child, origin)
}

func skillsReviewAuthorBound(parent skillsTask, expected string, child, origin object) bool {
	start, e := time.Parse(time.RFC3339Nano, str(origin["started_at"]))
	reviewStart, e2 := time.Parse(time.RFC3339Nano, str(child["started_at"]))
	if e != nil || e2 != nil || reviewStart.Before(start) || origin["id"] != expected ||
		!skillsJobTask(origin, parent.ID) || !same(str(origin["cwd"]), parent.Scheduler.Worktree) {
		return false
	}
	switch origin["status"] {
	case "running", "succeeded", "failed", "cancelled", "canceled", "lost":
		return true
	}
	return false
}

func skillsReceive(r skillsResultRequest, reg skillsRegistry, s *skillsFS, review bool) (skillsReceived, string) {
	var out skillsReceived
	if r.Task == 3686 && !review {
		return skillsReceiveOriginalRepair(r, reg, s)
	}
	if r.SpecPath != "" {
		return out, "result_binding"
	}
	if r.Task < 1 || !skillsDigest(r.SpecSHA) || (r.FinalSHA != "" && !skillsDigest(r.FinalSHA)) {
		return out, "result_binding"
	}
	row, ok := reg.Tasks[r.Task]
	if !ok || !skillsTaskSpec(s, row, r.SpecSHA) {
		return out, "result_binding"
	}
	// The spec is a bounded operational input, never returned to the caller.
	spec, ok := s.read(row.Spec, 1<<20)
	if !ok {
		return out, "result_binding"
	}
	job, ok := skillsReadJob(s, r.Job)
	if !ok || !skillsResultBinding(r, reg, job, spec, review) {
		return out, "result_binding"
	}
	_, originalPublic := skillsOriginalSpecs[r.Task]
	if !originalPublic && r.Task != 3676 && r.Task != 3709 && row.Subwork.Kind == "review" && (row.Subwork.Parent == 3080 || row.Subwork.Parent == 3686) {
		// The earlier checks already restricted this to an actual parent's
		// registered job history. Read only that exact author's job metadata.
		origin, ok := skillsReadJob(s, row.Subwork.ParentJob)
		if !ok || !skillsReviewOriginBound(reg.Tasks[row.Subwork.Parent], row, job, origin) {
			return out, "result_binding"
		}
		if launch := str(job["parent_job_id"]); launch != "" && launch != row.Subwork.ParentJob {
			actual, ok := skillsReadJob(s, launch)
			if !ok || !skillsReviewAuthorBound(reg.Tasks[row.Subwork.Parent], launch, job, actual) {
				return out, "result_binding"
			}
		}
	}
	if !skillsModelIdentifier(str(job["model"])) {
		return out, "result_binding"
	}
	switch str(job["effort"]) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return out, "result_binding"
	}
	// A typed scope or this exact legacy migration establishes a candidate,
	// never permission by itself. Verify public source and original author
	// before opening the result. Creation retains its separate merge/notice
	// and acceptance checks.
	publicScope, publicInput := skillsPublicReviewScopeFor(r, spec)
	consumerScope, consumerInput := skillsParseConsumer(spec)
	codeInput := row.Subwork.Parent == 3686 && row.Subwork.Kind == "review" && strings.Contains(string(spec), "SKILLS3080_IMPLEMENTATION_REVIEW ")
	var native []object
	if publicInput {
		if !skillsPublicReviewAuthor(publicScope, reg, row, job, s) || !skillsPublicReviewInputs(publicScope, reg.writer.Workspace, s) {
			return out, "result_binding"
		}
		// Detect a scope/spec added after launch before reading its raw result.
		native, ok = skillsPublicReviewNative(s, job, spec, r.FinalSHA)
		if !ok {
			return out, "native_context"
		}
	}
	if consumerInput {
		if !skillsConsumerAuthor(consumerScope, reg, job, s) {
			return out, "result_binding"
		}
		if _, _, _, reason := skillsConsumerProvenance(consumerScope, reg, s); reason != "" {
			return out, reason
		}
		native, ok = skillsPublicReviewNative(s, job, spec, r.FinalSHA)
		if !ok {
			return out, "native_context"
		}
	}
	if codeInput {
		// The normal registered software review must also prove its unchanged
		// launch prompt and native final digest before the canonical body opens.
		native, ok = skillsCodeReviewNative(s, job, spec, r.FinalSHA)
		if !ok {
			return out, "native_context"
		}
	}
	body, ok := s.read(filepath.Join(skillsJobsRoot, r.Job, "last_message.md"), 2<<20)
	if !ok || !bytes.Equal(body, []byte(str(job["last_message"]))) || (r.FinalSHA != "" && hashBytes(body) != r.FinalSHA) || (r.Task == 3709 && hashBytes(body) != skillsReplacementReviewFinal) || (r == skillsPublicReview3850 && len(body) != 4020) {
		return out, "result_unavailable"
	}
	if !publicInput && !consumerInput && !codeInput {
		native, ok = skillsNativeJob(s, job)
	}
	if !ok || (r.Task == 3709 && !skillsReplacementNativeBound(native)) {
		return out, "native_context"
	}
	out = skillsReceived{Body: body, Spec: spec, Native: native, Metadata: object{"id": r.Job, "task": r.Task, "status": job["status"], "model": job["model"], "effort": job["effort"], "model_origin": job["model_origin"], "effort_origin": job["effort_origin"], "exit_code": job["exit_code"], "thread_id": job["thread_id"], "spec_sha256": r.SpecSHA, "final_sha256": hashBytes(body)}}
	if publicInput {
		out.Metadata["public_input_commit"] = publicScope.Commit
		out.Metadata["public_input_file_count"] = len(publicScope.Files)
		out.Metadata["public_input_scope"] = "immutable_typed_prelaunch_spec"
		if r == skillsPublicReview3850 {
			out.Metadata["public_input_scope"] = "fixed_legacy_migration_3850"
		}
	}
	if consumerInput {
		out.Metadata["public_input_scope"] = "immutable_admitted_public_consumer"
		out.Metadata["manifest_sha256"] = consumerScope.ManifestSHA
	}
	return out, ""
}

var skillsUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Use the same exact native snapshot as historical results and creation calls;
// never cap a normal registered review at the former 256MiB whole-file limit.
// All unrelated payloads are discarded and never returned.
func skillsNativeJob(s *skillsFS, job object) ([]object, bool) {
	id := str(job["thread_id"])
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if e != nil || !skillsUUID.MatchString(id) {
		return nil, false
	}
	end, e := time.Parse(time.RFC3339Nano, str(job["finished_at"]))
	if e != nil || end.Before(start) {
		return nil, false
	}
	stream, ok := skillsNativeFrozenSnapshot(s, job)
	if !ok {
		return nil, false
	}
	return skillsNativeProjection(stream, id, str(job["cwd"]), start, end)
}
func skillsNativeProjection(input io.Reader, id, cwd string, start, end time.Time) ([]object, bool) {
	return skillsNativeProjectionBound(input, id, cwd, start, end, nil)
}
func skillsNativeProjectionBound(input io.Reader, id, cwd string, start, end time.Time, proof *skillsNativeResultProof) ([]object, bool) {
	if end.Before(start) {
		return nil, false
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	out := []object{}
	session := false
	for scanner.Scan() {
		raw := scanner.Bytes()
		if !skillsNativeUniqueJSON(raw) {
			return nil, false
		}
		var envelope struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if !skillsCreationHeaderKeys(raw, "type", "timestamp", "payload") || json.Unmarshal(raw, &envelope) != nil {
			return nil, false
		}
		if envelope.Type != "session_meta" && envelope.Type != "turn_context" && (proof == nil || envelope.Type != "response_item") {
			continue
		}
		if !skillsCreationHeaderKeys(envelope.Payload, "type", "id", "cwd", "role", "phase", "content", "model", "effort", "reasoning_effort", "service_tier") {
			return nil, false
		}
		if envelope.Type == "response_item" {
			at, e := time.Parse(time.RFC3339Nano, envelope.Timestamp)
			if e != nil {
				return nil, false
			}
			if at.Before(start) || at.After(end) {
				if proof.runtimeSourceUsed {
					var header struct {
						Role string `json:"role"`
					}
					if json.Unmarshal(envelope.Payload, &header) != nil || header.Role == "user" {
						return nil, false
					}
				}
				continue
			}
			if !session || !proof.observe(raw, at) {
				return nil, false
			}
			continue
		}
		if envelope.Type == "turn_context" {
			at, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
			if err != nil {
				return nil, false
			}
			if at.Before(start) || at.After(end) {
				continue
			}
		}
		m, ok := conversationJSON(raw)
		if !ok {
			return nil, false
		}
		p := obj(m["payload"])
		if envelope.Type == "session_meta" {
			if session || p["id"] != id || !same(str(p["cwd"]), cwd) {
				return nil, false
			}
			if proof != nil && proof.runtimeSourceUsed {
				at, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
				if err != nil || at.Before(start) || at.After(end) {
					return nil, false
				}
			}
			session = true
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, envelope.Timestamp)
		if e != nil {
			return nil, false
		}
		if at.Before(start) || at.After(end) {
			continue
		}
		if value, exists := p["cwd"]; exists && (!same(str(value), cwd) || str(value) == "") {
			return nil, false
		}
		model := str(p["model"])
		if !skillsModelIdentifier(model) {
			return nil, false
		}
		projected := object{"model": model, "source": "native_turn_context"}
		effort := str(p["effort"])
		if effort == "" {
			effort = str(p["reasoning_effort"])
		}
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			projected["effort"] = effort
		case "":
		default:
			return nil, false
		}
		switch tier := str(p["service_tier"]); tier {
		case "fast", "priority", "default", "standard", "flex", "ultrafast":
			projected["service_tier"] = tier
		case "":
		default:
			return nil, false
		}
		out = append(out, projected)
		if len(out) > 4096 {
			return nil, false
		}
	}
	return out, scanner.Err() == nil && session && len(out) > 0 && (proof == nil || proof.bound())
}
func skillsResultID(r skillsResultRequest) bool {
	m := currentSubworkJobName.FindStringSubmatch(r.Job)
	return len(m) == 3 && m[2] == strconv.Itoa(r.Task)
}
