package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Official public PowerShell/PowerShell v7.6.7 linux-x64.tar.gz.
const pr148PowerShellSHA = "6896f9e3a6b0a354bf470835a96da76d2f801cd50dcf047ad7c4563bf93bf431"

func pr148Transfer(ctx context.Context, n *pr148Namespace, binary []byte) error {
	if len(binary) < 64 || !bytes.Equal(binary[:4], []byte{127, 'E', 'L', 'F'}) {
		return errors.New("reviewed Linux helper required")
	}
	c := cmd(ctx, docker, "exec", "-i", n.ID, "sh", "-c", "cat > /runner/product-ci && chmod 755 /runner/product-ci")
	c.Stdin = bytes.NewReader(binary)
	return c.Run()
}

// Integration entry point for parent2736 ONLY. This implementation child runs
// fixtures, never this function. Tokens and source bytes are delivered by stdin
// to a new tmpfs namespace; the existing resident bridge is never contacted.
func executePR148Linux(ctx context.Context, p pr148LinuxPlan, dir, guestFile, powerShellFile string) (ret error) {
	if e := verifyPR148LinuxRun(ctx, p, get, false); e != nil {
		return e
	}
	guest, e := os.ReadFile(guestFile)
	if e != nil {
		return e
	}
	pwsh, e := os.ReadFile(powerShellFile)
	if e != nil || digest(pwsh) != pr148PowerShellSHA {
		return errors.New("official PowerShell asset digest rejected")
	}
	rec := struct {
		Plan                                                 pr148LinuxPlan
		Namespace                                            *pr148Namespace
		ArchiveSHA, LogSHA, Failure                          string
		LogBytes                                             int
		ListenerExit                                         int
		RegistrationMayExist, RegistrationGone, LockReleased bool
		AllRequiredChecksSuccess                             bool
	}{Plan: p}
	if e = pr148SaveNew(filepath.Join(dir, "pr148-intent.private.json"), rec); e != nil {
		return e
	}
	lockPath := filepath.Join(os.TempDir(), "todo2736-repo1392152644-linux.lock")
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("PR148 Linux candidate occupied; inspect its original receipt")
	}
	_, e = fmt.Fprintf(lock, "%s\n%s\n", p.Head, dir)
	if e == nil {
		e = lock.Sync()
	}
	if e != nil {
		_ = lock.Close()
		return e
	}
	defer func() {
		if ret != nil {
			rec.Failure = ret.Error()
		}
		ret = errors.Join(ret, lock.Close())
		if (rec.Namespace == nil || rec.Namespace.Removed) && (!rec.RegistrationMayExist || rec.RegistrationGone) {
			if e := os.Remove(lockPath); e == nil {
				rec.LockReleased = true
			} else {
				ret = errors.Join(ret, e)
			}
		}
		ret = errors.Join(ret, pr148SaveNew(filepath.Join(dir, "pr148-result.private.json"), rec))
	}()
	n, e := newPR148Namespace(ctx, dir)
	rec.Namespace = n
	if n != nil && n.ID != "" {
		defer func() {
			if !n.Removed {
				ret = errors.Join(ret, n.dispose())
			}
		}()
	}
	if e != nil {
		return e
	}
	if e = pr148Transfer(ctx, n, guest); e != nil {
		return e
	}
	x := cmd(ctx, docker, "exec", "-i", n.ID, "sh", "-c", "mkdir -p /toolchain/cargo /toolchain/rustup /work/powershell && tar -xz -C /work/powershell && chmod 755 /work/powershell/pwsh")
	x.Stdin = bytes.NewReader(pwsh)
	if e = x.Run(); e != nil {
		return errors.New("public PowerShell stdin supply failed")
	}
	c := candidate{1392152644, 148, p.Head}
	oldPlan := codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: c.SHA, SHA: p.Head, Tree: p.Tree, RunID: p.RunID, Attempt: p.Attempt, Repo: pr148Repo}
	rec.ArchiveSHA, e = materializeFixedSource(ctx, c, oldPlan, n.ID)
	if e != nil {
		return e
	}
	// Reconfirm actual materialized HEAD, complete index tree and absence of
	// checkout auth before any registration token is requested.
	for expr, want := range map[string]string{"HEAD": p.Head, "HEAD^{tree}": p.Tree} {
		b, e := cmd(ctx, docker, "exec", n.ID, "git", "-C", pr148Workspace, "rev-parse", expr).Output()
		if e != nil || strings.TrimSpace(string(b)) != want {
			return errors.New("host preflight actual Git pin rejected")
		}
	}
	workflow, e := pr148Contents(ctx, get, pr148Workflows[0].Path, pr148Head)
	if e != nil {
		return e
	}
	policy, e := pr148Policy(workflow)
	if e != nil {
		return e
	}
	if e = verifyPR148LinuxRun(ctx, p, get, false); e != nil {
		return e
	}
	queuePlan := codeQLPlan{Repo: pr148Repo, RunID: p.RunID, Label: pr148LinuxLabel}
	if e = checkCodeQLQueue(ctx, queuePlan, get); e != nil {
		return e
	}
	flight, cancel := context.WithCancel(ctx)
	defer cancel()
	h := &pr148CargoHost{plan: p, policy: policy, ns: n, dir: dir, target: filepath.Join(dir, "cargo-target")}
	h.recheck = func(c context.Context) error { return verifyPR148LinuxRun(c, p, get, false) }
	h.execute = h.run
	endpoint, closeServer, e := startPR148Cargo(flight, h)
	if e != nil {
		return e
	}
	defer func() { cancel(); ret = errors.Join(ret, closeServer()) }()
	rs, e := repoRunners(ctx, pr148Repo)
	if e != nil {
		return e
	}
	for _, r := range rs {
		for _, l := range r.Labels {
			if l.Name == pr148LinuxLabel {
				return errors.New("dedicated Linux runner label occupied")
			}
		}
	}
	var token struct{ Token string }
	if e = mutateJSON(ctx, "POST", "repos/"+pr148Repo+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return e
	}
	if token.Token == "" {
		return errors.New("official registration token absent")
	}
	// Persist registration uncertainty before handing the RAM-only token over.
	rec.RegistrationMayExist = true
	if e = pr148SaveNew(filepath.Join(dir, "registration-intent.private.json"), map[string]any{"name": n.Name, "plan": p}); e != nil {
		return e
	}
	defer func() {
		cancel()
		ret = errors.Join(ret, closeServer())
		if !n.Removed {
			ret = errors.Join(ret, n.dispose())
		}
		if !n.TreeGone {
			return
		}
		cc, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		rs, e := repoRunners(cc, pr148Repo)
		if e != nil {
			ret = errors.Join(ret, e)
			return
		}
		for _, r := range rs {
			if r.Name == n.Name {
				ret = errors.Join(ret, mutateJSON(cc, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", pr148Repo, r.ID), nil, nil))
			}
		}
		rs, e = repoRunners(cc, pr148Repo)
		if e != nil {
			ret = errors.Join(ret, e)
			return
		}
		for _, r := range rs {
			if r.Name == n.Name {
				ret = errors.Join(ret, errors.New("official registration absence unverified"))
				return
			}
		}
		rec.RegistrationGone = true
	}()
	packet := pr148GuestPacket{p, policy, pr148Repo, n.Name, token.Token, endpoint, h.key}
	input, e := json.Marshal(packet)
	token.Token = ""
	packet.Token = ""
	if e != nil {
		return e
	}
	defer func() {
		for i := range input {
			input[i] = 0
		}
	}()
	log, e := os.OpenFile(filepath.Join(dir, "official-listener.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	x = cmd(ctx, docker, "exec", "-i", n.ID, "/runner/product-ci", "pr148-guest")
	x.Stdin = bytes.NewReader(input)
	x.Stdout = log
	x.Stderr = log
	e = x.Run()
	rec.ListenerExit = exitStatus(e)
	ret = errors.Join(ret, log.Sync(), log.Close())
	data, le := os.ReadFile(log.Name())
	if le != nil {
		return le
	}
	rec.LogBytes = len(data)
	rec.LogSHA = digest(data)
	if e != nil {
		return errors.New("official listener failed; preserve original private output")
	}
	for _, stage := range []string{"start", "complete"} {
		b, e := cmd(ctx, docker, "exec", n.ID, "cat", "/home/runner/pr148-"+stage+".json").Output()
		var observed pr148LinuxPlan
		if e != nil || json.Unmarshal(b, &observed) != nil || observed != p {
			return errors.New("official job hook pin missing")
		}
		if e = os.WriteFile(filepath.Join(dir, "hook-"+stage+".private.json"), b, 0600); e != nil {
			return e
		}
	}
	for expr, want := range map[string]string{"HEAD": p.Head, "HEAD^{tree}": p.Tree} {
		b, e := cmd(ctx, docker, "exec", n.ID, "git", "-C", pr148Workspace, "rev-parse", expr).Output()
		if e != nil || strings.TrimSpace(string(b)) != want {
			return errors.New("host postflight actual Git pin rejected")
		}
	}
	if e = verifyPR148LinuxRun(ctx, p, get, true); e != nil {
		return e
	}
	// No success/check/status API exists. Parent must inspect official job/run,
	// all original logs and all OS jobs, even when listener and hooks exited 0.
	return nil
}
