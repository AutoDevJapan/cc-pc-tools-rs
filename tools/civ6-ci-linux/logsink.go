package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// Neither a preview limit nor a failed log destination can stop pipe drainage.
// The complete stream is hashed; a storage error is fatal only after the
// process group has drained, and truncated Actions output is never a green job.
type logSink struct {
	file    io.Writer
	preview bytes.Buffer
	limit   int
	full    hash.Hash
	bytes   int64
	err     error
}

func (s *logSink) Write(p []byte) (int, error) {
	if s.full == nil {
		s.full = sha256.New()
	}
	_, _ = s.full.Write(p)
	s.bytes += int64(len(p))
	if left := s.limit - s.preview.Len(); left > 0 {
		n := len(p)
		if n > left {
			n = left
		}
		_, _ = s.preview.Write(p[:n])
	}
	if s.err == nil && s.file != nil {
		n, e := s.file.Write(p)
		if e != nil {
			s.err = e
		} else if n != len(p) {
			s.err = io.ErrShortWrite
		}
	}
	return len(p), nil
}

func (s *logSink) SHA() string {
	if s.full == nil {
		return digest(nil)
	}
	return hex.EncodeToString(s.full.Sum(nil))
}

func forwardAndDrain(w io.Writer, r io.Reader) error {
	_, e := io.Copy(w, r)
	if e != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return e
}
