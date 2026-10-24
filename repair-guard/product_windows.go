package main

// The owner named these original records on 2026-10-13. This is a finite
// verification scope, never a replacement for the missing private registry.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const productStore = "D:/CC/Tools/todo/todo.json"
const gameRoot = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1"
const gameTree = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-1626-1"
const reviewTree = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-2407-work"
const reviewHead = "3b14dd1f43ef5a19e1f09b0959819f3e24f151c3"
const videoTree = "D:/CC/Work/youtube-daily/beats/2026-10-14"
const gameJob = "20261011-234558-orch-9a5f7b7c6fb45eb2815aa60ea8a5df95"
const videoJob = "20261013-060349-Copyright-Free-Beat-Maker---AI-Beats-10-"
const gameSession = "01a12690-d332-7140-bc8c-47974cdcd2cb"
const videoSession = "01a131ed-e534-7722-aa53-6941663ddccc"

type object = map[string]any

func obj(v any) object {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return object{}
}
func str(v any) string { s, _ := v.(string); return s }
func num(v any) int {
	n, _ := v.(float64)
	if n != math.Trunc(n) {
		return -1
	}
	return int(n)
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("record_unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("record_unavailable")
	}
	return b, nil
}
func readObject(path string, limit int64) (object, error) {
	b, err := readBounded(path, limit)
	if err != nil {
		return nil, err
	}
	var m object
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, errors.New("record_invalid")
	}
	return m, nil
}
func itemAt(db object, id int) object {
	rows, _ := db["items"].([]any)
	var found object
	for _, v := range rows {
		if num(obj(v)["id"]) == id {
			if found != nil {
				return nil
			}
			found = obj(v)
		}
	}
	return found
}
func hasJob(item object, id string) bool {
	rows, _ := item["jobs"].([]any)
	for _, v := range rows {
		if str(v) == id {
			return true
		}
	}
	return false
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func exactPhysical(path string) bool {
	p, e := physical(path)
	return e == nil && same(p, path) && !legacyMatch(path) && !legacyMatch(p)
}
func finiteProductScope(at time.Time) bool {
	return !at.Before(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) && at.Before(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
}
func productSnapshot(db, item object) object {
	scheduler, recovery := obj(item["scheduler"]), obj(item["recovery"])
	return object{"id": item["id"], "project": item["project"], "registered_path": obj(db["projects"])[str(item["project"])], "status": item["status"], "dispatchable": item["dispatchable"], "spec": item["spec"], "jobs": item["jobs"], "wait_on": item["wait_on"], "scheduler_job": scheduler["job"], "worktree": scheduler["worktree"], "resume_session": scheduler["resume_session"], "verified_status": recovery["verified_status"], "hold_reason": recovery["automation_hold_reason"], "request_hash": recovery["preserved_worker_request_sha256"]}
}

type productIO struct {
	readObject func(string, int64) (object, error)
	read       func(string, int64) ([]byte, error)
	physical   func(string) bool
	origin     func(string, string) bool
	glob       func(string) ([]string, error)
	at         time.Time
}

func verifyProduct(db object, id int) object {
	return verifyProductWith(db, id, productIO{readObject, readBounded, exactPhysical, sourceOriginVerified, filepath.Glob, time.Now()})
}
func verifyProductWith(db object, id int, access productIO) object {
	r := object{"id": id, "verified": false, "reason": "record_mismatch", "launched": 0, "expires_at": "2026-10-21T00:00:00Z"}
	if !finiteProductScope(access.at) {
		r["reason"] = "scope_expired"
		return r
	}
	return verifyOriginalProductRecords(db, id, access, r)
}

// Record identity has no clock override. A new request must independently
// qualify its own source before reusing these original record comparisons.
// The old public API still expires above, before any record access.
func verifyOriginalProductRecords(db object, id int, access productIO, r object) object {
	if id != 1626 && id != 2407 && id != 2475 {
		return r
	}
	item := itemAt(db, id)
	if item == nil {
		return r
	}
	r["snapshot"] = productSnapshot(db, item)
	project, registered := "PhosGaea-Vol1", gameRoot
	if id == 2475 {
		project, registered = "youtube-daily", "D:/CC/Tools/youtube-daily"
	}
	if item["project"] != project || !same(str(obj(db["projects"])[project]), registered) {
		return r
	}
	jobID, session, tree := gameJob, gameSession, gameTree
	if id == 2475 {
		jobID, session, tree = videoJob, videoSession, videoTree
	}
	job, err := access.readObject(filepath.Join("D:/CC/Tools/codex-jobs/jobs", jobID, "job.json"), 4<<20)
	if err != nil || job["id"] != jobID || job["status"] != "succeeded" || job["thread_id"] != session || !same(str(job["cwd"]), tree) {
		r["reason"] = "worker_receipt_mismatch"
		return r
	}
	if id != 2407 && !hasJob(item, jobID) {
		return r
	}
	if id == 1626 && num(job["todo_id"]) != id {
		return r
	}
	if id == 2407 && (num(obj(item["subwork"])["parent_todo_id"]) != 1626 || obj(item["subwork"])["origin_job_id"] != gameJob) {
		return r
	}
	if !access.physical(tree) {
		r["reason"] = "physical_path_unverified"
		return r
	}
	request, expectedHash := str(item["spec"]), ""
	if id != 2475 {
		recovery := obj(item["recovery"])
		request = str(recovery["preserved_worker_request"])
		expectedHash = str(recovery["preserved_worker_request_sha256"])
		fixedPath, fixedHash := "D:/CC/Work/ledger-recovery-20261012/recovered-specs/1626-worker-request-a600dda0a9dc0f26.md", "a600dda0a9dc0f26a3507e081ef5b938cd7de40bad92e500068f3a191b99ecb8"
		if id == 2407 {
			fixedPath, fixedHash = "D:/CC/Work/ledger-recovery-20261012/recovered-specs/2407-worker-request-45acf355ad003318.md", "45acf355ad003318dc6ba4b06c0c1b0825e6805d8cb6427df53e9ae994b92f27"
		}
		if !same(request, fixedPath) || expectedHash != fixedHash {
			return r
		}
	} else if !same(request, "D:/CC/Tools/todo/specs/2475-9c08c1c10273475f87e003ca75cb4629.md") || !same(str(job["prompt_source"]), filepath.Join(videoTree, "prompt.md")) {
		return r
	}
	if !access.physical(request) {
		r["reason"] = "spec_unavailable"
		return r
	}
	body, err := access.read(request, 1<<20)
	if err != nil {
		r["reason"] = "spec_unavailable"
		return r
	}
	if expectedHash != "" && hashBytes(body) != expectedHash {
		r["reason"] = "spec_hash_mismatch"
		return r
	}
	if id == 1626 {
		// job.prompt is the durable prompt FILE, not the instruction body.
		originalPrompt := filepath.Join("D:/CC/Tools/codex-jobs/jobs", gameJob, "prompt.md")
		promptBytes, err := access.read(originalPrompt, 1<<20)
		if !same(str(job["prompt"]), originalPrompt) || !access.physical(originalPrompt) || err != nil || hashBytes(promptBytes) != expectedHash {
			r["reason"] = "worker_request_mismatch"
			return r
		}
	}
	if id == 2407 && hashBytes([]byte(str(obj(obj(item["scheduler"])["console_request"])["prompt"]))) != expectedHash {
		return r
	}
	if id == 2475 && (!strings.Contains(strings.ReplaceAll(string(body), "\\", "/"), videoTree) || !strings.Contains(string(body), "2026-10-14")) {
		return r
	}
	r["job"], r["session"], r["worktree"], r["request"], r["request_sha256"] = jobID, session, tree, request, hashBytes(body)
	date := "2026/10/11"
	if id == 2475 {
		date = "2026/10/13"
	}
	sessions, err := access.glob(filepath.Join("C:/Users/makhmeto/.codex/sessions", date, "*-"+session+".jsonl"))
	if err != nil || len(sessions) != 1 || !access.physical(sessions[0]) {
		r["reason"] = "session_receipt_unavailable"
		return r
	}
	r["session_receipt_verified"] = true
	if id == 2407 {
		r["source_session"] = session
		r["session"] = nil
	}
	if id != 2475 && (!access.origin(tree, "autodevjapan/phosgaea-vol1") || !same(str(obj(job["work_owner"])["worktree"]), gameTree) || obj(job["work_owner"])["repository"] != "autodevjapan/phosgaea-vol1") {
		r["reason"] = "git_origin_unavailable_or_mismatch"
		return r
	}
	r["verified"], r["reason"] = true, "verified_original_scope"
	return r
}
func productPathAllowed(path string) bool {
	if !finiteProductScope(time.Now()) {
		return false
	}
	absolute, e := filepath.Abs(path)
	if e != nil || legacyMatch(absolute) {
		return false
	}
	// Unknown resources do not cause even metadata reads or Git invocations.
	membership := within(gameTree, absolute) || within(gameRoot, absolute) || within(videoTree, absolute) || within(reviewTree, absolute)
	// A regular `todo edit --spec` creates a new saved copy. Only the exact
	// selected row's copy, with identical verified bytes, can qualify below.
	membership = membership || (same(filepath.Dir(absolute), "D:/CC/Tools/todo/specs") && savedSpecName.MatchString(filepath.Base(absolute)))
	for _, request := range []string{
		"D:/CC/Work/ledger-recovery-20261012/recovered-specs/1626-worker-request-a600dda0a9dc0f26.md",
		"D:/CC/Work/ledger-recovery-20261012/recovered-specs/2407-worker-request-45acf355ad003318.md",
		"D:/CC/Tools/todo/specs/2475-9c08c1c10273475f87e003ca75cb4629.md",
		"D:/CC/Work/product-worker-recovery-20261013/non-client-handoff.md",
		"D:/CC/Work/product-worker-recovery-20261013/safe-status.json",
		"D:/CC/Work/product-worker-recovery-20261013/game-allocation-handoff.md",
	} {
		membership = membership || same(absolute, request)
	}
	if !membership {
		return false
	}
	db, e := readObject(productStore, 64<<20)
	if e != nil {
		return false
	}
	if same(absolute, "D:/CC/Work/product-worker-recovery-20261013/game-allocation-handoff.md") {
		worker := itemAt(db, 2539)
		spec := "D:/CC/Tools/todo/specs/2539-fa72675c1abb4eeb955f41d4dcefa8f0.md"
		if worker == nil || worker["project"] != "cc-pc-tools-rs" || !specRetained(worker, spec) || !exactPhysical(spec) {
			return false
		}
		body, err := readBounded(spec, 1<<20)
		if err != nil || hashBytes(body) != "375cc857a2ac53b1fdb8a1b37c30604614785bbd1b29290a3dfc99807746c567" || !sourceVerified(base) {
			return false
		}
		if !exactPhysical(filepath.Dir(absolute)) {
			return false
		}
		if _, err = os.Lstat(absolute); os.IsNotExist(err) {
			return true
		}
		return exactPhysical(absolute)
	}
	for _, id := range []int{1626, 2407, 2475} {
		r := verifyProduct(db, id)
		if r["verified"] != true {
			continue
		}
		root := str(r["worktree"])
		if physicalOrPlannedWithin(root, absolute) {
			return true
		}
		if same(absolute, str(r["request"])) && exactPhysical(absolute) {
			return true
		}
		item := itemAt(db, id)
		if id == 2407 && within(reviewTree, absolute) && reviewBinding(item) {
			container := filepath.Dir(reviewTree)
			if reviewPhysical(absolute, container) {
				return true
			}
		}
		if same(absolute, str(item["spec"])) && within("D:/CC/Tools/todo/specs", absolute) && exactPhysical(absolute) {
			body, err := readBounded(absolute, 1<<20)
			if err == nil && hashBytes(body) == r["request_sha256"] {
				return true
			}
		}
		if id == 1626 && within(gameRoot, absolute) && exactPhysical(gameRoot) && sourceOriginVerified(gameRoot, "autodevjapan/phosgaea-vol1") {
			if physicalOrPlannedWithin(gameRoot, absolute) {
				return true
			}
		}
	}
	// These two outputs are named in the saved original implementation request.
	// Their existence is neither evidence of product verification nor authority.
	for _, name := range []string{"non-client-handoff.md", "safe-status.json"} {
		output := filepath.Join("D:/CC/Work/product-worker-recovery-20261013", name)
		if !same(absolute, output) {
			continue
		}
		caller, worker := itemAt(db, 2524), itemAt(db, 2530)
		if caller == nil || worker == nil || caller["project"] != "cc-pc-tools-rs" || worker["project"] != "cc-pc-tools-rs" {
			return false
		}
		// Normal todo edit retains the original spec in spec_history. Bind to
		// those original bytes rather than a newly edited pointer or output.
		originalSpec := "D:/CC/Tools/todo/specs/2530-6f464583542140238ae35b600ee07dc3.md"
		if !specRetained(worker, originalSpec) || !exactPhysical(originalSpec) {
			return false
		}
		body, err := readBounded(originalSpec, 1<<20)
		if err != nil || hashBytes(body) != "f35ae3704699b3433f8cc9063478f5bf3ec5dd4a534fd358c21446c7d676855f" || !strings.Contains(strings.ReplaceAll(string(body), "\\", "/"), "D:/CC/Work/product-worker-recovery-20261013/non-client-handoff.md") || !strings.Contains(string(body), "safe-status.json") {
			return false
		}
		if !sourceVerified(base) {
			return false
		}
		parent := filepath.Dir(output)
		if !exactPhysical(parent) {
			return false
		}
		if _, err = os.Lstat(output); os.IsNotExist(err) {
			return true
		}
		return exactPhysical(output)
	}
	return false
}

func reviewPhysical(target, container string) bool {
	if !exactPhysical(container) {
		return false
	}
	if _, err := os.Lstat(reviewTree); err == nil {
		return exactPhysical(reviewTree) && physicalOrPlannedWithin(reviewTree, target)
	} else if !os.IsNotExist(err) {
		return false
	}
	// A missing root has no descendants; only the precise planned tree is allowed.
	return within(reviewTree, target) && physicalOrPlannedWithin(container, target)
}

func reviewBinding(item object) bool {
	scheduler, rel := obj(item["scheduler"]), obj(item["subwork"])
	b := obj(obj(item["product_review_allocation"])["binding"])
	needs, ok := item["needs"].([]any)
	return num(item["id"]) == 2407 && item["project"] == "PhosGaea-Vol1" && ok && len(needs) == 0 &&
		same(str(scheduler["worktree"]), reviewTree) && scheduler["branch"] == "scheduler/todo-2407-work" &&
		num(rel["parent_todo_id"]) == 1626 && rel["origin_job_id"] == gameJob && rel["kind"] == "review" &&
		b["head"] == reviewHead && b["request_sha256"] == "45acf355ad003318dc6ba4b06c0c1b0825e6805d8cb6427df53e9ae994b92f27" &&
		b["key"] == "c8809d28af00fce0cad714928b5aa3cf" && same(str(b["worktree"]), reviewTree) &&
		str(b["branch"]) != "" && str(b["branch"]) == str(scheduler["branch"]) &&
		str(b["spec"]) != "" && str(b["spec"]) == str(item["spec"]) &&
		jsonEqual(b["subwork"], item["subwork"]) && jsonEqual(b["needs"], item["needs"])
}

func jsonEqual(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && string(x) == string(y)
}

var savedSpecName = regexp.MustCompile(`(?i)^(1626|2407|2475)-[0-9a-f]{32}\.md$`)

func specRetained(item object, original string) bool {
	if same(str(item["spec"]), original) {
		return true
	}
	history, _ := item["spec_history"].([]any)
	for _, row := range history {
		if same(str(obj(row)["spec"]), original) {
			return true
		}
	}
	return false
}

// Resolve the closest existing ancestor for new files. A broken or escaping
// link is never interpreted as a missing directory that may be created.
func physicalOrPlannedWithin(root, target string) bool {
	if !within(root, target) {
		return false
	}
	for path := target; within(root, path); path = filepath.Dir(path) {
		resolved, err := physical(path)
		if err == nil {
			return within(root, resolved) && !legacyMatch(resolved)
		}
		if !os.IsNotExist(err) {
			return false
		}
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			return false
		}
		if same(path, root) {
			return false
		}
	}
	return false
}
