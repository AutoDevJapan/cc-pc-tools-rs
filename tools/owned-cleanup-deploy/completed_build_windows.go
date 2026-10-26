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
const completedHead = "0d90458ae07ee2eb7f6a6f8acabf5b556d9e0736"
const completedBuildJob = "20261026-164814-todo-3942"
const completedManifestPath = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5249eb2a5e0382b871074f454633ec84/build-manifest.private.json"
const completedManifestSHA = "407ef0f4d9a90e1122be7a213cf23d2ba3e159b17b75cea92c527afa6807e566"

var completedLeaf = leaf{"cleanup-8h", completedRoot + "/target/release/cleanup-8h-delivery-" + completedHead + ".exe", "D:/CC/Tools/cleanup-8h/cleanup-8h.exe", leaves[1].New, "99e0c0ec161e930eb762e6fa74e5df7254fbbdc9f3eed338a6e35e29f6018ec3"}
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
	p, e := prepareAt(completedLeaf, "todo3942")
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
