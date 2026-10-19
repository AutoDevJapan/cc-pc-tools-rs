package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const safeDelete = "D:\\CC\\Tools\\safe-delete\\safe-delete.exe"

// ownedReceiptRoot requires the NEW, EMPTY directory created by safe-delete
// create (its own plan call must name this exact path).
func ownedReceiptRoot(ctx context.Context, dir string) error {
	abs, e := filepath.Abs(dir)
	if e != nil {
		return e
	}
	id := strings.TrimPrefix(filepath.Base(abs), "codex-owned-")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("safe-delete created receipt root required")
	}
	if entries, e := os.ReadDir(abs); e != nil || len(entries) != 0 {
		return errors.New("receipt root must be new and empty")
	}
	b, e := cmd(ctx, safeDelete, "plan", "--id", id).Output()
	var r struct{ ID, Path, Action string }
	if e != nil || json.Unmarshal(b, &r) != nil || r.ID != id || !strings.EqualFold(filepath.Clean(r.Path), filepath.Clean(abs)) || r.Action != "submit_to_exec_tool" {
		return errors.New("receipt root ownership verification failed")
	}
	return nil
}

// hostCheckout re-proves the namespace's actual Git HEAD/tree and the absence of
// checkout authentication from the host side.
func hostCheckout(ctx context.Context, p Plan, n *namespace) error {
	if _, e := n.inspect(ctx); e != nil {
		return e
	}
	for expr, want := range map[string]string{"HEAD": p.Head, "HEAD^{tree}": p.Tree} {
		b, e := cmd(ctx, dockerExe, "exec", "-e", "GIT_CONFIG_GLOBAL=/dev/null", "-e", "GIT_CONFIG_NOSYSTEM=1", "-e", "GIT_NO_REPLACE_OBJECTS=1", n.ID, "git", "-C", workspace, "rev-parse", expr).Output()
		if e != nil || strings.TrimSpace(string(b)) != want {
			return errors.New("host actual Git HEAD/tree pin rejected")
		}
	}
	b, e := cmd(ctx, dockerExe, "exec", n.ID, "git", "-C", workspace, "config", "--local", "--get-regexp", "http\\..*\\.extraheader|credential\\..*").Output()
	if len(b) != 0 || (e != nil && exitStatus(e) != 1) {
		return errors.New("host observed checkout authentication still present")
	}
	return nil
}

func credentialCleanupDenied(exit int, log []byte) bool {
	return exit == 2 && bytes.Contains(log, []byte("An error occurred: Access to the path '/runner/.credentials' is denied."))
}

type receipt struct {
	RepoID                                      int64
	Plan                                        Plan
	Kind                                        Kind
	Namespace                                   *namespace
	GuestSHA, PowerShellSHA, ArchiveSHA, LogSHA string
	Failure                                     string
	LogBytes                                    int
	ListenerExit                                int
	RegistrationMayExist, RegistrationGone      bool
	LockReleased, CredentialCleanupDenied       bool
	CapacityObserved                            bool
	VMAvailable, HostAvailable, DiskFree        uint64
	MemoryObservation                           string
	AllRequiredChecksSuccess                    bool
	Resources                                   string
}

func jobBudget(k Kind, pol execPolicy) time.Duration {
	if k == KindRust {
		return pol.timeout()
	}
	return 6 * time.Hour
}

func isELF(b []byte) bool { return len(b) >= 64 && bytes.Equal(b[:4], []byte{127, 'E', 'L', 'F'}) }

// runJob executes exactly one Plan. It returns nil only after the official job
// concluded success on THIS helper's unique runner AND every hook/source proof
// matched; listener exit alone never certifies a job.
func runJob(ctx context.Context, planFile, dir, guestFile, pwshFile string) (ret error) {
	p, e := LoadPlan(planFile)
	if e != nil {
		return e
	}
	k, _ := p.kind()
	if e = activeContract.Validate(p); e != nil {
		return e
	}
	custom, defaults, e := registrationLabels(k, activeContract.Labels(p))
	if e != nil {
		return e
	}
	if e = ownedReceiptRoot(ctx, dir); e != nil {
		return e
	}
	guest, e := os.ReadFile(guestFile)
	if e != nil || !isELF(guest) {
		return errors.New("reviewed Linux helper binary required")
	}
	pwsh, e := os.ReadFile(pwshFile)
	if e != nil || digest(pwsh) != powerShellSHA {
		return errors.New("official PowerShell asset digest rejected")
	}
	preflight, stop := context.WithTimeout(ctx, 2*time.Minute)
	e = activeContract.Verify(preflight, p, githubRead)
	var policy execPolicy
	if e == nil && k == KindRust {
		var wf []byte
		if wf, e = fetchWorkflow(preflight, p); e == nil {
			policy, e = workflowExecution(wf)
		}
	}
	stop()
	if e != nil {
		return e
	}
	ctx, cancelBudget := context.WithTimeout(ctx, jobBudget(k, policy)+7*time.Minute)
	defer cancelBudget()
	rec := receipt{RepoID: repoID, Plan: p, Kind: k, GuestSHA: digest(guest), PowerShellSHA: digest(pwsh),
		Resources: fmt.Sprintf("2 CPU, %d GiB, no swap, 1 GiB reserve; Cargo/test/lld threads=1", profileFor(k).memGiB)}
	if e = saveNew(filepath.Join(dir, "intent.private.json"), rec); e != nil {
		return e
	}
	lockPath := filepath.Join(os.TempDir(), "civ6-ci-linux.lock")
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("the single Linux worker is occupied; inspect its receipt")
	}
	_, e = fmt.Fprintf(lock, "%d\n%d\n%s\n", p.RunID, p.JobID, dir)
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
		ret = errors.Join(ret, saveNew(filepath.Join(dir, "result.private.json"), rec))
	}()
	n, e := newNamespace(ctx, dir, k, p)
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
	// Real VM/host capacity before anything is registered or any token exists.
	vm, e := n.vmMemAvailable(ctx)
	if e != nil {
		return e
	}
	phys, disk, e := hostResources(dir)
	if e != nil {
		return e
	}
	rec.CapacityObserved, rec.VMAvailable, rec.HostAvailable, rec.DiskFree = true, vm, phys, disk
	if e = checkCapacity(k, vm, phys, disk); e != nil {
		return e
	}
	if e = n.execStdin(ctx, guest, "cat > "+guestBinary+" && chmod 755 "+guestBinary); e != nil {
		return errors.New("guest helper transfer failed")
	}
	if k == KindRust {
		if e = cmd(ctx, dockerExe, "exec", n.ID, "sh", "-c", "mkdir -p /toolchain/cargo/bin /toolchain/rustup /work/tool-tmp && cp /opt/rust/bin/rustup /toolchain/cargo/bin/rustup").Run(); e != nil {
			return errors.New("toolchain preparation failed")
		}
		if e = n.execStdin(ctx, pwsh, "mkdir -p /work/powershell && tar -xz -C /work/powershell && chmod 755 /work/powershell/pwsh"); e != nil {
			return errors.New("public PowerShell stdin supply failed")
		}
		if rec.ArchiveSHA, e = materializeSource(ctx, p, n); e != nil {
			return e
		}
		if e = cmd(ctx, dockerExe, "exec", n.ID, "git", "-C", workspace, "remote", "add", "origin", "https://github.com/"+repoFull).Run(); e != nil {
			return errors.New("credential-free origin setup failed")
		}
		if e = hostCheckout(ctx, p, n); e != nil {
			return e
		}
	}
	if e = activeContract.Verify(ctx, p, githubRead); e != nil {
		return e
	}
	if e = checkQueue(ctx, p, custom, githubRead); e != nil {
		return e
	}
	rs, e := repoRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range rs {
		for _, l := range r.Labels {
			if l.Name == custom {
				return errors.New("dedicated runner label already has a runner")
			}
		}
	}
	flight, cancel := context.WithCancel(ctx)
	defer cancel()
	h := &host{plan: p, kind: k, policy: policy, ns: n, dir: dir, target: filepath.Join(dir, "cargo-target")}
	h.recheck = func(c context.Context) error {
		if e := activeContract.VerifyAssigned(c, p, n.Name, githubRead); e != nil {
			return e
		}
		if k == KindRust {
			return hostCheckout(c, p, n)
		}
		return nil
	}
	h.execute = h.run
	endpoint, closeServer, e := startHost(flight, h)
	if e != nil {
		return e
	}
	defer func() { cancel(); ret = errors.Join(ret, closeServer()) }()
	token, e := registrationToken(ctx)
	if e != nil {
		return e
	}
	// Persist registration uncertainty before the RAM-only token is handed over.
	rec.RegistrationMayExist = true
	if e = saveNew(filepath.Join(dir, "registration-intent.private.json"), map[string]any{"name": n.Name, "run_id": p.RunID, "job_id": p.JobID}); e != nil {
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
		cc, stopCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCleanup()
		rs, e := repoRunners(cc)
		if e != nil {
			ret = errors.Join(ret, e)
			return
		}
		for _, r := range rs {
			if r.Name == n.Name {
				ret = errors.Join(ret, deleteRunner(cc, r.ID))
			}
		}
		if rs, e = repoRunners(cc); e != nil {
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
	packet := guestPacket{Plan: p, Kind: k, Policy: policy, Name: n.Name, Token: token, Callback: endpoint, Nonce: h.key, Custom: custom, Defaults: defaults}
	input, e := json.Marshal(packet)
	token, packet.Token = "", ""
	if e != nil {
		return e
	}
	defer func() {
		for i := range input {
			input[i] = 0
		}
	}()
	logFile, e := os.OpenFile(filepath.Join(dir, "official-listener.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	x := cmd(ctx, dockerExe, "exec", "-i", n.ID, guestBinary, "guest-runner")
	x.Stdin = bytes.NewReader(input)
	x.Stdout, x.Stderr = logFile, logFile
	e = x.Run()
	rec.ListenerExit = exitStatus(e)
	ret = errors.Join(ret, logFile.Sync(), logFile.Close())
	if ret != nil {
		return ret
	}
	data, le := os.ReadFile(logFile.Name())
	if le != nil {
		return le
	}
	rec.LogBytes, rec.LogSHA = len(data), digest(data)
	// Kernel counters only (max, peak, events): no credential, no product output.
	mctx, stopMemory := context.WithTimeout(context.Background(), 5*time.Second)
	memory, me := boundedOutput(cmd(mctx, dockerExe, "exec", n.ID, "cat", "/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory.peak", "/sys/fs/cgroup/memory.events"), 4096)
	stopMemory()
	if me == nil {
		rec.MemoryObservation = string(memory)
	}
	if e != nil && !(k == KindRust && credentialCleanupDenied(rec.ListenerExit, data)) {
		return errors.New("official listener failed; preserve the original private output")
	}
	rec.CredentialCleanupDenied = e != nil
	for _, stage := range []string{"start", "complete"} {
		b, e := cmd(ctx, dockerExe, "exec", n.ID, "cat", "/home/runner/hook-"+stage+".json").Output()
		var observed Plan
		if e != nil || json.Unmarshal(b, &observed) != nil || observed != p {
			return errors.New("official job hook pin missing")
		}
		if e = os.WriteFile(filepath.Join(dir, "hook-"+stage+".private.json"), b, 0600); e != nil {
			return e
		}
	}
	if k == KindRust {
		if e = hostCheckout(ctx, p, n); e != nil {
			return e
		}
	}
	if e = awaitSuccess(ctx, p, n.Name, activeContract.RequireSuccess, githubRead, pauseOneSecond, 30); e != nil {
		return e
	}
	rec.AllRequiredChecksSuccess = true
	out, _ := json.Marshal(map[string]any{"status": "success", "kind": k, "run_id": p.RunID, "job_id": p.JobID, "log_sha256": rec.LogSHA, "log_bytes": rec.LogBytes})
	fmt.Println(string(out))
	return nil
}

// awaitSuccess polls the parent's RequireSuccess for a bounded number of tries.
// Only a completed/success official job on this runner ends it with nil.
func awaitSuccess(ctx context.Context, p Plan, runner string, require func(context.Context, Plan, string, ReadAPI) error, read ReadAPI, pause func(context.Context) error, tries int) error {
	var last error
	for i := 0; i < tries; i++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		if last = require(ctx, p, runner, read); last == nil {
			return nil
		}
		if i+1 < tries {
			if e := pause(ctx); e != nil {
				return e
			}
		}
	}
	return errors.New("official target job success not confirmed: " + last.Error() + " (tries=" + strconv.Itoa(tries) + ")")
}
