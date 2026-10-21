package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const runnerTailSize = 128

// Output is untrusted. Keep only fixed observations, never a log, preview,
// exception message, URL, environment or token. These flags do not authorize a
// retry and are not proof of authentication, registration or job success.
var runnerPatterns = []struct{ label, phrase string }{
	{"connected", "connected to github"},
	{"crypto", "cryptographicexception"},
	{"access", "unauthorizedaccessexception"},
	{"network", "no such host is known"},
	{"http_exception", "httprequestexception"},
	{"tls", "ssl connection could not be established"},
	{"http_401", "(401)"}, {"http_401", "status code: 401"},
	{"http_403", "(403)"}, {"http_403", "status code: 403"},
	{"http_404", "(404)"}, {"http_404", "status code: 404"},
	{"http_429", "(429)"}, {"http_429", "status code: 429"},
	// EnsureSuccessStatusCode in the pinned official listener emits this form;
	// confirmed with a dummy token, without any real registration credential.
	{"http_401", "success: 401 ("}, {"http_403", "success: 403 ("},
	{"http_404", "success: 404 ("}, {"http_429", "success: 429 ("},
}

type runnerSignals struct {
	mu   sync.Mutex
	tail []byte
	seen uint32
}

func (s *runnerSignals) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		take := min(len(p), 256)
		// A constant allocation also makes the bound independent of integer
		// arithmetic involving an output-derived length.
		b := make([]byte, runnerTailSize+256)
		used := copy(b[:runnerTailSize], s.tail)
		used += copy(b[used:], p[:take])
		b = b[:used]
		for i, c := range b {
			if c >= 'A' && c <= 'Z' {
				b[i] = c + ('a' - 'A')
			}
		}
		for i, pattern := range runnerPatterns {
			if bytes.Contains(b, []byte(pattern.phrase)) {
				s.seen |= 1 << i
			}
		}
		clear(s.tail)
		s.tail = append(s.tail[:0], b[max(0, len(b)-runnerTailSize):]...)
		clear(b)
		p = p[take:]
	}
	return n, nil
}

func (s *runnerSignals) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []string
	for i, p := range runnerPatterns {
		if s.seen&(1<<i) == 0 {
			continue
		}
		duplicate := false
		for _, v := range result {
			if v == p.label {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, p.label)
		}
	}
	return result
}

func (s *runnerSignals) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.tail)
	s.tail = nil
}

type runnerCommandResult struct {
	Started  bool     `json:"started"`
	OK       bool     `json:"ok"`
	Exit     *int     `json:"exit,omitempty"`
	Error    string   `json:"error,omitempty"`
	Observed []string `json:"observed,omitempty"`
}

func runRunnerCommand(runtime, stage string, c *exec.Cmd) error {
	defer func() { c.Env = nil }()
	if stage != "configure" && stage != "listener" {
		return errors.New("runner stage rejected")
	}
	path := filepath.Join(runtime, "runner-"+stage)
	if err := saveNew(path+"-start.json", struct {
		SingleUse bool `json:"single_use"`
	}{true}); err != nil {
		return errors.New("runner start receipt failed")
	}
	var out runnerSignals
	defer out.clear()
	c.Stdout, c.Stderr = &out, &out
	c.WaitDelay = 2 * time.Second
	result := runnerCommandResult{}
	err := c.Start()
	if err == nil {
		result.Started = true
		err = c.Wait()
		if c.ProcessState != nil {
			code := c.ProcessState.ExitCode()
			result.Exit = &code
		} else if err == nil {
			err = errors.New("runner exit not observed")
		}
		if err != nil {
			result.Error = "exit_or_drain"
		}
	} else {
		result.Error = "start"
	}
	result.OK = err == nil
	result.Observed = out.snapshot()
	if writeErr := saveNew(path+"-result.json", result); writeErr != nil {
		return errors.New("runner result receipt failed")
	}
	if err != nil {
		return errors.New("official runner " + stage + " failed")
	}
	return nil
}
