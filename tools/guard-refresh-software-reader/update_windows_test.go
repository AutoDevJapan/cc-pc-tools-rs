//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestFixedUpdatePurposeIsWiredBeforeAnySoftwareRead(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	code := run([]string{"--verify-refresh-guard-update"}, &out, func() softwareObservation {
		t.Fatal("old-only observation is not update qualification")
		return softwareObservation{}
	}, updateOperations{verify: func() updateResult { calls++; return updateResult{Qualified: true} }})
	if code != 0 || calls != 1 {
		t.Fatal("normal fixed software update qualification is not available")
	}
}

type fakeUpdateObject struct {
	raw     []byte
	id      softwareIdentity
	acl     string
	failACL bool
	closed  bool
	write   func(string, []byte, string) (softwareIdentity, error)
}

func (o *fakeUpdateObject) Raw() []byte          { return o.raw }
func (o *fakeUpdateObject) ID() softwareIdentity { return o.id }
func (o *fakeUpdateObject) ACL() (string, error) {
	if o.failACL {
		return "", errors.New("owned_fault")
	}
	return o.acl, nil
}
func (o *fakeUpdateObject) Close() error { o.closed = true; return nil }
func (o *fakeUpdateObject) CreateNew(p string, raw []byte, acl string) (softwareIdentity, error) {
	return o.write(p, raw, acl)
}

func TestFixedSoftwarePrepareValidatesAndConsumesSameHeldBytes(t *testing.T) {
	for _, name := range []string{"verify", "prepare", "old", "proof", "candidate", "collision_stage", "collision_backup", "acl", "identity", "first_write", "second_write"} {
		t.Run(name, func(t *testing.T) {
			creates := []string{}
			readPaths := []string{}
			objects := []*fakeUpdateObject{}
			old := &fakeUpdateObject{raw: []byte("old"), id: softwareIdentity{updateOldVolume, updateOldFileID, updateOldCreated}, acl: "owned-acl"}
			proof := &fakeUpdateObject{raw: []byte("proof")}
			candidate := &fakeUpdateObject{raw: []byte("candidate")}
			old.write = func(p string, raw []byte, acl string) (softwareIdentity, error) {
				creates = append(creates, p)
				if p != updateStagePath && p != updateBackupPath {
					t.Fatal("other leaf")
				}
				expected := old.raw
				if p == updateStagePath {
					expected = candidate.raw
				}
				if len(raw) == 0 || &raw[0] != &expected[0] || acl != old.acl {
					t.Fatal("different input buffer")
				}
				id := softwareIdentity{"v", p, "2"}
				if name == "first_write" && len(creates) == 1 || name == "second_write" && len(creates) == 2 {
					return id, errors.New("owned_write_fault")
				}
				return id, nil
			}
			b := updateBoundary{open: func(p string) (updateObject, error) {
				readPaths = append(readPaths, p)
				var o *fakeUpdateObject
				switch p {
				case installedGuardPath:
					o = old
				case updateBuildProofPath:
					if name == "proof" {
						return nil, errors.New("unavailable")
					}
					o = proof
				case updateCandidatePath:
					o = candidate
				default:
					t.Fatal("unexpected read")
				}
				objects = append(objects, o)
				return o, nil
			}, absent: func(p string) bool {
				return !(name == "collision_stage" && p == updateStagePath || name == "collision_backup" && p == updateBackupPath)
			}, oldMatches: func(raw []byte) bool { return string(raw) == "old" && name != "old" }, candidateMatches: func(raw, proofRaw []byte) bool {
				return string(raw) == "candidate" && string(proofRaw) == "proof" && name != "candidate"
			}}
			if name == "acl" {
				old.failACL = true
			}
			if name == "identity" {
				old.id = softwareIdentity{}
			}
			r := executeFixedUpdate(name != "verify", b)
			for _, o := range objects {
				if !o.closed {
					t.Fatal("handle not released")
				}
			}
			switch name {
			case "verify":
				if !r.Qualified || r.Prepared || len(creates) != 0 {
					t.Fatal(r)
				}
			case "prepare":
				if !r.Prepared || !r.Qualified || len(creates) != 2 || r.StageIdentity == nil || r.BackupIdentity == nil {
					t.Fatal(r)
				}
			case "first_write", "second_write":
				if r.Prepared || r.Qualified || r.Reason != "prepare_incomplete_preserved" || len(r.Created) != len(creates) {
					t.Fatal(r)
				}
			default:
				if r.Qualified || r.Prepared || len(creates) != 0 {
					t.Fatal("invalid input reached write", r)
				}
			}
			if r.DataAccessGranted || r.UpdatePerformed {
				t.Fatal("data or replacement grant")
			}
		})
	}
}

func TestFixedSoftwareUpdateBadArgumentsReadAndWriteNothing(t *testing.T) {
	for _, args := range [][]string{nil, {"--prepare-refresh-guard-update=1"}, {"--prepare-refresh-guard-update", updateStagePath}, {"--verify-refresh-guard-update", "--prepare-refresh-guard-update"}, {"--guard-path", updateStagePath}} {
		calls := 0
		f := func() updateResult { calls++; return updateResult{Qualified: true, Prepared: true} }
		var out bytes.Buffer
		if run(args, &out, func() softwareObservation { calls++; return softwareObservation{} }, updateOperations{verify: f, prepare: f}) != 2 || calls != 0 {
			t.Fatal("bad arguments reached authority")
		}
	}
}

func TestUpdateFailureNeverGrantsDataAndKeepsPartialCreation(t *testing.T) {
	var out bytes.Buffer
	r := updateResult{Reason: "prepare_incomplete_preserved", Created: []string{updateBackupPath}}
	if runUpdate([]string{"--prepare-refresh-guard-update"}, &out, updateOperations{prepare: func() updateResult { return r }}) != 1 {
		t.Fatal("failure exit")
	}
	var decoded updateResult
	if json.Unmarshal(out.Bytes(), &decoded) != nil || decoded.DataAccessGranted || decoded.UpdatePerformed || decoded.SoftwareReplaceAllowed || len(decoded.Created) != 1 {
		t.Fatal("failure contract")
	}
}

func TestUpdateWrongBinaryNeverReachesGit(t *testing.T) {
	if candidateMatches([]byte("wrong"), nil, func(...string) ([]byte, bool) { t.Fatal("unverified input reached git"); return nil, false }) {
		t.Fatal("unknown bytes accepted")
	}
	for _, p := range []string{installedGuardPath, updateStagePath + ":stream", updateStagePath + ".other", updateStagePath + ".bad", updateCandidatePath} {
		if sameSoftwareParent(p) {
			t.Fatal("other path admitted")
		}
	}
	if !sameSoftwareParent(updateStagePath) || !sameSoftwareParent(updateBackupPath) {
		t.Fatal("compiled leaves rejected")
	}
}

func TestUpdateOutputFailureIsNotSuccess(t *testing.T) {
	if runUpdate([]string{"--verify-refresh-guard-update"}, failingUpdateWriter{}, updateOperations{verify: func() updateResult { return updateResult{Qualified: true} }}) != 1 {
		t.Fatal("writer failure ignored")
	}
}

type failingUpdateWriter struct{}

func (failingUpdateWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
