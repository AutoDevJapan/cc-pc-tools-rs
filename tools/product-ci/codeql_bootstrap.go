package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func githubMutationCommand(ctx context.Context, method, endpoint string, body any) (*exec.Cmd, error) {
	args := []string{"api", "--method", method, endpoint}
	var b []byte
	if body != nil {
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			return nil, e
		}
		args = append(args, "--input", "-")
	}
	c := cmd(ctx, gh, args...)
	if body != nil {
		c.Stdin = bytes.NewReader(b)
	}
	return c, nil
}

func classifiedGitHubError(e error, stderr []byte) error {
	code := "unclassified"
	if m := regexp.MustCompile(`(?i)HTTP[ :()]+([0-9]{3})`).FindSubmatch(stderr); len(m) > 1 {
		code = string(m[1])
	}
	// Never include stderr, argv, response JSON or registration tokens. A stable
	// fingerprint plus HTTP/exit distinguishes validation, access and tool errors.
	return fmt.Errorf("GitHub mutation failed: http=%s exit=%d stderr_sha256=%s", code, exitStatus(e), digest(stderr))
}

type bootstrapReceipt struct {
	RepoID                              int64  `json:"repo_id"`
	CandidateSHA                        string `json:"candidate_sha"`
	Image                               string `json:"image"`
	Stage, Instance, ContainerID        string
	RunID                               int64 `json:"run_id"`
	TreeGone, Removed, RegistrationGone bool
	RouteRestored                       bool   `json:"route_restored_on_failure"`
	AdmissionLocked                     bool   `json:"admission_lifetime_locked"`
	Failure                             string `json:"failure,omitempty"`
	AllRequiredChecksSuccess            bool   `json:"all_required_checks_success"`
}

// Default setup requires an existing label. Configure one official ephemeral
// runner OFFLINE, submit routing, then dispose/deregister it before any job runner
// is launched. A label reservation and setup run are never candidate success.
func bootstrapCodeQL(ctx context.Context, c candidate, dir, binary string) (ret error) {
	return bootstrapCodeQLMode(ctx, c, dir, binary, false)
}

func bootstrapCodeQLMode(ctx context.Context, c candidate, dir, binary string, revalidate bool) (ret error) {
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	if revalidate {
		if _, _, e = revalidationPreflight(ctx, c, get); e != nil {
			return e
		}
	}
	guest, e := os.ReadFile(binary)
	if e != nil || len(guest) < 64 || !bytes.Equal(guest[:4], []byte{127, 'E', 'L', 'F'}) {
		return errors.New("Linux helper binary required")
	}
	rec := bootstrapReceipt{RepoID: c.ID, CandidateSHA: c.SHA, Image: codeQLSDK, Stage: "offline-label-intent", Instance: fmt.Sprintf("todo2648-repo-%d-codeql-bootstrap-%d", c.ID, time.Now().UnixNano())}
	filePath := filepath.Join(dir, "codeql-bootstrap.private.json")
	f, e := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new bootstrap receipt required")
	}
	if e = f.Close(); e != nil {
		return e
	}
	save := func() error {
		b, _ := json.MarshalIndent(rec, "", "  ")
		f, e := os.CreateTemp(dir, "bootstrap-*.private.tmp")
		if e != nil {
			return e
		}
		if _, e = f.Write(b); e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			return errors.Join(e, ce)
		}
		return os.Rename(f.Name(), filePath)
	}
	defer func() {
		if ret != nil {
			rec.Failure = ret.Error()
		}
		ret = errors.Join(ret, save())
	}()
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", c.ID))
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("candidate already occupied")
	}
	if e = lockAdmission(lock); e != nil {
		_ = lock.Close()
		return errors.New("candidate admission lock unavailable")
	}
	// The OS releases its byte lock after a crash. The durable nonce still
	// prevents recovery from confusing this bootstrap with a later product job.
	admission, _ := json.Marshal(map[string]any{"repo_id": c.ID, "candidate_sha": c.SHA, "instance": rec.Instance})
	if _, e = lock.WriteAt(admission, 1); e == nil {
		e = lock.Sync()
	}
	if e != nil {
		_ = lock.Close()
		return errors.New("candidate admission intent unavailable")
	}
	rec.AdmissionLocked = true
	creationUncertain, registrationMayExist, routeAttempted := false, false, false
	defer func() {
		ret = errors.Join(ret, lock.Close())
		if (!creationUncertain || rec.Removed) && (!registrationMayExist || rec.RegistrationGone) && !(ret != nil && routeAttempted && !rec.RouteRestored) {
			if e := os.Remove(lockPath); e != nil {
				ret = errors.Join(ret, e)
			}
		}
	}()
	rs, e := repoRunners(ctx, r.FullName)
	if e != nil {
		return e
	}
	for _, rr := range rs {
		for _, l := range rr.Labels {
			if l.Name == codeQLLabel(c.ID) {
				return errors.New("bootstrap label already assigned; inspect existing route")
			}
		}
	}
	creationUncertain = true
	if e = save(); e != nil {
		return e
	}
	b, createErr := cmd(ctx, docker, codeQLCreateArgs(rec.Instance)...).Output()
	probe, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	inspection, inspectErr := cmd(probe, docker, "inspect", rec.Instance).Output()
	var found []struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
	}
	if inspectErr != nil || json.Unmarshal(inspection, &found) != nil || len(found) != 1 || found[0].Name != "/"+rec.Instance || found[0].Config.Image != codeQLSDK || found[0].Config.Labels["cc.todo"] != "2648" || found[0].Config.Labels["cc.instance"] != rec.Instance {
		return errors.New("bootstrap creation unverified; retain admission")
	}
	rec.ContainerID = found[0].ID
	recoveryAttempted := false
	recoverRoute := func() {
		if ret != nil && routeAttempted {
			recoveryAttempted = true
			cc, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			var e error
			if revalidate {
				if _, statErr := os.Stat(filepath.Join(dir, "codeql-revalidation-intent.private.json")); os.IsNotExist(statErr) {
					// Preflight failed before any state-changing request.
					e = nil
				} else if statErr != nil {
					e = statErr
				} else {
					e = recoverCodeQLRevalidation(cc, c, dir)
				}
			} else {
				e = restoreCodeQL(cc, c, dir)
			}
			if e != nil {
				ret = errors.Join(ret, errors.New("route restoration pending; retain admission"), e)
			} else {
				rec.RouteRestored = true
			}
		}
	}
	cleanupRegistration := func() {
		var ce error
		rec.TreeGone, rec.Removed, ce = disposeOwned(rec.ContainerID)
		ret = errors.Join(ret, ce)
		if !rec.TreeGone || !registrationMayExist {
			return
		}
		if revalidate && ret != nil && routeAttempted && !rec.RouteRestored {
			// Keep the offline label available for the crash-recovery command.
			// No container or listener remains, and admission stays occupied.
			return
		}
		cc, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rs, ce := repoRunners(cc, r.FullName)
		if ce != nil {
			ret = errors.Join(ret, ce)
			return
		}
		for _, rr := range rs {
			if rr.Name == rec.Instance {
				if ce = mutateJSON(cc, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", r.FullName, rr.ID), nil, nil); ce != nil {
					ret = errors.Join(ret, ce)
					return
				}
			}
		}
		rs, ce = repoRunners(cc, r.FullName)
		if ce != nil {
			ret = errors.Join(ret, ce)
			return
		}
		for _, rr := range rs {
			if rr.Name == rec.Instance {
				ret = errors.Join(ret, errors.New("bootstrap registration retained"))
				return
			}
		}
		rec.RegistrationGone = true
	}
	defer func() {
		// Re-enable an interrupted setup while its offline label still exists.
		// Otherwise a cleanup error can trigger one recovery after disposal.
		if revalidate && ret != nil {
			recoverRoute()
		}
		cleanupRegistration()
		if !recoveryAttempted {
			recoverRoute()
		}
	}()
	// Install cleanup before the first durable-ID write can fail, and persist
	// the verified ID before starting the container or requesting any token.
	if e = save(); e != nil {
		return e
	}
	if createErr != nil || strings.TrimSpace(string(b)) != rec.ContainerID {
		return errors.New("bootstrap create completion uncertain")
	}
	if e = codeQLIsolation(inspection); e != nil {
		return e
	}
	if e = cmd(ctx, docker, "start", rec.ContainerID).Run(); e != nil {
		return errors.New("bootstrap container start failed")
	}
	copy := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "sh", "-c", "cat > /runner/product-ci && chmod 755 /runner/product-ci")
	copy.Stdin = bytes.NewReader(guest)
	if e = copy.Run(); e != nil {
		return errors.New("bootstrap helper transfer failed")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	var token struct{ Token string }
	if e = mutateJSON(ctx, "POST", "repos/"+r.FullName+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return e
	}
	if token.Token == "" {
		return errors.New("registration token absent")
	}
	packet := struct {
		Plan                             codeQLPlan
		Repo, Token, Name, Callback, Key string
	}{codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: c.SHA, SHA: c.SHA, Label: codeQLLabel(c.ID), Role: "offline-bootstrap-no-job"}, r.FullName, token.Token, rec.Instance, "", ""}
	input, e := json.Marshal(packet)
	packet.Token = ""
	token.Token = ""
	if e != nil {
		return e
	}
	configure := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "/runner/product-ci", "codeql-offline-guest")
	configure.Stdin = bytes.NewReader(input)
	registrationMayExist = true
	e = configure.Run()
	for i := range input {
		input[i] = 0
	}
	if e != nil {
		return errors.New("offline official runner configuration failed")
	}
	rs, e = repoRunners(ctx, r.FullName)
	if e != nil {
		return e
	}
	own := false
	for _, rr := range rs {
		if rr.Name == rec.Instance && !rr.Busy && rr.Status == "offline" && len(rr.Labels) == 1 {
			for _, l := range rr.Labels {
				if l.Name == codeQLLabel(c.ID) {
					own = true
				}
			}
		}
	}
	if !own {
		return errors.New("offline label registration unconfirmed")
	}
	// No listener was started, so this container cannot accept any workflow job.
	rec.Stage = "official-runner-configured-offline"
	if e = save(); e != nil {
		return e
	}
	routeAttempted = true
	if revalidate {
		e = revalidateCodeQL(ctx, c, dir)
	} else {
		e = routeCodeQL(ctx, c, dir)
	}
	if e != nil {
		return e
	}
	b, e = os.ReadFile(filepath.Join(dir, "codeql-route-response.private.json"))
	var response struct {
		RunID int64 `json:"run_id"`
	}
	if e != nil || json.Unmarshal(b, &response) != nil || response.RunID < 0 {
		return errors.New("setup update response unavailable")
	}
	// A runner-only update can complete synchronously (HTTP 200) without
	// creating a validation run. routeCodeQL already read back every setting.
	// Keep zero as "no run created"; never invent a workflow identity.
	rec.RunID = response.RunID
	rec.Stage = "route-submitted-no-job"
	return nil
}
