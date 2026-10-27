package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// This is the accepted finite repair input, not an image allowance or a claim
// that a historical delegation edge exists. Its full text remains private.
const currentConversationSourceJob = "20261027-104546-todo-4084"
const currentConversationSourceSpec = "98c276cb0fd75854830f3736a810058a47b355b79b44d981990333fba6f85491"
const receivedInputCLI = "D:/CC/Tools/received-input/received_input.exe"

type currentActorRejection string

func (currentActorRejection) Error() string { return "conversation actor unverified" }

func currentConversationPair(origin, actor uint64, project string) bool {
	return origin == 3264 && actor == 3350 && project == "localai-web-rs"
}

type currentConversationInput struct {
	Domain  string `json:"domain"`
	OK      bool   `json:"ok"`
	Todo    uint64 `json:"todo_id"`
	Job     string `json:"job_id"`
	Kind    string `json:"source_kind"`
	Root    string `json:"registered_scope"`
	Human   *bool  `json:"human_identity_attested"`
	Asset   *bool  `json:"asset_qualified"`
	Edge    *bool  `json:"delegation_edge_recorded"`
	Version struct {
		SpecRef  string          `json:"spec_ref"`
		Spec     string          `json:"spec_sha256"`
		Dispatch string          `json:"dispatch_sha256"`
		Received string          `json:"received_sha256"`
		Binding  string          `json:"binding_sha256"`
		Parent   json.RawMessage `json:"parent_edge"`
	} `json:"version"`
}

func (r *Runner) currentConversationInput(ctx context.Context, row *TodoRow, job string) (currentConversationInput, error) {
	var result currentConversationInput
	fail := errors.New("conversation actor unverified")
	// The official API chooses all data roots itself. This file is only its
	// comparison request; no caller-authored response/manifest is consumed.
	b, _ := json.Marshal(map[string]any{"todo_id": row.ID, "job_id": job, "registered_scope": row.conversationRoot, "source_kind": "scheduler_dispatch"})
	path := filepath.Join(r.Config.Home, "conversation-actor-inputs", fmt.Sprintf("%d-%s.json", row.ID, job))
	if writeConversationOnce(path, b) != nil {
		return result, fail
	}
	raw, err := r.Run(ctx, receivedInputCLI, "--request-file", path)
	var spec string
	if err != nil || json.Unmarshal([]byte(raw), &result) != nil || result.Domain != "received-input-v1" || !result.OK || result.Todo != row.ID || result.Job != job || result.Kind != "scheduler_dispatch" || result.Root != row.conversationRoot || result.Human == nil || *result.Human || result.Asset == nil || *result.Asset || result.Edge == nil || *result.Edge || string(result.Version.Parent) != "null" || json.Unmarshal(row.Spec, &spec) != nil || filepath.Base(spec) != result.Version.SpecRef {
		return result, fail
	}
	for _, hash := range []string{result.Version.Spec, result.Version.Dispatch, result.Version.Received, result.Version.Binding} {
		if !shaPattern.MatchString(hash) {
			return result, fail
		}
	}
	return result, nil
}

func (r *Runner) currentConversationRepository(ctx context.Context, root string) bool {
	raw, err := r.Run(ctx, "git", "-C", root, "remote", "get-url", "origin")
	remote := strings.ToLower(strings.TrimSpace(raw))
	if err != nil || (remote != "https://github.com/autodevjapan/local-ai-console.git" && remote != "https://github.com/autodevjapan/local-ai-console" && remote != "git@github.com:autodevjapan/local-ai-console.git") {
		return false
	}
	raw, err = r.Run(ctx, "C:/Program Files/GitHub CLI/gh.exe", "api", "--hostname", "github.com", "repos/AutoDevJapan/local-ai-console")
	var repo struct {
		ID      uint64 `json:"id"`
		Name    string `json:"full_name"`
		Private bool   `json:"private"`
		Owner   struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	return err == nil && json.Unmarshal([]byte(raw), &repo) == nil && repo.ID == 1429803672 && strings.EqualFold(repo.Name, "AutoDevJapan/local-ai-console") && strings.EqualFold(repo.Owner.Login, "AutoDevJapan") && repo.Private
}

func (r *Runner) currentConversationSpec(row *TodoRow) ([]byte, error) {
	var path string
	if json.Unmarshal(row.Spec, &path) != nil || !filepath.IsAbs(path) || !regexp.MustCompile(fmt.Sprintf(`^%d-[a-f0-9]{32}\.md$`, row.ID)).MatchString(filepath.Base(path)) || conversationPathSHA(filepath.Dir(path)) != conversationPathSHA(filepath.Join(filepath.Dir(r.TodoPath), "specs")) {
		return nil, errors.New("conversation actor unverified")
	}
	return boundedRegular(path)
}

func (r *Runner) checkCurrentConversationActor(ctx context.Context, origin, actor *TodoRow, job string, saved *conversationActor, historical json.RawMessage, readonly bool) (*conversationActor, error) {
	if !currentConversationPair(origin.ID, actor.ID, origin.Project) || actor.Project != origin.Project || actor.conversationRoot != origin.conversationRoot || !r.repairGuard(ctx, actor.Scheduler.Worktree) || !r.currentConversationRepository(ctx, actor.conversationRoot) {
		return nil, currentActorRejection("current_scope")
	}
	var proof map[string]any
	// As with the existing succession, an exact posted question hold or a
	// metadata-only close may use its retained proof. Every runnable actor is
	// freshly checked by the official controller before new instructions/waits.
	if historical != nil && (readonly || actor.Status == "block") {
		if json.Unmarshal(historical, &proof) != nil || proof["domain"] != "discord-current-assignment-v1" {
			return nil, currentActorRejection("retained_source")
		}
	} else {
		source, err := r.conversationTask(ctx, 4084)
		if err != nil || source.Project != "cc-pc-tools-rs" || conversationActorHeld(source.conversationState) {
			return nil, currentActorRejection("request_scope")
		}
		sourceJob, err := r.repairJob(ctx, source, currentConversationSourceJob)
		if err != nil || sourceJob.Cancelled || (sourceJob.Status != "running" && sourceJob.Status != "succeeded") {
			return nil, currentActorRejection("request_job")
		}
		anchor := job
		if historical != nil {
			var retained struct {
				Job string `json:"actor_job_id"`
			}
			if json.Unmarshal(historical, &retained) != nil || !validJob(retained.Job) {
				return nil, currentActorRejection("anchor_job")
			}
			anchor = retained.Job
		}
		request, e1 := r.currentConversationInput(ctx, source, currentConversationSourceJob)
		work, e2 := r.currentConversationInput(ctx, actor, anchor)
		if e1 != nil || e2 != nil || request.Version.Spec != currentConversationSourceSpec {
			return nil, currentActorRejection("received_input")
		}
		if anchor != job {
			current, err := r.currentConversationInput(ctx, actor, job)
			if err != nil || current.Version.Spec != work.Version.Spec {
				return nil, currentActorRejection("resumed_input")
			}
		}
		proof = map[string]any{"domain": "discord-current-assignment-v1", "origin_todo_id": origin.ID, "actor_todo_id": actor.ID, "actor_job_id": anchor, "project": actor.Project, "source_todo_id": source.ID, "source_job_id": currentConversationSourceJob, "source_kind": "scheduler_dispatch", "source_version": request.Version, "actor_version": work.Version, "delegation_edge_recorded": false, "human_identity_attested": false, "asset_qualified": false}
	}
	a, e1 := r.currentConversationSpec(actor)
	o, e2 := r.currentConversationSpec(origin)
	if e1 != nil || e2 != nil {
		return nil, currentActorRejection("canonical_specs")
	}
	var checked struct {
		Spec string `json:"spec_sha256"`
	}
	b, _ := json.Marshal(proof["actor_version"])
	if json.Unmarshal(b, &checked) != nil || checked.Spec != bytesHash(a) {
		return nil, currentActorRejection("actor_spec_version")
	}
	proof["registered_path_sha256"] = conversationPathSHA(actor.conversationRoot)
	proof["origin_spec_sha256"] = bytesHash(o)
	proof["actor_spec_sha256"] = bytesHash(a)
	bound := &conversationActor{Todo: actor.ID, Job: job, OriginSpecSHA: bytesHash(o), SpecSHA: bytesHash(a), RootSHA: conversationPathSHA(actor.conversationRoot), SourceSHA: canonicalHash(proof)}
	bound.proof, _ = json.Marshal(proof)
	if saved != nil && !sameConversationActor(bound, saved) {
		return nil, currentActorRejection("saved_binding")
	}
	return bound, nil
}

// Keep the tracker informed without making it a second UI writer or releasing
// its dependency. Its original question and API-verified answer are metadata.
func (r *Runner) deliverCurrentConversationTracker(ctx context.Context, q ConversationQuestion, reply conversationReply, req *Request) error {
	if q.Actor == nil || !currentConversationPair(q.Todo, q.Actor.Todo, q.Project) {
		return nil
	}
	var proof conversationProof
	if json.Unmarshal([]byte(req.Context), &proof) != nil || proof.RequestID != q.SenderID || reply.Todo != q.Todo || proof.Owner != reply.Author || bytesHash([]byte(proof.Body)) != q.QuestionSHA {
		return errors.New("conversation actor unverified")
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || conversationActorHeld(row.conversationState) || row.Status != "wait" {
		return errors.New("conversation actor unverified")
	}
	marker := conversationReplyMarker(q, req)
	if hasNoteMarker(row.Note, marker) {
		return nil
	}
	receipt, err := boundedRegular(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json"))
	if err != nil {
		return err
	}
	note := row.Note + "\n\n" + r.conversationReplyInstructions(q, reply, req, receipt, marker)
	if len(note) > 1<<20 {
		return errors.New("conversation delivery note limit exceeded")
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".tracker-note.txt")
	if err = writeText(path, note); err != nil {
		return err
	}
	scope, err := r.conversationScopeFile(row, q)
	if err != nil {
		return err
	}
	expected, err := r.conversationSnapshotFile(row, q, reply.Message+"-tracker")
	if err != nil {
		return err
	}
	if _, err = r.Run(ctx, conversationTodo, "edit", fmt.Sprint(q.Todo), "--note-file", path, "--expected-note-sha256", bytesHash([]byte(row.Note)), "--expected-state-file", expected, "--scope-file", scope); err != nil {
		return errors.New("conversation normal task delivery deferred")
	}
	row, err = r.conversationTask(ctx, q.Todo)
	if err != nil || !hasNoteMarker(row.Note, marker) {
		return errors.New("conversation task delivery readback deferred")
	}
	return nil
}
