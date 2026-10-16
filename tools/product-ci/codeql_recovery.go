package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Recovery is bounded to the persisted bootstrap nonce. Its OS byte lock makes
// a still-running owner reject recovery, and is automatically released on crash.
func recoverCodeQLBootstrap(ctx context.Context, c candidate, dir string) (ret error) {
	if _, e := readRevalidation(c, dir); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(dir, "codeql-bootstrap.private.json"))
	var old bootstrapReceipt
	if e != nil || json.Unmarshal(b, &old) != nil || old.RepoID != c.ID || old.CandidateSHA != c.SHA || old.Image != codeQLSDK || !old.AdmissionLocked || !strings.HasPrefix(old.Instance, fmt.Sprintf("todo2648-repo-%d-codeql-bootstrap-", c.ID)) || len(old.ContainerID) != 64 || strings.Trim(old.ContainerID, "0123456789abcdef") != "" || (old.Stage != "official-runner-configured-offline" && old.Stage != "route-submitted-no-job") {
		return errors.New("owned crash-recovery bootstrap receipt rejected")
	}
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", c.ID))
	entry, e := os.Lstat(lockPath)
	if e != nil || !entry.Mode().IsRegular() {
		return errors.New("owned admission path unavailable")
	}
	f, e := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if e != nil {
		return errors.New("owned admission unavailable")
	}
	defer func() { ret = errors.Join(ret, f.Close()) }()
	if e = lockAdmission(f); e != nil {
		return errors.New("bootstrap owner or another recovery is still active")
	}
	info, e := f.Stat()
	if e != nil || !os.SameFile(entry, info) || info.Size() < 2 || info.Size() > 1024 {
		return errors.New("admission identity rejected")
	}
	b = make([]byte, info.Size()-1)
	if _, e = f.ReadAt(b, 1); e != nil && e != io.EOF {
		return e
	}
	var admission struct {
		RepoID   int64  `json:"repo_id"`
		SHA      string `json:"candidate_sha"`
		Instance string
	}
	if json.Unmarshal(b, &admission) != nil || admission.RepoID != c.ID || admission.SHA != c.SHA || admission.Instance != old.Instance {
		return errors.New("admission belongs to another operation")
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	proof := map[string]any{"repo_id": c.ID, "candidate_sha": c.SHA, "instance": old.Instance, "container_id": old.ContainerID, "tree_gone": false, "container_removed": false, "registration_gone": false, "lock_released": false, "all_required_checks_success": false}
	defer func() {
		if ret != nil {
			proof["failure"] = ret.Error()
		}
		data, _ := json.MarshalIndent(proof, "", "  ")
		ret = errors.Join(ret, os.WriteFile(filepath.Join(dir, "codeql-recovery.private.json"), data, 0600))
	}()
	runners, e := repoRunners(ctx, r.FullName)
	if e != nil {
		return e
	}
	for _, runner := range runners {
		if runner.Name == old.Instance && (runner.Busy || runner.Status != "offline") {
			return errors.New("recorded bootstrap registration is no longer offline")
		}
	}
	inspect := cmd(ctx, docker, "inspect", old.ContainerID)
	stderr := boundedBuffer{limit: 64 << 10}
	inspect.Stderr = &stderr
	data, inspectErr := boundedOutput(inspect, 1<<20)
	if inspectErr != nil {
		if !strings.Contains(stderr.String(), "No such object: "+old.ContainerID) && !strings.Contains(stderr.String(), "No such container: "+old.ContainerID) {
			return errors.New("recorded bootstrap container absence unverified")
		}
		proof["tree_gone"], proof["container_removed"] = true, true
	} else {
		var rows []struct {
			ID, Name string
			Config   struct{ Labels map[string]string }
		}
		if json.Unmarshal(data, &rows) != nil || len(rows) != 1 || rows[0].ID != old.ContainerID || rows[0].Name != "/"+old.Instance || rows[0].Config.Labels["cc.instance"] != old.Instance || codeQLIsolation(data) != nil {
			return errors.New("recorded bootstrap isolation rejected")
		}
		gone, removed, e := disposeOwned(old.ContainerID)
		proof["tree_gone"], proof["container_removed"] = gone, removed
		if e != nil || !gone || !removed {
			return errors.Join(errors.New("bootstrap tree cleanup incomplete"), e)
		}
	}
	// Keep the offline GitHub label until the setup is confirmed enabled again.
	if e = recoverCodeQLRevalidation(ctx, c, dir); e != nil {
		return e
	}
	proof["configuration_recovered"] = true
	runners, e = repoRunners(ctx, r.FullName)
	if e != nil {
		return e
	}
	for _, runner := range runners {
		if runner.Name == old.Instance {
			if runner.Busy || runner.Status != "offline" {
				return errors.New("offline registration changed independently")
			}
			if e = mutateJSON(ctx, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", r.FullName, runner.ID), nil, nil); e != nil {
				return e
			}
		}
	}
	runners, e = repoRunners(ctx, r.FullName)
	if e != nil {
		return e
	}
	for _, runner := range runners {
		if runner.Name == old.Instance {
			return errors.New("bootstrap registration absence unconfirmed")
		}
	}
	proof["registration_gone"] = true
	now, e := os.Lstat(lockPath)
	if e != nil || !os.SameFile(now, info) {
		return errors.New("admission path changed independently")
	}
	if e = os.Remove(lockPath); e != nil {
		return errors.New("completed admission removal failed")
	}
	proof["lock_released"] = true
	return nil
}

func finalizeCodeQLRevalidation(ctx context.Context, c candidate, dir string) error {
	s, e := readRevalidation(c, dir)
	if e != nil {
		return e
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	var response struct {
		RunID int64 `json:"run_id"`
	}
	b, e := os.ReadFile(filepath.Join(dir, "codeql-route-response.private.json"))
	if e != nil || json.Unmarshal(b, &response) != nil {
		return errors.New("setup validation receipt unavailable")
	}
	if e = verifyRevalidationRun(ctx, c, s, r.FullName, response.RunID, get); e != nil {
		return e
	}
	var run struct{ Status, Conclusion string }
	if e = get(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, response.RunID), &run); e != nil || run.Status != "completed" || run.Conclusion != "success" {
		return errors.New("setup validation is not successful yet")
	}
	var now codeQLSetup
	if e = get(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &now); e != nil {
		return e
	}
	if !sameCodeQLSettings(now, s.Prior) || now.RunnerType != s.Prior.RunnerType || now.RunnerLabel != s.Prior.RunnerLabel {
		return errors.New("final setup coverage differs from original")
	}
	b, _ = json.MarshalIndent(map[string]any{"repo_id": c.ID, "candidate_sha": c.SHA, "source_sha": s.DefaultSHA, "run_id": response.RunID, "original_settings_equal": true, "role": "setup-validation-not-candidate", "all_required_checks_success": false}, "", "  ")
	return os.WriteFile(filepath.Join(dir, "codeql-revalidation-final.private.json"), b, 0600)
}
