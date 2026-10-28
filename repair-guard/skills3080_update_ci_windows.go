package main

func skillsRuntimeNewCIReview(r skillsRequest) bool {
	current := func(a *skillsResultRequest) bool {
		return a != nil && (a.Task == skillsRuntimeSelfReviewTask || a.Task == skillsRuntimePeerReviewTask)
	}
	return current(r.Review) || (r.Runtime != nil && current(r.Runtime.PeerReview))
}

// Only this new, finite software pair uses PR627. Old exact software pairs keep
// their existing readonly qualification; callers cannot supply a CI receipt.
func skillsRuntimeCI(u *skillsRuntimeRequest) bool {
	if u == nil || !civ6Commit.MatchString(u.SourceHead) || !civ6Commit.MatchString(u.ReviewedHead) {
		return false
	}
	raw, ok := skillsGitHub("pr", "view", "627", "--repo", "AutoDevJapan/cc-pc-tools-rs", "--json", "state,mergeCommit,headRefOid,statusCheckRollup")
	if !ok {
		return false
	}
	pr, ok := conversationJSON(raw)
	return ok && skillsRuntimeCIBound(pr, u)
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
	return true
}
