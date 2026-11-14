//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

type askProducerExpected struct{ Domain, Nonce, Source, InputManifestSHA string }
type askProducerCompilerProcess struct {
	PID     uint32 `json:"PID"`
	Created uint64 `json:"Created"`
}
type askProducerReady struct {
	Domain           string                     `json:"domain"`
	Phase            string                     `json:"phase"`
	Nonce            string                     `json:"nonce"`
	Source           string                     `json:"source"`
	InputManifestSHA string                     `json:"input_manifest_sha256"`
	ArtifactSHA      string                     `json:"artifact_sha256"`
	ArtifactBytes    int                        `json:"artifact_bytes"`
	ArtifactIdentity softwareIdentity           `json:"artifact_identity"`
	EvidenceSHA      string                     `json:"evidence_sha256"`
	EvidenceBytes    int                        `json:"evidence_bytes"`
	EvidenceIdentity softwareIdentity           `json:"evidence_identity"`
	CompilerProcess  askProducerCompilerProcess `json:"compiler_process"`
}
type askProducerACK struct {
	Domain           string           `json:"domain"`
	Nonce            string           `json:"nonce"`
	Source           string           `json:"source"`
	InputManifestSHA string           `json:"input_manifest_sha256"`
	ArtifactSHA      string           `json:"artifact_sha256"`
	ArtifactIdentity softwareIdentity `json:"artifact_identity"`
	EvidenceSHA      string           `json:"evidence_sha256"`
}
type askProducerFinal struct {
	Ready       askProducerReady `json:"ready"`
	Phase       string           `json:"phase"`
	ACKReceived bool             `json:"ack_received"`
}
type askProducerFlightResult struct {
	Child                                  askChildResult
	Ready                                  *askProducerReady
	Final                                  askProducerFinal
	NativeProcess                          *askNativeProcess
	ACKSent, ACKReceived, ProtocolComplete bool
}
type askOwnedProducerFlight struct {
	Ready  <-chan askProducerReady
	Accept chan<- askProducerACK
	Done   <-chan askProducerFlightResult
	Abort  context.CancelFunc
}
type askProducerRuntime struct {
	expected           askProducerExpected
	ready              chan askProducerReady
	accept             chan askProducerACK
	done               chan askProducerFlightResult
	cancel             context.CancelFunc
	output             askProducerOutput
	native             *askNativeProcess
	seen               *askProducerReady
	final              askProducerFinal
	finalValid, failed bool
	once               sync.Once
}
type askProducerOutput struct {
	frame                [65536]byte
	n, total, framesSeen int
	frames               chan []byte
	invalid, eof         bool
}

func (o *askProducerOutput) Write(p []byte) (int, error) {
	for _, b := range p {
		o.total++
		if o.total > 81920 || o.n == len(o.frame) {
			o.invalid = true
			return 0, errors.New("producer_frame_limit")
		}
		if b == '\n' {
			o.framesSeen++
			if o.framesSeen > 2 {
				o.invalid = true
				return 0, errors.New("producer_extra_frame")
			}
			frame := append([]byte(nil), o.frame[:o.n]...)
			clear(o.frame[:o.n])
			o.n = 0
			select {
			case o.frames <- frame:
			default:
				clear(frame)
				o.invalid = true
				return 0, errors.New("producer_frame_queue")
			}
		} else {
			o.frame[o.n] = b
			o.n++
		}
	}
	return len(p), nil
}
func (o *askProducerOutput) ReadFrom(r io.Reader) (int64, error) {
	var b [4096]byte
	defer clear(b[:])
	var total int64
	for {
		n, e := r.Read(b[:])
		if n > 0 {
			_, we := o.Write(b[:n])
			total += int64(n)
			if we != nil {
				return total, we
			}
		}
		if e == io.EOF {
			o.eof = o.n == 0
			return total, nil
		}
		if e != nil {
			return total, e
		}
	}
}

func askProducerHex(v string, n int) bool {
	if len(v) != n {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
func askProducerID(i softwareIdentity) bool {
	for _, s := range []string{i.Volume, i.FileID, i.Created} {
		if len(s) == 0 || len(s) > 20 {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
func askProducerJSON(raw []byte, v any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() bool
	walk = func() bool {
		t, e := d.Token()
		if e != nil {
			return false
		}
		s, ok := t.(json.Delim)
		if !ok {
			return true
		}
		if s == '{' {
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return false
				}
				name, ok := key.(string)
				if !ok {
					return false
				}
				name = strings.ToLower(name)
				if seen[name] {
					return false
				}
				seen[name] = true
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		}
		if s == '[' {
			for d.More() {
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		}
		return false
	}
	if !walk() {
		return false
	}
	if _, e := d.Token(); e != io.EOF {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(new(any)) == io.EOF
}
func (p *askProducerRuntime) receiveReady(raw []byte) bool {
	var r askProducerReady
	if len(raw) == 0 || len(raw) > 65536 || p.seen != nil || !askProducerJSON(raw, &r) || r.Domain != p.expected.Domain || r.Phase != "ready" || r.Nonce != p.expected.Nonce || r.Source != p.expected.Source || r.InputManifestSHA != p.expected.InputManifestSHA || !askProducerHex(r.ArtifactSHA, 64) || r.ArtifactBytes <= 0 || r.ArtifactBytes > 32<<20 || !askProducerID(r.ArtifactIdentity) || !askProducerHex(r.EvidenceSHA, 64) || r.EvidenceBytes <= 0 || r.EvidenceBytes > 8<<20 || !askProducerID(r.EvidenceIdentity) || r.CompilerProcess.PID == 0 || r.CompilerProcess.Created == 0 {
		p.failed = true
		return false
	}
	p.seen = &r
	p.ready <- r
	return true
}
func (p *askProducerRuntime) validACK(a askProducerACK) bool {
	if p.seen == nil {
		return false
	}
	r := *p.seen
	return a == askProducerACK{r.Domain, r.Nonce, r.Source, r.InputManifestSHA, r.ArtifactSHA, r.ArtifactIdentity, r.EvidenceSHA}
}
func (p *askProducerRuntime) receiveFinal(raw []byte, attempted bool) {
	var f askProducerFinal
	if len(raw) == 0 || len(raw) > 16384 || !attempted || p.seen == nil || p.finalValid || !askProducerJSON(raw, &f) || f.Ready != *p.seen || f.Phase != "ack_received" || !f.ACKReceived {
		p.failed = true
		p.cancel()
		return
	}
	p.final = f
	p.finalValid = true
}
func (p *askProducerRuntime) finish(child askChildResult, sent, stderrEOF bool) {
	p.once.Do(func() {
		for {
			select {
			case frame := <-p.output.frames:
				if p.seen == nil {
					if !p.receiveReady(frame) {
						p.failed = true
					}
				} else {
					p.receiveFinal(frame, sent)
				}
				clear(frame)
			default:
				goto drained
			}
		}
	drained:
		clear(p.output.frame[:])
		result := askProducerFlightResult{Child: child, Ready: p.seen, Final: p.final, NativeProcess: p.native, ACKSent: sent, ACKReceived: p.finalValid}
		result.ProtocolComplete = sent && p.finalValid && !p.failed && !p.output.invalid && p.output.eof && stderrEOF && child.Started && child.Assigned && child.WaitVerified && child.Reaped && !child.TimedOut && child.Exit == 0
		p.done <- result
		close(p.done)
		close(p.ready)
	})
}

func askOwnedProducerStart(ctx context.Context, executable string, args []string, expected askProducerExpected) (*askOwnedProducerFlight, error) {
	self, e := os.Executable()
	if e != nil || !sameCleanPath(self, executable) || len(args) != 4 || args[0] != "--unslop-native-build-worker" || args[1] != expected.Nonce || args[2] != expected.InputManifestSHA || len(args[3]) != 2 || args[3][1] != ':' || args[3][0] < 'P' || args[3][0] > 'Z' || expected.Domain != "godd-unslop-held-producer-v1" || !askProducerHex(expected.Nonce, 32) || !askProducerHex(expected.Source, 40) || !askProducerHex(expected.InputManifestSHA, 64) {
		return nil, errors.New("producer_launch_tuple_invalid")
	}
	return askOwnedProducerStartUnchecked(ctx, executable, args, expected), nil
}
func askOwnedProducerStartUnchecked(parent context.Context, executable string, args []string, expected askProducerExpected) *askOwnedProducerFlight {
	ctx, cancel := context.WithTimeout(parent, 15*60*1000000000)
	p := &askProducerRuntime{expected: expected, ready: make(chan askProducerReady, 1), accept: make(chan askProducerACK, 1), done: make(chan askProducerFlightResult, 1), cancel: cancel, output: askProducerOutput{frames: make(chan []byte, 2)}}
	go func() {
		defer cancel()
		child := askOwnedChildWithHandshake(context.WithValue(ctx, askOwnedProducerKey{}, p), executable, nil, args...)
		p.finish(child, false, false)
	}()
	return &askOwnedProducerFlight{Ready: p.ready, Accept: p.accept, Done: p.done, Abort: cancel}
}
