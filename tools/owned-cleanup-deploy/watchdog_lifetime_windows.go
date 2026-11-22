//go:build windows

package main

import (
	"context"
	"errors"
	"time"
)

var errWatchdogCollectionUnknown = errors.New("watchdog_lifetime_collection_unconfirmed")

// A live collection resource comes from the normal owner of the entire task
// generation. It must cover membership from before any root/child can run,
// prevent escape and prove every member collected. Process snapshots, task
// instance count, a JSON record and caller booleans cannot implement this proof.
// Implementations retain their same-generation resources on any partial/error.
// These methods do not grant a target purpose or issue a build artifact.
type watchdogLifetimeCollection interface {
	BeforeStop(context.Context, watchdogTaskSnapshot, []*watchdogProcess) error
	ConfirmStopped(context.Context, watchdogTaskSnapshot) error
	BeforeRelease(context.Context, watchdogTaskSnapshot, watchdogFileIdentity, time.Time) error
	ConfirmPublishedStopped(context.Context, watchdogTaskSnapshot, watchdogFileIdentity, time.Time) error
	// This acquires the normal owner's real launch/watch hold after proving the
	// same published generation completely quiescent, including never-started
	// and already-exited roots. Zero task instances alone must never issue it.
	AcquireQuiescentHold(context.Context, watchdogTaskSnapshot, watchdogFileIdentity, time.Time) (watchdogLifetimeHold, error)
	Close() error
}

type watchdogLifetimeHold interface {
	Release(context.Context) error
	Close() error
}

// The registered task has not supplied its normal lifetime/Job owner. Do not
// manufacture that ownership by attaching a running snapshot to a fresh Job.
// The current normal entry refuses before any stop; a future issuer must supply
// the above live contract and receive its own source and owned OS acceptance.
func openNormalWatchdogLifetime(context.Context, watchdogTaskSnapshot, []*watchdogProcess) (watchdogLifetimeCollection, error) {
	return nil, errors.New("watchdog_lifetime_issuer_unavailable")
}

// Native Stop can have an unknown effect even when its command returns error.
// Query-handle Close is never a substitute for full-generation collection.
// Check-only observations intentionally leave required=false.
type watchdogCollectionState struct {
	stopAttempted bool
	required bool
	confirmed bool
}
func (s *watchdogCollectionState) begin(stop bool) {
	s.stopAttempted = s.stopAttempted || stop
	s.required = true
	s.confirmed = false
}
func (s *watchdogCollectionState) confirm() { s.confirmed = true }
func (s watchdogCollectionState) cleanupError() error {
	if s.required && !s.confirmed { return errWatchdogCollectionUnknown }
	return nil
}

// Each recovery hold is closed at most once, including a nonnil partial returned
// with an error. A later successful Close cannot erase a failed Release.
type watchdogLifetimeHoldState struct {
	hold watchdogLifetimeHold
	releaseAttempted bool
	releaseErr error
	closeAttempted bool
	closeErr error
}
func (s *watchdogLifetimeHoldState) release(ctx context.Context) error {
	if s==nil||s.hold==nil{return errors.New("watchdog_recovery_hold_missing")}
	if !s.releaseAttempted { s.releaseAttempted=true; s.releaseErr=s.hold.Release(ctx) }
	return s.releaseErr
}
func (s *watchdogLifetimeHoldState) close() error {
	if s==nil{return nil}
	if !s.closeAttempted { s.closeAttempted=true; if s.hold!=nil{s.closeErr=s.hold.Close()} }
	return errors.Join(s.releaseErr,s.closeErr)
}

// Kept separate from handle-close errors and the primary operation reason.
func (r *watchdogResidentNative) CollectionPending() bool {
	return r!=nil&&r.collection.required&&!r.collection.confirmed
}

func (r *watchdogResidentNative) StopAttempted() bool { return r!=nil&&r.collection.stopAttempted }
