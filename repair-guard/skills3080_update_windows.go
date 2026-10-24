package main

import (
	"bytes"
	"debug/buildinfo"
	"strings"
)

type skillsRuntimeRequest struct {
	SourceRoot   string `json:"source_root"`
	SourceHead   string `json:"source_head"`
	ReviewedHead string `json:"reviewed_head"`
	Candidate    string `json:"candidate"`
	CandidateSHA string `json:"candidate_sha256"`
}

const skillsRuntimeTarget = "D:/CC/Tools/client-records/scrub391.exe"
const skillsGuardModule = "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard"

// Independent previously published distribution facts (3258), not a caller
// supplied baseline or an assertion made by the artifact being installed.
var skillsGuardBaselines = map[string]string{
	"04cb7fc34e3e08e8b88166f0d9a6d53f20f90b7aac89c8317f3afebaaf253363": "e8cf0e2d9adb231ef1636c907edfec2b50a2886c",
	"8ad7256ecd0b443ea0d9f90a995dd2fe99d352e1504895072bcff0753f1208ac": "8a4452d47a01895eee6701dd9f153585869e17f8",
}

func skillsBuildRevision(raw []byte) (string, bool) {
	info, e := buildinfo.Read(bytes.NewReader(raw))
	if e != nil || info.Path != skillsGuardModule {
		return "", false
	}
	values := map[string]string{}
	for _, s := range info.Settings {
		if _, exists := values[s.Key]; exists {
			return "", false
		}
		values[s.Key] = s.Value
	}
	return values["vcs.revision"], values["GOOS"] == "windows" && values["GOARCH"] == "amd64" && values["vcs"] == "git" && values["vcs.modified"] == "false" && civ6Commit.MatchString(values["vcs.revision"])
}
func skillsCodeReviewAccepted(received skillsReceived, head string) bool {
	if !strings.Contains(string(received.Spec), "SKILLS3080_IMPLEMENTATION_REVIEW "+head) || len(received.Native) == 0 {
		return false
	}
	model := skillsModel(str(received.Metadata["model"]))
	if model == "" || model == "gpt-6.1-sol" {
		return false
	}
	wantEffort := skillsReviewEffort(model)
	if received.Metadata["effort"] != wantEffort {
		return false
	}
	for _, native := range received.Native {
		if skillsModel(str(native["model"])) != model || native["effort"] != wantEffort {
			return false
		}
	}
	count := 0
	for _, line := range strings.Split(string(received.Body), "\n") {
		const prefix = "SKILLS3080_CODE_ACCEPTANCE "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		count++
		var a struct {
			Domain   string `json:"domain"`
			Head     string `json:"head"`
			Must     *int   `json:"must"`
			Accepted bool   `json:"accepted"`
		}
		if !skillsStrict([]byte(strings.TrimPrefix(line, prefix)), &a) || a.Domain != "skills-3080-code-review-v1" || a.Head != head || a.Must == nil || *a.Must != 0 || !a.Accepted {
			return false
		}
	}
	return count == 1
}
func skillsRuntimeQualification(r skillsRequest, reg skillsRegistry, s *skillsFS) object {
	failure := object{"qualified": false, "reason": "source"}
	u := r.Runtime
	if u == nil || r.Writer.Task != 3686 || r.Result != nil || r.ManifestSHA != "" || r.ManifestPath != "" || r.ManifestCommit != "" || r.Review == nil || !skillsResultID(*r.Review) || !skillsDigest(u.CandidateSHA) || !within(r.Writer.Workspace, u.SourceRoot) || !within(r.Writer.Workspace, u.Candidate) || same(u.Candidate, skillsRuntimeTarget) || !skillsMerged(u.SourceHead) || !skillsMerged(u.ReviewedHead) || !sourceVerified(u.SourceRoot) || !modelSourceHead(u.SourceRoot, u.SourceHead) {
		return failure
	}
	if _, ok := s.directory(u.SourceRoot); !ok {
		return failure
	}
	if _, e := permanentGitAt(u.SourceRoot, "diff", "--quiet", "--no-ext-diff", "--no-textconv", u.SourceHead, "--"); e != nil {
		return failure
	}
	// Same complete guard build inputs as the reviewed fixed candidate, even if
	// an ordinary merge commit advances the top-level repository revision.
	if _, e := permanentGitAt(u.SourceRoot, "diff", "--quiet", "--no-ext-diff", "--no-textconv", u.ReviewedHead, u.SourceHead, "--", "repair-guard", "tools/civ6-ci-contract", "localai-web-rs/src/operations_catalog.rs", "localai-web-rs/src/operations.rs"); e != nil {
		return failure
	}
	review, reason := skillsReceive(*r.Review, reg, s, false)
	if reason != "" || !skillsCodeReviewAccepted(review, u.ReviewedHead) {
		return object{"qualified": false, "reason": "review"}
	}
	candidate, ok := s.read(u.Candidate, 64<<20)
	if !ok || hashBytes(candidate) != u.CandidateSHA {
		return failure
	}
	candidateHead, ok := skillsBuildRevision(candidate)
	if !ok || candidateHead != u.SourceHead {
		return failure
	}
	installed, ok := s.read(skillsRuntimeTarget, 64<<20)
	if !ok {
		return failure
	}
	currentSHA := hashBytes(installed)
	currentHead, ok := skillsBuildRevision(installed)
	if !ok {
		return failure
	}
	already := currentSHA == u.CandidateSHA
	if (!already && skillsGuardBaselines[currentSHA] != currentHead) || (already && currentHead != u.SourceHead) {
		return failure
	}
	return object{"qualified": true, "domain": "skills-3080-runtime-update-v1", "todo_id": 3686, "target_kind": "guard", "current_sha256": currentSHA, "new_sha256": u.CandidateSHA, "source_head": u.SourceHead, "reviewed_head": u.ReviewedHead, "already_current": already, "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false}
}
