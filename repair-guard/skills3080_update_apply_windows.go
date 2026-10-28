package main

import (
	"os"
	"path/filepath"
	"regexp"
)

func skillsRuntimeApply(op, path string, raw []byte, r skillsRequest, reg skillsRegistry, s *skillsFS, ids []int) object {
	fail := func(reason string) object {
		return object{"qualified": false, "reason": reason, "data_access_granted": false}
	}
	u := r.Runtime
	if u == nil || u.PeerReview == nil || !regexp.MustCompile("^[0-9a-f]{32}$").MatchString(u.OperationNonce) {
		return fail("review")
	}
	recovery := op == "recover-skills3080-update"
	baselinePath := skillsRuntimeTarget
	if recovery {
		if _, e := os.Lstat(skillsUpdateBackupPath(u.OperationNonce)); e == nil {
			baselinePath = skillsUpdateBackupPath(u.OperationNonce)
		}
	}
	q := skillsRuntimeQualificationRead(r, reg, s, func() ([]byte, bool) { return s.read(baselinePath, 64<<20) })
	if q["qualified"] != true {
		return q
	}
	var original *os.File
	for _, f := range s.files {
		if same(f.Name(), baselinePath) {
			original = f
		}
	}
	if original == nil {
		return fail("source")
	}
	id, sd, ok := skillsUpdateSnapshot(original)
	if !ok {
		return fail("source")
	}
	// Qualification retained every review/source/candidate buffer. Release only
	// the baseline's read handle so the transaction can reopen it with DELETE.
	// Its actual identity, SHA and metadata must all still match after reopening.
	if original.Close() != nil {
		return fail("source")
	}
	for i, f := range s.files {
		if f == original {
			s.files = append(s.files[:i], s.files[i+1:]...)
			break
		}
	}
	fresh, ok := skillsRegistryRead(ids)
	if !ok || !skillsActorCheck(r.Writer, fresh, s, r.Writer.Job) {
		return fail("writer")
	}
	request := skillsUpdateRequest{Nonce: u.OperationNonce, Writer: r.Writer.Job, OldSHA: str(q["current_sha256"]), NewSHA: u.CandidateSHA, Source: u.SourceHead, Reviewed: u.ReviewedHead, SDDL: sd, OriginalID: id, Record: filepath.Join(filepath.Dir(path), "deployment-operation.json")}
	var result skillsUpdateResult
	if recovery {
		result = skillsUpdateRecoverVerified(request)
	} else {
		if q["already_current"] == true {
			q["installed"] = true
			return q
		}
		result = skillsUpdateApplyVerified(request)
	}
	action := "apply"
	if recovery {
		action = "recover"
	}
	saved := skillsUpdateSaveOutcome(request.Record, action, result)
	qualified := skillsUpdateOutcomeQualified(result, recovery, saved)
	return object{"qualified": qualified, "domain": "skills-3080-runtime-update-v1", "todo_id": 3686, "target_kind": "guard", "installed": result.Installed, "recovered": recovery && result.Restored, "source_head": u.SourceHead, "reviewed_head": u.ReviewedHead, "old_sha256": request.OldSHA, "new_sha256": u.CandidateSHA, "backup": skillsUpdateBackupPath(u.OperationNonce), "stage": skillsUpdateStagePath(u.OperationNonce), "owner_group_dacl_equal": qualified, "sacl_verified": false, "native": result, "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false}
}
func skillsUpdateOutcomeQualified(result skillsUpdateResult, recovery, saved bool) bool {
	return saved && (result.Installed || (recovery && result.Restored))
}
