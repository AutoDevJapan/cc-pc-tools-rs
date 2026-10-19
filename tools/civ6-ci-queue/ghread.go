package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// readEndpoint admits only GET resources of the one explicit repository (or
// its numeric id). The path is a fixed shape: no host, no query beyond plain
// parameters, no traversal.
var readEndpoint = regexp.MustCompile(`^(repositories/1392152644|repos/AutoDevJapan/civ6-banseiki(/[A-Za-z0-9_.%@-]+)+)(\?[A-Za-z0-9_.=&%@/-]*)?$`)
var immutableEndpoint = regexp.MustCompile(`^repos/AutoDevJapan/civ6-banseiki/(git/commits/[0-9a-f]{40}|git/trees/[0-9a-f]{40}\?recursive=1|contents/[A-Za-z0-9_./-]+\?ref=[0-9a-f]{40})$`)

func checkEndpoint(ep string) error {
	if !readEndpoint.MatchString(ep) || strings.Contains(ep, "/../") || strings.Contains(ep, "//") || len(ep) > 400 {
		return errors.New("GitHub endpoint outside the fixed read-only allowlist")
	}
	for _, banned := range []string{"registration-token", "remove-token", "/secrets", "/tarball", "/zipball", "/logs", "/artifacts", "/runners"} {
		if strings.Contains(ep, banned) {
			return errors.New("GitHub endpoint outside the fixed read-only allowlist")
		}
	}
	return nil
}

// RunFunc runs one fixed executable with bounded output. Tests replace it with
// a fake; the real one hides the console window and never uses a shell.
type RunFunc func(ctx context.Context, exe string, stdin []byte, limit int64, args ...string) ([]byte, error)

type limitWriter struct {
	b        bytes.Buffer
	limit    int64
	overflow bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	left := w.limit - int64(w.b.Len())
	if int64(len(p)) > left {
		if left > 0 {
			w.b.Write(p[:left])
		}
		w.overflow = true
		return len(p), nil
	}
	return w.b.Write(p)
}

func realRun(ctx context.Context, exe string, stdin []byte, limit int64, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, exe, args...)
	hide(c)
	c.Env = scrubbedEnv()
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	out := &limitWriter{limit: limit}
	c.Stdout = out
	c.WaitDelay = 2 * time.Second
	if e := c.Run(); e != nil {
		return nil, errors.New("command failed or timed out")
	}
	if out.overflow {
		return nil, errors.New("command output exceeds limit")
	}
	return out.b.Bytes(), nil
}

// scrubbedEnv keeps the OAuth login store usable and removes API-key style
// credentials, so a metered key is never picked up by accident.
func scrubbedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
}

// Reader is the GET-only GitHub adapter handed to the parent's contract. It
// counts calls so one tick cannot spend an unbounded API budget, memoises
// within the tick, and caches only commit-addressed (immutable) resources on
// disk so a verification that needs more than one tick still makes progress.
type Reader struct {
	GH       Pinned
	Run      RunFunc
	CacheDir string
	MaxCalls int
	Calls    int
	mem      map[string][]byte
	pinned   bool
}

var errCallBudget = errors.New("GitHub read budget for this pass exhausted")

const maxResponse = 16 << 20

func (r *Reader) cachePath(ep string) string {
	h := sha256.Sum256([]byte(ep))
	return filepath.Join(r.CacheDir, hex.EncodeToString(h[:])+".json")
}

func (r *Reader) Read(ctx context.Context, ep string, out any) error {
	if e := checkEndpoint(ep); e != nil {
		return e
	}
	if r.mem == nil {
		r.mem = map[string][]byte{}
	}
	body, ok := r.mem[ep]
	if !ok && r.CacheDir != "" && immutableEndpoint.MatchString(ep) {
		if b, e := os.ReadFile(r.cachePath(ep)); e == nil && len(b) > 0 && len(b) <= maxResponse {
			body, ok = b, true
		}
	}
	if !ok {
		if r.MaxCalls > 0 && r.Calls >= r.MaxCalls {
			return errCallBudget
		}
		if !r.pinned {
			if e := r.GH.Verify(); e != nil {
				return e
			}
			r.pinned = true
		}
		r.Calls++
		// GET is the default of "gh api"; no method, field or header is passed.
		b, e := r.Run(ctx, r.GH.Path, nil, maxResponse, "api", ep)
		if e != nil {
			return errors.New("GitHub read unavailable")
		}
		if !json.Valid(b) {
			return errors.New("GitHub read returned invalid JSON")
		}
		body = b
		if r.CacheDir != "" && immutableEndpoint.MatchString(ep) {
			_ = os.MkdirAll(r.CacheDir, 0700)
			if f, e := os.CreateTemp(r.CacheDir, ".c-*.tmp"); e == nil {
				n := f.Name()
				_, we := f.Write(b)
				ce := f.Close()
				if we == nil && ce == nil && replaceFile(n, r.cachePath(ep)) == nil {
					n = ""
				}
				if n != "" {
					_ = os.Remove(n)
				}
			}
		}
	}
	r.mem[ep] = body
	return json.Unmarshal(body, out)
}

func (r *Reader) API() ReadAPI { return r.Read }

// TokenSource obtains a one-shot runner registration credential. The value
// lives only in RAM and is handed to the helper's stdin; it is never written to
// the ledger, a log or an argument.
type TokenSource interface {
	Registration(ctx context.Context) ([]byte, error)
}

type ghToken struct {
	GH  Pinned
	Run RunFunc
}

func (g ghToken) Registration(ctx context.Context) ([]byte, error) {
	if e := g.GH.Verify(); e != nil {
		return nil, e
	}
	out, e := g.Run(ctx, g.GH.Path, nil, 64<<10, "api", "--method", "POST", "repos/"+Repository+"/actions/runners/registration-token", "--jq", ".token")
	if e != nil {
		return nil, errors.New("registration credential unavailable")
	}
	t := bytes.TrimSpace(out)
	if len(t) < 16 || len(t) > 512 || bytes.ContainsAny(t, " \r\n\t\x00") {
		return nil, errors.New("registration credential shape rejected")
	}
	return t, nil
}
