package main

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const originalRepo = "AutoDevJapan/GoDD-MCP-Alpha"

func runnerAPI(ctx context.Context, method, endpoint string, payload, out any) error {
	args := []string{"api", "--method", method, endpoint}
	var input []byte
	if payload != nil {
		var e error
		input, e = json.Marshal(payload)
		if e != nil {
			return errors.New("runner request rejected")
		}
		args = append(args, "--input", "-")
	}
	c := command(ctx, `D:\CC\Tools\codex-jobs\gh_gate.exe`, args...)
	c.Stderr = io.Discard
	c.Stdin = bytes.NewReader(input)
	var b limitedCapture
	b.limit = 1 << 20
	c.Stdout = &b
	if c.Run() != nil {
		return errors.New("runner API operation failed")
	}
	if out != nil && json.Unmarshal(b.data.Bytes(), out) != nil {
		return errors.New("runner API response rejected")
	}
	return nil
}

type limitedCapture struct {
	data  bytes.Buffer
	limit int
}

func (b *limitedCapture) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.data.Len() {
		return 0, errors.New("capture bound exceeded")
	}
	return b.data.Write(p)
}
func verifyRunnerRepository(ctx context.Context) error {
	var r struct {
		ID            int64
		FullName      string `json:"full_name"`
		Private, Fork bool
		Owner         struct{ Login string }
		Permissions   struct{ Admin bool }
	}
	if runnerAPI(ctx, "GET", "repositories/1154066087", nil, &r) != nil || r.ID != 1154066087 || r.FullName != originalRepo || r.Owner.Login != "AutoDevJapan" || !r.Private || r.Fork || !r.Permissions.Admin {
		return errors.New("runner repository identity rejected")
	}
	return nil
}

type originalRunner struct {
	ID           int64
	Name, Status string
	Busy         bool
	Labels       []struct{ Name string }
}

func originalRunners(ctx context.Context) ([]originalRunner, error) {
	if e := verifyRunnerRepository(ctx); e != nil {
		return nil, e
	}
	var all []originalRunner
	for page := 1; page <= 10; page++ {
		var b struct {
			Total   int `json:"total_count"`
			Runners []originalRunner
		}
		if e := runnerAPI(ctx, "GET", fmt.Sprintf("repos/%s/actions/runners?per_page=100&page=%d", originalRepo, page), nil, &b); e != nil {
			return nil, e
		}
		if b.Total > 1000 {
			return nil, errors.New("runner inventory exceeds bound")
		}
		all = append(all, b.Runners...)
		if page*100 >= b.Total {
			return all, nil
		}
	}
	return nil, errors.New("runner inventory incomplete")
}
func exactOfflineRunner(r originalRunner, name string) bool {
	return r.Name == name && len(r.Labels) == 1 && r.Labels[0].Name == offlineLabel
}

func bootSDK(ctx context.Context, root, sdkRoot, helper, instance string) (s vmState, i identity, r ready, ret error) {
	return bootSDKForSource(ctx, root, sdkRoot, helper, instance, fixedSourcePin(), 2, 2)
}
func bootSDKForSource(ctx context.Context, root, sdkRoot, helper, instance string, pin sourcepacket.Pin, cpus, memoryGiB int) (s vmState, i identity, r ready, ret error) {
	var template struct {
		Path, SHA256, Instance string
		VMID                   string `json:"source_vm"`
		Removed                bool   `json:"vm_removed"`
		Source                 bool   `json:"product_source"`
		Registered             bool   `json:"runner_registered"`
	}
	b, e := os.ReadFile(filepath.Join(sdkRoot, "sdk-template-ready.private.json"))
	if e != nil || json.Unmarshal(b, &template) != nil || !template.Removed || template.Source || template.Registered || !validVMID(template.VMID) || !validInstance(template.Instance) {
		return s, i, r, errors.New("SDK identity rejected")
	}
	rel, e := filepath.Rel(sdkRoot, template.Path)
	if e != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(template.Path) != "guest.vhdx" {
		return s, i, r, errors.New("SDK path rejected")
	}
	state := vmState{Instance: template.Instance, Name: "cc-ci-2737-" + template.Instance, Root: filepath.Dir(template.Path), Pipe: `\\.\pipe\cc-ci-2737-` + template.Instance}
	if e = admin(ctx, root, "sdk-absence", vmPrelude(state)+"Assert-Absent "+psQuote(template.VMID)+"\n"); e != nil {
		return s, i, r, e
	}
	f, e := os.Open(template.Path)
	if e != nil {
		return s, i, r, e
	}
	h, e := hashReader(f)
	_ = f.Close()
	if e != nil || !strings.EqualFold(h, template.SHA256) {
		return s, i, r, errors.New("SDK digest rejected")
	}
	i, e = newIdentity()
	if e != nil {
		return s, i, r, e
	}
	tools, e := readTools(sdkRoot)
	if e != nil {
		return s, i, r, e
	}
	if e = toolInspect(ctx, tools); e != nil {
		return s, i, r, e
	}
	if command(ctx, dockerExe, "start", tools.ID).Run() != nil {
		return s, i, r, errors.New("owned SDK tool start failed")
	}
	if e = prepareSeedForSource(ctx, root, sdkRoot, helper, instance, i, pin); e != nil {
		return s, i, r, e
	}
	s, e = createVMBudget(ctx, root, instance, template.Path, "", cpus, memoryGiB)
	if e != nil {
		return s, i, r, e
	}
	exe, e := os.Executable()
	if e != nil {
		return s, i, r, e
	}
	body := "& " + psQuote(exe) + " serial " + psQuote(s.Pipe) + " " + psQuote(instance) + " " + psQuote(filepath.Join(root, "guest-ready.private.json")) + "\nif($LASTEXITCODE -ne 0){throw 'SERIAL_PROBE_FAILED'}\n"
	if e = admin(ctx, root, "vm-readiness", body); e != nil {
		return s, i, r, e
	}
	b, e = os.ReadFile(filepath.Join(root, "guest-ready.private.json"))
	if e != nil || json.Unmarshal(b, &r) != nil || validateReady(r, instance) != nil {
		return s, i, r, errors.New("guest readiness rejected")
	}
	return s, i, r, nil
}

func bootstrapNormalRunner(ctx context.Context, root, sdkRoot, helper string) (ret error) {
	p, e := sourcepacket.Fetch(ctx, fixedSourcePin())
	if e != nil {
		return e
	}
	raw, e := sourcepacket.Encode(p)
	if e != nil {
		return e
	}
	instance := newID()
	attempt := filepath.Join(root, "bootstrap-"+instance)
	if e = os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	name := "todo2737-" + instance
	mayExist := false
	registrationGone := false
	vmGone := false
	s, i, r, e := bootSDK(ctx, attempt, sdkRoot, helper, instance)
	defer func() {
		if s.ID != "" {
			clean, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			ce := cleanupVM(clean, s)
			ret = errors.Join(ret, ce)
			vmGone = ce == nil
		}
		if mayExist && vmGone {
			clean, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			list, ce := originalRunners(clean)
			ret = errors.Join(ret, ce)
			if ce != nil {
				return
			}
			for _, runner := range list {
				if runner.Name == name {
					if !exactOfflineRunner(runner, name) {
						ret = errors.Join(ret, errors.New("offline registration ownership changed"))
						return
					}
					if ce = runnerAPI(clean, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", originalRepo, runner.ID), nil, nil); ce != nil {
						ret = errors.Join(ret, ce)
						return
					}
				}
			}
			list, ce = originalRunners(clean)
			if ce != nil {
				ret = errors.Join(ret, ce)
				return
			}
			for _, runner := range list {
				if runner.Name == name {
					ret = errors.Join(ret, errors.New("offline registration absence unverified"))
					return
				}
			}
			registrationGone = true
		}
		ret = errors.Join(ret, record(attempt, "bootstrap-cleanup.private.json", map[string]any{"vm_gone": vmGone, "registration_may_exist": mayExist, "registration_gone": registrationGone, "official_ci_success": false}))
	}()
	if e != nil {
		return e
	}
	c, e := guestDial(i, r)
	if e != nil {
		return e
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Minute))
	if e = json.NewEncoder(c).Encode(request{"materialize-source", instance}); e == nil {
		e = writeBlob(c, raw)
	}
	var proof sourceResult
	if e == nil {
		e = json.NewDecoder(io.LimitReader(c, 8192)).Decode(&proof)
	}
	_ = c.Close()
	raw = nil
	if e != nil || proof.ID != instance || proof.Failure != "" || proof.OfficialCI || proof.Proof.HeadSHA != fixedSourcePin().HeadSHA || proof.Proof.TreeSHA != fixedSourcePin().TreeSHA || !proof.Proof.PhysicalBlobsVerified {
		return errors.New("offline source proof rejected")
	}
	if e = record(attempt, "source-proof.private.json", proof); e != nil {
		return e
	}
	if _, e = sourcepacket.Fetch(ctx, fixedSourcePin()); e != nil {
		return e
	}
	list, e := originalRunners(ctx)
	if e != nil {
		return e
	}
	for _, runner := range list {
		if runner.Name == name {
			return errors.New("offline registration already exists")
		}
	}
	if e = record(attempt, "registration-intent.private.json", map[string]any{"repo_id": 1154066087, "name": name, "label": offlineLabel, "listener_started": false}); e != nil {
		return e
	}
	var token struct{ Token string }
	if e = runnerAPI(ctx, "POST", "repos/"+originalRepo+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return e
	}
	if token.Token == "" {
		return errors.New("registration token absent")
	}
	c, e = guestDial(i, r)
	if e != nil {
		return e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Minute))
	if e = json.NewEncoder(c).Encode(request{"configure-offline", instance}); e != nil {
		return e
	}
	input, _ := json.Marshal(offlineConfiguration{Name: name, Token: token.Token})
	token.Token = ""
	mayExist = true
	e = writeBlob(c, input)
	for n := range input {
		input[n] = 0
	}
	input = nil
	if e != nil {
		return errors.New("offline configuration transfer failed")
	}
	var response offlineResult
	if e = json.NewDecoder(io.LimitReader(c, 8192)).Decode(&response); e != nil || response.ID != instance || !response.Configured || response.Failure != "" {
		return errors.New("official offline configuration failed")
	}
	list, e = originalRunners(ctx)
	if e != nil {
		return e
	}
	found := false
	for _, runner := range list {
		if runner.Name == name {
			if found || !exactOfflineRunner(runner, name) || runner.Status != "offline" || runner.Busy {
				return errors.New("offline registration state rejected")
			}
			found = true
		}
	}
	if !found {
		return errors.New("official offline runner absent")
	}
	if _, e = sourcepacket.Fetch(ctx, fixedSourcePin()); e != nil {
		return e
	}
	return record(attempt, "bootstrap-proof.private.json", map[string]any{"repo_id": 1154066087, "source_sha": fixedSourcePin().HeadSHA, "configured_offline": true, "listener_started": false, "cargo_compiled": false, "official_ci_success": false})
}

func runGatedBootstrap(ctx context.Context, root, sdkRoot, helper string) error {
	s, e := startGatedWork(ctx, gatedHello{Mode: "offline-bootstrap", Root: root, SDKRoot: sdkRoot, Helper: helper})
	if e != nil {
		return e
	}
	held, e := probeTargetLease(s.frame.Target)
	if e != nil || !held {
		_ = s.input.Close()
		_ = s.cmd.Wait()
		return errors.New("bootstrap target lease unverified")
	}
	if e = s.start(); e != nil {
		_ = s.input.Close()
		_ = s.cmd.Wait()
		return e
	}
	// Closing stdin is the only admitted cancellation route. Never kill the
	// gate while its VM/work/registration cleanup is in flight.
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
		return errors.New("bootstrap target lease release unverified")
	}
	return nil
}
