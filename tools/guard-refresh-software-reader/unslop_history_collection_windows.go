//go:build windows

package main

// Recorded only from the existing strict collector's final native observation.
// These fields report collection; they do not issue a history or packet grant.
type unslopHistoryCollectionObservation struct {
	rootPID uint32
	rootCreation uint64
	observed, signalled, total uint32
	totalKnown, collected bool
}

// Run after the supervisor's deferred handle closes. Never change Reaped here:
// a protocol response or a diagnostic cannot repair an uncollected native Job.
func publishUnslopHistoryCollection(result *askChildResult, observed *unslopHistoryCollectionObservation) {
	if result == nil || observed == nil { return }
	result.CollectionObserved = observed.observed
	result.CollectionSignalled = observed.signalled
	result.CollectionTotal = observed.total
	result.CollectionTotalKnown = observed.totalKnown
	result.CollectionRootPID = observed.rootPID
	result.CollectionRootCreation = observed.rootCreation
	result.CollectionReason = "history_job_collection_unavailable"
	if observed.collected && result.Started && result.Assigned && result.WaitVerified && result.Reaped &&
		observed.totalKnown && observed.total > 0 && observed.observed == observed.total &&
		observed.signalled == observed.observed && observed.rootPID > 0 && observed.rootCreation > 0 {
		result.CollectionReason = "history_job_collected"
	}
}

// A completed exchange alone is never sufficient. Require the native root
// generation and the full strict Job observation from this same child result.
func unslopHistoryChildComplete(result askChildResult) bool {
	if !result.Started || !result.Assigned || !result.WaitVerified || !result.Reaped ||
		result.TimedOut || result.Exit != 0 || result.CollectionReason != "history_job_collected" ||
		!result.CollectionTotalKnown || result.CollectionTotal == 0 ||
		result.CollectionObserved != result.CollectionTotal || result.CollectionSignalled != result.CollectionObserved ||
		result.CollectionRootPID == 0 || result.CollectionRootCreation == 0 || result.Capture == nil {
		return false
	}
	capture := result.Capture
	stdin:=capture.historyStdin
	return stdin.CopyStarted && stdin.CopyFinished && !stdin.CopyFailed && stdin.CloseAttempted && stdin.CloseCompleted && !stdin.CloseFailed && !stdin.ReaderCloseFailed && capture.Complete && !capture.Overflow && capture.protocolReason == "history_protocol_complete" && capture.protocolWritesComplete && !capture.protocolWriteFailed && !capture.protocolIOFailed &&
		capture.NativeProcess != nil && capture.NativeProcess.PID == result.CollectionRootPID &&
		capture.NativeProcess.Creation == result.CollectionRootCreation
}
