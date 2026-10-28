// This finite installer is invoked only by the reviewed administrator wrapper,
// after the existing scrub391 --verify-skills3080-update has accepted the build.
// It grants no data access and exposes no arbitrary target or replace operation.
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
	"runtime/debug"
	"strings"
)

const fixedTarget = "D:/CC/Tools/client-records/scrub391.exe"
const fixedRecords = "D:/CC/Work/unattended-operations-20261013-wt/todo-3686-skills-3080-a/.ci-work/todo3686"
const modulePath = "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/deploy3686"

type operation struct {
	ID       string "json:\"operation_id\""
	Writer   string "json:\"writer_job\""
	Target   string "json:\"target\""
	Stage    string "json:\"stage\""
	Backup   string "json:\"backup\""
	Old      string "json:\"expected_old_sha256\""
	New      string "json:\"expected_new_sha256\""
	Source   string "json:\"source_head\""
	Reviewed string "json:\"reviewed_head\""
	SDDL     string "json:\"owner_group_dacl_before\""
	State    string "json:\"state\""
	SACL     bool   "json:\"sacl_verified\""
}
type result struct {
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
type hooks struct {
	afterValidation func() error
	afterOldMove    func() error
	afterPublish    func() error
}

func digest(f *os.File) (string, bool) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", false
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, (64<<20)+1))
	return hex.EncodeToString(h.Sum(nil)), e == nil && n > 0 && n <= 64<<20
}
func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func parseOperation(raw []byte) (operation, bool) {
	var o operation
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
func validOperation(o operation) bool {
	hexN := func(s string, n int) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(s) == n && len(b) == n/2 && strings.ToLower(s) == s
	}
	base := "scrub391.todo3686-" + o.ID
	return hexN(o.ID, 32) && hexN(o.Old, 64) && hexN(o.New, 64) && o.Old != o.New && hexN(o.Source, 40) && hexN(o.Reviewed, 40) &&
		regexp.MustCompile("^20[0-9]{6}-[0-9]{6}-todo-3686$").MatchString(o.Writer) &&
		o.State == "before_stage_and_handle_move" && !o.SACL && o.SDDL != "" && len(o.SDDL) <= 64<<10 &&
		samePath(o.Target, fixedTarget) && samePath(o.Stage, filepath.Join(filepath.Dir(fixedTarget), base+".staging.exe")) &&
		samePath(o.Backup, filepath.Join(filepath.Dir(fixedTarget), base+".backup.exe"))
}
func writeJournal(record string, o operation, r result) bool {
	p, e := parentAt(filepath.Dir(record))
	if e != nil {
		return false
	}
	defer p.close()
	name := filepath.Base(record) + ".native.json"
	f, e := openAt(p.last, name, false, false, true)
	if e != nil {
		return false
	}
	defer f.Close()
	b, e := json.MarshalIndent(struct {
		Operation           operation
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
// passes only the fixed-target operation validated above and no callbacks.
// Both rename operations use the held source objects, a held physical parent,
// and ReplaceIfExists=false. No cooperating writer lock is needed for integrity.
func applyAt(o operation, record string, h hooks) (r result) {
	r.Reason = "transaction_not_verified"
	r.Journal = record + ".native.json"
	parent, e := parentAt(filepath.Dir(o.Target))
	if e != nil {
		return
	}
	defer parent.close()
	old, e := openAt(parent.last, filepath.Base(o.Target), false, true, false)
	if e != nil {
		return
	}
	defer old.Close()
	stage, e := openAt(parent.last, filepath.Base(o.Stage), false, true, false)
	if e != nil {
		return
	}
	defer stage.Close()
	oldSHA, oldOK := digest(old)
	newSHA, newOK := digest(stage)
	oldSD, oldE := sddl(old)
	newSD, newE := sddl(stage)
	r.OriginalID, e = identity(old)
	if e != nil {
		return
	}
	r.StageID, e = identity(stage)
	if e != nil {
		return
	}
	if !oldOK || !newOK || oldSHA != o.Old || newSHA != o.New || oldSD != o.SDDL || newSD != o.SDDL || oldE != nil || newE != nil {
		return
	}
	if !writeJournal(record, o, r) {
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
			if renameHeld(stage, parent.last, filepath.Base(o.Stage)) != nil {
				r.RestoreFailed = true
				return
			}
			r.Published = false
		}
		if renameHeld(old, parent.last, filepath.Base(o.Target)) != nil {
			r.RestoreFailed = true
			return
		}
		r.Restored = true
	}()
	if h.afterValidation != nil && h.afterValidation() != nil {
		return
	}
	if renameHeld(old, parent.last, filepath.Base(o.Backup)) != nil {
		r.Reason = "backup_collision_or_move_failed"
		return
	}
	r.OldMoved = true
	if h.afterOldMove != nil && h.afterOldMove() != nil {
		return
	}
	if renameHeld(stage, parent.last, filepath.Base(o.Target)) != nil {
		r.Reason = "target_collision_or_publish_failed"
		return
	}
	r.Published = true
	if h.afterPublish != nil && h.afterPublish() != nil {
		return
	}
	oldSHA, oldOK = digest(old)
	newSHA, newOK = digest(stage)
	oldSD, oldE = sddl(old)
	newSD, newE = sddl(stage)
	r.BackupID, e = identity(old)
	if e != nil {
		return
	}
	stageID, e := identity(stage)
	if e != nil || !oldOK || !newOK || oldSHA != o.Old || newSHA != o.New || oldSD != o.SDDL || newSD != o.SDDL || oldE != nil || newE != nil || r.BackupID != r.OriginalID || stageID != r.StageID {
		return
	}
	r.Installed = true
	r.Reason = ""
	return
}
func run(args []string) result {
	bad := result{Reason: "operation_binding"}
	if len(args) != 2 || args[0] != "--operation" || !filepath.IsAbs(args[1]) || filepath.Base(args[1]) != "deployment-operation.json" {
		return bad
	}
	relative, e := filepath.Rel(fixedRecords, filepath.Dir(args[1]))
	if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return bad
	}
	p, e := parentAt(filepath.Dir(args[1]))
	if e != nil {
		return bad
	}
	defer p.close()
	f, e := openAt(p.last, filepath.Base(args[1]), false, false, false)
	if e != nil {
		return bad
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 {
		return bad
	}
	o, parsed := parseOperation(raw)
	if !parsed || !validOperation(o) || o.Writer != os.Getenv("CC_JOB_ID") {
		return bad
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Path != modulePath {
		return bad
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		if _, exists := settings[s.Key]; exists {
			return bad
		}
		settings[s.Key] = s.Value
	}
	if settings["vcs.revision"] != o.Source || settings["vcs.modified"] != "false" || settings["vcs"] != "git" || settings["GOOS"] != "windows" || settings["GOARCH"] != "amd64" {
		return bad
	}
	return applyAt(o, args[1], hooks{})
}
func main() {
	r := run(os.Args[1:])
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if !r.Installed {
		os.Exit(1)
	}
}
