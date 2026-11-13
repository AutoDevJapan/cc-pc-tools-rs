//go:build windows

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

const mp4Source = "3a69da420e2797c501eb40dd3c660394f8b7cb3b"

// Exact build pins are supplied by the reviewed software source revision,
// never arguments, environment values or a data-file permission claim.
var mp4AskPlan = askSoftwarePlan{
	Candidate: updateSourceRoot + "/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-project-3a69.exe",
	Proof:     "D:/CC/Tools/codex-jobs/fourbros4431-project-ask-candidate-proof.json", Source: mp4Source,
	SHA:      "0a693fa7013d733b3e7fbb009227cb37f4e2705e1d087ab40a4f5bf8857e9747",
	ProofSHA: "85dfadb0b549d4a9970c24ee4c1b11c7cd2d4b59019ce3c8bdbc92709ac9ddeb",
	Bytes:    13560320,
}
var mp4RunnerPlan = askSoftwarePlan{
	Candidate: updateSourceRoot + "/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/unattended-runner-project-3a69.exe",
	Proof:     "D:/CC/Tools/codex-jobs/fourbros4431-project-runner-candidate-proof.json", Source: mp4Source, GoRunner: true,
}

var mp4AskBaseline = softwareBaseline{
	Target: askTarget, Stage: "D:/CC/Tools/discord-ask/discord_ask.project3a69.stage.exe", Backup: "D:/CC/Tools/discord-ask/discord_ask.project3a69.before.exe",
	SHA: "6d22dc15797c821794a56e9d05c432b4a1694369e6d97628925580ec42c29a28", Bytes: 13416448,
	Identity: softwareIdentity{"1215965535", "281474995436467", "134389898427447729"},
}
var mp4RunnerBaseline = softwareBaseline{
	Target: "D:/CC/Tools/discord-ask/unattended-runner.exe", Stage: "D:/CC/Tools/discord-ask/unattended-runner.project3a69.stage.exe", Backup: "D:/CC/Tools/discord-ask/unattended-runner.project3a69.before.exe",
	SHA: "385f4207dd5491cc0c88060f71f1e270b543e42a1c45898b1de15348afb7efb4", Bytes: 14110208,
	Identity: softwareIdentity{"1215965535", "844424946531910", "134386801692725612"},
}

func mp4Software(kind string) (askSoftwarePlan, softwareBaseline, bool) {
	switch kind {
	case "ask":
		return mp4AskPlan, mp4AskBaseline, true
	case "runner":
		return mp4RunnerPlan, mp4RunnerBaseline, true
	}
	return askSoftwarePlan{}, softwareBaseline{}, false
}

func mp4PlanReady(plan askSoftwarePlan, baseline softwareBaseline) bool {
	for _, hash := range []string{plan.SHA, plan.ProofSHA, baseline.SHA} {
		if len(hash) != 64 {
			return false
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return false
		}
	}
	return plan.Bytes > 0 && len(plan.Source) == 40 && baseline.Bytes > 0 && baseline.Identity.Volume != "" && baseline.Identity.FileID != "" && baseline.Identity.Created != "" && filepath.Dir(baseline.Stage) == filepath.Dir(baseline.Target) && filepath.Dir(baseline.Backup) == filepath.Dir(baseline.Target)
}

func isMP4SoftwareFlag(flag string) bool {
	return flag == "--verify-mp4-ask-update" || flag == "--prepare-mp4-ask-update" || flag == "--verify-mp4-runner-update" || flag == "--prepare-mp4-runner-update"
}

func runMP4Software(flag string, out io.Writer) int {
	kind, prepare := "ask", false
	switch flag {
	case "--verify-mp4-ask-update":
	case "--prepare-mp4-ask-update":
		prepare = true
	case "--verify-mp4-runner-update":
		kind = "runner"
	case "--prepare-mp4-runner-update":
		kind, prepare = "runner", true
	default:
		return 2
	}
	r := supervisedFixedSoftware(prepare, []string{"--mp4-software-worker", kind}, []string{"--mp4-inspection-worker", kind})
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || prepare && !r.Prepared {
		return 1
	}
	return 0
}

type mp4NativeObject struct {
	updateObject
	ctx      context.Context
	baseline softwareBaseline
}

func (o mp4NativeObject) CreateNew(path string, raw []byte, acl string) (softwareIdentity, error) {
	if o.ctx.Err() != nil || path != o.baseline.Stage && path != o.baseline.Backup || !sameParentPath(path, o.baseline.Target) {
		return softwareIdentity{}, errUpdateObject
	}
	native, ok := o.updateObject.(*nativeUpdateObject)
	if !ok {
		return softwareIdentity{}, errUpdateObject
	}
	return native.createNewSibling(path, raw, acl)
}

func mp4SoftwareBoundary(ctx context.Context, plan askSoftwarePlan, baseline softwareBaseline) updateBoundary {
	git := func(args ...string) ([]byte, bool) {
		return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
	}
	return updateBoundary{open: func(path string) (updateObject, error) {
		if ctx.Err() != nil || path != baseline.Target && path != plan.Candidate && path != plan.Proof {
			return nil, errUpdateObject
		}
		o, e := openUpdateObject(path)
		if e != nil {
			return nil, e
		}
		return mp4NativeObject{o, ctx, baseline}, nil
	}, absent: func(path string) bool {
		if ctx.Err() != nil {
			return false
		}
		_, e := os.Lstat(path)
		return os.IsNotExist(e)
	}, oldMatches: func(raw []byte) bool { return digest(raw) == baseline.SHA }, candidateMatches: func(raw, proof []byte) bool {
		return askCandidateMatchesContext(ctx, raw, proof, plan, git)
	}}
}

// The outer worker gate has independently checked the actual OS parent and
// inherited permit. Only these two compiled software namespaces are admitted.
func runMP4SoftwareWorker(args []string, out io.Writer) int {
	if len(args) < 2 {
		return 2
	}
	plan, baseline, ok := mp4Software(args[1])
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if len(args) == 2 && args[0] == "--mp4-inspection-worker" {
		r := reconcileSoftwareObjects(ctx, func(path string) (updateObject, error) {
			if path != baseline.Target && path != baseline.Stage && path != baseline.Backup {
				return nil, errUpdateObject
			}
			return openUpdateObject(path)
		}, os.Lstat, baseline)
		if json.NewEncoder(out).Encode(r) != nil {
			return 1
		}
		return 0
	}
	if len(args) != 3 || args[0] != "--mp4-software-worker" || args[2] != "verify" && args[2] != "prepare" {
		return 2
	}
	// An unbound candidate or baseline cannot perform a preparation, including
	// before the final reviewed distribution pins have been compiled.
	if !mp4PlanReady(plan, baseline) {
		_ = json.NewEncoder(out).Encode(updateResult{Reason: "qualification_unavailable"})
		return 1
	}
	prepare := args[2] == "prepare"
	r := executeSoftwareContext(ctx, prepare, plan, baseline, mp4SoftwareBoundary(ctx, plan, baseline))
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || prepare && !r.Prepared {
		return 1
	}
	return 0
}
