package main

import (
	"bytes"
	"io"
)

// frameMarker starts every framing line that the testing package prints
// under -test.v=test2json, such as "=== RUN   TestName".
const frameMarker = 0x16

var (
	runFrame  = []byte("=== RUN   ")
	nameFrame = []byte("=== NAME")
)

// frameScanner echoes test output without framing markers and records the
// top-level cases whose RUN frame appears. Output text that merely looks like
// a RUN line has no marker and does not count. NAME frames only attribute the
// following output to a test for test2json, so they are not echoed.
type frameScanner struct {
	out     io.Writer
	pending []byte
	started []string
}

func newFrameScanner(out io.Writer) *frameScanner {
	return &frameScanner{out: out}
}

func (s *frameScanner) Write(p []byte) (int, error) {
	s.pending = append(s.pending, p...)
	for {
		i := bytes.IndexByte(s.pending, '\n')
		if i < 0 {
			break
		}
		s.line(s.pending[:i+1])
		s.pending = s.pending[i+1:]
	}
	return len(p), nil
}

// Flush handles output that does not end with a newline.
func (s *frameScanner) Flush() {
	if len(s.pending) > 0 {
		s.line(s.pending)
		s.pending = nil
	}
}

func (s *frameScanner) line(line []byte) {
	for i, segment := range bytes.Split(line, []byte{frameMarker}) {
		if i > 0 {
			if bytes.HasPrefix(segment, nameFrame) {
				continue
			}
			s.frame(segment)
		}
		s.out.Write(segment)
	}
}

func (s *frameScanner) frame(segment []byte) {
	name, ok := bytes.CutPrefix(segment, runFrame)
	if !ok {
		return
	}
	name = bytes.TrimRight(name, "\r\n")
	if len(name) > 0 && bytes.IndexByte(name, '/') < 0 {
		s.started = append(s.started, string(name))
	}
}
