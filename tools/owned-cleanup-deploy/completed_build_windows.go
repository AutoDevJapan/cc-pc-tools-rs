//go:build windows

package main

// The next cleanup-only generation reuses the deployed updater's handle CAS,
// no-replace rename and ACL preservation. It does not open/update safe-delete.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const completedRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3942-work"
const completedHead = "396d83d988b65607cf12c9375d691b7daef42387"
const completedBuildJob = "20261027-032215-todo-3942"
const completedManifestPath = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5249eb2a5e0382b871074f454633ec84/build-manifest-396d83d988b65607cf12c9375d691b7daef42387.private.json"
const completedManifestSHA = "8571c44be11579e60de3ece3928a55917d7817e94447a100db2568a6c69ba135"
const completedPreviousProof = "D:/CC/Tools/admin-broker/results/20261026-124621-7006f98d9abcd959c150132e6f3bcea1.json"
const completedPreviousProofSHA = "c0e1455fd8acd3e9ca61d8f3d84f5299db31868da9cce1310e501bfc66747722"
const completedPreviousHead = "5c9fce950d30c682e5f36fca3506af72aeb352a5"
const completedPreviousSHA = "11cd5c8bbe855065ea27788543a79a70b4d63827f18e7bed918856e3526e2bd7"
const completedCurrentProof = "D:/CC/Tools/admin-broker/results/20261026-140040-998935e13191aee2aff250a1e476ae4d.json"
const completedCurrentProofSHA = "31a4bdf4486f8175b96f1fe8ec2ab9d51aa302551f52fb77a507e19ab1e5a7ff"
const completedCurrentHead = "c2ea7b0b7c81b05789d46dc76eb0b1fd4c8eb7bd"
const completedCurrentSHA = "7d1d86f361eba1a05e7c21b8c37077836fe56f3b209871064a2c930040772bba"
const completedObservedProof = "D:/CC/Tools/admin-broker/results/20261026-153502-03ac7d6649c83b16bb064a90c37f41cd.json"
const completedObservedProofSHA = "4787670b8a7e8bade25dd3c7bdf59ce2652fe5843f20d3c1fc48c9128d003d2d"
const completedObservedHead = "9e1d0186707f49a61f9f56a1a6644e56bbf8de1b"
const completedObservedSHA = "dde2d2856c204eb094d77b9e9688996ead292843ecf8cff1d6f8fc3dedf931b0"

var completedLeaf = leaf{"cleanup-8h", completedRoot + "/target/release/cleanup-8h-delivery-" + completedHead + ".exe", "D:/CC/Tools/cleanup-8h/cleanup-8h.exe", completedObservedSHA, "64309eade35838560970175f4052d925d7443818856e40524c082a0f565b10c0"}
var completedInputs = []string{"Cargo.toml", "Cargo.lock", "cleanup-8h", "cc-jobs", "cc-job-policy", "cc-chrome", "cc-task-title", "cc-redact"}

type completedManifest struct {
	BuildOutput  string `json:"build_output"`
	OutputSHA    string `json:"build_output_sha256"`
	CopyVerified bool   `json:"copy_verified"`
	Schema       int    `json:"schema"`
	Todo         int    `json:"todo"`
	Job          string `json:"job"`
	Head         string `json:"source_head"`
	Root         string `json:"source_root"`
	Command      string `json:"build_command"`
	Candidate    string `json:"candidate"`
	New          string `json:"candidate_sha256"`
	Old          string `json:"existing_cleanup_sha256"`
	Inputs       []struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"inputs"`
}

func completedGit(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", completedRoot}, args...)...)
	c.WaitDelay = 2 * time.Second
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, e := c.Output()
	if e != nil || len(out) > 4<<20 {
		return nil, errCode("COMPLETED_SOURCE_CHANGED")
	}
	return out, nil
}
func completedManifestIdentity(m completedManifest) error {
	if !same(m.BuildOutput, completedRoot+"/target/release/cleanup-8h.exe") || m.OutputSHA != m.New || !m.CopyVerified {
		return errCode("COMPLETED_BUILD_COPY_UNVERIFIED")
	}
	if m.Schema != 1 || m.Todo != 3942 || m.Job != completedBuildJob || m.Head != completedHead || !same(m.Root, completedRoot) || m.Command != "cargo build --release --locked -p cleanup-8h" || !same(m.Candidate, completedLeaf.Source) || m.New != completedLeaf.New || m.Old != completedLeaf.Old {
		return errCode("COMPLETED_MANIFEST_CHANGED")
	}
	return nil
}

// Decode only the two relevant rows. Legacy unrelated fields are not sources
// of authority and must not invalidate a normally registered software update.
func completedTodoReference(b []byte) error {
	var root struct {
		Projects map[string]json.RawMessage `json:"projects"`
		Items    []json.RawMessage          `json:"items"`
	}
	if json.Unmarshal(b, &root) != nil || len(root.Items) == 0 {
		return errCode("COMPLETED_TODO_UNVERIFIED")
	}
	var project string
	if json.Unmarshal(root.Projects["cc-pc-tools-rs"], &project) != nil || !same(project, "D:/CC/Work/unattended-operations-20261013") {
		return errCode("COMPLETED_PROJECT_CHANGED")
	}
	seen := map[int]bool{}
	for _, raw := range root.Items {
		var id struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(raw, &id) != nil || (id.ID != 3942 && id.ID != 3757) {
			continue
		}
		if seen[id.ID] {
			return errCode("COMPLETED_TODO_AMBIGUOUS")
		}
		seen[id.ID] = true
		var row struct {
			Project   string   `json:"project"`
			Status    string   `json:"status"`
			Jobs      []string `json:"jobs"`
			Wait      []int    `json:"wait_on"`
			Scheduler struct {
				Worktree string `json:"worktree"`
				Branch   string `json:"branch"`
			} `json:"scheduler"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Project != "cc-pc-tools-rs" {
			return errCode("COMPLETED_TODO_UNVERIFIED")
		}
		if id.ID == 3757 {
			if row.Status != "done" {
				return errCode("PRIOR_UPDATE_UNFINISHED")
			}
			continue
		}
		found := false
		for _, job := range row.Jobs {
			found = found || job == completedBuildJob
		}
		if !found || (row.Status != "doing" && row.Status != "todo") || len(row.Wait) > 0 || !same(row.Scheduler.Worktree, completedRoot) || row.Scheduler.Branch != "scheduler/todo-3942-work" {
			return errCode("COMPLETED_OWNER_CHANGED")
		}
	}
	if len(seen) != 2 {
		return errCode("COMPLETED_TODO_MISSING")
	}
	return nil
}

func completedPreviousGeneration(b []byte) error {
	return verifiedCompletedGeneration(b, completedPreviousHead, leaves[1].New, completedPreviousSHA, "COMPLETED_PREVIOUS")
}
func completedCurrentGeneration(b []byte) error {
	return verifiedCompletedGeneration(b, completedCurrentHead, completedPreviousSHA, completedCurrentSHA, "COMPLETED_CURRENT")
}
func completedObservedGeneration(b []byte) error {
	return verifiedCompletedGeneration(b, completedObservedHead, completedCurrentSHA, completedObservedSHA, "COMPLETED_OBSERVED")
}
func verifiedCompletedGeneration(b []byte, expectedHead, expectedOld, expectedNew, phase string) error {
	var broker struct {
		Exit   int    `json:"exit"`
		Stdout string `json:"stdout"`
	}
	if json.Unmarshal(b, &broker) != nil || broker.Exit != 0 {
		return errCode(phase + "_BROKER_UNVERIFIED")
	}
	var result struct {
		Domain    string `json:"domain"`
		Head      string `json:"source_head"`
		Qualified bool   `json:"qualified"`
		Data      bool   `json:"data_access_granted"`
		Guard     bool   `json:"general_guard_changed"`
		Results   []struct {
			Kind     string `json:"kind"`
			Updated  bool   `json:"updated"`
			Verified bool   `json:"verified"`
			Old      string `json:"old_sha256"`
			New      string `json:"new_sha256"`
			Steps    struct {
				OldRetired bool `json:"old_retired"`
				Published  bool `json:"published"`
				Verified   bool `json:"verified"`
			} `json:"steps"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(broker.Stdout), &result) != nil || result.Domain != "completed-build-cleanup-software-3942" || result.Head != expectedHead || !result.Qualified || result.Data || result.Guard || len(result.Results) != 1 {
		return errCode(phase + "_INSTALL_UNVERIFIED")
	}
	r := result.Results[0]
	if r.Kind != "cleanup-8h" || !r.Updated || !r.Verified || r.Old != expectedOld || r.New != expectedNew || !r.Steps.OldRetired || !r.Steps.Published || !r.Steps.Verified {
		return errCode(phase + "_INSTALL_UNVERIFIED")
	}
	return nil
}

func completedProofs() error {
	// Independently pinned original successful 3757 broker event, not a hash
	// learned from the currently installed executable or a copied receipt.
	b, e := readProofEvents("D:/CC/Tools/codex-jobs/jobs/20261025-165120-todo-3757/events.jsonl")
	if e != nil {
		return e
	}
	if e = nativePriorInstall(b, nativePrior); e != nil {
		return e
	}
	// Keep the original 3757 chain and bind the already installed 3942
	// generation to its original successful normal broker result. Never learn
	// the expected old bytes from the currently installed executable.
	b, e = readPinned(completedPreviousProof, 1<<20)
	if e != nil || sha(b) != completedPreviousProofSHA {
		return errCode("COMPLETED_PREVIOUS_RECEIPT_CHANGED")
	}
	if e = completedPreviousGeneration(b); e != nil {
		return e
	}
	b, e = readPinned(completedCurrentProof, 1<<20)
	if e != nil || sha(b) != completedCurrentProofSHA {
		return errCode("COMPLETED_CURRENT_RECEIPT_CHANGED")
	}
	if e = completedCurrentGeneration(b); e != nil {
		return e
	}
	b, e = readPinned(completedObservedProof, 1<<20)
	if e != nil || sha(b) != completedObservedProofSHA {
		return errCode("COMPLETED_OBSERVED_RECEIPT_CHANGED")
	}
	if e = completedObservedGeneration(b); e != nil {
		return e
	}
	b, e = readPinned("D:/CC/Tools/todo/todo.json", 64<<20)
	if e != nil {
		return e
	}
	if e = completedTodoReference(b); e != nil {
		return e
	}
	b, e = readPinned(completedManifestPath, 1<<20)
	if e != nil || sha(b) != completedManifestSHA {
		return errCode("COMPLETED_BUILD_RECEIPT_CHANGED")
	}
	var manifest completedManifest
	if json.Unmarshal(b, &manifest) != nil {
		return errCode("COMPLETED_BUILD_RECEIPT_INVALID")
	}
	if e = completedManifestIdentity(manifest); e != nil {
		return e
	}
	origin, e := completedGit("remote", "get-url", "origin")
	if e != nil || strings.TrimSpace(string(origin)) != "https://github.com/AutoDevJapan/cc-pc-tools-rs.git" {
		return errCode("COMPLETED_SOURCE_OWNER_CHANGED")
	}
	if _, e = completedGit("merge-base", "--is-ancestor", completedHead, "origin/main"); e != nil {
		return e
	}
	for _, args := range [][]string{{"diff", "--quiet", completedHead, "HEAD", "--"}, {"diff", "--quiet", "HEAD", "--"}} {
		if _, e = completedGit(append(args, completedInputs...)...); e != nil {
			return e
		}
	}
	status, e := completedGit(append([]string{"status", "--porcelain=v1", "--untracked-files=all", "--"}, completedInputs...)...)
	if e != nil || len(status) != 0 {
		return errCode("COMPLETED_SOURCE_DIRTY")
	}
	list, e := completedGit(append([]string{"ls-files", "-z", "--"}, completedInputs...)...)
	if e != nil {
		return e
	}
	files := strings.Split(strings.TrimSuffix(string(list), "\x00"), "\x00")
	if len(files) != len(manifest.Inputs) {
		return errCode("COMPLETED_INPUT_SET_CHANGED")
	}
	pins := map[string]string{}
	for _, entry := range manifest.Inputs {
		if pins[entry.Path] != "" || len(entry.SHA) != 64 {
			return errCode("COMPLETED_INPUT_SET_CHANGED")
		}
		pins[entry.Path] = entry.SHA
	}
	for _, rel := range files {
		if filepath.IsAbs(rel) || strings.Contains(rel, "\\") || strings.Contains(rel, "..") || pins[rel] == "" {
			return errCode("COMPLETED_INPUT_SET_CHANGED")
		}
		b, e = readPinned(filepath.Join(completedRoot, rel), 16<<20)
		if e != nil || sha(b) != pins[rel] {
			return errCode("COMPLETED_INPUT_CHANGED")
		}
	}
	b, e = readPinned("D:/CC/Tools/codex-jobs/jobs/"+completedBuildJob+"/job.json", 1<<20)
	if e != nil {
		return e
	}
	var job struct {
		ID   string `json:"id"`
		Todo int    `json:"todo_id"`
		CWD  string `json:"cwd"`
	}
	if json.Unmarshal(b, &job) != nil || job.ID != completedBuildJob || job.Todo != 3942 || !same(job.CWD, completedRoot) {
		return errCode("COMPLETED_BUILD_JOB_CHANGED")
	}
	return nil
}

func refreshCompletedBuild(apply bool) ([]map[string]any, error) {
	if e := completedProofs(); e != nil {
		return nil, e
	}
	p, e := prepareAt(completedLeaf, "todo3942-legacy")
	if e != nil {
		return nil, e
	}
	defer p.close()
	if !apply {
		return []map[string]any{{"kind": "cleanup-8h", "qualified": true, "changed": false, "safe_delete_untouched": true}}, nil
	}
	lock, e := openPin("D:/CC/Tools/cleanup-8h/run.lock", syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, 3)
	if e != nil {
		return nil, errCode("HOURLY_CLEANER_LOCK_MISSING_OR_BUSY")
	}
	defer lock.close()
	e = p.install(hooks{})
	row := p.result()
	row["safe_delete_untouched"] = true
	if e != nil {
		row["retained_reason"] = e.Error()
	}
	return []map[string]any{row}, e
}
func completedBuildMain(apply bool) {
	rows, e := refreshCompletedBuild(apply)
	r := map[string]any{"domain": "completed-build-cleanup-software-3942", "source_head": completedHead, "qualified": e == nil, "results": rows, "data_access_granted": false, "general_guard_changed": false, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	if e != nil {
		r["reason"] = e.Error()
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		fmt.Fprintln(os.Stderr, "completed build result failed")
		os.Exit(1)
	}
	if e != nil {
		os.Exit(1)
	}
}
