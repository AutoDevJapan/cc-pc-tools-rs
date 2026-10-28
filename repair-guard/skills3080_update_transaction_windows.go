// Native software transaction library; there is no standalone executable.
// Only the guard invokes it after its complete dual-review qualification.
// It grants no data access and exposes no arbitrary target or replace skillsUpdateOperation.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"strings"
)

type skillsUpdateOperation struct {
	ExpectedOldID [3]uint32 "json:\"-\""
	ID            string    "json:\"operation_id\""
	Writer        string    "json:\"writer_job\""
	Target        string    "json:\"target\""
	Stage         string    "json:\"stage\""
	Backup        string    "json:\"backup\""
	Old           string    "json:\"expected_old_sha256\""
	New           string    "json:\"expected_new_sha256\""
	Source        string    "json:\"source_head\""
	Reviewed      string    "json:\"reviewed_head\""
	SDDL          string    "json:\"owner_group_dacl_before\""
	State         string    "json:\"state\""
	SACL          bool      "json:\"sacl_verified\""
}
type skillsUpdateResult struct {
	Installed        bool      "json:\"installed\""
	Reason           string    "json:\"reason\""
	OldMoved         bool      "json:\"old_moved\""
	Published        bool      "json:\"candidate_published\""
	RestoreAttempted bool      "json:\"restore_attempted\""
	Restored         bool      "json:\"restored\""
	RestoreFailed    bool      "json:\"restore_failed\""
	OriginalID       [3]uint32 "json:\"original_id\""
	StageID          [3]uint32 "json:\"stage_id\""
	BackupID         [3]uint32 "json:\"backup_id\""
	Journal          string    "json:\"journal\""
}
type skillsUpdateHooks struct {
	afterValidation func() error
	afterOldMove    func() error
	afterPublish    func() error
}

func skillsUpdateDigest(f *os.File) (string, bool) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", false
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, (64<<20)+1))
	return hex.EncodeToString(h.Sum(nil)), e == nil && n > 0 && n <= 64<<20
}
func skillsUpdateSamePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
func skillsUpdateParseOperation(raw []byte) (skillsUpdateOperation, bool) {
	var o skillsUpdateOperation
	keys := map[string]bool{"operation_id": false, "writer_job": false, "target": false, "stage": false, "backup": false, "expected_old_sha256": false, "expected_new_sha256": false, "source_head": false, "reviewed_head": false, "owner_group_dacl_before": false, "state": false, "sacl_verified": false}
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, e := d.Token(); e != nil || token != json.Delim('{') {
		return o, false
	}
	for d.More() {
		token, e := d.Token()
		key, ok := token.(string)
		seen, known := keys[key]
		if e != nil || !ok || !known || seen {
			return o, false
		}
		keys[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return o, false
		}
	}
	if token, e := d.Token(); e != nil || token != json.Delim('}') {
		return o, false
	}
	if d.Decode(new(any)) != io.EOF {
		return o, false
	}
	for _, seen := range keys {
		if !seen {
			return o, false
		}
	}
	if json.Unmarshal(raw, &o) != nil {
		return o, false
	}
	return o, true
}
func skillsUpdateValidOperation(o skillsUpdateOperation) bool {
	hexN := func(s string, n int) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(s) == n && len(b) == n/2 && strings.ToLower(s) == s
	}
	base := "scrub391.todo3686-" + o.ID
	return hexN(o.ID, 32) && hexN(o.Old, 64) && hexN(o.New, 64) && o.Old != o.New && hexN(o.Source, 40) && hexN(o.Reviewed, 40) &&
		regexp.MustCompile("^20[0-9]{6}-[0-9]{6}-todo-3686$").MatchString(o.Writer) &&
		o.State == "before_stage_and_handle_move" && !o.SACL && o.SDDL != "" && len(o.SDDL) <= 64<<10 &&
		skillsUpdateSamePath(o.Target, skillsRuntimeTarget) && skillsUpdateSamePath(o.Stage, filepath.Join(filepath.Dir(skillsRuntimeTarget), base+".staging.exe")) &&
		skillsUpdateSamePath(o.Backup, filepath.Join(filepath.Dir(skillsRuntimeTarget), base+".backup.exe"))
}
func skillsUpdateWriteJournal(record string, o skillsUpdateOperation, r skillsUpdateResult) bool {
	p, e := skillsUpdateParentAt(filepath.Dir(record))
	if e != nil {
		return false
	}
	defer p.close()
	name := filepath.Base(record) + ".native.json"
	f, e := skillsUpdateOpenAt(p.last, name, false, false, true)
	if e != nil {
		return false
	}
	defer f.Close()
	b, e := json.MarshalIndent(struct {
		Operation           skillsUpdateOperation
		OriginalID, StageID [3]uint32
		State               string
	}{o, r.OriginalID, r.StageID, "pinned_before_move"}, "", "  ")
	if e != nil {
		return false
	}
	n, e := f.Write(b)
	return e == nil && n == len(b) && f.Sync() == nil
}

// Tests supply synthetic paths and deterministic boundary callbacks. Production
// passes only the fixed-target skillsUpdateOperation validated above and no callbacks.
// Both rename operations use the held source objects, a held physical parent,
// and ReplaceIfExists=false. No cooperating writer lock is needed for integrity.
func skillsUpdateApplyAt(o skillsUpdateOperation, record string, h skillsUpdateHooks) (r skillsUpdateResult) {
	r.Reason = "transaction_not_verified"
	r.Journal = record + ".native.json"
	parent, e := skillsUpdateParentAt(filepath.Dir(o.Target))
	if e != nil {
		return
	}
	defer parent.close()
	old, e := skillsUpdateOpenAt(parent.last, filepath.Base(o.Target), false, true, false)
	if e != nil {
		return
	}
	defer old.Close()
	stage, e := skillsUpdateOpenAt(parent.last, filepath.Base(o.Stage), false, true, false)
	if e != nil {
		return
	}
	defer stage.Close()
	oldSHA, oldOK := skillsUpdateDigest(old)
	newSHA, newOK := skillsUpdateDigest(stage)
	oldSD, oldE := skillsUpdateSDDL(old)
	newSD, newE := skillsUpdateSDDL(stage)
	r.OriginalID, e = skillsUpdateIdentity(old)
	if e != nil {
		return
	}
	r.StageID, e = skillsUpdateIdentity(stage)
	if e != nil {
		return
	}
	if (o.ExpectedOldID != [3]uint32{} && r.OriginalID != o.ExpectedOldID) || !oldOK || !newOK || oldSHA != o.Old || newSHA != o.New || oldSD != o.SDDL || newSD != o.SDDL || oldE != nil || newE != nil {
		return
	}
	if !skillsUpdateWriteJournal(record, o, r) {
		r.Reason = "journal_unavailable"
		return
	}
	// A failed publication never replaces an intervening new target. Restore
	// only by no-replace rename of the same proven original held object.
	defer func() {
		if r.Installed || !r.OldMoved {
			return
		}
		r.RestoreAttempted = true
		if r.Published {
			if skillsUpdateRenameHeld(stage, parent.last, filepath.Base(o.Stage)) != nil {
				r.RestoreFailed = true
				return
			}
			r.Published = false
		}
		if skillsUpdateRenameHeld(old, parent.last, filepath.Base(o.Target)) != nil {
			r.RestoreFailed = true
			return
		}
		r.Restored = true
	}()
	if h.afterValidation != nil && h.afterValidation() != nil {
		return
	}
	if skillsUpdateRenameHeld(old, parent.last, filepath.Base(o.Backup)) != nil {
		r.Reason = "backup_collision_or_move_failed"
		return
	}
	r.OldMoved = true
	if h.afterOldMove != nil && h.afterOldMove() != nil {
		return
	}
	if skillsUpdateRenameHeld(stage, parent.last, filepath.Base(o.Target)) != nil {
		r.Reason = "target_collision_or_publish_failed"
		return
	}
	r.Published = true
	if h.afterPublish != nil && h.afterPublish() != nil {
		return
	}
	oldSHA, oldOK = skillsUpdateDigest(old)
	newSHA, newOK = skillsUpdateDigest(stage)
	oldSD, oldE = skillsUpdateSDDL(old)
	newSD, newE = skillsUpdateSDDL(stage)
	r.BackupID, e = skillsUpdateIdentity(old)
	if e != nil {
		return
	}
	stageID, e := skillsUpdateIdentity(stage)
	if e != nil || !oldOK || !newOK || oldSHA != o.Old || newSHA != o.New || oldSD != o.SDDL || newSD != o.SDDL || oldE != nil || newE != nil || r.BackupID != r.OriginalID || stageID != r.StageID {
		return
	}
	r.Installed = true
	r.Reason = ""
	return
}
