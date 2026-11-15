//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
)

// Fixed facts from one attempt remain independent when failures overlap.
type unslopHistoryFacts struct {
	ChildObserved bool `json:"child_observed"`
	Started bool `json:"started"`
	WaitVerified bool `json:"wait_verified"`
	Reaped bool `json:"reaped"`
	Assigned bool `json:"assigned"`
	CollectionReason string `json:"collection_reason,omitempty"`
	CollectionObserved uint32 `json:"collection_observed"`
	CollectionSignalled uint32 `json:"collection_signalled"`
	CollectionTotal uint32 `json:"collection_total"`
	CollectionTotalKnown bool `json:"collection_total_known"`
	CollectionRootMatched bool `json:"collection_root_matched"`
	TimedOut bool `json:"timed_out"`
	Cancelled bool `json:"cancelled"`
	StdinCopyStarted bool `json:"stdin_copy_started"`
	StdinCopyFinished bool `json:"stdin_copy_finished"`
	StdinCopyFailed bool `json:"stdin_copy_failed"`
	StdinCloseAttempted bool `json:"stdin_close_attempted"`
	StdinCloseCompleted bool `json:"stdin_close_completed"`
	StdinCloseFailed bool `json:"stdin_close_failed"`
	StdinReaderCloseFailed bool `json:"stdin_reader_close_failed"`
	ExitCode *int `json:"exit_code,omitempty"`
	ProtocolReason string `json:"protocol_reason,omitempty"`
	WritesComplete bool `json:"writes_complete"`
	WriteFailed bool `json:"write_failed"`
	IOFailed bool `json:"io_failed"`
	AcquisitionFailed bool `json:"acquisition_failed"`
	PartialAcquisition bool `json:"partial_acquisition"`
	ResourceCleanupFailed bool `json:"resource_cleanup_failed"`
	PartialCleanupFailed bool `json:"partial_cleanup_failed"`
	CleanupAttempted int `json:"cleanup_attempted"`
	CleanupUnconfirmed int `json:"cleanup_unconfirmed"`
}

type unslopHistoryFailure struct { reason string; facts unslopHistoryFacts; cause error }
func (e *unslopHistoryFailure) Error() string { return e.reason }
func (e *unslopHistoryFailure) Unwrap() error { return e.cause }

func safeUnslopHistoryReason(reason string) string {
	switch reason {
	case "history_protocol_plan_invalid", "history_protocol_failed", "history_protocol_canceled",
		"history_protocol_stdin_unavailable", "history_protocol_untracked_stdin", "history_protocol_stdin_writer_invalid", "history_protocol_stdin_write_failed",
		"history_protocol_stdin_state_invalid", "history_protocol_output_limit", "history_protocol_frame_limit",
		"history_protocol_response_unexpected", "history_protocol_response_invalid", "history_protocol_notification_limit",
		"history_protocol_stdout_incomplete", "history_protocol_frame_incomplete", "history_protocol_response_incomplete",
		"history_protocol_collection_incomplete", "history_protocol_complete", "history_protocol_closed", "history_protocol_io_failed":
		return reason
	default: return "history_protocol_unavailable"
	}
}

func historyFailureFacts(err error) (unslopHistoryFacts, bool) {
	var failure *unslopHistoryFailure
	if errors.As(err,&failure) && failure!=nil {return failure.facts,true}
	return unslopHistoryFacts{},false
}

func safeUnslopHistoryFailureReason(reason string) string {
	switch reason {
	case "history_operation_failed","history_launcher_unqualified","history_capture_unavailable",
		"history_protocol_unavailable","history_process_collection_unavailable","history_response_size_invalid",
		"history_response_json_invalid","history_response_count_incomplete","history_cancelled",
		"history_acquisition_failed","history_io_failed","history_child_exit_failed","history_resource_cleanup_failed":
		return reason
	default:return "history_operation_failed"
	}
}

func projectUnslopHistoryFailure(result *unslopOriginInspection, err error) {
	facts,ok:=historyFailureFacts(err)
	if !ok {return}
	copy:=facts
	if copy.ProtocolReason!="" {copy.ProtocolReason=safeUnslopHistoryReason(copy.ProtocolReason)}
	if copy.CollectionReason!="history_job_collected" && copy.CollectionReason!="history_job_collection_unavailable" {copy.CollectionReason=""}
	result.History=&copy
	var failure *unslopHistoryFailure
	if errors.As(err,&failure) {result.Reason=safeUnslopHistoryFailureReason(failure.reason)}
}

func observeUnslopHistoryChild(facts *unslopHistoryFacts, child askChildResult) {
	facts.ChildObserved=true
	facts.Started=child.Started
	facts.WaitVerified=child.WaitVerified
	facts.Reaped=child.Reaped
	facts.Assigned=child.Assigned
	facts.CollectionObserved=child.CollectionObserved
	facts.CollectionSignalled=child.CollectionSignalled
	facts.CollectionTotal=child.CollectionTotal
	facts.CollectionTotalKnown=child.CollectionTotalKnown
	if child.CollectionReason=="history_job_collected" || child.CollectionReason=="history_job_collection_unavailable" {facts.CollectionReason=child.CollectionReason}
	facts.CollectionRootMatched=child.Capture!=nil && child.Capture.NativeProcess!=nil && child.CollectionRootPID>0 && child.CollectionRootCreation>0 && child.Capture.NativeProcess.PID==child.CollectionRootPID && child.Capture.NativeProcess.Creation==child.CollectionRootCreation
	facts.TimedOut=child.TimedOut
	facts.Cancelled=child.TimedOut
	if child.Started && child.WaitVerified {exit:=child.Exit;facts.ExitCode=&exit}
	if child.Capture!=nil {
		facts.ProtocolReason=safeUnslopHistoryReason(child.Capture.protocolReason)
		facts.WritesComplete=child.Capture.protocolWritesComplete
		facts.WriteFailed=child.Capture.protocolWriteFailed
		facts.IOFailed=child.Capture.protocolIOFailed
		stdin:=child.Capture.historyStdin
		facts.StdinCopyStarted=stdin.CopyStarted
		facts.StdinCopyFinished=stdin.CopyFinished
		facts.StdinCopyFailed=stdin.CopyFailed
		facts.StdinCloseAttempted=stdin.CloseAttempted
		facts.StdinCloseCompleted=stdin.CloseCompleted
		facts.StdinCloseFailed=stdin.CloseFailed
		facts.StdinReaderCloseFailed=stdin.ReaderCloseFailed
	}
}

func noteUnslopHistoryAcquisition(facts *unslopHistoryFacts, err error) {
	facts.AcquisitionFailed=true
	report:=softwareCleanupFacts(err)
	facts.PartialAcquisition=report.Attempted>0
	facts.PartialCleanupFailed=report.Failed>0
	facts.CleanupAttempted+=report.Attempted
	facts.CleanupUnconfirmed+=report.Failed
}

// Complete all owned cleanup without replacing native or protocol facts.
func finishUnslopHistoryOutcome(answers []json.RawMessage, previous error, facts unslopHistoryFacts, resources []io.Closer) ([]json.RawMessage,error) {
	answers,_,err:=finishUnslopHistoryOutcomeObserved(answers,previous,facts,resources)
	return answers,err
}

func finishUnslopHistoryOutcomeObserved(answers []json.RawMessage, previous error, facts unslopHistoryFacts, resources []io.Closer) ([]json.RawMessage,unslopHistoryFacts,error) {
	report:=closeSoftwareOwnedResources(resources)
	facts.CleanupAttempted+=report.Attempted
	facts.CleanupUnconfirmed+=report.Failed
	facts.ResourceCleanupFailed=facts.ResourceCleanupFailed||report.Failed>0
	if errors.Is(previous,context.Canceled)||errors.Is(previous,context.DeadlineExceeded) {facts.Cancelled=true}
	failed:=facts.Cancelled||previous!=nil||facts.ResourceCleanupFailed||facts.PartialCleanupFailed||facts.AcquisitionFailed||
		facts.ChildObserved&&(!facts.Started||!facts.WaitVerified||!facts.Reaped||!facts.Assigned||facts.CollectionReason!="history_job_collected"||!facts.CollectionTotalKnown||facts.CollectionTotal==0||facts.CollectionObserved!=facts.CollectionTotal||facts.CollectionSignalled!=facts.CollectionObserved||!facts.CollectionRootMatched||facts.TimedOut||facts.ExitCode==nil||*facts.ExitCode!=0||!facts.WritesComplete||facts.WriteFailed||facts.IOFailed||!facts.StdinCopyStarted||!facts.StdinCopyFinished||facts.StdinCopyFailed||!facts.StdinCloseAttempted||!facts.StdinCloseCompleted||facts.StdinCloseFailed||facts.StdinReaderCloseFailed||facts.ProtocolReason!="history_protocol_complete")
	if !failed {return answers,facts,nil}
	for _,answer:=range answers {clear(answer)}
	reason:="history_operation_failed"
	if previous!=nil {
		switch previous.Error() {
		case "history_launcher_unqualified","history_capture_unavailable","history_protocol_unavailable","history_process_collection_unavailable",
			"history_response_size_invalid","history_response_json_invalid","history_response_count_incomplete": reason=previous.Error()
		}
	}
	if facts.Cancelled {reason="history_cancelled"}
	if facts.AcquisitionFailed {reason="history_acquisition_failed"}
	if facts.ProtocolReason!=""&&facts.ProtocolReason!="history_protocol_complete" {reason="history_protocol_unavailable"}
	if facts.IOFailed {reason="history_io_failed"}
	if facts.ChildObserved&&facts.Started&&facts.ExitCode!=nil&&*facts.ExitCode!=0 {reason="history_child_exit_failed"}
	if facts.TimedOut {reason="history_cancelled"}
	if facts.ResourceCleanupFailed||facts.PartialCleanupFailed {reason="history_resource_cleanup_failed"}
	if facts.ChildObserved&&facts.Started&&!facts.Reaped {reason="history_process_collection_unavailable"}
	return nil,facts,&unslopHistoryFailure{reason:reason,facts:facts,cause:previous}
}

// Cmd.Wait can prefer ExitError over copier errors. This branch owns stdin
// and requires its actual copy/close facts independently of the native exit.
func unslopHistoryIOFailed(waitErr error, writeFailed, stdoutEOF, stderrEOF bool, stdin unslopHistoryStdinFacts) bool {
	if !stdin.CopyStarted || !stdin.CopyFinished || stdin.CopyFailed || !stdin.CloseAttempted || !stdin.CloseCompleted || stdin.CloseFailed || stdin.ReaderCloseFailed || writeFailed || !stdoutEOF || !stderrEOF {return true}
	if waitErr==nil {return false}
	_,nativeExitOnly:=waitErr.(*exec.ExitError)
	return !nativeExitOnly
}
