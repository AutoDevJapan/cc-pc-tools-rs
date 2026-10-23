package main

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const normalCIIntent = `D:\CC\Tools\product-ci\todo2737-normal-ci.intent.private.json`

type uncreatedIntent struct {
	Instance string           `json:"instance"`
	Attempt  string           `json:"attempt"`
	RunID    int64            `json:"run_id"`
	Source   sourcepacket.Pin `json:"source"`
}

type uncreatedCleanup struct {
	VMGone               bool  `json:"vm_gone"`
	RegistrationGone     bool  `json:"registration_gone"`
	RegistrationMayExist bool  `json:"registration_may_exist"`
	RunnerID             int64 `json:"runner_id"`
	OfficialCISuccess    bool  `json:"official_ci_success"`
}

func decodeRecoveryJSON(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 65536 {
		return errors.New("recovery receipt size rejected")
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte{239, 187, 191})))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("recovery receipt rejected")
	}
	return nil
}

func validateUncreatedRecovery(attempt string, q linuxJobRequest, intent uncreatedIntent, vm, journal vmState, clean uncreatedCleanup) error {
	if !filepath.IsAbs(attempt) || filepath.Clean(attempt) != attempt || !validInstance(intent.Instance) || filepath.Base(attempt) != "job-"+intent.Instance || intent.Attempt != attempt || intent.RunID != q.RunID || intent.Source != q.Source || !validSourcePin(q.Source) || q.RunID < 1 || q.Attempt < 1 || q.Role != "ci-gate" || len(q.Inputs) != 0 {
		return errors.New("uncreated job recovery binding rejected")
	}
	want := vmState{Instance: intent.Instance, Name: "cc-ci-2737-" + intent.Instance, Root: attempt, Pipe: `\\.\pipe\cc-ci-2737-` + intent.Instance}
	if vm != want {
		return errors.New("uncreated VM intent changed")
	}
	want.Stage = "intent"
	if journal != want || clean.VMGone || !clean.RegistrationGone || clean.RegistrationMayExist || clean.RunnerID != 0 || clean.OfficialCISuccess {
		return errors.New("recovery requires an uncreated, unregistered failed job")
	}
	return nil
}

// This explicit recovery has no VM deletion or runner registration capability.
// It releases only the original intent after fresh physical absence and queue
// proofs. The original failed cleanup receipt remains untouched.
func recoverUncreatedJob(ctx context.Context, attempt string) error {
	if _, e := linkedExecutorIdentity(); e != nil {
		return e
	}
	lock, e := takeNormalCILock(`D:\CC\Tools\product-ci\todo2737-normal-ci.lock`)
	if e != nil {
		return e
	}
	defer lock.Close()
	q, e := readJobRequest(filepath.Join(filepath.Dir(attempt), "ci-gate-request.private.json"))
	if e != nil {
		return e
	}
	raw, e := os.ReadFile(normalCIIntent)
	if e != nil {
		return e
	}
	var intent uncreatedIntent
	var vm, journal vmState
	var clean uncreatedCleanup
	if decodeRecoveryJSON(raw, &intent) != nil {
		return errors.New("original normal CI intent rejected")
	}
	for name, out := range map[string]any{"vm-intent.private.json": &vm, "vm-recovery.private.json": &journal, "job-cleanup.private.json": &clean} {
		b, e := os.ReadFile(filepath.Join(attempt, name))
		if e != nil || decodeRecoveryJSON(b, out) != nil {
			return errors.New("uncreated job receipts unavailable")
		}
	}
	if e = validateUncreatedRecovery(attempt, q, intent, vm, journal, clean); e != nil {
		return e
	}
	// Reject linked ancestors before the elevated observer consumes these paths.
	for p := attempt; ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil || !s.IsDir() || s.Mode()&os.ModeSymlink != 0 || s.Mode()&os.ModeIrregular != 0 {
			return errors.New("recovery path is not a physical directory")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if _, e = bindOfficialJob(ctx, attempt, q, 0, ""); e != nil {
		return e
	}
	runners, e := originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range runners {
		if r.Name == "todo2737-"+intent.Instance {
			return errors.New("uncreated job has a runner registration")
		}
	}
	name := "uncreated-absence-" + newID()
	body := vmPrelude(vm) + `
$boundary=[IO.Path]::GetFullPath($root).TrimEnd('\')+'\'
$disk=[IO.Path]::GetFullPath((Join-Path $root 'guest.vhdx'))
foreach($v in @(Get-VM -ErrorAction Stop)) {
  if($v.Name -ceq $name -or $v.Notes -ceq $notes -or [IO.Path]::GetFullPath($v.ConfigurationLocation).StartsWith($boundary,[StringComparison]::OrdinalIgnoreCase)) {throw 'UNCREATED_VM_PRESENT'}
  foreach($d in @(Get-VMHardDiskDrive -VM $v -ErrorAction Stop)) {
    if($d.Path -and [IO.Path]::GetFullPath($d.Path).Equals($disk,[StringComparison]::OrdinalIgnoreCase)) {throw 'UNCREATED_DISK_IN_USE'}
  }
}
if((Get-VHD -Path $disk -ErrorAction Stop).Attached){throw 'UNCREATED_DISK_ATTACHED'}
`
	if e = admin(ctx, attempt, name, body); e != nil {
		return e
	}
	// Preserve a durable proof before touching the canonical intent. The lock
	// excludes every normal controller; exact bytes also prevent stale release.
	sum := sha256.Sum256(raw)
	proof := map[string]any{"instance": intent.Instance, "run_id": q.RunID, "vm_gone": true, "disk_detached": true, "registration_gone": true, "runner_id": 0, "official_ci_success": false, "intent_sha256": hex.EncodeToString(sum[:]), "physical_observer": name}
	if e = record(attempt, name+".private.json", proof); e != nil {
		return e
	}
	now, e := os.ReadFile(normalCIIntent)
	if e != nil || !bytes.Equal(now, raw) {
		return errors.New("original normal CI intent changed during recovery")
	}
	if e = os.Remove(normalCIIntent); e != nil {
		return e
	}
	return record(attempt, "uncreated-intent-released-"+strings.TrimPrefix(name, "uncreated-absence-")+".private.json", proof)
}
