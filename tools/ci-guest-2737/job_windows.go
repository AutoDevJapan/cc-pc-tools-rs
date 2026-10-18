package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// A caller must explicitly supply the reviewed revision and official run. This
// command cannot update a ref, enable routes, dispatch a workflow or select HEAD.
type linuxJobRequest struct {
	Source     sourcepacket.Pin
	RunID      int64
	Attempt    int
	Role       string
	RouteLease string
	Inputs     map[string]string
}
type jobBinding = normalci.Binding

func readJobRequest(path string) (q linuxJobRequest, e error) {
	f, e := os.Open(path)
	if e != nil {
		return q, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 32769))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF || !validSourcePin(q.Source) || q.RunID < 1 || q.Attempt < 1 || (q.Role != "verify" && q.Role != "ci-gate" && q.Role != "windows-native-check") || !normalci.ValidLease(q.RouteLease) || len(q.Inputs) != 0 {
		return q, errors.New("live request rejected")
	}
	return q, nil
}
func bindOfficialJob(ctx context.Context, root string, q linuxJobRequest, runnerID int64, name string) (b jobBinding, e error) {
	// Persist only expectations and returned numeric/hash metadata, never source.
	pin := struct {
		Source struct {
			ID  int64
			PR  int
			SHA string
		}
		Tree, WorkflowSHA string
		RunID             int64
		Attempt           int
		Role              string
		RunnerID          int64
		RunnerName        string
	}{}
	pin.Source.ID = 1154066087
	pin.Source.PR = 3613
	pin.Source.SHA = q.Source.HeadSHA
	pin.Tree = q.Source.TreeSHA
	pin.WorkflowSHA = q.Source.WorkflowSHA256
	pin.RunID = q.RunID
	pin.Attempt = q.Attempt
	pin.Role = q.Role
	pin.RunnerID = runnerID
	pin.RunnerName = name
	file := "binding-" + newID() + ".private.json"
	if e = record(root, file, pin); e != nil {
		return b, e
	}
	// Both the CLI and this controller link the SAME existing read-only policy.
	// There is no second executable, request-selected path or process environment.
	p := normalci.Pin{Source: normalci.Candidate{ID: pin.Source.ID, PR: pin.Source.PR, SHA: pin.Source.SHA}, Tree: pin.Tree, WorkflowSHA: pin.WorkflowSHA, RunID: q.RunID, Attempt: q.Attempt, Role: q.Role, RunnerID: runnerID, RunnerName: name, RouteLease: q.RouteLease}
	b, e = normalci.Plan(ctx, p, func(ctx context.Context, endpoint string, out any) error {
		return runnerAPI(ctx, "GET", endpoint, nil, out)
	})
	if e != nil {
		return b, fmt.Errorf("official job admission failed: %w", e)
	}
	if !validOfficialBinding(q, b) {
		return b, errors.New("official binding response rejected")
	}
	return b, nil
}
func validOfficialBinding(q linuxJobRequest, b jobBinding) bool {
	timeout := 30
	osName := "Linux"
	if q.Role == "ci-gate" {
		timeout = 5
	} else if q.Role == "windows-native-check" {
		timeout, osName = 20, "Windows"
	} else if q.Role != "verify" {
		return false
	}
	return b.RepoID == 1154066087 && b.PR == 3613 && b.RunID == q.RunID && b.Attempt == q.Attempt && b.JobID > 0 && b.SHA == q.Source.HeadSHA && b.Tree == q.Source.TreeSHA && b.WorkflowSHA == q.Source.WorkflowSHA256 && b.Role == q.Role && b.OS == osName && b.Label == normalci.RuntimeLabel(q.Role, q.RunID) && b.TimeoutMinutes == timeout && b.JobName != ""
}
func sendLiveFrame(c net.Conn, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	defer func() {
		for n := range b {
			b[n] = 0
		}
	}()
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return writeBlob(c, b)
}
func checkPhysicalProof(p sourcepacket.Receipt, q linuxJobRequest) bool {
	return p.RepoID == 1154066087 && p.PR == 3613 && p.HeadSHA == q.Source.HeadSHA && p.TreeSHA == q.Source.TreeSHA && p.PhysicalBlobsVerified && p.BlobCount > 0
}
func clearOwnedRegistration(ctx context.Context, name, label string, id int64) error {
	list, e := originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range list {
		if r.Name == name {
			if (id > 0 && r.ID != id) || len(r.Labels) != 1 || r.Labels[0].Name != label {
				return errors.New("live registration ownership changed")
			}
			if e = runnerAPI(ctx, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", originalRepo, r.ID), nil, nil); e != nil {
				return e
			}
		}
	}
	list, e = originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range list {
		if r.Name == name || (id > 0 && r.ID == id) {
			return errors.New("live registration absence unverified")
		}
	}
	return nil
}

func executeLinuxJob(ctx context.Context, root, sdkRoot, helper, requestFile string, jobs, threads int) (ret error) {
	q, e := readJobRequest(requestFile)
	if e != nil {
		return e
	}
	if q.Role == "windows-native-check" {
		return errors.New("Windows job requires Windows guest backend")
	}
	executor, e := linkedExecutorIdentity()
	if e != nil {
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
	// One dedicated kernel lock, separate from all old product-ci admissions.
	lock, e := os.OpenFile(`D:\CC\Tools\product-ci\todo2737-normal-ci.lock`, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	var over syscall.Overlapped
	ok, _, _ := lockFileProc.Call(lock.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&over)))
	if ok == 0 {
		return errors.New("another normal CI job owns admission")
	}
	// An unresolved durable intent survives process/host loss and prevents a new
	// invocation from masking a live or unknown VM. Recovery is explicit.
	intentPath := `D:\CC\Tools\product-ci\todo2737-normal-ci.intent.private.json`
	intent, e := os.OpenFile(intentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("normal CI recovery intent already exists")
	}
	e = json.NewEncoder(intent).Encode(map[string]any{"instance": instance, "attempt": attempt, "run_id": q.RunID, "source": q.Source})
	if e == nil {
		e = intent.Sync()
	}
	e = errors.Join(e, intent.Close())
	if e != nil {
		return e
	}
	vmGone, registrationGone, mayExist := true, true, false
	var vm vmState
	var conn net.Conn
	var runnerID int64
	name := "todo2737-" + instance
	label := normalci.RuntimeLabel(q.Role, q.RunID)
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
		clean, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		if vm.ID != "" {
			ce := cleanupVM(clean, vm)
			ret = errors.Join(ret, ce)
			vmGone = ce == nil
		}
		if mayExist && vmGone {
			ce := clearOwnedRegistration(clean, name, label, runnerID)
			ret = errors.Join(ret, ce)
			registrationGone = ce == nil
		}
		ce := record(attempt, "job-cleanup.private.json", map[string]any{"vm_gone": vmGone, "registration_gone": registrationGone, "registration_may_exist": mayExist, "runner_id": runnerID, "official_ci_success": ret == nil})
		ret = errors.Join(ret, ce)
		if vmGone && registrationGone && ce == nil {
			ret = errors.Join(ret, os.Remove(intentPath))
		}
		if !vmGone {
			ret = errors.Join(ret, &unsafeVMLease{State: vm, Root: attempt, Instance: instance})
		}
	}()
	binding, e := bindOfficialJob(ctx, attempt, q, 0, "")
	if e != nil {
		return e
	}
	plan := linuxJobPlan{Source: q.Source, RunID: q.RunID, JobID: binding.JobID, Attempt: q.Attempt, Jobs: jobs, Threads: threads, Role: q.Role, Name: name, Label: label, Inputs: q.Inputs}
	if !validLinuxJob(plan, q.Source, instance) {
		return errors.New("live plan rejected")
	}
	packet, e := sourcepacket.Fetch(ctx, q.Source)
	if e != nil {
		return e
	}
	raw, e := sourcepacket.Encode(packet)
	if e != nil {
		return e
	}
	packet = sourcepacket.Packet{}
	var ident identity
	var readyState ready
	vmGone = false
	vm, ident, readyState, e = bootSDKForSource(ctx, attempt, sdkRoot, helper, instance, q.Source, jobs, 8)
	if e != nil {
		if vm.ID == "" { // Constructor journal, rather than an empty ID, decides recovery.
			vmGone = false
		}
		return e
	}
	conn, e = guestDial(ident, readyState)
	if e != nil {
		return e
	}
	_ = conn.SetDeadline(time.Now().Add(time.Duration(binding.TimeoutMinutes+6) * time.Minute))
	connectionFinished := make(chan struct{})
	defer close(connectionFinished)
	go func(c net.Conn) {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-connectionFinished:
		}
	}(conn)
	if json.NewEncoder(conn).Encode(request{"live-job", instance}) != nil || sendLiveFrame(conn, plan) != nil {
		return errors.New("live initial transfer failed")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	if writeBlob(conn, raw) != nil {
		return errors.New("live source transfer failed")
	}
	raw = nil
	decoder := json.NewDecoder(io.LimitReader(conn, 1<<20))
	var event liveJobEvent
	if decoder.Decode(&event) != nil || event.ID != instance || event.Stage != "source-ready" || !checkPhysicalProof(event.Proof, q) {
		return errors.New("live source proof rejected")
	}
	if e = record(attempt, "source-proof.private.json", event); e != nil {
		return e
	}
	again, e := bindOfficialJob(ctx, attempt, q, 0, "")
	if e != nil || again != binding {
		return errors.New("queued job changed before registration")
	}
	list, e := originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range list {
		if r.Name == name {
			return errors.New("live runner nonce already exists")
		}
	}
	if e = record(attempt, "registration-intent.private.json", map[string]any{"repo_id": 1154066087, "name": name, "label": label, "job_id": binding.JobID}); e != nil {
		return e
	}
	var token struct{ Token string }
	if e = runnerAPI(ctx, "POST", "repos/"+originalRepo+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return e
	}
	if token.Token == "" {
		return errors.New("live token absent")
	}
	mayExist = true
	registrationGone = false
	e = sendLiveFrame(conn, token)
	token.Token = ""
	if e != nil {
		return errors.New("live configuration transfer failed")
	}
	event = liveJobEvent{}
	if decoder.Decode(&event) != nil || event.ID != instance || event.Stage != "configured" || !event.Configured {
		return errors.New("live configuration failed")
	}
	list, e = originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, r := range list {
		if r.Name == name {
			if runnerID != 0 || len(r.Labels) != 1 || r.Labels[0].Name != label || r.Busy || r.Status != "offline" {
				return errors.New("live offline runner rejected")
			}
			runnerID = r.ID
		}
	}
	if runnerID < 1 {
		return errors.New("configured live runner absent")
	}
	if e = record(attempt, "runner-identity.private.json", map[string]any{"runner_id": runnerID, "name": name, "label": label, "job_id": binding.JobID}); e != nil {
		return e
	}
	again, e = bindOfficialJob(ctx, attempt, q, 0, "")
	if e != nil || again != binding {
		return errors.New("queued job changed before listener")
	}
	if e = sendLiveFrame(conn, liveJobAck{ID: instance, Stage: "listen", Accepted: true, RunnerID: runnerID}); e != nil {
		return e
	}
	plan.RunnerID = runnerID
	for _, stage := range []string{"start", "complete"} {
		event = liveJobEvent{}
		if decoder.Decode(&event) != nil || event.ID != instance || event.Stage != stage || !checkPhysicalProof(event.Proof, q) || validateJobContext(plan, event.Context) != nil {
			return errors.New("live job hook proof rejected")
		}
		observed, e := bindOfficialJob(ctx, attempt, q, runnerID, name)
		if e != nil || observed != binding {
			return errors.New("official claim changed during hook")
		}
		if e = record(attempt, "hook-"+stage+".private.json", event); e != nil {
			return e
		}
		if e = sendLiveFrame(conn, liveJobAck{ID: instance, Stage: stage, Accepted: true, RunnerID: runnerID}); e != nil {
			return e
		}
	}
	event = liveJobEvent{}
	if decoder.Decode(&event) != nil || event.ID != instance || event.Stage != "runner-exited" || event.Exit == nil || *event.Exit != 0 {
		return errors.New("live runner exit rejected")
	}
	return awaitOfficialCompletion(ctx, attempt, q, binding, runnerID, name)
}

// Failure to prove that a VM stopped must not release CPU admission while the
// VM might still compile. The gate child retains its lease until a read-only
// broker observation proves absence or Off/memory-zero. Durable intent remains
// for registration/disk reconciliation. No rejected stop/delete is retried.
type unsafeVMLease struct {
	State          vmState
	Root, Instance string
	WindowsClone   bool
}

func (*unsafeVMLease) Error() string {
	return "VM quiescence unverified; shared Cargo admission fenced"
}
func observeVMQuiescence(ctx context.Context, f *unsafeVMLease) error {
	s := f.State
	if s.Instance == "" {
		s = vmState{Instance: f.Instance, Name: "cc-ci-2737-" + f.Instance, Root: f.Root, Pipe: `\\.\pipe\cc-ci-2737-` + f.Instance}
	}
	if !validInstance(s.Instance) || !validVMPipe(s) || s.Name != "cc-ci-2737-"+s.Instance || s.Root != f.Root {
		return errors.New("VM fence identity rejected")
	}
	body := vmPrelude(s) + `$all=@(Get-VM -ErrorAction Stop)
$owned=@($all|Where-Object {$_.Name -ceq $name})
if($owned.Count -gt 1){throw 'VM_FENCE_AMBIGUOUS'}
if($owned.Count -eq 1){
 $v=$owned[0]
 if($v.Notes -cne $notes -and -not [string]::IsNullOrEmpty($v.Notes)){throw 'VM_FENCE_NOTES_CHANGED'}
 $boundary=[IO.Path]::GetFullPath($root).TrimEnd('\')+'\'
 if(-not [IO.Path]::GetFullPath($v.ConfigurationLocation).StartsWith($boundary,[StringComparison]::OrdinalIgnoreCase)){throw 'VM_FENCE_LOCATION_CHANGED'}
 if($v.State -ne 'Off' -or $v.MemoryAssigned -ne 0){throw 'VM_FENCE_STILL_ACTIVE'}
}
`
	if validVMID(s.ID) {
		body += "$same=@($all|Where-Object {$_.Id.ToString() -ceq " + psQuote(s.ID) + "})\nif($same.Count -eq 1 -and $owned.Count -ne 1){throw 'VM_FENCE_NAME_CHANGED'}\n"
	}
	if f.WindowsClone {
		body += windowsCloneDetachedScript(f.Root)
	}
	return admin(ctx, f.Root, "vm-fence-observe-"+newID(), body)
}

// No hook result is an official success. The job API must independently finish.
func awaitOfficialCompletion(ctx context.Context, root string, q linuxJobRequest, b jobBinding, id int64, name string) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		var j struct {
			ID                       int64
			RunID                    int64  `json:"run_id"`
			RunnerID                 int64  `json:"runner_id"`
			RunnerName               string `json:"runner_name"`
			Status, Conclusion, Name string
			HeadSHA                  string `json:"head_sha"`
			Steps                    []struct {
				Number             int
				Status, Conclusion string
			}
		}
		if runnerAPI(ctx, "GET", fmt.Sprintf("repos/%s/actions/jobs/%d", originalRepo, b.JobID), nil, &j) != nil {
			return errors.New("official completion unavailable")
		}
		if j.ID != b.JobID || j.RunID != q.RunID || j.RunnerID != id || j.RunnerName != name || j.Name != b.JobName || j.HeadSHA != q.Source.HeadSHA {
			return errors.New("official completion identity rejected")
		}
		if j.Status == "completed" {
			if e := record(root, "official-job.private.json", j); e != nil {
				return e
			}
			if e := preserveOfficialJobLog(ctx, root, b.JobID); e != nil {
				return e
			}
			if j.Conclusion != "success" {
				return errors.New("official job failed; preserve original log before diagnosis")
			}
			return nil
		}
		timer.Reset(2 * time.Second)
	}
}

type boundedLogFile struct {
	f         *os.File
	remaining int64
}

func (w *boundedLogFile) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, errors.New("official log size bound exceeded")
	}
	n, e := w.f.Write(b)
	w.remaining -= int64(n)
	return n, e
}
func preserveOfficialJobLog(ctx context.Context, root string, id int64) error {
	// The CLI follows GitHub's signed redirect internally. Neither URL nor log
	// body reaches stdout/model. Partial bytes and the initial failure are kept.
	file := filepath.Join(root, "official-job-log.private.bin")
	f, e := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	c := command(ctx, `D:\CC\Tools\codex-jobs\gh_gate.exe`, "api", "--allow-escape-sequences", fmt.Sprintf("repos/%s/actions/jobs/%d/logs", originalRepo, id))
	c.Stderr = io.Discard
	c.Stdout = &boundedLogFile{f: f, remaining: 512 << 20}
	runErr := c.Run()
	syncErr := f.Sync()
	closeErr := f.Close()
	saved, e := os.Open(file)
	if e != nil {
		return e
	}
	sha, e := hashReader(saved)
	_ = saved.Close()
	if e != nil {
		return e
	}
	info, e := os.Stat(file)
	if e != nil {
		return e
	}
	e = record(root, "official-job-log-hash.private.json", map[string]any{"job_id": id, "bytes": info.Size(), "sha256": sha, "download_success": runErr == nil && syncErr == nil && closeErr == nil})
	if runErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(e, errors.New("official log download incomplete; preserve partial bytes"))
	}
	return e
}

func runGatedLinuxJob(ctx context.Context, root, sdkRoot, helper, file string) error {
	return runGatedJob(ctx, gatedHello{Mode: "linux-job", Root: root, SDKRoot: sdkRoot, Helper: helper, Request: file})
}
func runGatedWindowsJob(ctx context.Context, root, helper, file string) error {
	return runGatedJob(ctx, gatedHello{Mode: "windows-job", Root: root, Helper: helper, Request: file})
}
func runGatedJob(ctx context.Context, q gatedHello) error {
	s, e := startGatedWork(ctx, q)
	if e != nil {
		return e
	}
	held, e := probeTargetLease(s.frame.Target)
	if e != nil || !held {
		_ = s.input.Close()
		_ = s.cmd.Wait()
		return errors.New("live shared lease unverified")
	}
	if e = s.start(); e != nil {
		_ = s.input.Close()
		_ = s.cmd.Wait()
		return e
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.input.Close()
		case <-done:
		}
	}()
	_, e = s.finish(false)
	close(done)
	held, probe := probeTargetLease(s.frame.Target)
	if e != nil {
		return e
	}
	if probe != nil || held {
		return errors.New("live shared lease not released")
	}
	return nil
}
