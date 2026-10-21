package main

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"io"
	"time"
)

// This operation only materializes and hashes the pinned source. It does not
// execute source commands, Cargo, a workflow or an official runner.
func materializeSource(incoming io.Reader, id string) (result sourceResult) {
	result.ID = id
	raw, e := readBlob(incoming, 256<<20)
	if e != nil {
		result.Failure = "source_frame_rejected"
		return
	}
	p, e := sourcepacket.Decode(raw, fixedSourcePin())
	if e != nil {
		result.Failure = "source_packet_rejected"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	result.Proof, e = sourcepacket.Materialize(ctx, p, fixedSourcePin(), "/home/ci2737/source")
	if e != nil {
		result.Failure = "source_materialization_failed"
	}
	return
}
