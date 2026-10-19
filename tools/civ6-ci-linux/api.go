package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Read allowlist. The parent's Verify may only read these families of endpoints
// of the one fixed repository; anything else is rejected before gh is started.
var readAllowed = regexp.MustCompile("^(repositories/1392152644|repos/AutoDevJapan/civ6-banseiki/(" +
	"pulls/[0-9]+|git/(commits|trees)/[0-9a-f]{40}|git/ref/heads/[A-Za-z0-9._%+-]{1,300}|contents/[A-Za-z0-9._/-]{1,200}|" +
	"actions/runs|actions/runs/[0-9]+|actions/runs/[0-9]+/(attempts/[0-9]+/)?jobs|actions/jobs/[0-9]+|actions/runners|" +
	"code-scanning/default-setup|compare/[0-9a-f]{40}[.][.][.][0-9a-f]{40}))$")
var readQuery = regexp.MustCompile("^[A-Za-z0-9=&_.%-]{0,200}$")

func allowedRead(endpoint string) error {
	path, query, _ := strings.Cut(endpoint, "?")
	if !readAllowed.MatchString(path) || !readQuery.MatchString(query) || strings.Contains(path, "..") && !strings.Contains(path, "/compare/") {
		return errors.New("GitHub read endpoint outside the fixed repository allowlist")
	}
	if strings.Contains(path, "/contents/") {
		if v, e := url.ParseQuery(query); e != nil || len(v) != 1 || !isHex(v.Get("ref"), 40) {
			return errors.New("contents read must name one exact commit")
		}
	}
	return nil
}

// readOverride lets fixtures supply a credential-free API.
var readOverride ReadAPI

// githubRead is the only read path handed to the parent and used locally.
func githubRead(ctx context.Context, endpoint string, out any) error {
	if e := allowedRead(endpoint); e != nil {
		return e
	}
	if readOverride != nil {
		return readOverride(ctx, endpoint, out)
	}
	b, e := readGitHub(ctx, endpoint)
	if e != nil {
		return e
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("GitHub response invalid")
	}
	return nil
}

// Only fixed categories leave this boundary: neither response bodies nor stderr
// (which can name a repository or echo a token) become errors.
type gitHubReadError struct{ class string }

func (e *gitHubReadError) Error() string { return "GitHub read failed (" + e.class + ")" }

func classifyGitHubRead(stderr string) *gitHubReadError {
	class := "unclassified"
	switch {
	case strings.Contains(stderr, "gh-gate: refused"), strings.Contains(stderr, "blocked by policy"):
		class = "policy-refused"
	case strings.Contains(stderr, "HTTP 401"), strings.Contains(stderr, "HTTP 403"), strings.Contains(stderr, "permission denied"):
		class = "authorization-or-permission"
	case strings.Contains(stderr, "HTTP 404"):
		class = "not-found"
	case transientGitHubRead(stderr):
		class = "temporary-transport-or-server"
	}
	return &gitHubReadError{class: class}
}

func transientGitHubRead(stderr string) bool {
	for _, refused := range []string{"gh-gate: refused", "blocked by policy", "HTTP 401", "HTTP 403", "HTTP 404", "permission denied"} {
		if strings.Contains(stderr, refused) {
			return false
		}
	}
	for _, temporary := range []string{"TLS handshake timeout", "i/o timeout", "connection reset by peer", "unexpected EOF", "HTTP 502", "HTTP 503", "HTTP 504"} {
		if strings.Contains(stderr, temporary) {
			return true
		}
	}
	return false
}

func readGitHub(ctx context.Context, endpoint string) ([]byte, error) {
	return finiteRead(ctx, func() ([]byte, string, error) {
		c := cmd(ctx, ghGate, "api", endpoint)
		stderr := boundedBuffer{limit: 64 << 10}
		c.Stderr = &stderr
		b, e := boundedOutput(c, 16<<20)
		return b, stderr.String(), e
	}, func() error {
		t := time.NewTimer(time.Second)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	})
}

// Three attempts at most, only for explicit transport/server failures.
func finiteRead(ctx context.Context, attempt func() ([]byte, string, error), pause func() error) ([]byte, error) {
	for i := 0; i < 3; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		b, stderr, e := attempt()
		if e == nil {
			return b, nil
		}
		if i == 2 || !transientGitHubRead(stderr) {
			return nil, classifyGitHubRead(stderr)
		}
		if e = pause(); e != nil {
			return nil, e
		}
	}
	return nil, errors.New("GitHub read failed")
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("response size exceeded")
	}
	return b.Buffer.Write(p)
}
func boundedOutput(c *exec.Cmd, limit int) ([]byte, error) {
	b := boundedBuffer{limit: limit}
	c.Stdout = &b
	if e := c.Run(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}

// mutateOverride lets fixtures observe the only two mutations without gh.
var mutateOverride func(ctx context.Context, method, endpoint string, out any) error

// mutate has no caller-chosen endpoint: it only serves registrationToken and
// deleteRunner, each of which builds its own fixed path.
func mutate(ctx context.Context, method, endpoint string, out any) error {
	if mutateOverride != nil {
		return mutateOverride(ctx, method, endpoint, out)
	}
	args := []string{"api", "--method", method, endpoint}
	var body []byte
	if method == "POST" {
		args = append(args, "--input", "-")
		body = []byte("{}")
	}
	c := cmd(ctx, ghGate, args...)
	if body != nil {
		c.Stdin = bytes.NewReader(body)
	}
	stderr := boundedBuffer{limit: 64 << 10}
	c.Stderr = &stderr
	data, e := boundedOutput(c, 1<<20)
	if e != nil {
		code := "unclassified"
		if m := regexp.MustCompile("(?i)HTTP[ :()]+([0-9]{3})").FindSubmatch(stderr.Bytes()); len(m) > 1 {
			code = string(m[1])
		}
		return fmt.Errorf("GitHub mutation failed: http=%s exit=%d stderr_sha256=%s", code, exitStatus(e), digest(stderr.Bytes()))
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errors.New("GitHub mutation response unverified; preserve recovery record")
	}
	return nil
}

// registrationToken's value is RAM-only: callers hand it to the guest through
// stdin and clear it. It is never logged, stored or put in an error.
func registrationToken(ctx context.Context) (string, error) {
	var t struct{ Token string }
	if e := mutate(ctx, "POST", "repos/"+repoFull+"/actions/runners/registration-token", &t); e != nil {
		return "", e
	}
	if t.Token == "" {
		return "", errors.New("official registration token absent")
	}
	return t.Token, nil
}

func deleteRunner(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("runner id required")
	}
	return mutate(ctx, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", repoFull, id), nil)
}

type runnerEntry struct {
	ID     int64
	Name   string
	Status string
	Busy   bool
	Labels []struct{ Name string }
}

func repoRunners(ctx context.Context) ([]runnerEntry, error) {
	var r struct {
		Total   int `json:"total_count"`
		Runners []runnerEntry
	}
	if e := githubRead(ctx, "repos/"+repoFull+"/actions/runners?per_page=100", &r); e != nil {
		return nil, e
	}
	if r.Total != len(r.Runners) {
		return nil, errors.New("runner inventory incomplete")
	}
	return r.Runners, nil
}
