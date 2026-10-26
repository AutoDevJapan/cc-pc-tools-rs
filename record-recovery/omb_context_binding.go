package main

import (
	"io"
	"time"
)

// These are compatibility claims supplied by a future qualified producer, not
// evidence of its authority or the authenticity of the native source.
type omb3255ContextBinding struct {
	assignmentID, botID, taskID, responseID string
	startedAt, finishedAt                   int64
	executions                              uint32
	status, outcome                         string
	nativeThreadID, nativeTurnID            string
}

type omb3255BoundContextStream interface {
	io.ReadCloser
	Binding() (omb3255ContextBinding, error)
}

func omb3255ProjectOriginal4559(binding omb3255ContextBinding, open func() (omb3255BoundContextStream, error)) omb3255ContextObservation {
	unavailable := omb3255ContextUnavailable()
	if !omb3255Original4559Compatible(binding) || open == nil {
		return unavailable
	}
	stream, err := open()
	if stream == nil {
		return unavailable
	}
	if err != nil {
		_ = stream.Close()
		return unavailable
	}
	before, err := stream.Binding()
	if err != nil || before != binding {
		_ = stream.Close()
		return unavailable
	}
	scope := omb3255ContextScope{binding.nativeThreadID, binding.nativeTurnID,
		time.UnixMilli(binding.startedAt), time.UnixMilli(binding.finishedAt)}
	result := omb3255ProjectContext(scope, stream)
	after, bindingErr := stream.Binding()
	closeErr := stream.Close()
	if bindingErr != nil || after != before || closeErr != nil {
		return unavailable
	}
	// Matching claims do not authenticate their producer or grant access. The
	// projection retains false qualification/verification/grant flags.
	return result
}

func omb3255Original4559Compatible(b omb3255ContextBinding) bool {
	return b.assignmentID == "4559e952-315a-4053-aee9-125d627cb639" &&
		b.botID == "52137063-f227-4e4f-b0ff-c91661cb8950" &&
		b.taskID == "62780a26-01a6-4464-bb87-c7f3fa09df81" &&
		b.responseID == "6c38b9d1-b4a6-4860-a530-71f711bbd8f5" &&
		b.startedAt == 1792848636233 && b.finishedAt == 1792848928582 &&
		b.executions == 1 && b.status == "completed" && b.outcome == "done" &&
		omb3255ContextID(b.nativeThreadID) && omb3255ContextID(b.nativeTurnID) &&
		b.nativeThreadID != b.nativeTurnID
}
