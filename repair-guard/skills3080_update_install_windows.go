package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// This library has no executable entry. The guard supplies these values only
// after its real actor, both reviews, merged source and candidate have qualified.
type skillsUpdateRequest struct {
	Nonce, Writer, OldSHA, NewSHA, Source, Reviewed, SDDL, Record string
	OriginalID                                                    [3]uint32
}
type skillsUpdateResultAlias = skillsUpdateResult

func skillsUpdateSaveOutcome(record, action string, r skillsUpdateResultAlias) bool {
	if action != "apply" && action != "recover" {
		return false
	}
	p, e := skillsUpdateParentAt(filepath.Dir(record))
	if e != nil {
		return false
	}
	defer p.close()
	f, e := skillsUpdateOpenAt(p.last, filepath.Base(record)+"."+action+"-outcome.json", false, false, true)
	if e != nil {
		return false
	}
	defer f.Close()
	b, e := json.Marshal(r)
	if e != nil {
		return false
	}
	n, e := f.Write(b)
	return e == nil && n == len(b) && f.Sync() == nil
}

func skillsUpdateSnapshot(f *os.File) ([3]uint32, string, bool) {
	id, e := skillsUpdateIdentity(f)
	sd, e2 := skillsUpdateSDDL(f)
	return id, sd, e == nil && e2 == nil
}
func skillsUpdateBackupPath(nonce string) string {
	return filepath.Join(filepath.Dir(skillsRuntimeTarget), "scrub391.todo3686-"+nonce+".backup.exe")
}
func skillsUpdateStagePath(nonce string) string {
	return filepath.Join(filepath.Dir(skillsRuntimeTarget), "scrub391.todo3686-"+nonce+".staging.exe")
}
func skillsUpdateRequestOperation(r skillsUpdateRequest) skillsUpdateOperation {
	return skillsUpdateOperation{ID: r.Nonce, Writer: r.Writer, Target: skillsRuntimeTarget, Stage: skillsUpdateStagePath(r.Nonce), Backup: skillsUpdateBackupPath(r.Nonce), Old: r.OldSHA, New: r.NewSHA, Source: r.Source, Reviewed: r.Reviewed, SDDL: r.SDDL, State: "before_stage_and_handle_move", ExpectedOldID: r.OriginalID}
}
func skillsUpdateApplyVerified(r skillsUpdateRequest) skillsUpdateResultAlias {
	o := skillsUpdateRequestOperation(r)
	if !skillsUpdateValidOperation(o) || r.OriginalID == [3]uint32{} {
		return skillsUpdateResult{Reason: "operation_binding"}
	}
	return skillsUpdateApplyAt(o, r.Record, skillsUpdateHooks{})
}
func skillsUpdateRecoverVerified(r skillsUpdateRequest) skillsUpdateResultAlias {
	o := skillsUpdateRequestOperation(r)
	if !skillsUpdateValidOperation(o) {
		return skillsUpdateResult{Reason: "operation_binding"}
	}
	return skillsUpdateRecoverAt(o, r.Record)
}

type skillsUpdateJournal struct {
	Operation           skillsUpdateOperation
	OriginalID, StageID [3]uint32
	State               string
}

func skillsUpdateRecoverAt(o skillsUpdateOperation, record string) (r skillsUpdateResult) {
	r.Reason = "recovery_not_verified"
	r.Journal = record + ".native.json"
	jp, e := skillsUpdateParentAt(filepath.Dir(record))
	if e != nil {
		return
	}
	defer jp.close()
	jf, e := skillsUpdateOpenAt(jp.last, filepath.Base(r.Journal), false, false, false)
	if e != nil {
		return
	}
	defer jf.Close()
	d := json.NewDecoder(io.LimitReader(jf, (64<<10)+1))
	d.DisallowUnknownFields()
	var j skillsUpdateJournal
	if d.Decode(&j) != nil || d.Decode(new(any)) != io.EOF || j.State != "pinned_before_move" || j.OriginalID == [3]uint32{} || j.StageID == [3]uint32{} || j.OriginalID == j.StageID {
		return
	}
	expected := o
	expected.ExpectedOldID = [3]uint32{}
	if j.Operation != expected {
		return
	}
	p, e := skillsUpdateParentAt(filepath.Dir(o.Target))
	if e != nil {
		return
	}
	defer p.close()
	var original *os.File
	if f, e := skillsUpdateOpenAt(p.last, filepath.Base(o.Backup), false, true, false); e == nil {
		original = f
	}
	if original != nil {
		defer original.Close()
		id, e := skillsUpdateIdentity(original)
		sha, ok := skillsUpdateDigest(original)
		sd, e2 := skillsUpdateSDDL(original)
		if e != nil || e2 != nil || !ok || id != j.OriginalID || sha != o.Old || sd != o.SDDL {
			return
		}
		r.OriginalID = id
		r.BackupID = id
	}
	current, e := skillsUpdateOpenAt(p.last, filepath.Base(o.Target), false, true, false)
	if e == nil {
		defer current.Close()
		id, e := skillsUpdateIdentity(current)
		sha, ok := skillsUpdateDigest(current)
		sd, e2 := skillsUpdateSDDL(current)
		if e != nil || e2 != nil || !ok || sd != o.SDDL {
			return
		}
		if id == j.OriginalID && sha == o.Old {
			r.OriginalID = id
			r.Restored = true
			r.Reason = ""
			return
		}
		if id == j.StageID && sha == o.New && original != nil {
			r.Installed = true
			r.Published = true
			r.StageID = id
			r.OldMoved = true
			r.Reason = ""
			return
		}
		// Unknown skillsUpdateIdentity/bytes are never moved, replaced or made a baseline.
		return
	}
	if _, e := os.Lstat(o.Target); !os.IsNotExist(e) || original == nil {
		return
	}
	r.RestoreAttempted = true
	if skillsUpdateRenameHeld(original, p.last, filepath.Base(o.Target)) != nil {
		r.RestoreFailed = true
		return
	}
	r.Restored = true
	r.Reason = ""
	return
}
