//go:build windows

package main

import (
	"encoding/json"
	"io"
)

// All worker cleanup goes through this finalizer. No intermediate cleanup
// failure writes a protocol record or hides another cleanup attempt.
type softwareWorkerCompletion struct {
	snapshot, config func() error
	run              bool
	ok               bool
}

func (c *softwareWorkerCompletion) close(r *askArtifactResult) bool {
	if c.run {
		return c.ok
	}
	c.run = true
	ok := true
	if c.snapshot != nil {
		if err := c.snapshot(); err != nil {
			ok = false
			r.Reason = "source_snapshot_cleanup_failed_preserved"
		}
	}
	if c.config != nil {
		if err := c.config(); err != nil {
			if ok {
				r.Reason = "compiler_configuration_cleanup_failed"
			} else {
				r.Reason = "multiple_compiler_cleanup_failed_preserved"
			}
			ok = false
		}
	}
	if c.snapshot != nil || c.config != nil {
		r.SnapshotCleanup = &ok
	}
	if !ok {
		r.Qualified = false
	}
	c.ok = ok
	return ok
}

func (c *softwareWorkerCompletion) finish(out io.Writer, r *askArtifactResult, exit *int, build softwareBuildPlan) {
	if !c.close(r) {
		*exit = 1
	}
	// Legacy privacy workers retain their previous successful no-output end.
	// New snapshot workers require exactly one cleanup-confirmed final record.
	if r.Reason == "producer_acknowledged" && build.Source != mp4Source {
		return
	}
	if json.NewEncoder(out).Encode(r) != nil {
		*exit = 1
	}
}
