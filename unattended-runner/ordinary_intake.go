package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ordinaryMentions = regexp.MustCompile(`<@!?[0-9]+>`)
var ordinaryAnswer = regexp.MustCompile(`(?i)^\s*(?:[0-9]*[abcd][\s,、]*)+$`)

// This is part of the existing Request, not a second routing registry. A binding
// never releases a wait, reopens a completed task or starts a worker.
type IntakeBinding struct {
	Todo      uint64 `json:"todo_id"`
	Project   string `json:"project"`
	SourceSHA string `json:"source_sha256"`
	Evidence  string `json:"evidence"`
}

func ordinaryVersion(q *Request) string {
	return canonicalHash([]any{q.Guild, q.Thread, q.Message, q.Author, q.Created, q.Edited,
		q.ReplyTo, q.ReplyChannel, q.ReplyGuild, q.Text, q.MessageType, q.Attachments})
}

func (r *Runner) parkUnbound(q *Request) {
	q.Phase, q.PendingOwner, q.NextAction = "task_binding_pending", "OMB", "read_source_and_bind_original_task"
}

func (r *Runner) observeOrdinary(q *Request, live historyReadMessage) (bool, error) {
	if q.Conversation != "" {
		return false, nil
	}
	if live.ID != q.Message || live.Channel != q.Thread || live.Author.ID != q.Author || live.Author.Bot || live.Timestamp != q.Created ||
		live.Reference.Message != q.ReplyTo || live.Reference.Channel != q.ReplyChannel || live.Reference.Guild != q.ReplyGuild {
		return false, errors.New("ordinary source identity differs")
	}
	next := *q
	next.Text, next.Edited = safeText(live.Content, r.Token), live.Edited
	next.MessageType, next.Attachments = live.Type, live.Attachments
	// Old saved receipts did not observe type/attachments. Keep that historical
	// unknown rather than replaying every completed task on a schema upgrade.
	// Actual text/edit changes still acquire the complete current observation.
	if q.MessageType == nil && q.Attachments == nil && next.Text == q.Text && next.Edited == q.Edited {
		return false, nil
	}
	if len(next.Text) > 32<<10 {
		return false, errors.New("ordinary source limit exceeded")
	}
	if ordinaryVersion(&next) == ordinaryVersion(q) {
		return false, nil
	}
	created, err := time.Parse(time.RFC3339Nano, live.Timestamp)
	if err != nil || created.After(r.Now().Add(time.Minute)) {
		return false, errors.New("ordinary source clock deferred")
	}
	if live.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, live.Edited)
		if err != nil || edited.Before(created) || edited.After(r.Now().Add(time.Minute)) {
			return false, errors.New("ordinary edit clock deferred")
		}
	}
	old, err := json.Marshal(q)
	if err != nil {
		return false, err
	}
	if err = writeConversationOnce(filepath.Join(r.Config.Home, "requests", "edits", q.Message+"-"+bytesHash(old)+".json"), old); err != nil {
		return false, err
	}
	next.PriorVersions = append(append([]string(nil), q.PriorVersions...), q.Text)
	next.PriorEdits = append(append([]EditVersion(nil), q.PriorEdits...), EditVersion{Edited: q.Edited, SHA256: bytesHash([]byte(q.Text))})
	next.Acquired, next.Reported, next.DeliveryVersion, next.Context = r.Now().UTC().Format(time.RFC3339Nano), "", "", ""
	next.QuestionContextVersion = ""
	if q.Todo != 0 {
		next.Phase, next.PendingOwner, next.NextAction = "source_edit_saved", "OMB", "deliver_current_version_to_original_task"
		if q.Origin != nil && q.Origin.Scope == productReportScope {
			next.Phase = "product_reply_saved"
			next.Context = q.Context
		}
	} else if q.Phase == "captured" || q.Phase == "context_pending" {
		// An actual pre-registration edit keeps its place in admission. Question
		// provenance is rechecked against the new source before registration.
		next.Phase = "captured"
		if ordinaryShortAnswer(next.Text) || r.KnownQuestionThreads[q.Thread] {
			next.Phase = "context_pending"
		}
	} else if q.Phase == "empty" || q.Phase == "status_answered" || q.Phase == "status_query" {
		next.Phase = "captured"
		if strings.TrimSpace(next.Text) == "" {
			next.Phase = "empty"
		} else if isPureStatusQuestion(next.Text, r.State.Destination.Bot) {
			next.Phase = "status_query"
		} else {
			r.parkUnbound(&next)
		}
	}
	previous := *q
	*q = next
	if err = r.save(); err != nil {
		*q = previous
		return false, err
	}
	return true, nil
}

func (r *Runner) ordinarySourceFile(q *Request) (string, error) {
	path := filepath.Join(r.Config.Home, "requests", "versions", q.Message+"-"+ordinaryVersion(q)+".json")
	if _, statErr := os.Lstat(path); statErr == nil {
		prior, err := boundedRegular(path)
		if err != nil {
			return "", err
		}
		var saved Request
		if json.Unmarshal(prior, &saved) != nil || saved.Source != "discord-api-v10" || saved.Key != q.Key || ordinaryVersion(&saved) != ordinaryVersion(q) {
			return "", errors.New("ordinary saved source differs")
		}
		return path, nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	// Store source facts only, so retries after task binding have identical bytes.
	source := Request{Thread: q.Thread, Guild: q.Guild, RecordVersion: q.RecordVersion,
		Message: q.Message, Author: q.Author, Created: q.Created, Edited: q.Edited,
		ReplyTo: q.ReplyTo, ReplyChannel: q.ReplyChannel, ReplyGuild: q.ReplyGuild,
		Acquired: q.Acquired, Source: q.Source, Text: q.Text, Key: q.Key,
		MessageType: q.MessageType, Attachments: q.Attachments, Phase: "source"}
	b, err := json.Marshal(source)
	if err != nil {
		return "", err
	}
	return path, writeConversationOnce(path, b)
}

func (r *Runner) bindOrdinary(ctx context.Context, message string, binding IntakeBinding) error {
	q := r.State.Requests[message]
	if q == nil || q.Conversation != "" || q.Origin != nil && q.Origin.Scope == productReportScope || binding.Todo == 0 || binding.Project == "" || !shaPattern.MatchString(binding.SourceSHA) ||
		strings.TrimSpace(binding.Evidence) == "" || len(binding.Evidence) > 4096 || safeText(binding.Evidence, r.Token) != binding.Evidence {
		return errors.New("ordinary binding input invalid")
	}
	// Do not turn a/b or a known question thread into a new question generation.
	if ordinaryShortAnswer(q.Text) || r.KnownQuestionThreads[q.Thread] {
		return errors.New("ordinary binding requires exact question delivery instead")
	}
	if conversation, err := r.conversationForThread(q.Thread); err != nil || conversation != "" {
		return errors.New("ordinary source belongs to an exact conversation")
	}
	if err := r.validateSource(ctx, q, true); err != nil {
		return err
	}
	if ordinaryVersion(q) != binding.SourceSHA || q.Todo != 0 && q.Todo != binding.Todo || q.Binding != nil && (q.Binding.Todo != binding.Todo || q.Binding.Project != binding.Project) {
		return errors.New("ordinary binding source or task changed")
	}
	if q.Binding != nil && q.DeliveryVersion == binding.SourceSHA && (q.Phase == "source_edit_applied" || q.Phase == "reported") {
		return nil
	}
	row, err := r.conversationTask(ctx, binding.Todo)
	if err != nil || row.Project != binding.Project || row.Status == "recovery_unverified" {
		return errors.New("ordinary binding task unavailable")
	}
	prior := *q
	q.Binding, q.Todo, q.Phase = &binding, binding.Todo, "source_edit_saved"
	q.PendingOwner, q.NextAction = "OMB", "deliver_current_version_to_original_task"
	if err := r.save(); err != nil {
		*q = prior
		return err
	}
	return r.deliverOrdinary(ctx, q)
}

func ordinaryShortAnswer(text string) bool {
	answer := strings.TrimSpace(text)
	answer = strings.TrimSpace(ordinaryMentions.ReplaceAllString(answer, ""))
	return ordinaryAnswer.MatchString(answer)
}

func (r *Runner) deliverOrdinary(ctx context.Context, q *Request) error {
	if q.Todo == 0 || q.Conversation != "" || q.Origin != nil && q.Origin.Scope == productReportScope || r.KnownQuestionThreads[q.Thread] {
		return errors.New("ordinary original task missing")
	}
	if err := r.validateSource(ctx, q, q.Binding != nil); err != nil {
		return err
	}
	if ordinaryShortAnswer(q.Text) {
		q.Phase, q.PendingOwner, q.NextAction = "source_context_pending", "OMB", "verify_exact_original_question_generation"
		_ = r.save()
		return errors.New("ordinary edit requires exact question delivery")
	}
	if q.MessageType != nil && *q.MessageType != 0 && *q.MessageType != 19 {
		q.Phase, q.PendingOwner, q.NextAction = "source_context_pending", "OMB", "read_message_type_and_original_context"
		_ = r.save()
		return errors.New("ordinary message type needs context")
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || row.Status == "recovery_unverified" {
		return errors.New("ordinary original task unavailable")
	}
	if q.Binding != nil && (q.Binding.Todo != q.Todo || q.Binding.Project != row.Project) {
		return errors.New("ordinary binding target differs")
	}
	sourceFile, err := r.ordinarySourceFile(q)
	if err != nil {
		return err
	}
	version := ordinaryVersion(q)
	// Existing workers are instructed to read this directory at safe boundaries.
	// Publish the current source there as well as the durable normal ToDo note.
	if err := publish(r.steeringReceiptPath(q.Todo, q.Message), q); err != nil {
		return err
	}
	marker := "intake-owner-source:" + q.Message + ":" + version
	block := fmt.Sprintf("%s\n本人の正規Discord投稿: %s\n元ToDo %d / source_sha256=%s。原文・編集版・返信先・添付の既知/不明を読み、元の対象と質問世代を保って反映する。旧版の反映票を使わない。停止・依存を解除せず、担当の正規再開を追う。\n実反映後は %s に source=discord-api-v10, todo_id=%d, message_id=%s, thread_id=%s, author_id=%s, source_sha256=%s, job_id（実担当）, revision（40桁）, applied_at（RFC3339）, verification を記録する。受領だけを実反映にしない。\n", marker, sourceFile, q.Todo, version, filepath.Join(r.Config.Home, "applied", q.Message+".json"), q.Todo, q.Message, q.Thread, q.Author, version)
	if !strings.Contains(row.Note, block) {
		note := strings.TrimRight(row.Note, "\r\n") + "\n\n" + block
		if len(note) > 1<<20 {
			return errors.New("ordinary note limit exceeded")
		}
		base := filepath.Join(r.Config.Home, "requests", "delivery", q.Message+"-"+version)
		scope, _ := json.Marshal(row.conversationScope)
		for path, data := range map[string]string{base + ".note.txt": note, base + ".scope.json": string(scope), base + ".expected.json": string(row.conversationState)} {
			if err := writeText(path, data); err != nil {
				return err
			}
		}
		_, editErr := r.Run(ctx, conversationTodo, "edit", fmt.Sprint(q.Todo), "--note-file", base+".note.txt", "--expected-note-sha256", bytesHash([]byte(row.Note)), "--scope-file", base+".scope.json", "--expected-state-file", base+".expected.json")
		current, readErr := r.conversationTask(ctx, q.Todo)
		if readErr != nil || current.Project != row.Project || !strings.Contains(current.Note, block) {
			_ = editErr
			return errors.New("ordinary task CAS delivery deferred")
		}
	}
	previous := *q
	q.DeliveryVersion, q.Phase, q.PendingOwner, q.NextAction = version, "source_edit_delivered", "OMB", "worker_read_apply_and_verify_current_source"
	if row.Status == "done" || row.Status == "block" || row.Status == "wait" {
		q.NextAction = "review_original_wait_stop_or_completion_and_track_authorized_worker_resume"
	}
	if q.Binding != nil {
		binding := *q.Binding
		binding.SourceSHA = version
		q.Binding = &binding
	}
	if err := r.save(); err != nil {
		*q = previous
		return err
	}
	return nil
}

// Independent of message Last/After, with an attempted cursor saved before I/O.
// Completed sources remain revisitable; no failed first source can starve later IDs.
func (r *Runner) refreshOrdinary(ctx context.Context, health *Health) {
	ids := make([]string, 0)
	for id, q := range r.State.Requests {
		if q != nil && q.Conversation == "" && (!r.State.Threads[q.Thread].Managed || r.Config.ManagementIntake) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.OrdinaryCursor })
	for i := 0; i < min(2, len(ids)); i++ {
		if ctx.Err() != nil {
			return
		}
		id := ids[(start+i)%len(ids)]
		r.State.OrdinaryCursor = id
		if r.save() != nil {
			health.Errors = append(health.Errors, "ordinary_cursor_deferred")
			return
		}
		q := r.State.Requests[id]
		if err := r.validateSourceMode(ctx, q, q.Binding != nil, false); err != nil {
			health.Errors = append(health.Errors, "ordinary_source_recheck_deferred")
			continue
		}
	}
}

// Delivery has its own bounded fair turn; a large historical edit watch cannot
// delay already captured instructions by N/2 ticks.
func (r *Runner) processOrdinaryPending(ctx context.Context, health *Health) {
	ids := make([]string, 0)
	for id, q := range r.State.Requests {
		if q != nil && q.Todo != 0 && q.Conversation == "" &&
			(q.Phase == "source_edit_saved" || q.Phase == "source_edit_delivered") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.OrdinaryDeliveryCursor })
	for i := 0; i < min(2, len(ids)); i++ {
		if ctx.Err() != nil {
			return
		}
		id := ids[(start+i)%len(ids)]
		r.State.OrdinaryDeliveryCursor = id
		if err := r.save(); err != nil {
			health.Errors = append(health.Errors, "ordinary_delivery_cursor_deferred")
			return
		}
		q := r.State.Requests[id]
		if q.Phase == "source_edit_saved" {
			if err := r.deliverOrdinary(ctx, q); err != nil {
				health.Errors = append(health.Errors, "ordinary_delivery_deferred")
				continue
			}
		}
		if q.Phase == "source_edit_delivered" {
			if err := r.observeOrdinaryApplication(ctx, q); err != nil {
				health.Errors = append(health.Errors, "ordinary_application_unverified")
			}
		}
	}
}

func (r *Runner) observeLegacySteeringApplication(q *Request, row *TodoRow) (bool, error) {
	if q.Phase != "steering_saved" || q.Edited != "" || len(q.PriorEdits) != 0 || q.DeliveryVersion != "" {
		return false, nil
	}
	var proof struct {
		Todo         uint64 `json:"todo_id"`
		Message      string `json:"message_id"`
		Job          string `json:"job_id"`
		Revision     string `json:"revision"`
		Verification string `json:"verification"`
		ReceiptSHA   string `json:"receipt_sha256"`
	}
	path := filepath.Join(r.Config.Home, "applied", q.Message+".json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return false, nil
	}
	if err := readJSON(path, 1<<20, &proof); err != nil {
		return false, err
	}
	if proof.Todo != q.Todo || proof.Message != q.Message || !validJob(proof.Job) || strings.TrimSpace(proof.Revision) == "" || strings.TrimSpace(proof.Verification) == "" {
		return false, nil
	}
	owned := false
	for _, raw := range row.Jobs {
		var job string
		if json.Unmarshal(raw, &job) == nil && job == proof.Job {
			owned = true
		}
	}
	if !owned {
		return false, nil
	}
	if proof.ReceiptSHA != "" {
		b, err := boundedRegular(r.steeringReceiptPath(q.Todo, q.Message))
		if err != nil || bytesHash(b) != proof.ReceiptSHA {
			return false, nil
		}
	}
	q.Phase, q.Reported, q.PendingOwner, q.NextAction = "reported", "applied", "OMB", "verify_parent_receipt_and_product_delivery"
	return true, r.save()
}

func (r *Runner) observeOrdinaryApplication(ctx context.Context, q *Request) error {
	var proof struct {
		Source       string `json:"source"`
		Todo         uint64 `json:"todo_id"`
		Message      string `json:"message_id"`
		Thread       string `json:"thread_id"`
		Author       string `json:"author_id"`
		SourceSHA    string `json:"source_sha256"`
		Job          string `json:"job_id"`
		Revision     string `json:"revision"`
		Applied      string `json:"applied_at"`
		Verification string `json:"verification"`
	}
	path := filepath.Join(r.Config.Home, "applied", q.Message+".json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	if err := readJSON(path, 1<<20, &proof); err != nil {
		return err
	}
	stamp, err := time.Parse(time.RFC3339Nano, proof.Applied)
	acquired, _ := time.Parse(time.RFC3339Nano, q.Acquired)
	if err != nil || stamp.Before(acquired) || stamp.After(r.Now()) || proof.Source != "discord-api-v10" || proof.Todo != q.Todo || proof.Message != q.Message || proof.Author != q.Author || proof.Thread != q.Thread || proof.SourceSHA != ordinaryVersion(q) || proof.SourceSHA != q.DeliveryVersion || !regexpRevision(proof.Revision) || strings.TrimSpace(proof.Verification) == "" || !validJob(proof.Job) {
		return errors.New("ordinary application source differs")
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil {
		return err
	}
	owned := false
	for _, raw := range row.Jobs {
		var id string
		if json.Unmarshal(raw, &id) == nil && id == proof.Job {
			owned = true
		}
	}
	if !owned {
		return errors.New("ordinary application worker differs")
	}
	q.Phase, q.PendingOwner, q.NextAction = "source_edit_applied", "OMB", "verify_parent_receipt_and_product_delivery"
	return r.save()
}
