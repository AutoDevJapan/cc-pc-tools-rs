package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func skillsUpdateTestSHA(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func skillsUpdateFixture(t *testing.T) (skillsUpdateOperation, string) {
	t.Helper()
	root := t.TempDir()
	old, new := []byte("known old software"), []byte("reviewed new software")
	o := skillsUpdateOperation{ID: strings.Repeat("a", 32), Writer: "20261028-103853-todo-3686", Target: filepath.Join(root, "scrub391.exe"), Stage: filepath.Join(root, "stage.exe"), Backup: filepath.Join(root, "backup.exe"), Old: skillsUpdateTestSHA(old), New: skillsUpdateTestSHA(new), Source: strings.Repeat("b", 40), Reviewed: strings.Repeat("c", 40), State: "before_stage_and_handle_move"}
	for p, b := range map[string][]byte{o.Target: old, o.Stage: new} {
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	f, e := os.Open(o.Target)
	if e != nil {
		t.Fatal(e)
	}
	o.SDDL, e = skillsUpdateSDDL(f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	return o, filepath.Join(root, "deployment-skillsUpdateOperation.json")
}
func skillsUpdateTestRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// Counterfactual uses the very ReplaceFileW skillsUpdateOperation used by v3 File.Replace.
// The explicit Red run demands the property v3 does not have. Ordinary runs
// preserve the witnessed regression and test the new implementation below.
func TestSkills3080LegacyV3ReplacementWindow(t *testing.T) {
	o, _ := skillsUpdateFixture(t)
	if skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Target)) != o.Old {
		t.Fatal("initial old comparison")
	}
	unknown := []byte("concurrent unknown software")
	if os.WriteFile(o.Target, unknown, 0600) != nil {
		t.Fatal("deterministic intervening update")
	}
	target, _ := syscall.UTF16PtrFromString(o.Target)
	stage, _ := syscall.UTF16PtrFromString(o.Stage)
	backup, _ := syscall.UTF16PtrFromString(o.Backup)
	ok, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW").Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(stage)), uintptr(unsafe.Pointer(backup)), 0, 0, 0)
	if ok == 0 {
		t.Fatal("legacy Windows skillsUpdateOperation did not run")
	}
	lost := skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Target)) == o.New && bytes.Equal(skillsUpdateTestRead(t, o.Backup), unknown)
	if !lost {
		t.Fatal("legacy race witness was not reached")
	}
	if os.Getenv("CC_NOTICE_DEPLOY_RED") == "1" {
		t.Fatal("v3 replaced the intervening unknown target after its old SHA check")
	}
	t.Log("legacy race reproduced; new held-object/no-replace tests below protect this boundary")
}

func TestSkills3080DeployHeldObjectsAndSameIdentity(t *testing.T) {
	o, record := skillsUpdateFixture(t)
	writerRejected, renameRejected, parentRejected := false, false, false
	r := skillsUpdateApplyAt(o, record, skillsUpdateHooks{afterValidation: func() error {
		writerRejected = os.WriteFile(o.Target, []byte("foreign"), 0600) != nil
		renameRejected = os.Rename(o.Target, o.Target+".foreign") != nil
		parentRejected = os.Rename(filepath.Dir(o.Target), filepath.Dir(o.Target)+".foreign") != nil
		return nil
	}})
	if !r.Installed || !r.OldMoved || !r.Published || !writerRejected || !renameRejected || !parentRejected || r.BackupID != r.OriginalID {
		t.Fatalf("held skillsUpdateIdentity transaction failed: %+v writer=%v rename=%v parent=%v", r, writerRejected, renameRejected, parentRejected)
	}
	if skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Target)) != o.New || skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Backup)) != o.Old {
		t.Fatal("published or backup bytes differ")
	}
	f, e := os.Open(o.Target)
	if e != nil {
		t.Fatal(e)
	}
	id, e := skillsUpdateIdentity(f)
	sd, se := skillsUpdateSDDL(f)
	f.Close()
	if e != nil || se != nil || id != r.StageID || sd != o.SDDL {
		t.Fatal("new skillsUpdateIdentity or original owner/group/DACL differs")
	}
	if len(skillsUpdateTestRead(t, r.Journal)) == 0 {
		t.Fatal("pre-move skillsUpdateJournal missing")
	}
}
func TestSkills3080DeployPreservesForeignCreationAtPublishBoundary(t *testing.T) {
	o, record := skillsUpdateFixture(t)
	unknown := []byte("foreign target created in publication gap")
	r := skillsUpdateApplyAt(o, record, skillsUpdateHooks{afterOldMove: func() error {
		f, e := os.OpenFile(o.Target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(unknown)
		ce := f.Close()
		if e != nil {
			return e
		}
		return ce
	}})
	if r.Installed || r.Published || !r.OldMoved || !r.RestoreAttempted || !r.RestoreFailed || r.Restored {
		t.Fatalf("collision state wrong: %+v", r)
	}
	if !bytes.Equal(skillsUpdateTestRead(t, o.Target), unknown) || skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Backup)) != o.Old || skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Stage)) != o.New {
		t.Fatal("foreign target, original backup or stage was overwritten")
	}
}
func TestSkills3080DeployFailureRestoresOnlySameKnownObject(t *testing.T) {
	for _, when := range []string{"after_old_move", "after_publish"} {
		t.Run(when, func(t *testing.T) {
			o, record := skillsUpdateFixture(t)
			stop := func() error { return errors.New("injected boundary") }
			h := skillsUpdateHooks{}
			if when == "after_old_move" {
				h.afterOldMove = stop
			} else {
				h.afterPublish = stop
			}
			r := skillsUpdateApplyAt(o, record, h)
			if r.Installed || r.Published || !r.Restored || r.RestoreFailed {
				t.Fatalf("restoration skillsUpdateResult wrong: %+v", r)
			}
			if skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Target)) != o.Old || skillsUpdateTestSHA(skillsUpdateTestRead(t, o.Stage)) != o.New {
				t.Fatal("failure lost known original or staged candidate")
			}
			f, e := os.Open(o.Target)
			if e != nil {
				t.Fatal(e)
			}
			id, e := skillsUpdateIdentity(f)
			f.Close()
			if e != nil || id != r.OriginalID {
				t.Fatal("restore did not use the held original object")
			}
		})
	}
}
func TestSkills3080DeployRejectsChangedOrAliasedInputs(t *testing.T) {
	for _, which := range []string{"unknown_current", "changed_stage", "backup_collision", "journal_collision", "stage_hardlink", "prior_reader"} {
		t.Run(which, func(t *testing.T) {
			o, record := skillsUpdateFixture(t)
			switch which {
			case "unknown_current":
				if os.WriteFile(o.Target, []byte("unknown current"), 0600) != nil {
					t.Fatal("skillsUpdateFixture")
				}
			case "changed_stage":
				if os.WriteFile(o.Stage, []byte("changed stage"), 0600) != nil {
					t.Fatal("skillsUpdateFixture")
				}
			case "backup_collision":
				if os.WriteFile(o.Backup, []byte("foreign backup"), 0600) != nil {
					t.Fatal("skillsUpdateFixture")
				}
			case "journal_collision":
				if os.WriteFile(record+".native.json", []byte("foreign skillsUpdateJournal"), 0600) != nil {
					t.Fatal("skillsUpdateFixture")
				}
			case "stage_hardlink":
				if os.Link(o.Stage, o.Stage+".alias") != nil {
					t.Fatal("skillsUpdateFixture")
				}
			case "prior_reader":
				f, e := os.Open(o.Target)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
			}
			before := skillsUpdateTestRead(t, o.Target)
			stageBefore := skillsUpdateTestRead(t, o.Stage)
			r := skillsUpdateApplyAt(o, record, skillsUpdateHooks{})
			if r.Installed || r.OldMoved || r.Published || !bytes.Equal(skillsUpdateTestRead(t, o.Target), before) || !bytes.Equal(skillsUpdateTestRead(t, o.Stage), stageBefore) {
				t.Fatalf("unverified input changed filesystem: %+v", r)
			}
		})
	}
}
func TestSkills3080DeployProductionBinding(t *testing.T) {
	o := skillsUpdateOperation{ID: strings.Repeat("a", 32), Writer: "20261028-103853-todo-3686", Target: skillsRuntimeTarget, Old: strings.Repeat("b", 64), New: strings.Repeat("c", 64), Source: strings.Repeat("d", 40), Reviewed: strings.Repeat("e", 40), SDDL: "O:skillsUpdateFixture", State: "before_stage_and_handle_move"}
	o.Stage = filepath.Join(filepath.Dir(skillsRuntimeTarget), "scrub391.todo3686-"+o.ID+".staging.exe")
	o.Backup = filepath.Join(filepath.Dir(skillsRuntimeTarget), "scrub391.todo3686-"+o.ID+".backup.exe")
	if !skillsUpdateValidOperation(o) {
		t.Fatal("fixed production tuple rejected")
	}
	for _, field := range []string{"target", "stage", "backup", "actor", "old", "sacl", "state"} {
		c := o
		switch field {
		case "target":
			c.Target = "D:/other.exe"
		case "stage":
			c.Stage += ".other"
		case "backup":
			c.Backup += ".other"
		case "actor":
			c.Writer = "20261028-103853-todo-3080"
		case "old":
			c.Old = c.New
		case "sacl":
			c.SACL = true
		case "state":
			c.State = "other"
		}
		if skillsUpdateValidOperation(c) {
			t.Fatalf("unbound %s accepted", field)
		}
	}
}

func TestSkills3080DeployStrictOperationRecord(t *testing.T) {
	o, _ := skillsUpdateFixture(t)
	raw, e := json.Marshal(o)
	if e != nil {
		t.Fatal(e)
	}
	got, ok := skillsUpdateParseOperation(raw)
	if !ok || got != o {
		t.Fatal("complete exact skillsUpdateOperation record rejected")
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte("\"sacl_verified\":false"), []byte("\"sacl_verified\":false,\"sacl_verified\":false"), 1),
		bytes.Replace(raw, []byte("\"sacl_verified\":false"), []byte("\"SACL_VERIFIED\":false"), 1),
		bytes.Replace(raw, []byte(",\"sacl_verified\":false"), nil, 1),
		append(append([]byte(nil), raw...), []byte("{}")...),
	} {
		if _, ok := skillsUpdateParseOperation(bad); ok {
			t.Fatal("duplicate, alternate spelling, missing flag or trailing data accepted")
		}
	}
}
