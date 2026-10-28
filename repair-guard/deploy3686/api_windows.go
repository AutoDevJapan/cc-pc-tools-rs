package deploy3686

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// This library has no executable entry. The guard supplies these values only
// after its real actor, both reviews, merged source and candidate have qualified.
type Request struct {
	Nonce, Writer, OldSHA, NewSHA, Source, Reviewed, SDDL, Record string
	OriginalID                                                    [3]uint32
}
type Result = result

func SaveOutcome(record, action string, r Result) bool {
	if action != "apply" && action != "recover" {
		return false
	}
	p, e := parentAt(filepath.Dir(record))
	if e != nil {
		return false
	}
	defer p.close()
	f, e := openAt(p.last, filepath.Base(record)+"."+action+"-outcome.json", false, false, true)
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

func Snapshot(f *os.File) ([3]uint32, string, bool) {
	id, e := identity(f)
	sd, e2 := sddl(f)
	return id, sd, e == nil && e2 == nil
}
func BackupPath(nonce string) string {
	return filepath.Join(filepath.Dir(fixedTarget), "scrub391.todo3686-"+nonce+".backup.exe")
}
func StagePath(nonce string) string {
	return filepath.Join(filepath.Dir(fixedTarget), "scrub391.todo3686-"+nonce+".staging.exe")
}
func requestOperation(r Request) operation {
	return operation{ID: r.Nonce, Writer: r.Writer, Target: fixedTarget, Stage: StagePath(r.Nonce), Backup: BackupPath(r.Nonce), Old: r.OldSHA, New: r.NewSHA, Source: r.Source, Reviewed: r.Reviewed, SDDL: r.SDDL, State: "before_stage_and_handle_move", ExpectedOldID: r.OriginalID}
}
func ApplyVerified(r Request) Result {
	o := requestOperation(r)
	if !validOperation(o) || r.OriginalID == [3]uint32{} {
		return result{Reason: "operation_binding"}
	}
	return applyAt(o, r.Record, hooks{})
}
func RecoverVerified(r Request) Result {
	o := requestOperation(r)
	if !validOperation(o) {
		return result{Reason: "operation_binding"}
	}
	return recoverAt(o, r.Record)
}

type journal struct {
	Operation           operation
	OriginalID, StageID [3]uint32
	State               string
}

func recoverAt(o operation, record string) (r result) {
	r.Reason = "recovery_not_verified"
	r.Journal = record + ".native.json"
	jp, e := parentAt(filepath.Dir(record))
	if e != nil {
		return
	}
	defer jp.close()
	jf, e := openAt(jp.last, filepath.Base(r.Journal), false, false, false)
	if e != nil {
		return
	}
	defer jf.Close()
	d := json.NewDecoder(io.LimitReader(jf, (64<<10)+1))
	d.DisallowUnknownFields()
	var j journal
	if d.Decode(&j) != nil || d.Decode(new(any)) != io.EOF || j.State != "pinned_before_move" || j.OriginalID == [3]uint32{} || j.StageID == [3]uint32{} || j.OriginalID == j.StageID {
		return
	}
	expected := o
	expected.ExpectedOldID = [3]uint32{}
	if j.Operation != expected {
		return
	}
	p, e := parentAt(filepath.Dir(o.Target))
	if e != nil {
		return
	}
	defer p.close()
	var original *os.File
	if f, e := openAt(p.last, filepath.Base(o.Backup), false, true, false); e == nil {
		original = f
	}
	if original != nil {
		defer original.Close()
		id, e := identity(original)
		sha, ok := digest(original)
		sd, e2 := sddl(original)
		if e != nil || e2 != nil || !ok || id != j.OriginalID || sha != o.Old || sd != o.SDDL {
			return
		}
		r.OriginalID = id
		r.BackupID = id
	}
	current, e := openAt(p.last, filepath.Base(o.Target), false, true, false)
	if e == nil {
		defer current.Close()
		id, e := identity(current)
		sha, ok := digest(current)
		sd, e2 := sddl(current)
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
		// Unknown identity/bytes are never moved, replaced or made a baseline.
		return
	}
	if _, e := os.Lstat(o.Target); !os.IsNotExist(e) || original == nil {
		return
	}
	r.RestoreAttempted = true
	if renameHeld(original, p.last, filepath.Base(o.Target)) != nil {
		r.RestoreFailed = true
		return
	}
	r.Restored = true
	r.Reason = ""
	return
}
