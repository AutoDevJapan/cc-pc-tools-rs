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

// Independent previously accepted distribution facts (3258 and3686), not a caller
// supplied baseline or an assertion made by the artifact being installed.
var skillsGuardBaselines = map[string]string{
	// PR514's independently completed Windows update. This software baseline
	// neither classifies central data nor retrofits any historical creation ID.
	// Evidence: docs/evidence/skills-3080-guard-installed-987f.json.
	"aa3fdfe3e1c64262b224b4beb76e3d81f95dd5dca0bbc405927223691aa41a02": "987fe2459f33711bcfcfefd8ccb70eb4129fe768",
	// PR510's already accepted Windows distribution, reobserved before this
	// repair. This exact software pair grants no access to review/data paths.
	// Evidence: docs/evidence/skills-3080-guard-installed-e1a4.json.
	"29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb": "e1a4c21dc6241c140d4d7364d34f42b16d7005be",
	// PR506's accepted installation, reobserved before this repair. This is
	// one software identity, not a candidate-selected data-access exception.
	// Evidence: docs/evidence/skills-3080-guard-installed-7ba6.json.
	"f487106ad65a261fd0938fb8fd78e092388f7b76beebb12089d3f20e72730b51": "7ba61f9536c4e42ac8417f9e5f109cde39ecfec4",
	// PR497's accepted Windows installation is the independent current
	// baseline, not an arbitrary digest declared by the new candidate.
	// Evidence: docs/evidence/skills-3080-guard-installed-c397.json.
	"ea7cbb49d73a37e972c0072a500406ab1f7f0b62f2ea1926b9ef149ad1e36f12": "c397910d3fc560ca61a31c6382494ac43c866852",
	// PR476 distribution was independently accepted before this revision. Its
	// exact current hash and build revision were rechecked without changing it.
	// Evidence: docs/evidence/skills-3080-guard-installed-a5da.json.
	"ba35332c45b5bb9f35c4465ee209c123be1309ec554ef0976c8240d232c451c1": "a5da2f694ab70589fb1dd29ed874d06df56bc579",
	// Independent public 775e6f30 rebuild: every byte matches this installed
	// artifact except the two declared Go cache action IDs (40 bytes). Both
	// content IDs and all remaining 6,225,368 bytes match; neither PE was changed.
	// This admits one exact old software digest, never arbitrary build-ID edits.
	// Evidence: docs/evidence/skills-3080-guard-baseline.json.
	"0abb667593f4694ac98dca40a9e386de25fe52363d9b6d32699d840b04ab5b9a": "775e6f308341475a54686ec7912b5fe6f98a8aa0",
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
