package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"errors"
	"io"
	"net"
)

var ErrHostEOF = errors.New("host channel EOF")
var ErrHostCancel = errors.New("host requested cancel")
var ErrServiceStop = errors.New("guest service stopped")
var ErrProtocol = errors.New("host channel frame rejected")

type hookCall struct {
	stage   Stage
	context HookContext
	reply   chan error
}
type processResult struct {
	exit    uint32
	drained bool
	logHash string
	err     error
}

// Only the fixed Windows implementation can execute a real process. Tests use
// synthetic operations; that result is never evidence of VM acceptance.
type guestOps interface {
	prepare(context.Context, sourcepacket.Packet) (sourcepacket.Receipt, error)
	configure(context.Context, []byte) error
	listen(context.Context, int64) <-chan processResult
	proof(context.Context) (sourcepacket.Receipt, error)
	hooks() <-chan hookCall
	close() error
}

func samePlan(a, b Plan) bool {
	return a.Validate() == nil && b.Validate() == nil && a.Source == b.Source && a.Instance == b.Instance && a.RunID == b.RunID && a.JobID == b.JobID && a.Attempt == b.Attempt && a.Jobs == b.Jobs && a.Threads == b.Threads && a.Role == b.Role && a.Name == b.Name && a.Label == b.Label
}

func guestSession(ctx context.Context, c net.Conn, seed Seed, ops guestOps) (ret error) {
	if seed.Validate() != nil {
		return errors.New("seed rejected")
	}
	ctx, cause := context.WithCancelCause(ctx)
	cancel := func() { cause(context.Canceled) }
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	var running <-chan processResult
	defer func() {
		if ret != nil {
			ret = errors.Join(ret, context.Cause(ctx))
		}
		cancel()
		if running != nil {
			result := <-running
			ret = errors.Join(ret, result.err)
			if !result.drained {
				ret = errors.Join(ret, errors.New("cancelled tree drain unconfirmed"))
			}
		} // Windows operations always finish a bounded drain
		ret = errors.Join(ret, ops.close())
	}()
	var p Plan
	if receive(c, &p) != nil || !samePlan(p, seed.Plan) {
		return errors.New("seed plan mismatch")
	}
	raw, e := readFrame(c, maxSource)
	if e != nil {
		return errors.New("source transfer failed")
	}
	packet, e := sourcepacket.Decode(raw, p.Source)
	clear(raw)
	if e != nil {
		return errors.New("source rejected")
	}
	proof, e := ops.prepare(ctx, packet)
	packet = sourcepacket.Packet{}
	if e != nil || !validProof(p, proof) {
		return errors.New("source preparation failed")
	}
	if send(c, Event{Instance: p.Instance, Stage: SourceReady, Proof: proof}) != nil {
		return errors.New("source proof transfer failed")
	}
	// A continuously reading goroutine distinguishes EOF/cancel from a pending
	// hook. Closing the socket unblocks it; it never outlives this session.
	commands := make(chan message)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(commands)
		for {
			var m message
			if e := receive(c, &m); e != nil {
				if errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) {
					cause(ErrHostEOF)
				} else {
					cause(ErrProtocol)
				}
				return
			}
			if m.Cancel {
				clear(m.Token)
				cause(ErrHostCancel)
				return
			}
			select {
			case commands <- m:
			case <-ctx.Done():
				clear(m.Token)
				return
			}
		}
	}()
	defer func() { cancel(); c.Close(); <-readDone }()
	next := func() (message, error) {
		select {
		case m, ok := <-commands:
			if !ok {
				return m, errors.New("host EOF")
			}
			if m.Cancel {
				return m, errors.New("host cancelled")
			}
			return m, nil
		case <-ctx.Done():
			return message{}, context.Cause(ctx)
		}
	}
	acknowledge := func(stage Stage, id int64) error {
		m, e := next()
		if e != nil {
			return e
		}
		if len(m.Token) != 0 || m.Ack.Instance != p.Instance || m.Ack.Stage != stage || !m.Ack.Accepted || m.Ack.RunnerID != id {
			return errors.New("stage acknowledgement rejected")
		}
		return nil
	}
	if e = acknowledge(SourceReady, 0); e != nil {
		return e
	}
	m, e := next()
	if e != nil {
		return e
	}
	if len(m.Token) == 0 || len(m.Token) > 4096 || m.Ack != (ack{}) {
		clear(m.Token)
		return errors.New("configuration rejected")
	}
	e = ops.configure(ctx, m.Token)
	clear(m.Token)
	if e != nil {
		return errors.New("stock runner configuration failed")
	}
	if send(c, Event{Instance: p.Instance, Stage: Configured}) != nil {
		return errors.New("configured transfer failed")
	}
	m, e = next()
	if e != nil {
		return e
	}
	if m.Ack.Instance != p.Instance || m.Ack.Stage != Configured || !m.Ack.Accepted || m.Ack.RunnerID < 1 || len(m.Token) != 0 {
		return errors.New("listener permission rejected")
	}
	runnerID := m.Ack.RunnerID
	running = ops.listen(ctx, runnerID)
	stage := Start
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case _, ok := <-commands:
			if !ok {
				return errors.New("host EOF")
			}
			return errors.New("unexpected host frame or cancel")
		case call := <-ops.hooks():
			if call.stage != stage || call.context.Validate(p) != nil {
				call.reply <- errors.New("hook order rejected")
				return errors.New("hook rejected")
			}
			proof, e = ops.proof(ctx)
			if e == nil && !validProof(p, proof) {
				e = errors.New("physical hook proof rejected")
			}
			if e == nil {
				e = send(c, Event{Instance: p.Instance, Stage: stage, RunnerID: runnerID, Proof: proof, Context: call.context})
			}
			if e == nil {
				e = acknowledge(stage, runnerID)
			}
			call.reply <- e
			if e != nil {
				return e
			}
			if stage == Start {
				stage = Complete
			} else {
				stage = RunnerExited
			}
		case result := <-running:
			running = nil
			if result.err != nil || !result.drained || stage != RunnerExited {
				code := "process"
				if errors.Is(result.err, context.DeadlineExceeded) {
					code = "timeout"
				}
				if !result.drained {
					code = "drain-unconfirmed"
				}
				_ = send(c, Event{Instance: p.Instance, Stage: Failed, RunnerID: runnerID, Exit: &result.exit, Drained: result.drained, LogSHA256: result.logHash, FailureCode: code})
				return errors.Join(errors.New("runner ended before complete or drain"), result.err)
			}
			return send(c, Event{Instance: p.Instance, Stage: RunnerExited, RunnerID: runnerID, Exit: &result.exit, Drained: result.drained, LogSHA256: result.logHash})
		}
	}
}
