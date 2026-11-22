//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"
)

const watchdogTarget = "D:/CC/Tools/ocx-health-watchdog/ocx_health_watchdog.exe"
const watchdogTask = "CC-OcxHealthWatchdog"
const watchdogLauncher = "D:/CC/Tools/resident-guard/resident-guard.exe"
const watchdogGeneration = "pr773-watchdog"
const watchdogApprovedCommit = "d336007366cee8bbc5bde89b2c23a3e8acdb4565"
const watchdogApprovedCrate = "347374c46ad1925b2afd4fd015742cc59dc0d2c8"
const watchdogCargoManifest = "4d0ac6b922b76f926c669030ab0ae2bfa638a7fa"
const watchdogCargoLock = "20170140f49703a7af648ca2cd8e434b49a131fc"

var errWatchdogUnavailable = errors.New("watchdog_distribution_issuer_unavailable")

func watchdogHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// JSON records describe observations. Only the live resources returned by the
// normal issuer may reach an installed target; a record never issues a lease.
type watchdogFileIdentity struct {
	Volume uint32
	File uint64
	Created uint64
	Bytes int64
	Links uint32
	SHA256 string
	SecuritySHA256 string
}

func (i watchdogFileIdentity) valid() bool {
	return i.Volume != 0 && i.File != 0 && i.Created != 0 && i.Bytes > 0 &&
		i.Bytes <= 64<<20 && i.Links == 1 && watchdogHex(i.SHA256, 64) &&
		watchdogHex(i.SecuritySHA256, 64)
}

type watchdogOrigin struct {
	InstallReceiptSHA256 string
	BuildReceiptSHA256 string
	SourceCommit string
	Target string
	Installed watchdogFileIdentity
}

func (o watchdogOrigin) valid() bool {
	return o.Target == watchdogTarget && o.Installed.valid() &&
		watchdogHex(o.InstallReceiptSHA256, 64) &&
		watchdogHex(o.BuildReceiptSHA256, 64) && watchdogHex(o.SourceCommit, 40)
}

type watchdogTaskView struct {
	Name string
	XMLSHA256 string
	S4U bool
	Limited bool
	Executable string
	Arguments string
	WorkingDirectory string
	ResidentGuardDisabled bool
	CreditWatchdogDisabled bool
}

func (v watchdogTaskView) valid() bool {
	return v.Name == watchdogTask && watchdogHex(v.XMLSHA256, 64) && v.S4U &&
		v.Limited && same(v.Executable, watchdogLauncher) &&
		v.Arguments == "--launch-service opencodex_watchdog" &&
		same(v.WorkingDirectory, "D:/CC/Tools/resident-guard") &&
		v.ResidentGuardDisabled && v.CreditWatchdogDisabled
}

type watchdogStopped struct {
	// Ownership is of the original issued task generation, including its
	// fully collected never-started or already-exited published successor.
	// This field does not claim that a current running instance exists.
	OwnedGeneration bool
	AllReaped bool
	HoldAcquired bool
}

func (v watchdogStopped) valid() bool {
	return v.OwnedGeneration && v.AllReaped && v.HoldAcquired
}

type watchdogSample struct {
	At time.Time
	WatchdogPID uint32
	Healthy bool
	ObservationKind string
	IdentityPresent bool
	MetricsPresent bool
	Eligible bool
	Failures uint32
	ErrorPresent bool
	WriteFailures uint64
}

type watchdogObservation struct {
	// This proof refers to the watchdog's own task instance and mapped image.
	// It is entirely separate from the untrusted proxy health response.
	OwnInstanceVerified bool
	OwnImage watchdogFileIdentity
	PID uint32
	PreviousPID uint32
	Started time.Time
	Published time.Time
	TerminationOrServiceEvent bool
	Samples []watchdogSample
}

func (v watchdogObservation) valid(expected watchdogFileIdentity) bool {
	if !v.OwnInstanceVerified || v.OwnImage != expected || v.PID == 0 || v.PreviousPID == 0 ||
		v.PID == v.PreviousPID || v.Started.Before(v.Published) ||
		v.TerminationOrServiceEvent || len(v.Samples) < 6 {
		return false
	}
	var previous time.Time
	for _, s := range v.Samples {
		if s.At.Before(v.Started) || !s.At.After(previous) || s.WatchdogPID != v.PID ||
			!s.Healthy || s.ObservationKind != "health_only" ||
			s.IdentityPresent || s.MetricsPresent || s.Eligible ||
			s.Failures != 0 || s.ErrorPresent || s.WriteFailures != 0 {
			return false
		}
		previous = s.At
	}
	return previous.Sub(v.Samples[0].At) >= 50*time.Second &&
		previous.Sub(v.Published) <= 150*time.Second
}

type watchdogSteps struct {
	StageCreated bool `json:"stage_created"`
	OldRetireAttempted bool `json:"old_retire_attempted"`
	OldRetired bool `json:"old_retired"`
	PublishAttempted bool `json:"publish_attempted"`
	Published bool `json:"published"`
	NamespaceUnknown bool `json:"namespace_unknown"`
	Verified bool `json:"verified"`
	CandidateRetired bool `json:"candidate_retired"`
	RestoreAttempted bool `json:"restore_attempted"`
	Restored bool `json:"restored"`
	PublicName string `json:"public_name_state"`
}

// The resident adapter owns the exact task instance, stop proof, child
// collection, launch hold and bounded observation. Implementations must hold
// real resources; JSON fields or a caller boolean are not a normal adapter.
type watchdogResident interface {
	Task(context.Context) (watchdogTaskView, error)
	StopAndHold(context.Context) (watchdogStopped, error)
	ReleaseHold(context.Context) error
	Observe(context.Context, watchdogFileIdentity) (watchdogObservation, error)
	StopPublishedAndHold(context.Context, watchdogFileIdentity) (watchdogStopped, error)
	Close() error
}

type watchdogTransaction interface {
	// Normal Check is the separately qualified baseline read. Stage acquires
	// the DELETE-capable native transaction only after StopAndHold. Tests may
	// use the native transaction directly on owned nonrunning fixture files.
	Check(context.Context) error
	Stage(context.Context) error
	Publish(context.Context) error
	Rollback(context.Context) error
	Candidate() watchdogFileIdentity
	Steps() watchdogSteps
	Close() error
}

type watchdogUpdateResources struct {
	Origin watchdogOrigin
	Artifact watchdogFileIdentity
	// Recheck binds both resources to one currently issued update generation.
	Recheck func(context.Context) error
	Files watchdogTransaction
	Resident watchdogResident
	closed bool
	closeErr error
}

type watchdogUpdateOpener func(context.Context) (*watchdogUpdateResources, error)

func (r *watchdogUpdateResources) close() error {
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	// Close every acquired object once, including opener partials. Preserve the
	// live references on the caller's stack until both close attempts finish.
	var es []error
	if r.Resident != nil {
		es = append(es, r.Resident.Close())
	}
	if r.Files != nil {
		es = append(es, r.Files.Close())
	}
	r.closeErr = errors.Join(es...)
	return r.closeErr
}

type watchdogUpdateResult struct {
	Domain string `json:"domain"`
	Qualified bool `json:"qualified"`
	Reason string `json:"reason,omitempty"`
	Updated bool `json:"update_performed"`
	ObservationAccepted bool `json:"observation_accepted"`
	RollbackAttempted bool `json:"rollback_attempted"`
	RollbackSucceeded bool `json:"rollback_succeeded"`
	ReconciliationRequired bool `json:"reconciliation_required"`
	CleanupFailed bool `json:"cleanup_failed"`
	Unreaped bool `json:"unreaped"`
	StopAttempted bool `json:"stop_attempted"`
	RecoveryReason string `json:"recovery_reason,omitempty"`
	RecoveryCleanupFailed bool `json:"recovery_cleanup_failed"`
	TaskUnchanged bool `json:"task_unchanged"`
	Steps watchdogSteps `json:"steps"`
	DataAccessGranted bool `json:"data_access_granted"`
	GeneralGuardChanged bool `json:"general_guard_changed"`
	retained *watchdogUpdateResources
}

func runWatchdogUpdate(ctx context.Context, apply bool, open watchdogUpdateOpener) (result watchdogUpdateResult) {
	result.Domain = "watchdog-software-update"
	result.Reason = "watchdog_context_invalid"
	if ctx == nil || open == nil || ctx.Err() != nil {
		return
	}
	r, err := open(ctx)
	if r != nil {
		defer func() {
			if stop,ok:=r.Resident.(interface{StopAttempted()bool});ok{result.StopAttempted=stop.StopAttempted()}
			if collection,ok:=r.Resident.(interface{CollectionPending()bool});ok{
				result.Unreaped=collection.CollectionPending()
			}
			if r.Files != nil {
				result.Steps = r.Files.Steps()
				result.Updated = result.Steps.Published
			}
			if e := r.close(); e != nil {
				result.retained = r
				result.CleanupFailed = true
				result.ReconciliationRequired = true
				result.Qualified = false
				if result.Reason == "" {
					result.Reason = "watchdog_cleanup_failed"
				}
			}
		}()
	}
	result.Reason = "watchdog_distribution_issuer_unavailable"
	if err != nil || r == nil {
		return
	}
	if !r.Origin.valid() || !r.Artifact.valid() || r.Recheck == nil ||
		r.Files == nil || r.Resident == nil {
		result.Reason = "watchdog_origin_or_artifact_unbound"
		return
	}
	recheck := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return r.Recheck(ctx)
	}
	result.Reason = "watchdog_generation_changed"
	if recheck() != nil {
		return
	}
	result.Reason = "watchdog_preflight_failed"
	if r.Files.Check(ctx) != nil {
		return
	}
	before, err := r.Resident.Task(ctx)
	if err != nil || !before.valid() {
		result.Reason = "watchdog_task_definition_unverified"
		return
	}
	taskEqual := func(c context.Context) bool {
		after, e := r.Resident.Task(c)
		return e == nil && after.valid() && after == before
	}
	result.TaskUnchanged = true
	if !apply {
		if recheck() != nil || !taskEqual(ctx) || ctx.Err() != nil {
			result.TaskUnchanged = false
			result.Reason = "watchdog_check_final_recheck_failed"
			return
		}
		result.Reason = ""
		result.Qualified = true // check only: never a serialized apply permit.
		return
	}
	held := false
	// StopAndHold or ReleaseHold may partially change lifetime state even on
	// error. Close is still attempted by the outer defer and reports cleanup.
	recoverFailure := func(reason string) {
		result.Reason = reason
		steps := r.Files.Steps()
		if !steps.OldRetired {
			result.ReconciliationRequired = steps.NamespaceUnknown
			return
		}
		result.ReconciliationRequired = true
		result.RollbackAttempted = true
		// Cancellation does not silently suppress recovery after namespace work.
		// This detached context is finite and permits only this generation's undo.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if !held {
			stop, e := r.Resident.StopPublishedAndHold(recovery, r.Files.Candidate())
			if e != nil || !stop.valid() {
				result.RecoveryReason = "watchdog_candidate_stop_unconfirmed"
				return
			}
			held = true
		}
		if r.Recheck(recovery) != nil {
			result.RecoveryReason = "watchdog_recovery_generation_changed"
			return
		}
		if r.Files.Rollback(recovery) != nil {
			result.RecoveryReason = "watchdog_rollback_failed"
			return
		}
		result.TaskUnchanged = taskEqual(recovery)
		if !result.TaskUnchanged {
			result.RecoveryReason = "watchdog_task_definition_changed"
			return
		}
		result.RollbackSucceeded = true
		result.ReconciliationRequired = false
		held = false
		if r.Resident.ReleaseHold(recovery) != nil {
			result.RecoveryReason = "watchdog_recovery_hold_release_failed"
			result.RecoveryCleanupFailed = true
			result.ReconciliationRequired = true
		}
		// Remains unsuccessful even when the predecessor was restored.
	}
	if recheck() != nil {
		result.Reason = "watchdog_generation_changed"
		return
	}
	stopped, err := r.Resident.StopAndHold(ctx)
	if err != nil || !stopped.valid() {
		result.Reason = "watchdog_owned_stop_unconfirmed"
		result.ReconciliationRequired = true
		return
	}
	held = true
	if recheck() != nil {
		result.Reason = "watchdog_generation_changed"
		return
	}
	if r.Files.Stage(ctx) != nil {
		result.Reason = "watchdog_stage_failed"
		return
	}
	stage := r.Files.Candidate()
	if !stage.valid() || stage.SHA256 != r.Artifact.SHA256 || stage.Bytes != r.Artifact.Bytes ||
		stage.SecuritySHA256 != r.Origin.Installed.SecuritySHA256 ||
		(stage.Volume == r.Origin.Installed.Volume && stage.File == r.Origin.Installed.File) {
		result.Reason = "watchdog_stage_not_same_artifact"
		return
	}
	if recheck() != nil {
		result.Reason = "watchdog_generation_changed"
		return
	}
	if r.Files.Publish(ctx) != nil {
		recoverFailure("watchdog_publish_or_postcheck_failed")
		return
	}
	if !taskEqual(ctx) {
		result.TaskUnchanged = false
		recoverFailure("watchdog_task_definition_changed")
		return
	}
	// A failed release may have released the hold; never infer quiescence from
	// an error. Recovery must reacquire it through the exact owned instance.
	held = false
	if r.Resident.ReleaseHold(ctx) != nil {
		recoverFailure("watchdog_hold_release_failed")
		return
	}
	observed, err := r.Resident.Observe(ctx, r.Files.Candidate())
	if err != nil || !observed.valid(r.Files.Candidate()) {
		recoverFailure("watchdog_postpublish_observation_failed")
		return
	}
	if recheck() != nil {
		recoverFailure("watchdog_final_generation_changed")
		return
	}
	if !taskEqual(ctx) {
		result.TaskUnchanged = false
		recoverFailure("watchdog_final_recheck_failed")
		return
	}
	if ctx.Err() != nil {
		recoverFailure("watchdog_final_context_expired")
		return
	}
	result.ObservationAccepted = true
	result.Reason = ""
	result.Qualified = true
	return
}

// Neither the predecessor origin, formal build product nor the resident stop
// adapter has been issued yet. Do not fill these from installed bytes, a path,
// JSON, a past clear verdict or the other tool's Guard-target issuer.
func openNormalWatchdogUpdate(ctx context.Context) (*watchdogUpdateResources, error) {
	product,err:=openNormalWatchdogArtifact(ctx)
	if err!=nil{
		if product!=nil{return &watchdogUpdateResources{Files:&watchdogProductFiles{product:product}},err}
		return nil,err
	}
	return openWatchdogUpdateFromProduct(ctx,product)
}

func watchdogCommand(mode string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var result watchdogUpdateResult
	if mode == "--watchdog-build" {
		product, err := produceWatchdogBuild(ctx, openNormalWatchdogBuild, "")
		result = watchdogUpdateResult{Domain: "watchdog-build-producer", Reason: "watchdog_build_issuer_unavailable"}
		if product != nil {
			result.CleanupFailed = product.Close() != nil
		}
		if err == nil {
			// The current normal issuer cannot reach this branch. Do not turn a
			// future partial implementation into an implicitly accepted build.
			result.Reason = "watchdog_build_product_handoff_unconnected"
		}
	} else {
		result = runWatchdogUpdate(ctx, mode == "--watchdog-apply", openNormalWatchdogUpdate)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return 1
	}
	if !result.Qualified {
		return 1
	}
	return 0
}
