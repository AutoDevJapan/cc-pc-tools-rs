package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func (r *Runner) validateRequest(ctx context.Context, request *Request) error {
	return r.validateSource(ctx, request, false)
}
func (r *Runner) validateSource(ctx context.Context, request *Request, binding bool) error {
	return r.validateSourceMode(ctx, request, binding, true)
}
func (r *Runner) validateSourceMode(ctx context.Context, request *Request, binding, admission bool) error {
	cutoff, cutoffErr := time.Parse(time.RFC3339Nano, r.State.Since)
	stamp, err := time.Parse(time.RFC3339Nano, request.Created)
	if err != nil || cutoffErr != nil || (request.HistoryWatch != nil || stamp.Before(cutoff)) && !r.validHistoryWatch(request) || stamp.After(r.Now().Add(time.Minute)) || request.Source != "discord-api-v10" || request.Author != r.State.Destination.Owner || request.Key != requestKey(request.Thread, request.Message) {
		return errors.New("owner receipt association differs")
	}
	if _, ok := number(request.Thread); !ok {
		return errors.New("owner thread invalid")
	}
	if _, ok := number(request.Message); !ok {
		return errors.New("owner message invalid")
	}
	var thread Thread
	if err := r.get(ctx, "/channels/"+request.Thread, &thread); err != nil {
		if admission && strings.Contains(err.Error(), "status 404") {
			request.Phase = "source_unavailable"
			_ = r.save()
		}
		return err
	}
	botRequest := thread.Parent == requestForum && thread.Owner == r.State.Destination.Bot && (r.ProgressLinks[thread.ID].Todo != 0 || r.KnownQuestionThreads[thread.ID])
	legacy := thread.Type == 11 && ((thread.Parent == requestForum && thread.Owner == r.State.Destination.Owner) || botRequest || (thread.Parent == r.State.Destination.Forum && thread.Owner == r.State.Destination.Bot))
	if thread.ID != request.Thread || thread.Guild != r.State.Destination.Guild {
		return errors.New("owner thread scope differs")
	}
	if !legacy {
		if _, err := monthlyReadable(ctx, r, request.Thread); err != nil {
			return err
		}
	}
	if admission && !binding && legacy && thread.Owner == r.State.Destination.Bot && request.Origin == nil && r.ProgressLinks[thread.ID].Todo == 0 && !r.KnownQuestionThreads[thread.ID] && !(request.Phase == "status_query" && isPureStatusQuestion(request.Text, r.State.Destination.Bot)) {
		request.Phase = "origin_pending"
		if err := r.save(); err != nil {
			return err
		}
		return errors.New("owner reply original association pending")
	}
	var live historyReadMessage
	if err := r.get(ctx, "/channels/"+request.Thread+"/messages/"+request.Message, &live); err != nil {
		if admission && strings.Contains(err.Error(), "status 404") {
			request.Phase = "source_unavailable"
			_ = r.save()
		}
		return err
	}
	if live.ID != request.Message || live.Channel != request.Thread || live.Author.ID != request.Author || live.Author.Bot || live.Timestamp != request.Created {
		return errors.New("owner message changed; receipt retained")
	}
	if request.Guild != "" && request.Guild != r.State.Destination.Guild {
		return errors.New("owner message guild changed; receipt retained")
	}
	if (request.ReplyTo != "" && request.ReplyTo != live.Reference.Message) || (request.ReplyChannel != "" && request.ReplyChannel != live.Reference.Channel) || (request.ReplyGuild != "" && request.ReplyGuild != live.Reference.Guild) {
		return errors.New("owner message reference changed; receipt retained")
	}
	if request.RecordVersion < 2 {
		request.Guild = r.State.Destination.Guild
		request.ReplyTo = live.Reference.Message
		request.ReplyChannel = live.Reference.Channel
		request.ReplyGuild = live.Reference.Guild
		request.RecordVersion = 2
		if err := r.save(); err != nil {
			return err
		}
		return errors.New("owner message metadata migration saved; validation deferred")
	}
	if request.RecordVersion > 2 || request.Guild != r.State.Destination.Guild || live.Reference.Message != request.ReplyTo || live.Reference.Channel != request.ReplyChannel || live.Reference.Guild != request.ReplyGuild {
		return errors.New("owner message metadata differs; receipt retained")
	}
	if changed, err := r.observeOrdinary(request, live); err != nil {
		return err
	} else if changed {
		return errors.New("owner edit saved; registration deferred")
	}
	return nil
}

func (r *Runner) questionContext(ctx context.Context, request *Request) error {
	// Only an exact question in the independent new-question receipts may resolve
	// a short answer. Arbitrary preceding bot progress is not question provenance.
	var ledger struct {
		Domain string `json:"domain"`
		Events []struct {
			Event     string `json:"event"`
			RequestID string `json:"request_id"`
			Phase     string `json:"phase"`
			Receipt   *struct {
				Thread  string `json:"thread_id"`
				Message string `json:"message_id"`
				Author  string `json:"author_id"`
			} `json:"receipt"`
		} `json:"events"`
	}
	if err := readJSON(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), maxState, &ledger); err != nil {
		return err
	}
	if ledger.Domain != "new-owner-recovery-messages" {
		return errors.New("question receipt domain differs")
	}
	for _, event := range ledger.Events {
		if event.Event != "verified" || event.Phase != "question:0" || event.Receipt == nil || event.Receipt.Thread != request.Thread || event.Receipt.Author != r.State.Destination.Bot {
			continue
		}
		raw, err := r.Run(ctx, "D:/CC/Tools/discord-ask/discord_ask.exe", "recovery-replies", "--thread", request.Thread, "--message", event.Receipt.Message, "--request-id", event.RequestID, "--read-only")
		if err != nil {
			return errors.New("exact question verification deferred")
		}
		var proof struct {
			Domain   string `json:"domain"`
			Source   string `json:"source"`
			Thread   string `json:"thread_id"`
			Owner    string `json:"owner_id"`
			Question string `json:"question_message_id"`
			Replies  []struct {
				ID       string `json:"message_id"`
				Author   string `json:"author_id"`
				Thread   string `json:"question_thread_id"`
				Question string `json:"question_message_id"`
				Content  string `json:"body"`
			} `json:"owner_replies"`
		}
		if json.Unmarshal([]byte(raw), &proof) != nil {
			return errors.New("exact question proof invalid")
		}
		matched := false
		for _, reply := range proof.Replies {
			if reply.ID == request.Message && reply.Author == request.Author && reply.Thread == request.Thread && reply.Question == event.Receipt.Message && safeText(reply.Content, r.Token) == request.Text {
				matched = true
			}
		}
		if proof.Domain != "new-owner-recovery-messages" || proof.Source != "discord-api-v10" || proof.Thread != request.Thread || proof.Owner != r.State.Destination.Owner || proof.Question != event.Receipt.Message || !matched {
			return errors.New("exact question/owner/reply association differs")
		}
		// The API reader validates the receipt/generation and owner. Keep its original
		// scope with this delivery rather than interpreting the answer in this runner.
		request.Context = safeText(raw, r.Token)
		request.QuestionContextVersion = ordinaryVersion(request)
		request.Phase, request.PendingOwner, request.NextAction = "captured", "", ""
		return r.save()
	}
	return errors.New("original question association pending")
}

type TodoRow struct {
	conversationRoot         string
	conversationOriginState  json.RawMessage
	conversationActorBinding *conversationActor
	conversationState        json.RawMessage
	conversationScope        conversationTodoScope
	ID                       uint64                              `json:"id"`
	Project                  string                              `json:"project"`
	Status                   string                              `json:"status"`
	Note                     string                              `json:"note"`
	Spec                     json.RawMessage                     `json:"spec"`
	Jobs                     []json.RawMessage                   `json:"jobs"`
	Progress                 *uint8                              `json:"progress"`
	WaitOn                   []uint64                            `json:"wait_on"`
	WaitReady                json.RawMessage                     `json:"wait_ready"`
	QuestionWait             json.RawMessage                     `json:"owner_question_wait,omitempty"`
	QuestionAnswers          []json.RawMessage                   `json:"owner_question_answer_receipts,omitempty"`
	QuestionDeliveries       []json.RawMessage                   `json:"owner_question_delivery_receipts,omitempty"`
	DirectionRecords         []conversationDirectionReceipt      `json:"owner_direction_records,omitempty"`
	ImplementationRecords    []conversationImplementationReceipt `json:"owner_implementation_records,omitempty"`
	ResumeGeneration         json.RawMessage                     `json:"resume_generation,omitempty"`
	Qualification            struct {
		Receipt productRetryProof `json:"receipt"`
	} `json:"product_resume_qualification"`
	Worker struct {
		Job string `json:"job"`
	} `json:"worker_receipt"`
	Scheduler struct {
		Job              string          `json:"job"`
		Worktree         string          `json:"worktree"`
		OwnerInstruction json.RawMessage `json:"owner_instruction"`
	} `json:"scheduler"`
}

func (row *TodoRow) conversationInstruction() string {
	var text string
	_ = json.Unmarshal(row.Scheduler.OwnerInstruction, &text)
	return text
}

func (r *Runner) todo(id uint64) (*TodoRow, error) {
	var db struct {
		Items []TodoRow `json:"items"`
	}
	if err := readJSON(r.TodoPath, maxTodo, &db); err != nil {
		return nil, err
	}
	var found *TodoRow
	for i := range db.Items {
		if db.Items[i].ID == id {
			if found != nil {
				return nil, fmt.Errorf("%w: duplicate task receipt", errTaskBindingAmbiguous)
			}
			found = &db.Items[i]
		}
	}
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("%w: task receipt missing", errTaskBindingAmbiguous)
}

var errTaskBindingAmbiguous = errors.New("original task binding ambiguous")

func (r *Runner) threadActive(request *Request) (*Request, error) {
	var found *Request
	for _, old := range r.State.Requests {
		if old.Message == request.Message || old.Todo == 0 || old.Thread != request.Thread {
			continue
		}
		row, err := r.todo(old.Todo)
		if err != nil {
			return nil, err
		}
		if row.Project != r.Config.Project {
			return nil, fmt.Errorf("%w: project differs", errTaskBindingAmbiguous)
		}
		if found != nil && found.Todo != old.Todo {
			return nil, fmt.Errorf("%w: multiple original tasks", errTaskBindingAmbiguous)
		}
		found = old
	}
	return found, nil
}
func (r *Runner) deliverSteering(ctx context.Context, request *Request, old *Request) error {
	if err := r.validateRequest(ctx, request); err != nil {
		return err
	}
	request.Todo = old.Todo
	request.Phase, request.PendingOwner, request.NextAction = "steering_saved", "", ""
	if err := r.publishSteeringSource(request); err != nil {
		return err
	}
	// This is durable delivery, not a claim that the live worker already applied it.
	return r.save()
}
func (r *Runner) sendStatus(ctx context.Context, request *Request, phase string) error {
	source := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", r.State.Destination.Guild, request.Thread, request.Message)
	body := fmt.Sprintf("状態: 継続中\nDiscordの指示を共有ToDo %dへ保存しました。担当が実行と検証を進めます。\n元の投稿: %s", request.Todo, source)
	if phase == "working" {
		body = fmt.Sprintf("状態: 継続中\n共有ToDo %dの担当が作業を開始しました。結果を確認して、この投稿へ返します。\n元の投稿: %s", request.Todo, source)
	}
	if phase == "steering" {
		body = fmt.Sprintf("状態: 継続中\n追加の指示を、共有ToDo %dの同じ担当へ保存しました。実反映を確認中です。\n元の投稿: %s", request.Todo, source)
	}
	if phase == "complete" {
		body = fmt.Sprintf("状態: 完了\n担当が実作業の完了を報告し、共有ToDo %dの完了を記録しました。\n元の投稿: %s", request.Todo, source)
		row, err := r.todo(request.Todo)
		if err != nil {
			return err
		}
		job := row.Scheduler.Job
		for _, raw := range row.Jobs {
			var jid string
			if json.Unmarshal(raw, &jid) == nil && validJob(jid) {
				job = jid
			}
		}
		if validJob(job) {
			last, err := os.ReadFile(filepath.Join("D:/CC/Tools/codex-jobs/jobs", job, "last_message.md"))
			if err == nil {
				links := pullRequestLinks(string(last), 5)
				if len(links) > 0 {
					body += "\n成果: " + strings.Join(links, "\n")
				}
			}
		}
	}
	noticeKey := request.Key
	if len(request.PriorEdits) != 0 || request.Binding != nil {
		noticeKey += "-" + ordinaryVersion(request)[:16]
	}
	path := filepath.Join(r.Config.Home, "notices", noticeKey+"-"+phase+".txt")
	if err := writeText(path, body); err != nil {
		return err
	}
	args := []string{"recovery-log", "--request-id", "unattended-" + noticeKey + "-" + phase, "--title", "【PCの道具】Discordの依頼への対応", "--body-file", path}
	if request.ProgressThread != "" {
		args = append(args, "--thread", request.ProgressThread)
	}
	if err := r.reserveNotice("requests"); err != nil {
		return err
	}
	raw, err := r.Run(ctx, "D:/CC/Tools/discord-ask/discord_ask.exe", args...)
	var receipt struct {
		Delivery string `json:"delivery"`
		Thread   string `json:"thread_id"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &receipt) != nil || receipt.Delivery != "verified" {
		thread, proofErr := r.recoverStatusReceipt(ctx, "unattended-"+noticeKey+"-"+phase, body)
		if proofErr != nil {
			return errors.New("status receipt unverified")
		}
		receipt.Thread = thread
		receipt.Delivery = "verified"
	}
	if _, ok := number(receipt.Thread); !ok {
		return errors.New("status thread invalid")
	}
	request.ProgressThread = receipt.Thread
	request.Reported = phase
	if phase == "complete" {
		request.Phase = "reported"
	}
	return r.save()
}

func pullRequestLinks(body string, limit int) []string {
	var links []string
	seen := make(map[string]bool)
	for _, field := range strings.Fields(body) {
		candidates := []string{field}
		if strings.HasPrefix(field, "[") {
			if open := strings.Index(field, "]("); open >= 0 {
				candidates = []string{field[open+2:]}
			}
		}
		for _, candidate := range candidates {
			candidate = strings.Trim(candidate, "<>[]),.;`\"")
			u, err := url.Parse(candidate)
			if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
			if len(parts) != 4 || parts[2] != "pull" || (parts[0] != "AutoDevJapan" && parts[0] != "BM1314" && parts[0] != "mashfromband") {
				continue
			}
			if parts[1] == "" || parts[1] == "." || parts[1] == ".." {
				continue
			}
			validRepository := true
			for _, c := range parts[1] {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
					validRepository = false
				}
			}
			if _, validID := number(parts[3]); !validRepository || !validID || seen[candidate] {
				continue
			}
			if len(links) >= limit {
				return links
			}
			seen[candidate] = true
			links = append(links, candidate)
		}
	}
	return links
}

// A sender may finish POST/GET verification before its pipe deadline expires.
// Recover that exact proven effect; never manufacture success or send elsewhere.
func (r *Runner) recoverStatusReceipt(ctx context.Context, id, body string) (string, error) {
	var ledger struct {
		Domain string `json:"domain"`
		Events []struct {
			Event   string `json:"event"`
			Request string `json:"request_id"`
			Phase   string `json:"phase"`
			Receipt *struct {
				Thread  string `json:"thread_id"`
				Message string `json:"message_id"`
				Author  string `json:"author_id"`
				Flags   uint64 `json:"flags"`
			} `json:"receipt"`
		} `json:"events"`
	}
	if err := readJSON(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), maxState, &ledger); err != nil {
		return "", err
	}
	if ledger.Domain != "new-owner-recovery-messages" {
		return "", errors.New("sender receipt domain differs")
	}
	for _, event := range ledger.Events {
		if event.Event != "verified" || event.Request != id || event.Phase != "log:0" || event.Receipt == nil || event.Receipt.Author != r.State.Destination.Bot || event.Receipt.Flags&4096 == 0 {
			continue
		}
		var channel Thread
		if err := r.get(ctx, "/channels/"+event.Receipt.Thread, &channel); err != nil {
			return "", err
		}
		if channel.ID != event.Receipt.Thread || channel.Guild != r.State.Destination.Guild || channel.Parent != r.State.Destination.Forum || channel.Owner != r.State.Destination.Bot || channel.Type != 11 {
			return "", errors.New("sender receipt destination differs")
		}
		var message Message
		if err := r.get(ctx, "/channels/"+channel.ID+"/messages/"+event.Receipt.Message, &message); err != nil {
			return "", err
		}
		if message.ID != event.Receipt.Message || message.Channel != channel.ID || message.Author.ID != r.State.Destination.Bot || !strings.HasPrefix(message.Content, body) || message.Flags&4096 == 0 {
			return "", errors.New("sender receipt content differs")
		}
		for _, mention := range message.Mentions {
			if mention.ID == r.State.Destination.Owner {
				return "", errors.New("unexpected owner notification")
			}
		}
		return channel.ID, nil
	}
	return "", errors.New("no exact verified sender receipt")
}
func validJob(s string) bool { return regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(s) }
func (r *Runner) results(ctx context.Context) error {
	ids := make([]string, 0, len(r.State.Requests))
	for id := range r.State.Requests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.ResultCursor })
	for i := 0; i < len(ids); i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id := ids[(start+i)%len(ids)]
		request := r.State.Requests[id]
		if request != nil && request.Conversation != "" {
			continue
		}
		if request != nil && request.Phase == "status_query" {
			r.State.ResultCursor = id
			if err := r.save(); err != nil {
				return err
			}
			return r.answerStatusQuery(ctx, request)
		}
		if request == nil || request.Todo == 0 || request.Phase == "reported" || request.Phase == "status_answered" || request.Phase == "origin_blocked" || request.Phase == "product_reply_saved" || request.Phase == "product_reply_delivered" || request.Phase == "product_reply_applied" || r.isConfiguredRequestReport(request.Todo) {
			continue
		}
		if request.Phase == "source_edit_saved" || request.Phase == "source_edit_delivered" || request.Phase == "source_context_pending" {
			continue
		}
		r.State.ResultCursor = id
		if err := r.save(); err != nil {
			return err
		}
		row, err := r.todo(request.Todo)
		if err != nil {
			return err
		}
		project := r.Config.Project
		if request.Binding != nil {
			project = request.Binding.Project
		}
		if row.Project != project {
			return errors.New("task project association differs")
		}
		if (request.Phase == "source_edit_applied" || request.Phase == "steering_applied") && row.Status != "done" {
			continue
		}
		stage := "accepted"
		if request.Phase == "steering_saved" {
			applied, err := r.observeLegacySteeringApplication(request, row)
			if err != nil {
				return err
			}
			if applied {
				if row.Status == "done" {
					return r.sendStatus(ctx, request, "complete")
				}
				return nil
			}
			stage = "steering"
			if row.Status == "done" || row.Status == "block" {
				request.PendingOwner, request.NextAction = "OMB", "review_original_wait_stop_or_completion_and_track_authorized_worker_resume"
				return r.save()
			}
		} else if row.Status == "doing" {
			stage = "working"
		} else if row.Status == "done" {
			stage = "complete"
		}
		if stage != request.Reported {
			if err := r.sendStatus(ctx, request, stage); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}
func schedulerOK(raw string) bool {
	for i, c := range raw {
		if c != '{' {
			continue
		}
		var report map[string]json.RawMessage
		if json.Unmarshal([]byte(raw[i:]), &report) != nil {
			continue
		}
		var mode, busy bool
		_ = json.Unmarshal(report["repair_mode"], &mode)
		_ = json.Unmarshal(report["busy"], &busy)
		var failures []string
		if value, exists := report["step_failures"]; exists && json.Unmarshal(value, &failures) != nil {
			return false
		}
		return mode && !busy && len(failures) == 0 && (len(report["error"]) == 0 || string(report["error"]) == "null")
	}
	return false
}
