//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// A loopback smart-Git origin serves ONLY the compiled-in synthetic objects.
// actions/checkout runs its real main and post code under the same filtered
// service token and Job Object as Rust. No product endpoint is contacted.
func checkoutFixture(p Plan) error {
	if !p.RuntimeFixture || p.Fixture == "" {
		return errors.New("checkout fixture is synthetic only")
	}
	h, tree, data := syntheticSource()
	s, e := fetchSource(h, tree, syntheticRead(data))
	if e != nil {
		return e
	}
	originSource := filepath.Join(p.runtime(), "checkout-origin")
	if e = writeSource(originSource, s); e != nil {
		return e
	}
	env, e := runtimeSDKEnv(p, workerEnv(p))
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return e
	}
	var reads, writes, denied atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		advertise := r.Method == "GET" && r.URL.Path == "/fixture/source/info/refs" && r.URL.Query().Get("service") == "git-upload-pack"
		upload := r.Method == "POST" && r.URL.Path == "/fixture/source/git-upload-pack"
		if !advertise && !upload {
			denied.Add(1)
			http.Error(w, "synthetic Git route only", 404)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		args := []string{"-c", "safe.directory=" + originSource, "-c", "core.hooksPath=NUL", "upload-pack", "--stateless-rpc"}
		if advertise {
			args = append(args, "--advertise-refs")
			reads.Add(1)
		} else {
			writes.Add(1)
		}
		args = append(args, filepath.Join(originSource, ".git"))
		c := exec.CommandContext(ctx, `C:\Program Files\Git\cmd\git.exe`, args...)
		hidden(c)
		c.WaitDelay = time.Second
		c.Env = env
		if r.Header.Get("Git-Protocol") == "version=2" {
			c.Env = append(append([]string{}, env...), "GIT_PROTOCOL=version=2")
		}
		if upload {
			b, err := boundedRead(r.Body, 16<<20)
			if err != nil {
				http.Error(w, "fixture request bound", 413)
				return
			}
			c.Stdin = bytes.NewReader(b)
		}
		var out bytes.Buffer
		c.Stdout = &sourceOutput{Buffer: &out}
		if c.Run() != nil {
			http.Error(w, "synthetic upload-pack failed", 500)
			return
		}
		if advertise {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			msg := "# service=git-upload-pack\n"
			fmt.Fprintf(w, "%04x%s0000", len(msg)+4, msg)
		} else {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		}
		_, _ = w.Write(out.Bytes())
	})
	serveDone := make(chan struct{})
	go func() { defer close(serveDone); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-serveDone }()
	base := "http://" + listener.Addr().String()
	origin := base + "/fixture/source"
	if _, e = sourceGit(p.source(), "remote", "set-url", "origin", origin); e != nil {
		return e
	}
	node, e := fixtureNode(p)
	if e != nil {
		return e
	}
	if _, e = sdkCommand(p, "fixture-checkout-node", node, env, "--version"); e != nil {
		return e
	}
	makeEnv := func(stage string) ([]string, error) {
		result := append([]string{}, env...)
		values := map[string]string{"INPUT_REPOSITORY": "fixture/source", "INPUT_REF": h, "INPUT_TOKEN": "synthetic-placeholder", "INPUT_PATH": "", "INPUT_FETCH-DEPTH": "1", "INPUT_PERSIST-CREDENTIALS": "false", "INPUT_CLEAN": "true", "INPUT_SET-SAFE-DIRECTORY": "true", "INPUT_GITHUB-SERVER-URL": base, "GITHUB_SERVER_URL": base, "GITHUB_API_URL": base + "/api/v3", "GITHUB_WORKSPACE": p.source(), "GITHUB_REPOSITORY": "fixture/source", "GITHUB_SHA": h, "GITHUB_EVENT_NAME": "workflow_dispatch", "RUNNER_TEMP": filepath.Join(p.runtime(), "tmp")}
		for _, name := range []string{"GITHUB_STATE", "GITHUB_OUTPUT", "GITHUB_ENV", "GITHUB_PATH"} {
			path := filepath.Join(p.runtime(), "tmp", stage+"-"+name)
			if err := writeNewBytes(path, nil); err != nil {
				return nil, err
			}
			values[name] = path
		}
		for k, v := range values {
			result = replaceEnv(result, k, v)
		}
		return result, nil
	}
	oldEnv, e := makeEnv("old-setting")
	if e != nil {
		return e
	}
	oldEnv = replaceEnv(oldEnv, "GIT_CONFIG_GLOBAL", "NUL")
	_, oldErr := sdkCommand(p, "fixture-checkout-original-setting", node, oldEnv, filepath.Join(p.runtime(), "sdk", "checkout.js"))
	// The old setting's result is an observation, not an assumed failure.
	if e = saveNew(filepath.Join(p.runtime(), "checkout-original-observation.json"), map[string]any{"succeeded": oldErr == nil, "setting": "NUL", "synthetic_only": true}); e != nil {
		return e
	}
	goodEnv, e := makeEnv("owned-setting")
	if e != nil {
		return e
	}
	if _, e = sdkCommand(p, "fixture-checkout", node, goodEnv, filepath.Join(p.runtime(), "sdk", "checkout.js")); e != nil {
		return e
	}
	// Run the action's genuine post entry with the exact saved repository path.
	statePath := ""
	for _, v := range goodEnv {
		if strings.HasPrefix(v, "GITHUB_STATE=") {
			statePath = strings.TrimPrefix(v, "GITHUB_STATE=")
		}
	}
	stateBytes, e := os.ReadFile(statePath)
	if e != nil {
		return e
	}
	states, e := checkoutStates(string(stateBytes))
	if e != nil || states["repositoryPath"] != p.source() || states["isPost"] != "true" {
		return errors.New("actual checkout state rejected")
	}
	postEnv := append([]string{}, goodEnv...)
	for k, v := range states {
		postEnv = replaceEnv(postEnv, "STATE_"+k, v)
	}
	if _, e = sdkCommand(p, "fixture-checkout-post", node, postEnv, filepath.Join(p.runtime(), "sdk", "checkout.js")); e != nil {
		return e
	}
	q := p
	q.Head = h
	q.Tree = tree
	if e = verifySourceOrigin(q, p.source(), true, origin); e != nil {
		return e
	}
	owner, e := ownedSourceSID(p.source())
	if e != nil {
		return e
	}
	want, e := serviceSID(p)
	if e != nil || owner != want {
		return errors.New("source owner is not the new service SID")
	}
	// Also use plain Git without verifySource's per-command safe.directory.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, `C:\Program Files\Git\cmd\git.exe`, "-C", p.source(), "rev-parse", "HEAD")
	hidden(c)
	c.Env = env
	b, e := c.Output()
	if e != nil || strings.TrimSpace(string(b)) != h {
		return errors.New("plain service Git failed after actual checkout")
	}
	if reads.Load() == 0 || writes.Load() == 0 || denied.Load() != 0 {
		return errors.New("checkout did not exclusively use the synthetic Git origin")
	}
	if e = saveNew(filepath.Join(p.runtime(), "checkout-verified.json"), map[string]any{"head": h, "tree": tree, "actual_action_main_post": true, "plain_git_without_override": true, "source_owner_sid": owner, "auth_absent": true, "advertisements": reads.Load(), "upload_requests": writes.Load(), "unexpected_routes": denied.Load(), "synthetic_only": true, "official_ci_claimed": false}); e != nil {
		return e
	}
	// Only the transport URL differs from production. Verify it BEFORE adapting
	// that fixture-only value back for the unchanged Rust tool-entry guard.
	_, e = sourceGit(p.source(), "remote", "set-url", "origin", "https://github.com/"+repository)
	return e
}

func checkoutStates(raw string) (map[string]string, error) {
	if len(raw) > 64<<10 {
		return nil, errors.New("checkout state bound")
	}
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		key, delimiter, ok := strings.Cut(lines[i], "<<")
		if !ok {
			k, v, ok := strings.Cut(lines[i], "=")
			if !ok {
				return nil, errors.New("checkout state format")
			}
			out[k] = v
			continue
		}
		var values []string
		i++
		for i < len(lines) && lines[i] != delimiter {
			values = append(values, lines[i])
			i++
		}
		if i == len(lines) {
			return nil, errors.New("checkout state delimiter")
		}
		out[key] = strings.Join(values, "\n")
	}
	allow := map[string]bool{"isPost": true, "repositoryPath": true, "setSafeDirectory": true, "sshKeyPath": true, "sshKnownHostsPath": true}
	for k := range out {
		if !allow[k] {
			return nil, errors.New("unrecognized checkout state")
		}
	}
	return out, nil
}

func ownedSourceSID(path string) (string, error) {
	var owner *syscall.SID
	var sd uintptr
	r, _, _ := advapi.NewProc("GetNamedSecurityInfoW").Call(ptr(path), 1, 1, uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if r != 0 {
		return "", fmt.Errorf("owned source owner query %d", r)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	return owner.String()
}
