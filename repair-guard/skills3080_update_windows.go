package main

import (
	"bytes"
	"debug/buildinfo"
	"strings"
)

type skillsRuntimeRequest struct {
	SourceRoot     string               `json:"source_root"`
	SourceHead     string               `json:"source_head"`
	ReviewedHead   string               `json:"reviewed_head"`
	Candidate      string               `json:"candidate"`
	CandidateSHA   string               `json:"candidate_sha256"`
	PeerReview     *skillsResultRequest `json:"peer_review,omitempty"`
	OperationNonce string               `json:"operation_nonce,omitempty"`
}

const skillsRuntimeTarget = "D:/CC/Tools/client-records/scrub391.exe"
const skillsGuardModule = "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard"

// Independent previously accepted distribution facts (3258 and3686), not a caller
// supplied baseline or an assertion made by the artifact being installed.
var skillsGuardBaselines = map[string]string{
	// Same-buffer PR668 observation precedes this successor candidate.
	official3894SourceWaitPredecessorSHA: official3894SourceWaitPredecessorHead,
	// Same-buffer normal PR662 observation precedes this successor candidate.
	official3894SourceDispatchPredecessorSHA: official3894SourceDispatchPredecessorHead,
	// Independent PR659 same-buffer readback, before this successor exists.
	// docs/evidence/skills-3080-guard-before-runtime-origin.json
	official3894RuntimeOriginPredecessorSHA:  official3894RuntimeOriginPredecessorHead,
	official3894PublicManifestPredecessorSHA: official3894PublicManifestPredecessorHead,
	official3894BootstrapPredecessorSHA:      official3894BootstrapPredecessorHead,
	// Same pre-candidate accepted PR624 observation as the shared updater.
	// docs/evidence/skills-3080-guard-before-native-path-eab175645.json
	official3894CreationPathPredecessorSHA: official3894CreationPathPredecessorHead,
	// PR618's normal update and same-handle readback precede this repair.
	// docs/evidence/skills-3080-guard-before-canonical-assignment-f8b.json
	official3894CanonicalAssignmentPredecessorSHA: official3894CanonicalAssignmentPredecessorHead,
	// Normally deployed PR615, independently read back before the alias successor.
	// docs/evidence/skills-3080-guard-before-source-alias-50c817.json
	"50c817a829ecb33c30e4ee2b129dee3716f822fc4627dd01df6f24cb98a7925b": "34cd8877587596539e8b4a8847468fc74fca8e57",
	// PR610's normal distribution was independently observed by the parent
	// and this writer before building the notice-representation successor.
	// docs/evidence/skills-3080-guard-before-notice-representation175645.json
	"9be9a066053183a099e78133571d394ecbb2845372e3cf603e292c74dac1a8d3": "deb430b8b089fde2391dbd243c01db48de48d493",
	// PR594's normally deployed runtime was independently matched to its public
	// distribution record before the native-log successor was generated.
	// Evidence: docs/evidence/skills-3080-guard-before-native609-16386.json.
	"16386d55e418ce271cfed923edede1e7472bb93aa691b15409bcdb99753b9899": "61de98373f8cb481302394be11d7d75cfef08a52",
	// Same accepted PR596 software as the other existing updater, from normal
	// publisher deployment and its parent's independent installed readback.
	// docs/evidence/official-ci3387-known-guard-baseline-444eae87.json
	official3894MaskedDistributionSHA: official3894MaskedDistributionHead,
	// PR589 was normally reviewed, merged, built and installed by the prior
	// execution of this updater. Its independent current readback was published
	// before generating the masked-source successor candidate.
	// Evidence: docs/evidence/skills-3080-guard-before-masked-source-9f914.json.
	"9f914c724231886438515c5ed3831a13eca3d8fbec09e4d3d6edb1d0113c6dd8": "4e117a2d813e4a989d1c3085d2734963d9c198a3",
	// PR586's other real updater completed this shared distribution while the
	// scoped result-reception repair was under review. Its normal deployment
	// and our independent installed readback precede the successor candidate.
	// Evidence: docs/evidence/skills-3080-guard-before-scope589-ad5.json.
	"ad5d37c7b216264d2e99007a2e6f3468e7529803454d88f1954cc74a7beb4783": "0d97c1952ff6f8b47a3d40939941639b35309408",
	// The normally reviewed, merged, built, qualified and installed PR585
	// distribution was independently read back before its successor was built.
	// Evidence was published first in commit 7f6e68f2; no proposed candidate
	// supplies a predecessor identity or any data-access qualification.
	// Evidence: docs/evidence/skills-3080-guard-installed-81700412.json.
	"94c08be91776df277a6a3442e71f291103345641558657e1872e36520ad924e0": "817004120a458b3a37830478512da0aa8040c9d0",
	// PR582's separately reviewed distribution was already installed before
	// this Skills update was built. Preserve that exact observed software pair;
	// its diagnostics and every native/data boundary remain unchanged.
	// Evidence: docs/evidence/skills-3080-guard-before-issuer-update-c043.json.
	"c0439d9ccab874ac68e8f054a895cb7f5e50165d3adc5b7ae3e4fe8f92ce3081": "7cea2e8f12631aeb5584dab5bc8535b7cd984f8c",
	// PR579 accepted the original OpenAI6 issuance/readback under this exact
	// runtime before this repair was proposed. Preserve the software pair;
	// all native, source, writer and current-object checks remain independent.
	// Evidence: docs/skills-3080-openai6-create-evidence214717.json and
	// docs/skills-3080-guard-current4348-observation214717.json.
	"434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f": "521c4c4dddd7abbb0135d13bffb380ec984374b8",
	// The original craft/codex3 users published this native runtime identity
	// before the later official-CI update. Official CI already preserves the
	// same pair; a different Skills consumer must not lose its original issuer.
	// Evidence: docs/evidence/official-ci3387-known-guard-baseline-f30f9e76.json.
	"02c8cad70e44f2bc4cd4e5a70dfdaf581a7ee6896eb63449fe1606eb28f5b5be": "f30f9e763e25884fa48d1cce0cac22da1cebfb69",
	// PR540's accepted merged-source build, formal Luna reviews and the
	// original parent's native prepare/20-buffer issuance, preserved as issued.
	// Evidence: docs/evidence/skills-3080-guard-installed-a067.json.
	"3a0f5065eb6ae56fdcc0a1f0ef78131f999e2331a0412b01faea91260adffeaa": "a067ff63db64a88fc218fc4c8400552454d5d691",
	// PR536's independently accepted Windows distribution and original
	// parent's 92-buffer receipt; never learned from the proposed new binary.
	// Evidence: docs/evidence/skills-3080-guard-installed-52d417.json.
	"aaf16eac624c3b974835f28c35805c2670d5f51320a2f04d78a4de6edf19b2f9": "52d417e60c20f9770d6bde2dde36092ab206ea01",
	// The subsequent official-CI update is independently reproduced from its
	// public merged source. Only its two Go build action-ID fields differ;
	// both content IDs and every remaining byte match. No PE was rewritten.
	// Evidence: docs/evidence/skills-3080-guard-baseline-f5f.json.
	"f3927b8850258098d54dae87196ae382960dc83a5b6e7dd11354ab97ffd07cdf": "f5f846a8470b544846e4c9ce43306061bf5668f9",
	// PR525 was independently accepted and deployed before the later CI-tool
	// update. Preserve its native issuance without rewriting the old receipt.
	// Evidence: docs/evidence/skills-3080-guard-installed-038bc.json.
	"ccb5e797e6b5627d98ade2a562f2f98f3c121a5403e1523071afc07f73af4556": "038bc2570014132321919bf6758d68979506b741",
	// PR524's reviewed and accepted runtime, independently read back before
	// repairing the creator-reader's handling of an older accepted issuer.
	// Evidence: docs/evidence/skills-3080-guard-installed-60a1.json.
	"916bff404f54548814152efcd186dd772b33fd32872fd86fa88bc8d8718df9e3": "60a1b82b62d60cfcd66136aa4527041af643c4d6",
	// PR520's already accepted installation. The later canonical-key defect
	// is a separate parser repair, not a retroactive claim about its inputs.
	// Evidence: docs/evidence/skills-3080-guard-installed-bb1a.json.
	"1eba87708c1d5ad8cccca60485ef2e78e9cdcee3db24852a914dd32dc2ef9b5c": "bb1a9df569b326bcfcaa14684243aa27479ad7fe",
	// PR517's accepted Windows runtime and original backup were independently
	// read back before the disconnected-reader regression was repaired.
	// Evidence: docs/evidence/skills-3080-guard-installed-3154.json.
	"03a2e0f115c8435b1a876eda3efba75ba6120a668bfed39539f6ef2503c5f165": "3154c8ac9135424e2caf2034dc24d4e7406d6368",
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
	return skillsCodeReviewAcceptedMode(received, head, false)
}
func skillsCodeReviewAcceptedMode(received skillsReceived, head string, strict bool) bool {
	if !strings.Contains(string(received.Spec), "SKILLS3080_IMPLEMENTATION_REVIEW "+head) || len(received.Native) == 0 {
		return false
	}
	model := skillsModel(str(received.Metadata["model"]))
	// The remaining multi-file reviews for this finite repair were explicitly
	// assigned to Luna/max by the owner. Derive origins from the real job, not
	// the reviewer body or caller. Other OAuth models remain valid elsewhere.
	if model != "gpt-6-luna" || received.Metadata["model_origin"] != "owner_request" || received.Metadata["effort_origin"] != "owner_request" {
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
			Domain     string `json:"domain"`
			Head       string `json:"head"`
			Must       *int   `json:"must"`
			Accepted   bool   `json:"accepted"`
			Suggestion *int   `json:"suggestion,omitempty"`
			Nit        *int   `json:"nit,omitempty"`
		}
		if !skillsStrict([]byte(strings.TrimPrefix(line, prefix)), &a) || a.Domain != "skills-3080-code-review-v1" || a.Head != head || a.Must == nil || *a.Must != 0 || !a.Accepted {
			return false
		}
		if (strict && (a.Suggestion == nil || a.Nit == nil)) || (a.Suggestion != nil && *a.Suggestion != 0) || (a.Nit != nil && *a.Nit != 0) {
			return false
		}
	}
	return count == 1
}
func skillsCodeReviewPairAccepted(a, b skillsReceived, head string) bool {
	return skillsCodeReviewAcceptedMode(a, head, true) && skillsCodeReviewAcceptedMode(b, head, true) && str(a.Metadata["id"]) != "" && a.Metadata["id"] != b.Metadata["id"] && str(a.Metadata["thread_id"]) != "" && str(b.Metadata["thread_id"]) != "" && a.Metadata["thread_id"] != b.Metadata["thread_id"]
}

const skillsRuntimeSelfReviewTask, skillsRuntimePeerReviewTask = 4307, 4308

func skillsRuntimeReviewTasks(a, b *skillsResultRequest) bool {
	return a != nil && b != nil && a.Task == skillsRuntimeSelfReviewTask && b.Task == skillsRuntimePeerReviewTask && skillsResultID(*a) && skillsResultID(*b)
}
func skillsRuntimeHistoricalNoticePair(r skillsRequest) bool {
	u := r.Runtime
	if u == nil || r.Review == nil || u.PeerReview == nil {
		return false
	}
	return (r.Review.Task == 3740 && u.PeerReview.Task == 3877 && u.SourceHead == "34cd8877587596539e8b4a8847468fc74fca8e57" && u.ReviewedHead == "3f6ed705d5908524caa3b02f9f0784f5d5320c93" && u.CandidateSHA == "50c817a829ecb33c30e4ee2b129dee3716f822fc4627dd01df6f24cb98a7925b") ||
		(r.Review.Task == 4300 && u.PeerReview.Task == 4301 && u.SourceHead == official3894RuntimeOriginPredecessorHead && u.ReviewedHead == "8736e9ddc4a9362cc2aa3444c0e586118163f243" && u.CandidateSHA == official3894RuntimeOriginPredecessorSHA) ||
		(r.Review.Task == 4203 && u.PeerReview.Task == 4204 && u.SourceHead == official3894PublicManifestPredecessorHead && u.ReviewedHead == "930bf282717e6b78a9a8b1fde7e7a85be944ebeb" && u.CandidateSHA == official3894PublicManifestPredecessorSHA) ||
		(r.Review.Task == 3870 && u.PeerReview.Task == 3876 && u.SourceHead == official3894CanonicalAssignmentPredecessorHead && u.ReviewedHead == "76a3133cee8be5a4a1f8b2b10714b6badb5f1967" && u.CandidateSHA == official3894CanonicalAssignmentPredecessorSHA) ||
		(r.Review.Task == 4198 && u.PeerReview.Task == 4199 && u.SourceHead == official3894CreationPathPredecessorHead && u.ReviewedHead == "4284eef98c916358d68abe134ff057421e82a87d" && u.CandidateSHA == official3894CreationPathPredecessorSHA)
}
func skillsRuntimeQualification(r skillsRequest, reg skillsRegistry, s *skillsFS) object {
	return skillsRuntimeQualificationRead(r, reg, s, nil)
}
func skillsRuntimeQualificationRead(r skillsRequest, reg skillsRegistry, s *skillsFS, loadInstalled func() ([]byte, bool)) object {
	return skillsRuntimeQualificationWithCI(r, reg, s, loadInstalled, skillsRuntimeCI)
}
func skillsRuntimeQualificationWithCI(r skillsRequest, reg skillsRegistry, s *skillsFS, loadInstalled func() ([]byte, bool), loadCI func(*skillsRuntimeRequest) bool) object {
	failure := object{"qualified": false, "reason": "source"}
	u := r.Runtime
	if u == nil || r.Writer.Task != 3686 || r.Result != nil || r.ManifestSHA != "" || r.ManifestPath != "" || r.ManifestCommit != "" || r.Review == nil || !skillsResultID(*r.Review) || !skillsDigest(u.CandidateSHA) || !within(r.Writer.Workspace, u.SourceRoot) || !within(r.Writer.Workspace, u.Candidate) || same(u.Candidate, skillsRuntimeTarget) {
		return failure
	}
	// The same admission is used by verify, apply and recover. A caller-made
	// candidate/request cannot bypass CI by omitting the preparation script.
	if skillsRuntimeNewCIReview(r) {
		if !skillsRuntimeReviewTasks(r.Review, u.PeerReview) {
			return object{"qualified": false, "reason": "review", "data_access_granted": false}
		}
		if loadCI == nil || !loadCI(u) {
			return object{"qualified": false, "reason": "ci", "data_access_granted": false}
		}
	}
	if !skillsMerged(u.SourceHead) || !skillsMerged(u.ReviewedHead) || !sourceVerified(u.SourceRoot) || !modelSourceHead(u.SourceRoot, u.SourceHead) {
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
	if reason != "" || !skillsCodeReviewAcceptedMode(review, u.ReviewedHead, u.PeerReview != nil) {
		return object{"qualified": false, "reason": "review"}
	}
	if u.PeerReview != nil {
		if !skillsRuntimeReviewTasks(r.Review, u.PeerReview) && !skillsRuntimeHistoricalNoticePair(r) {
			return object{"qualified": false, "reason": "review"}
		}
		if !skillsResultID(*u.PeerReview) || u.PeerReview.Task == r.Review.Task || u.PeerReview.Job == r.Review.Job {
			return object{"qualified": false, "reason": "review"}
		}
		peer, reason := skillsReceive(*u.PeerReview, reg, s, false)
		if reason != "" || !skillsCodeReviewPairAccepted(review, peer, u.ReviewedHead) {
			return object{"qualified": false, "reason": "review"}
		}
	}
	candidate, ok := s.read(u.Candidate, 64<<20)
	if !ok || hashBytes(candidate) != u.CandidateSHA {
		return failure
	}
	candidateHead, ok := skillsBuildRevision(candidate)
	if !ok || candidateHead != u.SourceHead {
		return failure
	}
	if loadInstalled == nil {
		loadInstalled = func() ([]byte, bool) { return s.read(skillsRuntimeTarget, 64<<20) }
	}
	installed, ok := loadInstalled()
	if !ok {
		return failure
	}
	currentSHA := hashBytes(installed)
	currentHead, ok := skillsBuildRevision(installed)
	if !ok {
		return failure
	}
	already := currentSHA == u.CandidateSHA
	// Preserve old already-current readback, but never authorize a new update
	// with a single historical review or caller-asserted peer metadata.
	if !already && u.PeerReview == nil {
		return object{"qualified": false, "reason": "review"}
	}
	if !already && !skillsRuntimeReviewTasks(r.Review, u.PeerReview) {
		return object{"qualified": false, "reason": "review"}
	}
	if (!already && skillsGuardBaselines[currentSHA] != currentHead) || (already && currentHead != u.SourceHead) {
		return failure
	}
	return object{"qualified": true, "domain": "skills-3080-runtime-update-v1", "todo_id": 3686, "target_kind": "guard", "current_sha256": currentSHA, "new_sha256": u.CandidateSHA, "source_head": u.SourceHead, "reviewed_head": u.ReviewedHead, "already_current": already, "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false}
}
