package main

import (
	contract "cc/civ6-ci-contract"
	"context"
)

type readAPI func(string, any) error

// TreeEntry and TreeReply mirror the recursive Git tree REST reply used by the
// source materializer. They carry no policy; policy lives in the shared contract.
type TreeEntry struct{ Path, Mode, Type, SHA string }
type TreeReply struct {
	SHA       string
	Truncated bool
	Entries   []TreeEntry `json:"tree"`
}

func blobID(b []byte) string { return objectID("blob", b) }

// verifyWith asks the shared contract to re-check the same fixed plan against
// the live repository (ownership, current branch/PR, official run/attempt/job,
// labels, whole source tree and the three reviewed workflow byte streams).
// The caller supplies the only network capability, so tests need no GitHub.
func verifyWith(ctx context.Context, p Plan, read readAPI) error {
	return contract.Verify(ctx, p.Plan, func(c context.Context, path string, v any) error {
		if e := c.Err(); e != nil {
			return e
		}
		return read(path, v)
	})
}

// verifierFor selects the shared contract check that fits each audit phase:
// Verify before registration, VerifyAssigned (the job must be on this unique
// runner, in progress or completed) while it runs, RequireSuccess (completed,
// success, steps present, on this runner) once it is terminal. The matrix
// GITHUB_JOB is shared by every OS expansion, so only the unique runner name
// identifies the assigned job.
func verifierFor(phase auditPhase) func(context.Context, Plan, readAPI) error {
	return func(ctx context.Context, p Plan, read readAPI) error {
		api := func(c context.Context, path string, v any) error {
			if e := c.Err(); e != nil {
				return e
			}
			return read(path, v)
		}
		switch phase {
		case phaseRunning:
			return contract.VerifyAssigned(ctx, p.Plan, p.name(), api)
		case phaseFinished:
			return contract.RequireSuccess(ctx, p.Plan, p.name(), api)
		}
		return contract.Verify(ctx, p.Plan, api)
	}
}
