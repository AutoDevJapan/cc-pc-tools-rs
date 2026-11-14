//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Only a finite plan crosses this context boundary. The concrete exchange is
// created inside one child supervisor and never supplied by a caller.
type unslopHistoryProtocolKey struct{}

const unslopHistoryFrameLimit = 2 << 20
const unslopHistoryOutputLimit = 8 << 20

var errUnslopHistoryProtocol = errors.New("history_protocol_failed")

type unslopHistoryExchange struct {
	mu sync.Mutex
	ctx context.Context
	steps []unslopHistoryStep
	step, offset int
	awaiting bool
	frame []byte
	total int
	notifications int
	stdoutEOF bool
	reason string
	changed chan struct{}
	waiting chan int
	waitNotified int
	failed chan struct{}
}

func newUnslopHistoryExchange(ctx context.Context, plan unslopHistoryProtocol) (*unslopHistoryExchange, error) {
	if ctx == nil || ctx.Err() != nil || len(plan.Steps) < 2 || len(plan.Steps) > 16 {
		return nil, errUnslopHistoryProtocol
	}
	// Rebuild the closed request vocabulary and compare the canonical bytes.
	// A changed method, initialize payload, ID or arbitrary stdin is not a plan.
	var requests []unslopHistoryRequest
	for i, step := range plan.Steps {
		if step.ResponseID != int64(i+1) || len(step.Input) == 0 || len(step.Input) > 1<<20 {
			return nil, errUnslopHistoryProtocol
		}
		if i == 0 { continue }
		lines := bytes.Split(bytes.TrimSuffix(step.Input, []byte{'\n'}), []byte{'\n'})
		if i == 1 {
			if len(lines) != 2 { return nil, errUnslopHistoryProtocol }
			lines = lines[1:]
		} else if len(lines) != 1 { return nil, errUnslopHistoryProtocol }
		var request struct {
			ID int64
			Method string
			Params json.RawMessage
		}
		decoder := json.NewDecoder(bytes.NewReader(lines[0]))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.ID != step.ResponseID {
			return nil, errUnslopHistoryProtocol
		}
		requests = append(requests, unslopHistoryRequest{request.Method, request.Params})
	}
	canonical, err := buildUnslopHistoryProtocol(requests)
	if err != nil { return nil, errUnslopHistoryProtocol }
	defer canonical.erase()
	if len(canonical.Steps) != len(plan.Steps) { return nil, errUnslopHistoryProtocol }
	x := &unslopHistoryExchange{ctx:ctx, changed:make(chan struct{},1), waiting:make(chan int,len(canonical.Steps)), waitNotified:-1, failed:make(chan struct{})}
	for i, step := range canonical.Steps {
		if !bytes.Equal(step.Input, plan.Steps[i].Input) { x.Close(); return nil, errUnslopHistoryProtocol }
		x.steps = append(x.steps, unslopHistoryStep{Input:append([]byte(nil), step.Input...), ResponseID:step.ResponseID})
	}
	return x, nil
}

func (p *unslopHistoryProtocol) erase() {
	for i := range p.Steps { clear(p.Steps[i].Input) }
	p.Steps = nil
}

func (x *unslopHistoryExchange) signalLocked() {
	select { case x.changed <- struct{}{}: default: }
}

func (x *unslopHistoryExchange) failLocked(reason string) {
	if x.reason != "" { return }
	x.reason = reason
	clear(x.frame); x.frame = nil
	close(x.failed)
	x.signalLocked()
}

// exec.Cmd's owned stdin copier calls this concrete Reader. Its next Read is
// blocked until stdout has accepted this same exchange's matching response.
func (x *unslopHistoryExchange) Read(p []byte) (int, error) {
	if len(p) == 0 { return 0, nil }
	for {
		x.mu.Lock()
		if x.ctx.Err() != nil { x.failLocked("history_protocol_canceled") }
		if x.reason != "" { x.mu.Unlock(); return 0, errUnslopHistoryProtocol }
		if x.step == len(x.steps) { x.mu.Unlock(); return 0, io.EOF }
		if !x.awaiting {
			step := &x.steps[x.step]
			n := copy(p, step.Input[x.offset:])
			x.offset += n
			if x.offset == len(step.Input) { x.awaiting = true }
			x.mu.Unlock()
			return n, nil
		}
		if x.waitNotified != x.step { x.waitNotified = x.step; x.waiting <- x.step }
		x.mu.Unlock()
		select {
		case <-x.changed:
		case <-x.ctx.Done():
		}
	}
}

// Only the stdout copier for the same child calls observe. Failure wakes stdin
// and the supervisor, but never stops the existing bounded stdout/stderr drain.
func (x *unslopHistoryExchange) observe(p []byte) {
	x.mu.Lock(); defer x.mu.Unlock()
	if x.reason != "" { return }
	if len(p) > unslopHistoryOutputLimit-x.total { x.failLocked("history_protocol_output_limit"); return }
	x.total += len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		n := len(p)
		if i >= 0 { n = i }
		if n > unslopHistoryFrameLimit-len(x.frame) { x.failLocked("history_protocol_frame_limit"); return }
		x.frame = append(x.frame, p[:n]...)
		p = p[n:]
		if i < 0 { return }
		p = p[1:]
		if x.stdoutEOF { x.failLocked("history_protocol_response_unexpected"); return }
		expected:=int64(x.step+1)
		if x.step==len(x.steps) {expected=int64(len(x.steps))}
		result, notification, err := classifyUnslopHistoryFrame(x.frame, expected)
		clear(result)
		clear(x.frame); x.frame = x.frame[:0]
		if err != nil { x.failLocked("history_protocol_response_invalid"); return }
		if notification {
			x.notifications++
			if x.notifications>32 {x.failLocked("history_protocol_notification_limit");return}
			continue
		}
		if x.step >= len(x.steps) || !x.awaiting { x.failLocked("history_protocol_response_unexpected"); return }
		x.step++; x.offset = 0; x.awaiting = false
		x.signalLocked()
	}
}

func (x *unslopHistoryExchange) endOutput(eof bool) {
	x.mu.Lock(); defer x.mu.Unlock()
	x.stdoutEOF = eof
	if !eof { x.failLocked("history_protocol_stdout_incomplete"); return }
	if len(x.frame) != 0 { x.failLocked("history_protocol_frame_incomplete"); return }
	if x.step != len(x.steps) { x.failLocked("history_protocol_response_incomplete") }
	x.signalLocked()
}

func (x *unslopHistoryExchange) resultReason() string {
	x.mu.Lock(); defer x.mu.Unlock()
	if x.reason != "" { return x.reason }
	if x.stdoutEOF && x.step == len(x.steps) && len(x.frame) == 0 { return "history_protocol_complete" }
	return "history_protocol_collection_incomplete"
}

func (x *unslopHistoryExchange) Close() error {
	x.mu.Lock(); defer x.mu.Unlock()
	if x.reason == "" && (!x.stdoutEOF || x.step != len(x.steps)) { x.failLocked("history_protocol_closed") }
	for i := range x.steps { clear(x.steps[i].Input) }
	clear(x.frame)
	x.frame = nil
	return nil
}

type unslopHistoryCaptureOutput struct {
	capture *askCaptureOutput
	exchange *unslopHistoryExchange
}

func (w *unslopHistoryCaptureOutput) Write(p []byte) (int, error) {
	n, err := w.capture.Write(p)
	w.exchange.observe(p)
	return n, err
}

func (w *unslopHistoryCaptureOutput) ReadFrom(r io.Reader) (int64, error) {
	var scratch [65536]byte
	defer clear(scratch[:])
	var total int64
	for {
		n, err := r.Read(scratch[:])
		if n > 0 { _, _ = w.Write(scratch[:n]); total += int64(n) }
		if err == io.EOF { w.capture.eof = true; w.exchange.endOutput(true); return total, nil }
		if err != nil { w.exchange.endOutput(false); return total, err }
	}
}
