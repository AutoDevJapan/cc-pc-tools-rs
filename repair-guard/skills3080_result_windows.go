package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const skillsJobsRoot = "D:/CC/Tools/codex-jobs/jobs"

type skillsTask struct {
	ID            int      `json:"id"`
	Project       string   `json:"project"`
	Status        string   `json:"status"`
	Spec          string   `json:"spec"`
	Jobs          []string `json:"jobs"`
	OwnerStopped  bool     `json:"owner_stopped"`
	Postponed     bool     `json:"owner_postponement_recorded"`
	StopKind      string   `json:"stop_kind"`
	OwnerQuestion any      `json:"owner_question_wait"`
	DecisionWait  any      `json:"decision_wait"`
	WaitReady     any      `json:"wait_ready"`
	Scheduler     struct {
		Job           string `json:"job"`
		LastJob       string `json:"last_job"`
		Worktree      string `json:"worktree"`
		Branch        string `json:"branch"`
		Key           string `json:"key"`
		SubworkParent string `json:"subwork_parent_job"`
		PendingCancel any    `json:"pending_cancel"`
		OwnerStop     any    `json:"owner_stop_job"`
	} `json:"scheduler"`
	Subwork struct {
		Parent    int    `json:"parent_todo_id"`
		ParentJob string `json:"origin_job_id"`
		Kind      string `json:"kind"`
	} `json:"subwork"`
}
type skillsRegistry struct {
	Root  string
	Tasks map[int]skillsTask
}

// Metadata projection: never retain titles, notes, history, unrelated rows or
// private department names. Only the declared self project and requested IDs.
func skillsRegistryProjection(input io.Reader, ids []int) (skillsRegistry, bool) {
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
				if json.Unmarshal(raw, &id) != nil {
					return out, false
				}
				if wanted[id.ID] {
					if _, exists := out.Tasks[id.ID]; exists {
						return out, false
					}
					if _, ok := conversationJSON(raw); !ok {
						return out, false
					}
					var row skillsTask
					if json.Unmarshal(raw, &row) != nil {
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
	if !ok || skillsHeld(row) || !skillsWriterValid(w) || w.Job != caller || row.Project != "cc-pc-tools-rs" || row.Status != "doing" || row.Scheduler.Job != w.Job || !same(row.Scheduler.Worktree, w.Workspace) || !skillsHasJob(row, w.Job) || !skillsTaskSpec(s, row, w.SpecSHA) {
		return false
	}
	job, ok := skillsReadJob(s, w.Job)
	return ok && skillsJobTask(job, 3080) && job["status"] == "running" && skillsActorLive(job) && skillsActorSource(w.Workspace, row.Scheduler.Branch) && same(str(job["cwd"]), w.Workspace) && job["launch_key"] == row.Scheduler.Key
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
	row, ok := reg.Tasks[r.Task]
	parent, hasParent := reg.Tasks[3080]
	if !ok || !hasParent || parent.Project != "cc-pc-tools-rs" || row.Project != parent.Project || !skillsHasJob(row, r.Job) || !skillsLatest(row, r.Job) || !skillsJobTask(job, r.Task) || job["id"] != r.Job || job["status"] != "succeeded" || job["exit_code"] != json.Number("0") || !same(str(job["cwd"]), row.Scheduler.Worktree) || hashBytes(spec) != r.SpecSHA {
		return false
	}
	if expected, ok := originals[r.Task]; ok && !forReview {
		return expected == r.SpecSHA && strings.Contains(string(spec), skillsParentRoot)
	}
	if r.Task == 3676 && !forReview {
		return job["sandbox"] == "read-only" && strings.Contains(string(spec), "3669") && same(str(obj(job["finite_review"])["spec"]), row.Spec)
	}
	// New acceptance reviewers must be real registered child reviews, not a
	// caller-created result file or another public/private task with similar text.
	parentJob := row.Scheduler.SubworkParent
	if parentJob == "" {
		parentJob = row.Subwork.ParentJob
	}
	return (forReview || (row.Subwork.Parent == 3686 && strings.Contains(string(spec), "SKILLS3080_IMPLEMENTATION_REVIEW "))) && row.Subwork.Kind == "review" && (row.Subwork.Parent == 3080 || row.Subwork.Parent == 3686) && job["sandbox"] == "read-only" &&
		conversationNumber(obj(job["subwork"])["parent_todo_id"]) == uint64(row.Subwork.Parent) && str(job["parent_job_id"]) != "" &&
		job["parent_job_id"] == parentJob && obj(job["subwork"])["origin_job_id"] == row.Subwork.ParentJob &&
		same(str(obj(job["finite_review"])["spec"]), row.Spec)
}
func skillsReceive(r skillsResultRequest, reg skillsRegistry, s *skillsFS, review bool) (skillsReceived, string) {
	var out skillsReceived
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
	if !skillsModelIdentifier(str(job["model"])) {
		return out, "result_binding"
	}
	switch str(job["effort"]) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return out, "result_binding"
	}
	body, ok := s.read(filepath.Join(skillsJobsRoot, r.Job, "last_message.md"), 2<<20)
	if !ok || !bytes.Equal(body, []byte(str(job["last_message"]))) || (r.FinalSHA != "" && hashBytes(body) != r.FinalSHA) {
		return out, "result_unavailable"
	}
	native, ok := skillsNativeJob(s, job)
	if !ok {
		return out, "native_context"
	}
	out = skillsReceived{Body: body, Spec: spec, Native: native, Metadata: object{"id": r.Job, "task": r.Task, "status": job["status"], "model": job["model"], "effort": job["effort"], "exit_code": job["exit_code"], "thread_id": job["thread_id"], "spec_sha256": r.SpecSHA, "final_sha256": hashBytes(body)}}
	return out, ""
}

var skillsUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// No recursive session/history search: only the start day and next day, exact
// native thread identity. All other payloads are discarded and never returned.
func skillsNativeJob(s *skillsFS, job object) ([]object, bool) {
	id := str(job["thread_id"])
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if e != nil || !skillsUUID.MatchString(id) {
		return nil, false
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, false
	}
	paths := []string{}
	for _, offset := range []int{0, 1} {
		dir := filepath.Join(home, ".codex/sessions", start.AddDate(0, 0, offset).Format("2006/01/02"))
		if _, ok := s.directory(dir); !ok {
			continue
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return nil, false
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), "-"+id+".jsonl") {
				paths = append(paths, filepath.Join(dir, entry.Name()))
			}
		}
	}
	if len(paths) != 1 {
		return nil, false
	}
	stream, ok := s.open(paths[0], 256<<20)
	if !ok {
		return nil, false
	}
	return skillsNativeProjection(io.LimitReader(stream, (256<<20)+1), id, str(job["cwd"]), start)
}
func skillsNativeProjection(input io.Reader, id, cwd string, start time.Time) ([]object, bool) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	out := []object{}
	session := false
	for scanner.Scan() {
		raw := scanner.Bytes()
		var envelope struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			return nil, false
		}
		if envelope.Type != "session_meta" && envelope.Type != "turn_context" {
			continue
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
			session = true
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, envelope.Timestamp)
		if e != nil {
			return nil, false
		}
		if at.Before(start) {
			continue
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
	return out, scanner.Err() == nil && session && len(out) > 0
}
func skillsResultID(r skillsResultRequest) bool {
	m := currentSubworkJobName.FindStringSubmatch(r.Job)
	return len(m) == 3 && m[2] == strconv.Itoa(r.Task)
}
