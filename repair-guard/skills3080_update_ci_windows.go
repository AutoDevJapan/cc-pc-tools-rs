package main

import (
	"encoding/json"
	"strconv"
	"time"
)

func skillsRuntimeNewCIReview(r skillsRequest) bool {
	current := func(a *skillsResultRequest) bool {
		return a != nil && (a.Task == skillsRuntimeSelfReviewTask || a.Task == skillsRuntimePeerReviewTask)
	}
	return current(r.Review) || (r.Runtime != nil && current(r.Runtime.PeerReview))
}

// The existing finite software pair must prove its actual merged PR. A resumed
// repair must not reuse a previous PR's checks merely because the role IDs are
// unchanged. The PR comes from GitHub's commit association, never a request.
func skillsRuntimeCI(u *skillsRuntimeRequest) bool {
	if u == nil || !civ6Commit.MatchString(u.SourceHead) || !civ6Commit.MatchString(u.ReviewedHead) {
		return false
	}
	raw, ok := skillsGitHub("api", "--hostname", "github.com", "repos/AutoDevJapan/cc-pc-tools-rs/commits/"+u.ReviewedHead+"/pulls?per_page=100")
	if !ok {
		return false
	}
	wrapper, valid := conversationJSON(append(append([]byte(`{"pulls":`), raw...), '}'))
	pulls, array := wrapper["pulls"].([]any)
	if !valid || !array {
		return false
	}
	number, ok := skillsRuntimeAssociatedPR(pulls, u)
	if !ok {
		return false
	}
	raw, ok = skillsGitHub("pr", "view", strconv.Itoa(number), "--repo", "github.com/AutoDevJapan/cc-pc-tools-rs", "--json", "state,mergeCommit,headRefOid,statusCheckRollup")
	if !ok {
		return false
	}
	pr, ok := conversationJSON(raw)
	if !ok || pr["state"] != "MERGED" || pr["headRefOid"] != u.ReviewedHead || obj(pr["mergeCommit"])["oid"] != u.SourceHead {
		return false
	}
	raw, ok = skillsGitHub("api", "--hostname", "github.com", "repos/AutoDevJapan/cc-pc-tools-rs/commits/"+u.ReviewedHead+"/check-runs?per_page=100&filter=latest")
	if !ok {
		return false
	}
	issuers, ok := conversationJSON(raw)
	if !ok {
		return false
	}
	pr["ci_issuers"] = issuers // official response only, never a request field
	return skillsRuntimeCIBound(pr, u)
}

func skillsRuntimeAssociatedPR(pulls []any, u *skillsRuntimeRequest) (int, bool) {
	if u == nil || len(pulls) == 0 || len(pulls) >= 100 {
		return 0, false
	}
	matched := 0
	for _, value := range pulls {
		pr := obj(value)
		if obj(pr["head"])["sha"] != u.ReviewedHead {
			continue
		}
		base, head := obj(pr["base"]), obj(pr["head"])
		n := conversationNumber(pr["number"])
		_, numberType := pr["number"].(json.Number)
		_, mergedTime := time.Parse(time.RFC3339Nano, str(pr["merged_at"]))
		if matched != 0 || !numberType || n == 0 || n > 1<<31 || pr["state"] != "closed" || mergedTime != nil || pr["merge_commit_sha"] != u.SourceHead || base["ref"] != "main" ||
			obj(base["repo"])["id"] != json.Number("1406818713") || obj(base["repo"])["full_name"] != "AutoDevJapan/cc-pc-tools-rs" ||
			obj(head["repo"])["id"] != json.Number("1406818713") {
			return 0, false
		}
		matched = int(n)
	}
	return matched, matched > 0
}

func skillsRuntimeCIBound(pr object, u *skillsRuntimeRequest) bool {
	if u == nil || !civ6Commit.MatchString(u.SourceHead) || !civ6Commit.MatchString(u.ReviewedHead) || pr["state"] != "MERGED" || pr["headRefOid"] != u.ReviewedHead || obj(pr["mergeCommit"])["oid"] != u.SourceHead {
		return false
	}
	required := map[string]bool{}
	for _, name := range []string{"documentation", "repair-guard", "analyze (actions, none)", "analyze (go, autobuild)", "analyze (javascript-typescript, none)", "analyze (rust, none)", "go (Windows, resident-guard)", "go (Windows, capacity-audit-3h)", "go (Windows, unattended-runner)", "go (Windows, tools/linux-runner-bridge)", "go (Linux, resident-guard)", "go (Linux, capacity-audit-3h)", "go (Linux, unattended-runner)", "go (Linux, tools/linux-runner-bridge)"} {
		required[name] = true
	}
	checks, ok := pr["statusCheckRollup"].([]any)
	if !ok || len(checks) < len(required) || len(checks) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range checks {
		check := obj(value)
		name, conclusion := str(check["name"]), str(check["conclusion"])
		if name == "" || seen[name] || check["status"] != "COMPLETED" || (conclusion != "SUCCESS" && conclusion != "NEUTRAL" && conclusion != "SKIPPED") || (required[name] && conclusion != "SUCCESS") {
			return false
		}
		seen[name] = true
	}
	for name := range required {
		if !seen[name] {
			return false
		}
	}
	// Independently observed before any candidate was built: all 14 required
	// workflows are GitHub Actions App15368/github-actions on this exact head.
	// Name-only success from another integration is not the same CI evidence.
	proof := obj(pr["ci_issuers"])
	runs, ok := proof["check_runs"].([]any)
	if !ok || len(runs) > 100 || proof["total_count"] != json.Number(strconv.Itoa(len(runs))) {
		return false
	}
	trusted := map[string]bool{}
	for _, value := range runs {
		run := obj(value)
		name := str(run["name"])
		if !required[name] {
			continue
		}
		app := obj(run["app"])
		if trusted[name] || app["id"] != json.Number("15368") || app["slug"] != "github-actions" || run["head_sha"] != u.ReviewedHead || run["status"] != "completed" || run["conclusion"] != "success" {
			return false
		}
		matched := false
		for _, value := range checks {
			check := obj(value)
			if check["name"] == name && str(check["detailsUrl"]) != "" && check["detailsUrl"] == run["details_url"] {
				matched = true
			}
		}
		if !matched {
			return false
		}
		trusted[name] = true
	}
	for name := range required {
		if !trusted[name] {
			return false
		}
	}
	return true
}
