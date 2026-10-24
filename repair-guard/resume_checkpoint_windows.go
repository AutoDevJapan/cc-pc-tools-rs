package main

// Recover only the stale branch checkpoint of the original 1626 worker. The
// original worker's actual successful tool result is a separate provenance
// record from the owner's new native request; neither is relabeled as the other.
import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const resume1626CheckpointJob = "20261020-170947-original-product-retry-1626"
const resume1626CheckpointSession = "01a134e8-aac0-7942-8996-a0fc69357a3d"
const resume1626CheckpointNative = "C:/Users/makhmeto/.codex/sessions/2026/10/13/rollout-2026-10-13T19-57-05-01a134e8-aac0-7942-8996-a0fc69357a3d.jsonl"
const resume1626BeforeBranch = "5dbb53a461175821c3fd4948e143717b50c9972649795c8abe670feeb7bb518f"
const resume1626AfterBranch = "9b56dd38ed6b918901c96f4f37ccd02629e8dcb9d5fcb827a7804bfcf3f139e3"
const resume1626TransitionCall = "a056c317bdd2ffed329931a23bf5e163ecacff46e2d037d8f8fe097af8f43d5f"
const resume1626TransitionResult = "1ec01c00fce7a03c6a6b76a6452872c8a29ddea43cca1377bcafd605b7848a88"

func resume1626TransitionMatches(reader io.Reader, branch string) bool {
	expected := map[int]string{1: "9c896ed81ab4741530cd051777f3ca8df80eeba500a8929747d458e9e9f4323f", 37385: resume1626TransitionCall, 37388: resume1626TransitionResult}
	if hashBytes([]byte(branch)) != resume1626AfterBranch {
		return false
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, 320<<20))
	scanner.Buffer(make([]byte, 64<<10), 36<<20)
	for line := 1; scanner.Scan(); line++ {
		sha, wanted := expected[line]
		if !wanted {
			continue
		}
		var event object
		if hashBytes(scanner.Bytes()) != sha || json.Unmarshal(scanner.Bytes(), &event) != nil {
			return false
		}
		p := obj(event["payload"])
		switch line {
		case 1:
			if event["type"] != "session_meta" || p["id"] != resume1626CheckpointSession || p["source"] != "exec" || !same(str(p["cwd"]), gameTree) {
				return false
			}
		case 37385:
			if event["timestamp"] != "2026-10-20T09:04:55.464Z" || event["type"] != "response_item" || p["type"] != "custom_tool_call" || p["name"] != "exec" || p["call_id"] != "call_20b8e70a09744d57a75ab04d65a4ca4f" || !strings.Contains(str(p["input"]), "switch -c "+branch) {
				return false
			}
		case 37388:
			if event["timestamp"] != "2026-10-20T09:04:57.679Z" || event["type"] != "response_item" || p["type"] != "custom_tool_call_output" || p["call_id"] != "call_20b8e70a09744d57a75ab04d65a4ca4f" {
				return false
			}
			blocks, ok := p["output"].([]any)
			if !ok {
				return false
			}
			for _, block := range blocks {
				text := str(obj(block)["text"])
				if obj(block)["type"] == "input_text" && strings.Contains(text, "Switched to a new branch '"+branch+"'") && regexp.MustCompile(`exit_code\D{1,8}0`).MatchString(text) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func requested1626Checkpoint(db object) object {
	row := itemAt(db, 1626)
	saved := obj(row["scheduler"])
	if hashBytes([]byte(str(saved["branch"]))) != resume1626BeforeBranch || !same(str(saved["worktree"]), gameTree) || obj(row["worker_receipt"])["job"] != resume1626CheckpointJob || !hasJob(row, resume1626CheckpointJob) {
		return nil
	}
	job, err := readObject("D:/CC/Tools/codex-jobs/jobs/"+resume1626CheckpointJob+"/job.json", 4<<20)
	if err != nil || job["id"] != resume1626CheckpointJob || job["status"] != "succeeded" || num(job["todo_id"]) != 1626 || job["thread_id"] != resume1626CheckpointSession || !same(str(job["cwd"]), gameTree) {
		return nil
	}
	started, se := time.Parse(time.RFC3339, str(job["started_at"]))
	finished, fe := time.Parse(time.RFC3339, str(job["finished_at"]))
	transition := time.Date(2026, 10, 20, 9, 4, 55, 464000000, time.UTC)
	if se != nil || fe != nil || transition.Before(started) || !transition.Before(finished) || legacyMatch(gameTree) || !currentPlain(gameTree, true) {
		return nil
	}
	branch, ok := currentGit(gameTree, "branch", "--show-current")
	common, co := currentGit(gameTree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	baseCommon, bo := currentGit(gameRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	head, ho := currentGit(gameTree, "rev-parse", "HEAD")
	if !ok || hashBytes([]byte(branch)) != resume1626AfterBranch || !co || !bo || !ho || !civ6Commit.MatchString(head) || !same(common, baseCommon) || !same(common, str(obj(job["work_owner"])["git_common"])) || !sourceOriginVerified(gameTree, "autodevjapan/phosgaea-vol1") {
		return nil
	}
	if legacyMatch(resume1626CheckpointNative) || !currentPlain(resume1626CheckpointNative, false) {
		return nil
	}
	u, err := syscall.UTF16PtrFromString(resume1626CheckpointNative)
	if err != nil {
		return nil
	}
	h, err := syscall.CreateFile(u, syscall.GENERIC_READ, 7, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil
	}
	f := os.NewFile(uintptr(h), resume1626CheckpointNative)
	defer f.Close()
	if !resume1626TransitionMatches(f, branch) {
		return nil
	}
	worktree, _ := currentPathHash(gameTree)
	return object{"domain": "original-worker-checkpoint-v1", "todo_id": 1626, "source_job": resume1626CheckpointJob, "source_session": resume1626CheckpointSession,
		"transition_call_sha256": resume1626TransitionCall, "transition_result_sha256": resume1626TransitionResult,
		"worktree_sha256": worktree, "before_branch_sha256": resume1626BeforeBranch, "after_branch_sha256": resume1626AfterBranch,
		"after_branch": branch, "head": head}
}
