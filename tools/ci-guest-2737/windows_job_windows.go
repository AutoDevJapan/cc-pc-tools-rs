package main

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	wr "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// This adapter links the reviewed backend. It cannot choose a host executable
// from a product request. API seams are internal, solely for meaningful refusal
// tests; the live constructor always supplies the existing fixed policy/API.
type windowsController struct {
	root                                      string
	q                                         linuxJobRequest
	seed                                      wr.Seed
	binding                                   jobBinding
	bind                                      func(context.Context, int64, string) (jobBinding, error)
	list                                      func(context.Context) ([]originalRunner, error)
	api                                       func(context.Context, string, string, any, any) error
	save                                      func(string, any) error
	runnerID                                  int64
	mayExist, sourceReady, started, completed bool
	clockReady                                bool
}

func (w *windowsController) ClockObserved(ctx context.Context, p wr.Plan, receipt wr.ClockReceipt) error {
	if ctx.Err() != nil || !w.valid(p) || w.clockReady || w.sourceReady || w.mayExist || receipt.Instance != p.Instance || receipt.AfterUnixMillis < receipt.HostUnixMillis-1000 || receipt.AfterUnixMillis > receipt.HostUnixMillis+6000 {
		return errors.New("Windows UTC observation rejected")
	}
	if e := w.save("windows-clock-sync.private.json", receipt); e != nil {
		return e
	}
	w.clockReady = true
	return nil
}

func (w *windowsController) valid(p wr.Plan) bool {
	return p.Validate() == nil && p.Source == w.seed.Plan.Source && p.Instance == w.seed.Plan.Instance && p.RunID == w.seed.Plan.RunID && p.JobID == w.seed.Plan.JobID && p.Attempt == w.seed.Plan.Attempt && p.Jobs == w.seed.Plan.Jobs && p.Threads == w.seed.Plan.Threads && p.Role == w.seed.Plan.Role && p.Label == w.seed.Plan.Label && p.Name == w.seed.Plan.Name
}
func (w *windowsController) Queued(ctx context.Context, p wr.Plan) (jobBinding, error) {
	if !w.valid(p) {
		return jobBinding{}, errors.New("Windows plan changed")
	}
	b, e := w.bind(ctx, 0, "")
	if e != nil || b != w.binding || wr.ValidateBinding(p, b) != nil {
		return jobBinding{}, errors.New("Windows queue changed")
	}
	return b, nil
}
func (w *windowsController) Token(ctx context.Context, p wr.Plan) ([]byte, error) {
	if !w.clockReady || !w.sourceReady || w.mayExist || !w.valid(p) {
		return nil, errors.New("Windows token stage rejected")
	}
	if _, e := w.Queued(ctx, p); e != nil {
		return nil, e
	}
	list, e := w.list(ctx)
	if e != nil {
		return nil, e
	}
	for _, r := range list {
		if r.Name == p.Name {
			return nil, errors.New("Windows runner nonce exists")
		}
	}
	profile, ok := p.Source.Profile()
	if !ok {
		return nil, errors.New("registration profile rejected")
	}
	if e = w.save("registration-intent.private.json", map[string]any{"repo_id": profile.RepoID, "name": p.Name, "label": p.Label, "job_id": p.JobID}); e != nil {
		return nil, e
	}
	// Any lost configuration reply must leave a recoverable nonce intent.
	w.mayExist = true
	var token struct{ Token string }
	if e = w.api(ctx, "POST", "repos/"+profile.Repository+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return nil, e
	}
	if len(token.Token) == 0 || len(token.Token) > 4096 {
		return nil, errors.New("Windows token unavailable")
	}
	value := []byte(token.Token)
	token.Token = ""
	return value, nil
}
func (w *windowsController) Registered(ctx context.Context, p wr.Plan) (int64, error) {
	if !w.valid(p) || !w.mayExist || w.runnerID != 0 {
		return 0, errors.New("Windows registration stage rejected")
	}
	list, e := w.list(ctx)
	if e != nil {
		return 0, e
	}
	var id int64
	for _, r := range list {
		if r.Name == p.Name {
			if id != 0 || r.ID < 1 || r.Status != "offline" || r.Busy || len(r.Labels) != 1 || r.Labels[0].Name != p.Label {
				return 0, errors.New("Windows offline registration rejected")
			}
			id = r.ID
		}
	}
	if id < 1 {
		return 0, errors.New("Windows offline registration absent")
	}
	// Save BEFORE acknowledging listener startup.
	if e = w.save("runner-identity.private.json", map[string]any{"runner_id": id, "name": p.Name, "label": p.Label, "job_id": p.JobID}); e != nil {
		return 0, e
	}
	w.runnerID = id
	return id, nil
}
func (w *windowsController) Claim(ctx context.Context, p wr.Plan, id int64, event wr.Event) error {
	if !w.valid(p) || id < 1 || id != w.runnerID || event.RunnerID != id || event.Instance != p.Instance || event.Context.Validate(p) != nil || !checkPhysicalProof(event.Proof, w.q) {
		return errors.New("Windows hook identity rejected")
	}
	if (event.Stage == wr.Start && (w.started || w.completed)) || (event.Stage == wr.Complete && (!w.started || w.completed)) || (event.Stage != wr.Start && event.Stage != wr.Complete) {
		return errors.New("Windows hook order rejected")
	}
	b, e := w.bind(ctx, id, p.Name)
	if e != nil || b != w.binding {
		return errors.New("Windows official claim changed")
	}
	if event.Stage == wr.Start {
		w.started = true
	} else {
		w.completed = true
	}
	return nil
}
func (w *windowsController) Observe(ctx context.Context, event wr.Event) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if event.Instance != w.seed.Plan.Instance {
		return errors.New("Windows observation instance rejected")
	}
	if event.Stage == wr.SourceReady && !w.clockReady {
		return errors.New("Windows source before UTC confirmation")
	}
	if e := w.save("windows-event-"+string(event.Stage)+".private.json", event); e != nil {
		return e
	}
	if event.Stage == wr.SourceReady {
		w.sourceReady = true
	}
	return nil
}

func executeWindowsJob(ctx context.Context, root, helper, file string, jobs, threads int) (ret error) {
	q, e := readJobRequest(file)
	if e != nil {
		return e
	}
	policy, ok := jobPolicy(q.Source, q.Role)
	if !ok || policy.OS != "Windows" {
		return errors.New("Windows role rejected")
	}
	executor, e := linkedExecutorIdentity()
	if e != nil {
		return e
	}
	if e = verifyWindowsHelper(helper, executor); e != nil {
		return e
	}
	instance := newID()
	attempt := filepath.Join(root, "job-"+instance)
	if e = os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	if e = record(attempt, "executor-identity.private.json", executor); e != nil {
		return e
	}
	lock, e := os.OpenFile(`D:\CC\Tools\product-ci\todo2737-normal-ci.lock`, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	var over syscall.Overlapped
	if ok, _, _ := lockFileProc.Call(lock.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&over))); ok == 0 {
		return errors.New("another normal CI job owns admission")
	}
	intentPath := `D:\CC\Tools\product-ci\todo2737-normal-ci.intent.private.json`
	if e = writeNew(intentPath, []byte(fmt.Sprintf("{\"instance\":%q,\"attempt\":%q,\"run_id\":%d,\"role\":%q}", instance, attempt, q.RunID, q.Role))); e != nil {
		return errors.New("normal CI recovery intent already exists or could not persist")
	}
	var vm vmState
	var conn net.Conn
	w := &windowsController{root: attempt, q: q}
	w.save = func(name string, v any) error { return record(attempt, name, v) }
	w.api = runnerAPI
	w.list = func(c context.Context) ([]originalRunner, error) { return originalRunnersFor(c, q.Source) }
	w.bind = func(c context.Context, id int64, name string) (jobBinding, error) {
		return bindOfficialJob(c, attempt, q, id, name)
	}
	vmGone := true
	var cloneLease windowsCloneLease
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
		clean, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		ret = finishWindowsCleanup(clean, ret, attempt, intentPath, instance, vm, vmGone, w, cloneLease, func() error { return observeWindowsCloneDetached(clean, attempt) })
	}()
	w.binding, e = w.bind(ctx, 0, "")
	if e != nil {
		return e
	}
	identity, e := sdk.NewHostIdentity(instance)
	if e != nil {
		return e
	}
	defer func() {
		if key, ok := identity.Certificate.PrivateKey.(ed25519.PrivateKey); ok {
			clear(key)
		}
	}()
	w.seed, e = wr.NewSeed(q.Source, w.binding, instance, sdk.CertificateHash(identity.Leaf), jobs, threads)
	if e != nil {
		return e
	}
	bootstrapSeed, runnerSeed, e := wr.PublicSeeds(w.seed, identity)
	if e != nil {
		return e
	}
	if e = prepareWindowsClone(ctx, attempt, helper, bootstrapSeed, runnerSeed, &cloneLease); e != nil {
		return e
	}
	vmGone = false
	vm, e = createVMPlatform(ctx, attempt, instance, "", "", jobs, 8, true)
	if e != nil {
		return e
	}
	boot, e := windowsBootstrap(ctx, attempt, instance)
	if e != nil {
		return e
	}
	conn, e = wr.Dial(ctx, boot, identity, w.seed)
	if e != nil {
		return e
	}
	packet, e := sourcepacket.Fetch(ctx, q.Source)
	if e != nil {
		return e
	}
	_, e = wr.HostSession(ctx, conn, w.seed, packet, w)
	packet = sourcepacket.Packet{}
	if e != nil {
		return e
	}
	return awaitOfficialCompletion(ctx, attempt, q, w.binding, w.runnerID, w.seed.Plan.Name)
}
func normalWindowsLabel(runID int64) string { return wr.Label(runID) }

// The same completion path is used for staging failures before New-VM and for
// post-run cleanup. No successful VM/runner absence can hide an attached clone.
// observe is an internal read-only seam; the live caller always uses the broker.
func finishWindowsCleanup(ctx context.Context, ret error, attempt, intentPath, instance string, vm vmState, vmGone bool, w *windowsController, cloneLease windowsCloneLease, observe func() error) error {
	if vm.ID != "" {
		ce := cleanupVM(ctx, vm)
		ret = errors.Join(ret, ce)
		vmGone = ce == nil
	}
	registrationGone := !w.mayExist
	if w.mayExist && vmGone {
		ce := clearOwnedRegistrationFor(ctx, w.q.Source, "todo2737-"+instance, jobLabel(w.q.Source, w.q.Role, w.q.RunID, w.q.Attempt), w.runnerID)
		ret = errors.Join(ret, ce)
		registrationGone = ce == nil
	}
	cloneErr := reconcileWindowsClone(cloneLease, observe)
	cloneDetached := cloneErr == nil
	ret = errors.Join(ret, cloneErr)
	ce := record(attempt, "job-cleanup.private.json", map[string]any{"vm_gone": vmGone, "clone_stage_started": cloneLease.StageStarted, "clone_detached": cloneDetached, "registration_gone": registrationGone, "registration_may_exist": w.mayExist, "runner_id": w.runnerID, "official_ci_success": ret == nil})
	ret = errors.Join(ret, ce)
	if vmGone && registrationGone && cloneDetached && ce == nil {
		ret = errors.Join(ret, os.Remove(intentPath))
	}
	if !vmGone || !cloneDetached {
		ret = errors.Join(ret, &unsafeVMLease{State: vm, Root: attempt, Instance: instance, WindowsClone: cloneLease.StageStarted})
	}
	return ret
}
