package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const skillsConsumerInstructions = "元3080の正規の読み取り専用受入担当として、保存specの対象だけを使う。新しいrequestやファイルを作らず、現在の実CC_JOB_IDのまま D:/CC/Tools/client-records/scrub391.exe --consume-skills3080-admitted を1回呼ぶ。本文を省略しないようfunctions.execとexec_commandの出力上限は各30000tokensとする。qualified:trueの返却に含まれる実際のcontent_utf8を全文読み、その同bufferの指示を使用して、各Skillの適用する具体例と適用しない具体例を1つずつ、理由とindexとともに最終返信へ返す。正規CLIが拒否したらreasonを保持し、対象本文を直接読む、別path・copy・別APIで取得する、callerや権限を偽装する、CreateNew、画像/UI/本番操作、新取得、新費用は行わない。メタデータ発見と実使用を区別し、過去の物理連続性や現在OS writerを証明済みとしない。結果は日本語の最終返信だけに返し、登録やCLI呼出しだけを利用成功にしない。"

// This entire JSON is the normally registered readonly child's prelaunch
// specification. The actual caller is derived from admission, never supplied
// as a future job ID by the creator or impersonated through a writer request.
type skillsConsumerScope struct {
	Domain          string               `json:"domain"`
	Instructions    string               `json:"instructions"`
	Author          skillsWriter         `json:"author"`
	AuthorSpecPath  string               `json:"author_spec_path"`
	CreatorSpecPath string               `json:"creator_spec_path"`
	ManifestCommit  string               `json:"manifest_commit"`
	ManifestPath    string               `json:"manifest_path"`
	ManifestSHA     string               `json:"manifest_sha256"`
	Review          skillsResultRequest  `json:"review"`
	Consume         skillsConsumeRequest `json:"consume"`
}

func skillsConsumerSpecPath(path string) bool {
	return within(currentTodoSpecsRoot, path) && strings.HasPrefix(filepath.Base(path), "3080-") && currentSubworkSpecName.MatchString(filepath.Base(path))
}
func skillsParseConsumer(raw []byte) (skillsConsumerScope, bool) {
	var c skillsConsumerScope
	if !skillsStrict(raw, &c) || c.Domain != "skills-3080-consumer-v1" || c.Instructions != skillsConsumerInstructions || !skillsWriterValid(c.Author) || !skillsWriterValid(c.Consume.Creator) ||
		!skillsConsumerSpecPath(c.AuthorSpecPath) || !skillsConsumerSpecPath(c.CreatorSpecPath) || !skillsGitObjectID(c.ManifestCommit) || !skillsDigest(c.ManifestSHA) ||
		!skillsRelative(c.ManifestPath) || !strings.HasPrefix(c.ManifestPath, "docs/skills-3080-operations/") || !strings.HasSuffix(c.ManifestPath, ".json") ||
		!skillsResultID(c.Review) || !skillsDigest(c.Review.SpecSHA) || !skillsDigest(c.Review.FinalSHA) || c.Review.SpecPath != "" ||
		!filepath.IsAbs(c.Consume.RequestPath) || !within(c.Consume.Creator.Workspace, c.Consume.RequestPath) || strings.ContainsAny(c.Consume.RequestPath, "'\r\n") || !skillsDigest(c.Consume.RequestSHA) || !skillsCallID.MatchString(c.Consume.CallID) ||
		len(c.Consume.Indices) < 1 || len(c.Consume.Indices) > 32 {
		return c, false
	}
	seen := map[int]bool{}
	for _, i := range c.Consume.Indices {
		if i < 0 || i >= 128 || seen[i] {
			return c, false
		}
		seen[i] = true
	}
	return c, true
}

func skillsConsumerActorBound(w skillsWriter, reg skillsRegistry, job object, c skillsConsumerScope) bool {
	row, parent := reg.Tasks[w.Task], reg.Tasks[3080]
	parts := currentSubworkJobName.FindStringSubmatch(w.Job)
	latest := parent.Scheduler.Job
	if latest == "" {
		latest = parent.Scheduler.LastJob
	}
	if latest == "" && len(parent.Jobs) > 0 {
		latest = parent.Jobs[len(parent.Jobs)-1]
	}
	launch := str(job["parent_job_id"])
	if (row.Scheduler.SubworkParent != "" && row.Scheduler.SubworkParent != launch) || (launch != "" && (!skillsRegisteredGeneration(parent, c.Author.Job, launch) || !skillsRegisteredGeneration(parent, launch, latest))) {
		return false
	}
	return w.Task > 0 && w.Task != 3080 && w.Task != 3686 && len(parts) == 3 && parts[2] == strconv.Itoa(w.Task) && skillsDigest(w.SpecSHA) &&
		row.ID == w.Task && parent.ID == 3080 && row.Project == "cc-pc-tools-rs" && parent.Project == row.Project && row.Status == "doing" && !skillsHeld(row) && !skillsHeld(parent) &&
		skillsLatest(row, w.Job) && row.Scheduler.Job == w.Job && same(row.Scheduler.Worktree, w.Workspace) && strings.HasPrefix(row.Scheduler.Branch, "scheduler/todo-"+strconv.Itoa(w.Task)+"-") &&
		row.Subwork.Parent == 3080 && row.Subwork.ParentJob == c.Author.Job && row.Subwork.Kind == "review" && same(row.Subwork.Owner.Worktree, c.Author.Workspace) && same(c.Author.Workspace, parent.Scheduler.Worktree) &&
		skillsLatest(parent, latest) && skillsRegisteredGeneration(parent, c.Author.Job, latest) && skillsRegisteredGeneration(parent, c.Consume.Creator.Job, latest) &&
		job["id"] == w.Job && skillsJobTask(job, w.Task) && job["status"] == "running" && job["sandbox"] == "read-only" && same(str(job["cwd"]), w.Workspace) && skillsReviewAdmissionBound(row, job) && same(str(obj(job["finite_review"])["spec"]), row.Spec)
}

func skillsConsumerAuthor(c skillsConsumerScope, reg skillsRegistry, child object, s *skillsFS) bool {
	parent := reg.Tasks[3080]
	origin, ok := skillsReadJob(s, c.Author.Job)
	if !ok || !skillsReviewAuthorBound(parent, c.Author.Job, child, origin) {
		return false
	}
	if launch := str(child["parent_job_id"]); launch != "" && launch != c.Author.Job {
		actual, valid := skillsReadJob(s, launch)
		if !valid || !skillsReviewAuthorBound(parent, launch, child, actual) {
			return false
		}
	}
	spec, ok := s.read(c.AuthorSpecPath, 1<<20)
	if !ok || hashBytes(spec) != c.Author.SpecSHA {
		return false
	}
	_, ok = skillsReviewLaunchPromptAt(s, skillsJobsRoot, origin, spec)
	return ok
}

func skillsAcceptedProducer(p, current skillsProducer) bool {
	return skillsDigest(p.SHA) && skillsGitObjectID(p.Revision) && (p == current || skillsGuardBaselines[p.SHA] == p.Revision)
}

func skillsConsumerNativeInput(s *skillsFS, job object, spec []byte) bool {
	if job["status"] != "running" {
		return false
	}
	prompt, ok := skillsReviewLaunchPromptAt(s, skillsJobsRoot, job, spec)
	if !ok {
		return false
	}
	native, ok := skillsNativeWindow(s, job, &skillsNativeResultProof{prompt: prompt, inputOnly: true}, time.Now())
	if !ok || len(native) == 0 {
		return false
	}
	for _, n := range native {
		if skillsModel(str(n["model"])) != skillsModel(str(job["model"])) || n["effort"] != job["effort"] {
			return false
		}
	}
	return true
}

func skillsConsumeAcceptedCurrent(m skillsManifest, e skillsCreatedEvidence, root string, s *skillsFS, indices []int, current skillsProducer) (object, string) {
	if !skillsAcceptedProducer(e.Producer, current) {
		return nil, "creation_evidence"
	}
	return skillsConsumeCurrent(m, e, root, s, indices)
}

// Revalidate provenance without opening central output. The accepted original
// runtime's native CreateNew proves its review acceptance. A fresh consumer
// does not pretend to be the original current writer to reread that review.
func skillsConsumerProvenance(c skillsConsumerScope, reg skillsRegistry, s *skillsFS) (skillsManifest, skillsCreatedEvidence, skillsProducer, string) {
	var m skillsManifest
	var e skillsCreatedEvidence
	var current skillsProducer
	parent := reg.Tasks[3080]
	latest := parent.Scheduler.Job
	if latest == "" {
		latest = parent.Scheduler.LastJob
	}
	if latest == "" && len(parent.Jobs) > 0 {
		latest = parent.Jobs[len(parent.Jobs)-1]
	}
	creator, ok := skillsReadJob(s, c.Consume.Creator.Job)
	if !ok || skillsHeld(parent) || !skillsRegisteredGeneration(parent, c.Consume.Creator.Job, latest) || !skillsJobTask(creator, 3080) || creator["id"] != c.Consume.Creator.Job || !same(str(creator["cwd"]), c.Consume.Creator.Workspace) || !same(parent.Scheduler.Worktree, c.Consume.Creator.Workspace) || !skillsModelIdentifier(str(creator["model"])) {
		return m, e, current, "writer"
	}
	switch creator["status"] {
	case "running", "succeeded", "failed", "cancelled", "canceled", "lost":
	default:
		return m, e, current, "writer"
	}
	spec, ok := s.read(c.CreatorSpecPath, 1<<20)
	if !ok || hashBytes(spec) != c.Consume.Creator.SpecSHA {
		return m, e, current, "creation_evidence"
	}
	if _, ok = skillsReviewLaunchPromptAt(s, skillsJobsRoot, creator, spec); !ok {
		return m, e, current, "creation_evidence"
	}
	raw, ok := s.read(c.Consume.RequestPath, 1<<20)
	var creation skillsRequest
	if !ok || hashBytes(raw) != c.Consume.RequestSHA || !skillsStrict(raw, &creation) || creation.Domain != "skills-3080-operation-v1" || creation.Writer != c.Consume.Creator || creation.Consume != nil || creation.Result != nil || creation.Runtime != nil || creation.Review == nil || *creation.Review != c.Review || creation.ManifestCommit != c.ManifestCommit || creation.ManifestPath != c.ManifestPath || creation.ManifestSHA != c.ManifestSHA {
		return m, e, current, "creation_evidence"
	}
	manifestRaw, ok := skillsManifestBytes(creation)
	if !ok {
		return m, e, current, "manifest"
	}
	m, ok = skillsParseManifest(manifestRaw)
	if !ok || m.Writer != c.Consume.Creator || !skillsSourcesBound(m, s) {
		return m, e, current, "source"
	}
	current, ok = skillsProducerObservation(s)
	if !ok {
		return m, e, current, "source"
	}
	e, ok = skillsReadCreationNative(s, creator, c.Consume)
	if !ok || !skillsAcceptedProducer(e.Producer, current) || !skillsCreatedBound(e, creation, c.Consume.RequestSHA, m, e.Producer) {
		return m, e, current, "creation_evidence"
	}
	return m, e, current, ""
}

func skillsFreshConsumerOperation() object {
	fail := func(reason string) object { return object{"qualified": false, "reason": reason} }
	caller := os.Getenv("CC_JOB_ID")
	parts := currentSubworkJobName.FindStringSubmatch(caller)
	if len(parts) != 3 {
		return fail("writer")
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id == 3080 || id == 3686 {
		return fail("writer")
	}
	reg, ok := skillsRegistryRead([]int{3080, 3686, id})
	if !ok {
		return fail("writer")
	}
	row := reg.Tasks[id]
	fs := skillsFS{blocked: skillsBlocked}
	defer fs.close()
	if !within(currentTodoSpecsRoot, row.Spec) || !strings.HasPrefix(filepath.Base(row.Spec), strconv.Itoa(id)+"-") || !currentSubworkSpecName.MatchString(filepath.Base(row.Spec)) {
		return fail("writer")
	}
	spec, ok := fs.read(row.Spec, 1<<20)
	if !ok {
		return fail("writer")
	}
	c, ok := skillsParseConsumer(spec)
	if !ok {
		return fail("scope")
	}
	w := skillsWriter{Task: id, Job: caller, SpecSHA: hashBytes(spec), Workspace: row.Scheduler.Worktree}
	job, ok := skillsReadJob(&fs, caller)
	if !ok || !skillsConsumerActorBound(w, reg, job, c) || !skillsActorLive(job) || !skillsActorSource(w.Workspace, row.Scheduler.Branch) || !skillsConsumerAuthor(c, reg, job, &fs) {
		return fail("writer")
	}
	if !skillsConsumerNativeInput(&fs, job, spec) {
		return fail("native_context")
	}
	scope, ok := fs.read(skillsScopePath, 1<<20)
	if !ok || !skillsScopeBytes(scope) {
		return fail("scope")
	}
	m, e, current, reason := skillsConsumerProvenance(c, reg, &fs)
	if reason != "" {
		return fail(reason)
	}
	result, reason := skillsConsumeAcceptedCurrent(m, e, skillsCentralRoot, &fs, c.Consume.Indices, current)
	if reason != "" {
		return fail(reason)
	}
	fresh, ok := skillsRegistryRead([]int{3080, 3686, id})
	actual, jobOK := skillsReadJob(&fs, caller)
	if !ok || !jobOK || !skillsConsumerActorBound(w, fresh, actual, c) || !skillsActorLive(actual) {
		return fail("writer")
	}
	if !skillsConsumerNativeInput(&fs, actual, spec) {
		return fail("native_context")
	}
	result["consumer"], result["runtime_producer"], result["manifest_sha256"], result["native_call_id"] = w, current, c.ManifestSHA, c.Consume.CallID
	result["original_creator_need_not_be_running"], result["original_producer_revalidated"] = true, true
	return result
}

func skillsConsumerResultCandidate(r skillsResultRequest, reg skillsRegistry, spec []byte) bool {
	c, ok := skillsParseConsumer(spec)
	row := reg.Tasks[r.Task]
	return ok && reg.writer.Task == 3080 && skillsDigest(r.FinalSHA) && row.Subwork.Parent == 3080 && row.Subwork.Kind == "review" && row.Subwork.ParentJob == c.Author.Job && same(row.Subwork.Owner.Worktree, c.Author.Workspace)
}
