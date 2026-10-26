package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// This source produced the existing authenticated v1 receipt. Reader repairs
// must not rewrite its producer revision, registration, ownership or payload.
const official3387PreparedProducer = "f5f846a8470b544846e4c9ce43306061bf5668f9"

func official3894ProviderSource(root, branch string) bool {
	actual, ok := currentGit(root, "branch", "--show-current")
	return ok && actual == branch && sourceOriginVerified(root, "autodevjapan/local-ai-console")
}

func official3894ProducerHead(reader string, saved *official3894Source) (string, bool) {
	if !official3894Hex(reader, 40) {
		return "", false
	}
	if saved == nil {
		return reader, true
	}
	if saved.Head == reader || saved.Head == official3387PreparedProducer {
		return saved.Head, true
	}
	return "", false
}

type official3894MergeOutcome uint8

const (
	official3894MergeUnknown official3894MergeOutcome = iota
	official3894MergeConfirmed
	official3894MergeCommandFailed
	official3894MergeNotMerged
	official3894MergeResponseInvalid
	official3894MergeDeadline
	official3894MergeCancelled
)

func (m official3894MergeOutcome) reason() string {
	switch m {
	case official3894MergeCommandFailed:
		return "command_failed"
	case official3894MergeNotMerged:
		return "not_merged"
	case official3894MergeResponseInvalid:
		return "response_invalid"
	case official3894MergeDeadline:
		return "deadline"
	case official3894MergeCancelled:
		return "cancelled"
	default:
		return "not_confirmed"
	}
}

func official3894SourceRevision(reader string, saved *official3894Source, unchanged func(string) bool, merged func(string) official3894MergeOutcome) (string, error) {
	producer, ok := official3894ProducerHead(reader, saved)
	if !ok {
		return "", official3894Error("prepared_source_revision_unrecognized")
	}
	if result := merged(reader); result != official3894MergeConfirmed {
		return "", official3894Error("normal_reader_source_merge_" + result.reason() + "_" + reader)
	}
	if producer != reader {
		if result := merged(producer); result != official3894MergeConfirmed {
			return "", official3894Error("normal_producer_source_merge_" + result.reason() + "_" + producer)
		}
	}
	// The retained creator worktree is compared with the original producer,
	// while the executing clean reader must independently be merged.
	if !unchanged(producer) {
		return "", official3894Error("current_source_inputs_changed")
	}
	return producer, nil
}

func official3894InputsUnchanged(root, head string) bool {
	for _, path := range []string{"repair-guard", "tools/civ6-ci-contract", "localai-web-rs/src/operations_catalog.rs", "localai-web-rs/src/operations.rs"} {
		if _, e := permanentGitAt(root, "diff", "--quiet", "--no-ext-diff", "--no-textconv", head, "--", path); e != nil {
			return false
		}
	}
	return true
}

func official3894Merged(ctx context.Context, head string) bool {
	return official3894MergeLookup(ctx, head, official3894Command) == official3894MergeConfirmed
}

// Only fixed status classes and a previously validated revision leave the
// boundary. Command output and errors may contain auth details and are never
// included in the diagnostic.
func official3894MergeLookup(ctx context.Context, head string, command func(context.Context, string, ...string) ([]byte, error)) official3894MergeOutcome {
	if !official3894Hex(head, 40) {
		return official3894MergeUnknown
	}
	raw, e := command(ctx, official3894Gate, "api", "repos/AutoDevJapan/cc-pc-tools-rs/compare/"+head+"...main", "--jq", ".status")
	if e != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			return official3894MergeDeadline
		case context.Canceled:
			return official3894MergeCancelled
		}
		return official3894MergeCommandFailed
	}
	switch strings.TrimSpace(string(raw)) {
	case "ahead", "identical":
		return official3894MergeConfirmed
	case "behind", "diverged":
		return official3894MergeNotMerged
	default:
		return official3894MergeResponseInvalid
	}
}

// The original parent may compare the one independently known installed
// software before its normal AdminBroker update. This cannot prepare, verify
// or consume an SDK, and does not depend on releasing a provider's retry wait.
func official3894ParentUpdaterBound(reg skillsRegistry, caller string, job object, common string) bool {
	m := currentSubworkJobName.FindStringSubmatch(caller)
	row := reg.Tasks[3387]
	owner := obj(job["work_owner"])
	kind := str(job["task_type"])
	return len(m) == 3 && m[2] == "3387" && row.ID == 3387 && row.Project == "cc-pc-tools-rs" &&
		(row.Status == "doing" || row.Status == "todo") && !skillsHeld(row) &&
		row.Subwork == (skillsSubwork{}) && row.Scheduler.SubworkParent == "" && row.Scheduler.Admission == nil &&
		row.Scheduler.Job == caller && skillsLatest(row, caller) && skillsHasJob(row, caller) &&
		strings.HasPrefix(row.Scheduler.Branch, "scheduler/todo-3387-") &&
		job["id"] == caller && skillsJobTask(job, 3387) && job["project"] == row.Project && job["scope_source"] == "launcher" &&
		(kind == "research" || kind == "implementation") && official3894IndependentJob(job) &&
		job["status"] == "running" && same(str(job["cwd"]), row.Scheduler.Worktree) && job["launch_key"] == row.Scheduler.Key &&
		len(owner) == 4 && strings.EqualFold(str(owner["repository"]), "autodevjapan/cc-pc-tools-rs") &&
		common != "" && same(str(owner["git_common"]), common) && same(str(owner["worktree"]), row.Scheduler.Worktree) && owner["branch"] == row.Scheduler.Branch
}

func official3894ParentUpdateSource(ctx context.Context) (official3894Source, error) {
	var result official3894Source
	reg, ok := official3894Registry(ctx)
	if !ok {
		return result, official3894Error("normal_task_metadata_unavailable")
	}
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	origins, ok := official3894OriginalLineage(reg, func(id string) (object, bool) { return official3894ReadJob(&s, id) })
	if !ok {
		return result, official3894Error("original_registered_job_lineage_required")
	}
	caller := os.Getenv("CC_JOB_ID")
	job, ok := official3894ReadJob(&s, caller)
	row := reg.Tasks[3387]
	common, commonOK := currentGitCommonDir(row.Scheduler.Worktree)
	if !ok || !commonOK || !official3894ParentUpdaterBound(reg, caller, job, common) || !official3894Descendant(job) {
		return result, official3894Error("native_parent_software_updater_required")
	}
	if !ownerRegisteredSource(row.Scheduler.Worktree).AccessAllowed || !skillsActorSource(row.Scheduler.Worktree, row.Scheduler.Branch) {
		return result, official3894Error("normal_registered_source_required")
	}
	head, ok := official3894BuildHead()
	if !ok || !official3894InputsUnchanged(row.Scheduler.Worktree, head) || !official3894Merged(ctx, head) {
		return result, official3894Error("merged_clean_software_update_source_required")
	}
	proof, e := json.Marshal(struct{ Domain, Root, Job, Workspace, Branch, OriginalJobs string }{
		"official-ci3387-parent-software-update-v1", reg.Root, caller, row.Scheduler.Worktree, row.Scheduler.Branch, origins,
	})
	if e != nil {
		return result, e
	}
	return official3894Source{Head: head, Registration: official3894Hash(proof), CreatorJob: caller, Workspace: row.Scheduler.Worktree, Branch: row.Scheduler.Branch}, ctx.Err()
}
