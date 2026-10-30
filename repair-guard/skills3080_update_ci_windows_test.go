package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestSkills3080RuntimeFindsOnlyItsActualMergedPR(t *testing.T) {
	for _, kind := range []string{"current", "historical", "other_head", "other_merge", "open", "unmerged", "other_base", "other_repo", "other_owner", "fork", "duplicate", "string_number", "string_repo", "nil", "missing"} {
		t.Run(kind, func(t *testing.T) {
			u := &skillsRuntimeRequest{SourceHead: strings.Repeat("b", 40), ReviewedHead: strings.Repeat("a", 40)}
			pr := object{"number": json.Number("655"), "state": "closed", "merged_at": "2026-10-30T14:00:00Z", "merge_commit_sha": u.SourceHead, "head": object{"sha": u.ReviewedHead, "repo": object{"id": json.Number("1406818713")}}, "base": object{"ref": "main", "repo": object{"id": json.Number("1406818713"), "full_name": "AutoDevJapan/cc-pc-tools-rs"}}}
			pulls := []any{pr}
			switch kind {
			case "historical":
				pr["number"] = json.Number("627")
			case "other_head":
				obj(pr["head"])["sha"] = strings.Repeat("c", 40)
			case "other_merge":
				pr["merge_commit_sha"] = strings.Repeat("c", 40)
			case "open":
				pr["state"] = "open"
			case "unmerged":
				pr["merged_at"] = nil
			case "other_base":
				obj(pr["base"])["ref"] = "other"
			case "other_repo":
				obj(obj(pr["base"])["repo"])["id"] = json.Number("1")
			case "other_owner":
				obj(obj(pr["base"])["repo"])["full_name"] = "other/cc-pc-tools-rs"
			case "fork":
				obj(obj(pr["head"])["repo"])["id"] = json.Number("1")
			case "duplicate":
				pulls = append(pulls, pr)
			case "string_number":
				pr["number"] = "655"
			case "string_repo":
				obj(obj(pr["base"])["repo"])["id"] = "1406818713"
			case "nil":
				u = nil
			case "missing":
				pulls = nil
			}
			_, ok := skillsRuntimeAssociatedPR(pulls, u)
			if ok != (kind == "current" || kind == "historical") {
				t.Fatalf("PR binding %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080BootstrapPredecessorIsTheSameInBothUpdaters(t *testing.T) {
	sha, head := official3894BootstrapPredecessorSHA, official3894BootstrapPredecessorHead
	if skillsGuardBaselines[sha] != head || !official3894InstalledBaseline(sha, head) {
		t.Fatal("accepted predecessor missing")
	}
	if official3894InstalledBaseline(sha, strings.Repeat("a", 40)) || official3894InstalledBaseline(strings.Repeat("a", 64), head) {
		t.Fatal("unknown predecessor admitted")
	}
}

func skillsRuntimeCIFixture() (object, *skillsRuntimeRequest) {
	u := &skillsRuntimeRequest{SourceHead: strings.Repeat("b", 40), ReviewedHead: strings.Repeat("a", 40)}
	checks := []any{}
	issuers := []any{}
	workflows := object{}
	for _, name := range []string{"documentation", "repair-guard", "analyze (actions, none)", "analyze (go, autobuild)", "analyze (javascript-typescript, none)", "analyze (rust, none)", "go (Windows, resident-guard)", "go (Windows, capacity-audit-3h)", "go (Windows, unattended-runner)", "go (Windows, tools/linux-runner-bridge)", "go (Linux, resident-guard)", "go (Linux, capacity-audit-3h)", "go (Linux, unattended-runner)", "go (Linux, tools/linux-runner-bridge)", "CodeQL", "Cursor Bugbot"} {
		workflowID, path, required := skillsRuntimeWorkflowIdentity(name)
		runID := "99"
		if required {
			switch path {
			case ".github/workflows/skills-3048.yml":
				runID = "1"
			case ".github/workflows/unattended-readiness.yml":
				runID = "2"
			default:
				runID = "3"
			}
		}
		jobID := strconv.Itoa(len(checks) + 1)
		checkID := strconv.Itoa(1000 + len(checks) + 1)
		url := "https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/" + runID + "/job/" + jobID
		checks = append(checks, object{"name": name, "status": "COMPLETED", "conclusion": "SUCCESS", "detailsUrl": url})
		issuers = append(issuers, object{"id": json.Number(checkID), "name": name, "status": "completed", "conclusion": "success", "head_sha": u.ReviewedHead, "details_url": url, "app": object{"id": json.Number("15368"), "slug": "github-actions"}})
		if required {
			if workflows[runID] == nil {
				workflows[runID] = object{"run": object{"id": json.Number(runID), "workflow_id": workflowID, "path": path, "head_sha": u.ReviewedHead, "status": "completed", "conclusion": "success", "repository": object{"id": json.Number("1406818713")}}, "jobs": object{"total_count": json.Number("0"), "jobs": []any{}}}
			}
			listing := obj(obj(workflows[runID])["jobs"])
			jobs := listing["jobs"].([]any)
			jobs = append(jobs, object{"id": json.Number(jobID), "run_id": json.Number(runID), "name": name, "head_sha": u.ReviewedHead, "status": "completed", "conclusion": "success", "check_run_url": "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/" + checkID})
			listing["jobs"] = jobs
			listing["total_count"] = json.Number(strconv.Itoa(len(jobs)))
		}
	}
	return object{"state": "MERGED", "headRefOid": u.ReviewedHead, "mergeCommit": object{"oid": u.SourceHead}, "statusCheckRollup": checks, "ci_issuers": object{"total_count": json.Number(strconv.Itoa(len(issuers))), "check_runs": issuers}, "ci_workflows": workflows}, u
}

func TestSkills3080RuntimeCIBindsTheWorkflowAndExactJob(t *testing.T) {
	for _, kind := range []string{"valid", "missing", "different_workflow_id", "different_path", "other_head", "other_repo", "wrong_run", "job_missing", "wrong_job_run", "wrong_check_run", "wrong_name", "pending_job", "duplicate_job", "string_id", "count"} {
		t.Run(kind, func(t *testing.T) {
			pr, u := skillsRuntimeCIFixture()
			proof := obj(pr["ci_workflows"])
			entry := obj(proof["2"])
			run := obj(entry["run"])
			listing := obj(entry["jobs"])
			jobs := listing["jobs"].([]any)
			job := obj(jobs[0])
			switch kind {
			case "missing":
				delete(pr, "ci_workflows")
			case "different_workflow_id":
				run["workflow_id"] = json.Number("1")
			case "different_path":
				run["path"] = ".github/workflows/spoof.yml"
			case "other_head":
				run["head_sha"] = strings.Repeat("c", 40)
			case "other_repo":
				obj(run["repository"])["id"] = json.Number("1")
			case "wrong_run":
				run["id"] = json.Number("3")
			case "job_missing":
				listing["jobs"] = jobs[1:]
				listing["total_count"] = json.Number(strconv.Itoa(len(jobs) - 1))
			case "wrong_job_run":
				job["run_id"] = json.Number("3")
			case "wrong_check_run":
				job["check_run_url"] = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/9"
			case "wrong_name":
				job["name"] = "other"
			case "pending_job":
				job["status"] = "in_progress"
			case "duplicate_job":
				listing["jobs"] = append(jobs, job)
				listing["total_count"] = json.Number(strconv.Itoa(len(jobs) + 1))
			case "string_id":
				run["workflow_id"] = "382798887"
			case "count":
				listing["total_count"] = json.Number("99")
			}
			if got := skillsRuntimeCIBound(pr, u); got != (kind == "valid") {
				t.Fatalf("workflow identity %s accepted=%v", kind, got)
			}
		})
	}
}

func TestSkills3080RuntimeCIRequiresTheActualActionsIssuer(t *testing.T) {
	for _, kind := range []string{"valid", "missing_proof", "missing_required", "wrong_app", "wrong_slug", "string_app_id", "wrong_head", "wrong_url", "failed_issuer", "pending_issuer", "duplicate_required", "count_mismatch", "missing_app"} {
		t.Run(kind, func(t *testing.T) {
			pr, u := skillsRuntimeCIFixture()
			proof := obj(pr["ci_issuers"])
			runs := proof["check_runs"].([]any)
			guard := obj(runs[1])
			switch kind {
			case "missing_proof":
				delete(pr, "ci_issuers")
			case "missing_required":
				proof["check_runs"] = append(runs[:1], runs[2:]...)
				proof["total_count"] = json.Number("15")
			case "wrong_app":
				obj(guard["app"])["id"] = json.Number("999")
			case "wrong_slug":
				obj(guard["app"])["slug"] = "other-app"
			case "string_app_id":
				obj(guard["app"])["id"] = "15368"
			case "wrong_head":
				guard["head_sha"] = u.SourceHead
			case "wrong_url":
				guard["details_url"] = "https://example.invalid/check"
			case "failed_issuer":
				guard["conclusion"] = "failure"
			case "pending_issuer":
				guard["status"] = "in_progress"
			case "duplicate_required":
				proof["check_runs"] = append(runs, guard)
				proof["total_count"] = json.Number("17")
			case "count_mismatch":
				proof["total_count"] = json.Number("17")
			case "missing_app":
				delete(guard, "app")
			}
			if skillsRuntimeCIBound(pr, u) != (kind == "valid") {
				t.Fatalf("issuer binding %s accepted=%v", kind, skillsRuntimeCIBound(pr, u))
			}
		})
	}
}

func TestSkills3080RuntimeCIBindsMergedReviewedSourceAndEveryRequiredCheck(t *testing.T) {
	for _, kind := range []string{"valid", "optional_neutral", "optional_skipped", "head", "merge", "open", "missing", "failed", "pending", "cancelled", "skipped_required", "neutral_required", "duplicate", "unknown_conclusion", "missing_name", "no_checks", "wrong_checks_type", "too_many", "optional_pending", "name_case", "status_case", "conclusion_case"} {
		t.Run(kind, func(t *testing.T) {
			pr, u := skillsRuntimeCIFixture()
			checks := pr["statusCheckRollup"].([]any)
			guard := obj(checks[1])
			switch kind {
			case "optional_neutral":
				obj(checks[15])["conclusion"] = "NEUTRAL"
			case "optional_skipped":
				obj(checks[15])["conclusion"] = "SKIPPED"
			case "head":
				pr["headRefOid"] = strings.Repeat("c", 40)
			case "merge":
				obj(pr["mergeCommit"])["oid"] = strings.Repeat("c", 40)
			case "open":
				pr["state"] = "OPEN"
			case "missing":
				pr["statusCheckRollup"] = append(checks[:1], checks[2:]...)
			case "failed":
				guard["conclusion"] = "FAILURE"
			case "pending":
				guard["status"] = "IN_PROGRESS"
			case "cancelled":
				guard["conclusion"] = "CANCELLED"
			case "skipped_required":
				guard["conclusion"] = "SKIPPED"
			case "neutral_required":
				guard["conclusion"] = "NEUTRAL"
			case "duplicate":
				pr["statusCheckRollup"] = append(checks, guard)
			case "unknown_conclusion":
				guard["conclusion"] = "UNKNOWN"
			case "missing_name":
				delete(guard, "name")
			case "no_checks":
				pr["statusCheckRollup"] = []any{}
			case "wrong_checks_type":
				pr["statusCheckRollup"] = object{}
			case "too_many":
				for len(checks) <= 128 {
					checks = append(checks, object{})
				}
				pr["statusCheckRollup"] = checks
			case "optional_pending":
				obj(checks[15])["status"] = "QUEUED"
			case "name_case":
				guard["name"] = "REPAIR-GUARD"
			case "status_case":
				guard["status"] = "completed"
			case "conclusion_case":
				guard["conclusion"] = "success"
			}
			want := kind == "valid" || kind == "optional_neutral" || kind == "optional_skipped"
			if skillsRuntimeCIBound(pr, u) != want {
				t.Fatalf("CI binding %s did not match %v", kind, want)
			}
		})
	}
}

func skillsRuntimeCIRequestFixture() skillsRequest {
	w := skillsWriter{Task: 3686, Job: "20261029-005403-todo-3686", Workspace: "D:/fixture", SpecSHA: strings.Repeat("a", 64)}
	a := &skillsResultRequest{Task: 4300, Job: "20261031-023000-todo-4300", SpecSHA: strings.Repeat("b", 64), FinalSHA: strings.Repeat("c", 64)}
	b := &skillsResultRequest{Task: 4301, Job: "20261031-023100-todo-4301", SpecSHA: strings.Repeat("d", 64), FinalSHA: strings.Repeat("e", 64)}
	return skillsRequest{Writer: w, Review: a, Runtime: &skillsRuntimeRequest{SourceRoot: w.Workspace + "/source", SourceHead: strings.Repeat("b", 40), ReviewedHead: strings.Repeat("a", 40), Candidate: w.Workspace + "/artifact.exe", CandidateSHA: strings.Repeat("f", 64), PeerReview: b, OperationNonce: strings.Repeat("1", 32)}}
}

func TestSkills3080RuntimeVerifyApplyRecoverCannotOmitCI(t *testing.T) {
	for _, op := range []string{"verify", "apply-skills3080-update", "recover-skills3080-update"} {
		for _, kind := range []string{"missing_loader", "missing_ci", "wrong_source", "failed_check"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				r := skillsRuntimeCIRequestFixture()
				fs := &skillsFS{blocked: func(string) bool { return true }}
				calls := 0
				var loader func(*skillsRuntimeRequest) bool
				if kind != "missing_loader" {
					loader = func(u *skillsRuntimeRequest) bool {
						calls++
						if kind == "missing_ci" {
							return false
						}
						pr, _ := skillsRuntimeCIFixture()
						if kind == "wrong_source" {
							obj(pr["mergeCommit"])["oid"] = strings.Repeat("c", 40)
						} else {
							obj(pr["statusCheckRollup"].([]any)[1])["conclusion"] = "FAILURE"
						}
						return skillsRuntimeCIBound(pr, u)
					}
				}
				var got object
				if op == "verify" {
					got = skillsRuntimeQualificationWithCI(r, skillsRegistry{}, fs, nil, loader)
				} else {
					got = skillsRuntimeApplyWithCI(op, "D:/fixture/request.json", nil, r, skillsRegistry{}, fs, nil, loader)
				}
				wantCalls := 1
				if kind == "missing_loader" {
					wantCalls = 0
				}
				if got["qualified"] != false || got["reason"] != "ci" || calls != wantCalls || len(fs.files) != 0 || len(fs.dirs) != 0 {
					t.Fatalf("CI rejection did not precede all source/candidate/backup IO: result=%v calls=%d", got, calls)
				}
			})
		}
	}
}

func TestSkills3080RuntimeCurrentRolesCannotDowngradeToLegacySingleReview(t *testing.T) {
	for _, kind := range []string{"missing_peer", "rubber_only", "mixed", "reversed"} {
		r := skillsRuntimeCIRequestFixture()
		switch kind {
		case "missing_peer":
			r.Runtime.PeerReview = nil
		case "rubber_only":
			r.Review = r.Runtime.PeerReview
			r.Runtime.PeerReview = nil
		case "mixed":
			r.Review.Task = 4198
			r.Review.Job = "20261029-023000-todo-4198"
		case "reversed":
			r.Review, r.Runtime.PeerReview = r.Runtime.PeerReview, r.Review
		}
		fs := &skillsFS{blocked: func(string) bool { return true }}
		calls := 0
		got := skillsRuntimeQualificationWithCI(r, skillsRegistry{}, fs, nil, func(*skillsRuntimeRequest) bool { calls++; return true })
		if got["qualified"] != false || got["reason"] != "review" || calls != 0 || len(fs.files) != 0 {
			t.Fatalf("current CI roles downgraded as %s: %v", kind, got)
		}
	}
	for _, id := range []int{3740, 3870, 4198} {
		r := skillsRuntimeCIRequestFixture()
		r.Review.Task = id
		r.Review.Job = "20261029-023000-todo-" + map[int]string{3740: "3740", 3870: "3870", 4198: "4198"}[id]
		r.Runtime.PeerReview = nil
		if skillsRuntimeNewCIReview(r) {
			t.Fatal("old readonly role gained the new CI contract")
		}
	}
}
