package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const productReportScope = "finite-product-report-v1"
const productReportInterval = 15 * time.Minute
const directOwnerChat = "direct_owner_chat"
const stateRequestSource = "state_request"
const reportDeliveryOnly = "report-and-private-reply-delivery-only"

// An explicit, finite reporting/delivery scope. This is not dispatch permission
// for a project, repository, game, paid operation or old question generation.
type ProductReportConfig struct {
	Todo    uint64      `json:"todo_id"`
	Project string      `json:"project"`
	Source  OwnerSource `json:"owner_source"`
}
type OwnerSource struct {
	Kind             string   `json:"kind,omitempty"`
	Thread           string   `json:"thread_id"`
	Message          string   `json:"message_id"`
	Created          string   `json:"created_at"`
	SHA256           string   `json:"body_sha256"`
	ReceivedAt       string   `json:"received_at,omitempty"`
	ObservedAt       string   `json:"observed_at,omitempty"`
	Provenance       string   `json:"provenance,omitempty"`
	ReceivedText     string   `json:"received_text,omitempty"`
	Targets          []uint64 `json:"target_todo_ids,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	DiscordURLExists *bool    `json:"discord_url_exists,omitempty"`
	Purpose          string   `json:"purpose,omitempty"`
	AnswerMessage    string   `json:"answer_message_id,omitempty"`
	AnswerReceipt    string   `json:"answer_receipt_file,omitempty"`
	AnswerReceiptSHA string   `json:"answer_receipt_sha256,omitempty"`
}
type ProductSnapshot struct {
	Task             string   `json:"task_status"`
	Progress         *uint8   `json:"progress,omitempty"`
	Job              string   `json:"job_id,omitempty"`
	Worker           string   `json:"worker_status"`
	Live             bool     `json:"process_identity_live"`
	Dependencies     []uint64 `json:"wait_on,omitempty"`
	Stage            string   `json:"stage"`
	RepliesSaved     int      `json:"replies_saved,omitempty"`
	RepliesDelivered int      `json:"replies_reference_delivered,omitempty"`
	RepliesApplied   int      `json:"replies_applied,omitempty"`
	StatusAnswer     string   `json:"status_answer_message_id,omitempty"`
}
type ProductNotice struct {
	ID        string          `json:"request_id"`
	Signature string          `json:"signature"`
	At        string          `json:"observed_at"`
	Body      string          `json:"body"`
	Snapshot  ProductSnapshot `json:"snapshot"`
	Heartbeat bool            `json:"heartbeat,omitempty"`
}
type ProductReportState struct {
	Identity   string         `json:"identity"`
	Generation uint64         `json:"generation"`
	Signature  string         `json:"signature,omitempty"`
	Sent       string         `json:"sent_at,omitempty"`
	Thread     string         `json:"thread_id,omitempty"`
	Pending    *ProductNotice `json:"pending,omitempty"`
}
type newMessageReceipt struct {
	Thread    string `json:"thread_id"`
	Message   string `json:"message_id"`
	Author    string `json:"author_id"`
	Timestamp string `json:"timestamp"`
	Flags     uint64 `json:"flags"`
}
type newMessageLedger struct {
	Domain string            `json:"domain"`
	Events []newMessageEvent `json:"events"`
}
type newMessageEvent struct {
	Event   string             `json:"event"`
	Request string             `json:"request_id"`
	Phase   string             `json:"phase"`
	Receipt *newMessageReceipt `json:"receipt"`
}

func productLabel(id uint64) string {
	switch id {
	case 2639:
		return "受託案件"
	case 2640:
		return "万世記"
	case 2641:
		return "GoDD MCP α"
	case 2642:
		return "GoDD Press"
	case 1626:
		return "PhosGaea"
	case 2626:
		return "司令塔"
	}
	return ""
}
func validateProductReports(config []ProductReportConfig) error {
	if len(config) > 16 {
		return errors.New("finite product report count exceeded")
	}
	seen := make(map[uint64]bool)
	for _, c := range config {
		if c.Todo == 0 || seen[c.Todo] || strings.TrimSpace(c.Project) == "" {
			return errors.New("report identity differs")
		}
		seen[c.Todo] = true
		if c.Source.Kind == stateRequestSource {
			_, ok := number(c.Source.Message)
			if !ok || c.Source.Thread != "" || c.Source.Created != "" || c.Source.SHA256 != "" || c.Source.ReceivedText != "" || c.Source.ReceivedAt != "" || c.Source.ObservedAt != "" || c.Source.Provenance != "" || len(c.Source.Targets) != 0 || c.Source.Scope != "" || c.Source.DiscordURLExists != nil {
				return errors.New("request report source differs")
			}
			if c.Source.Purpose == "status_question" {
				_, answerOK := number(c.Source.AnswerMessage)
				hash, e := hex.DecodeString(c.Source.AnswerReceiptSHA)
				if !answerOK || !filepath.IsAbs(c.Source.AnswerReceipt) || e != nil || len(hash) != 32 || strings.ToLower(c.Source.AnswerReceiptSHA) != c.Source.AnswerReceiptSHA {
					return errors.New("status answer source differs")
				}
			} else if c.Source.Purpose != "" || c.Source.AnswerMessage != "" || c.Source.AnswerReceipt != "" || c.Source.AnswerReceiptSHA != "" {
				return errors.New("request report purpose differs")
			}
			continue
		}
		if c.Source.Kind == directOwnerChat {
			if err := validateDirectChat(c); err != nil {
				return err
			}
			continue
		}
		if c.Source.Kind != "" && c.Source.Kind != "discord_api_v10" {
			return errors.New("report source kind differs")
		}
		_, threadOK := number(c.Source.Thread)
		_, messageOK := number(c.Source.Message)
		_, stampErr := time.Parse(time.RFC3339Nano, c.Source.Created)
		hash, hashErr := hex.DecodeString(c.Source.SHA256)
		if productLabel(c.Todo) == "" || !threadOK || !messageOK || stampErr != nil || hashErr != nil || len(hash) != 32 || strings.ToLower(c.Source.SHA256) != c.Source.SHA256 || c.Source.ReceivedText != "" || c.Source.ReceivedAt != "" || c.Source.ObservedAt != "" || c.Source.Provenance != "" || len(c.Source.Targets) != 0 || c.Source.Scope != "" || c.Source.Purpose != "" || c.Source.AnswerMessage != "" || c.Source.AnswerReceipt != "" || c.Source.AnswerReceiptSHA != "" {
			return errors.New("finite product report configuration differs")
		}
	}
	return nil
}
func validateDirectChat(c ProductReportConfig) error {
	s := c.Source
	if productLabel(c.Todo) == "" || s.Provenance != "codex_current_user_message" || s.Scope != reportDeliveryOnly || s.Thread != "" || s.Message != "" || s.Created != "" || s.DiscordURLExists == nil || *s.DiscordURLExists || len(s.ReceivedText) > 32<<10 || strings.TrimSpace(s.ReceivedText) == "" || len(s.Targets) == 0 || len(s.Targets) > 6 || s.Purpose != "" || s.AnswerMessage != "" || s.AnswerReceipt != "" || s.AnswerReceiptSHA != "" || s.ObservedAt != "" && s.ReceivedAt != "" {
		return errors.New("direct owner chat receipt differs")
	}
	if _, err := time.Parse(time.RFC3339Nano, directChatTime(s)); err != nil {
		return errors.New("direct owner chat time differs")
	}
	sum := sha256.Sum256([]byte(s.ReceivedText))
	if hex.EncodeToString(sum[:]) != s.SHA256 {
		return errors.New("direct owner chat hash differs")
	}
	seen := make(map[uint64]bool)
	found := false
	for _, id := range s.Targets {
		if productLabel(id) == "" || seen[id] {
			return errors.New("direct owner chat finite targets differ")
		}
		seen[id] = true
		if id == c.Todo {
			found = true
		}
	}
	if !found {
		return errors.New("direct owner chat target missing")
	}
	return nil
}
func directChatTime(s OwnerSource) string {
	if s.ObservedAt != "" {
		return s.ObservedAt
	}
	return s.ReceivedAt
}
func reportLabel(c ProductReportConfig) string {
	if c.Source.Kind == stateRequestSource {
		return "依頼の進捗"
	}
	return productLabel(c.Todo)
}
func (r *Runner) productConfig(todo uint64) (ProductReportConfig, bool) {
	if validateProductReports(r.Config.ProductReports) != nil {
		return ProductReportConfig{}, false
	}
	for _, c := range r.Config.ProductReports {
		if c.Todo == todo {
			return c, true
		}
	}
	return ProductReportConfig{}, false
}
func productIdentity(c ProductReportConfig) string {
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(append([]byte(productReportScope+"\x00"), data...))
	return hex.EncodeToString(sum[:])
}
func reportSnapshotSignature(c ProductReportConfig, s ProductSnapshot) string {
	// Finishing the bookkeeping of an already-answered status question must
	// not produce the same completion notice a second time.
	if c.Source.Purpose == "status_question" {
		s.Task = ""
	}
	bytes, _ := json.Marshal(s)
	return bytesHash(bytes)
}
func (r *Runner) verifyOwnerSource(ctx context.Context, c ProductReportConfig) bool {
	if c.Source.Kind == directOwnerChat {
		if validateDirectChat(c) != nil {
			return false
		}
		received, err := time.Parse(time.RFC3339Nano, directChatTime(c.Source))
		cutover, _ := time.Parse(time.RFC3339Nano, r.State.Since)
		return err == nil && !received.Before(cutover) && !received.After(r.Now().Add(time.Minute))
	}
	if c.Source.Kind == stateRequestSource {
		req := r.State.Requests[c.Source.Message]
		if req == nil || req.Message != c.Source.Message || req.Todo != c.Todo || req.Author != r.State.Destination.Owner || req.Source != "discord-api-v10" || req.Key != requestKey(req.Thread, req.Message) {
			return false
		}
		row, err := r.todo(req.Todo)
		return err == nil && row.Project == c.Project && r.verifyReportRequest(ctx, req) && (c.Source.Purpose != "status_question" || r.verifyExistingStatusAnswer(ctx, c, req))
	}
	created, err := time.Parse(time.RFC3339Nano, c.Source.Created)
	if err != nil || created.After(r.Now().Add(time.Minute)) {
		return false
	}
	var thread Thread
	if r.get(ctx, "/channels/"+c.Source.Thread, &thread) != nil || thread.ID != c.Source.Thread || thread.Type != 11 || thread.Guild != r.State.Destination.Guild {
		return false
	}
	if !(thread.Parent == requestForum && (thread.Owner == r.State.Destination.Owner || thread.Owner == r.State.Destination.Bot)) && !(thread.Parent == r.State.Destination.Forum && thread.Owner == r.State.Destination.Bot) {
		return false
	}
	var m Message
	if r.get(ctx, "/channels/"+c.Source.Thread+"/messages/"+c.Source.Message, &m) != nil || m.ID != c.Source.Message || m.Channel != thread.ID || m.Author.ID != r.State.Destination.Owner || m.Author.Bot || m.Timestamp != c.Source.Created {
		return false
	}
	sum := sha256.Sum256([]byte(m.Content))
	return hex.EncodeToString(sum[:]) == c.Source.SHA256
}
func (r *Runner) verifyProductScope(ctx context.Context, origin ProgressOrigin, row *TodoRow) bool {
	c, ok := r.productConfig(origin.Todo)
	if !ok || row == nil || origin.Scope != productReportScope || row.ID != c.Todo || row.Project != c.Project || origin.Project != c.Project || origin.SourceThread != c.Source.Thread || origin.SourceMessage != c.Source.Message || origin.SourceHash != c.Source.SHA256 || origin.SourceKind != c.Source.Kind || ((c.Source.Kind == directOwnerChat || c.Source.Kind == stateRequestSource) && origin.SourceIdentity != productIdentity(c)) {
		return false
	}
	if c.Todo == productRetryID {
		if !validProductRetryProof(row.Qualification.Receipt, r.Now()) {
			return false
		}
		raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--verify-product-resume", "1626")
		var fresh productRetryProof
		if err != nil || json.Unmarshal([]byte(raw), &fresh) != nil || !validProductRetryProof(fresh, r.Now()) {
			return false
		}
	}
	return r.verifyOwnerSource(ctx, c)
}
func productOrigin(c ProductReportConfig) ProgressOrigin {
	o := ProgressOrigin{Scope: productReportScope, Project: c.Project, Todo: c.Todo, SourceThread: c.Source.Thread, SourceMessage: c.Source.Message, SourceHash: c.Source.SHA256, SourceKind: c.Source.Kind}
	if c.Source.Kind == directOwnerChat || c.Source.Kind == stateRequestSource {
		o.SourceIdentity = productIdentity(c)
	}
	return o
}
func (r *Runner) reserveNotice(kind string) error {
	if r.NoticeAttempted {
		return errors.New("tick notice budget exhausted")
	}
	r.NoticeAttempted = true
	r.State.ReportTurn = "products"
	if kind == "products" {
		r.State.ReportTurn = "requests"
	}
	return r.save()
}
func latestTodoJob(row *TodoRow) string {
	if validJob(row.Worker.Job) {
		return row.Worker.Job
	}
	if validJob(row.Scheduler.Job) {
		return row.Scheduler.Job
	}
	for i := len(row.Jobs) - 1; i >= 0; i-- {
		var job string
		if json.Unmarshal(row.Jobs[i], &job) == nil && validJob(job) {
			return job
		}
	}
	return ""
}
func (r *Runner) productSnapshot(row *TodoRow) (ProductSnapshot, error) {
	s := ProductSnapshot{Task: row.Status, Progress: row.Progress, Worker: "unverified", Dependencies: append([]uint64(nil), row.WaitOn...), Job: latestTodoJob(row)}
	for _, req := range r.State.Requests {
		if req == nil || req.Todo != row.ID || req.Origin == nil || req.Origin.Scope != productReportScope {
			continue
		}
		switch req.Phase {
		case "product_reply_saved":
			s.RepliesSaved++
		case "product_reply_delivered":
			s.RepliesDelivered++
		case "product_reply_applied":
			s.RepliesApplied++
		}
	}
	if row.Progress != nil && *row.Progress > 100 {
		return s, errors.New("task progress invalid")
	}
	sort.Slice(s.Dependencies, func(i, j int) bool { return s.Dependencies[i] < s.Dependencies[j] })
	if s.Job != "" {
		root := r.JobsPath
		if root == "" {
			root = "D:/CC/Tools/codex-jobs/jobs"
		}
		var job struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			PID     uint32 `json:"pid"`
			Created uint64 `json:"pid_created"`
		}
		if readJSON(filepath.Join(root, s.Job, "job.json"), 1<<20, &job) == nil && job.ID == s.Job {
			switch job.Status {
			case "queued", "starting", "running", "succeeded", "failed", "cancelled", "lost":
				s.Worker = job.Status
			}
			live := r.ProcessAlive
			if live == nil {
				live = processReceiptAlive
			}
			if job.PID != 0 && job.Created != 0 {
				s.Live = live(job.PID, job.Created)
			}
		}
	}
	switch row.Status {
	case "done":
		s.Stage = "completion_recorded"
		if s.Worker == "succeeded" {
			s.Stage = "complete"
		}
	case "block":
		s.Stage = "owner_wait"
	case "wait":
		s.Stage = "waiting"
	case "todo":
		s.Stage = "queued"
	case "doing":
		s.Stage = "assigned"
		if s.Worker == "running" && s.Live {
			s.Stage = "running"
		}
		if s.Worker == "queued" || s.Worker == "starting" {
			s.Stage = "queued"
		}
	default:
		return s, errors.New("task status unverified")
	}
	return s, nil
}
func productBody(c ProductReportConfig, s ProductSnapshot, at string, guild string) string {
	state := "継続中"
	stage := "担当の割当てを記録しています。実行は未確認です。"
	switch s.Stage {
	case "status_answer_complete":
		state = "完了"
		stage = "状況確認への回答をDiscord上で確認しました。これは当該質問への回答の完了記録です。"
	case "complete":
		state = "完了"
		stage = "共有ToDoの完了と、担当の実行成功を確認しました。"
	case "completion_recorded":
		stage = "共有ToDoには完了の記録があります。担当の実行成功は未確認です。"
	case "running":
		stage = "担当プロセスの稼働を、保存されたプロセス識別情報と照合しました。"
	case "queued":
		stage = "実行の順番を待っています。"
	case "owner_wait":
		stage = "本人の判断または停止の記録に従って待機しています。"
	case "waiting":
		stage = "保存された再開条件または依存作業の完了を待っています。"
	}
	body := fmt.Sprintf("状態: %s\n%s（共有ToDo %d）: %s\n確認時刻: %s", state, reportLabel(c), c.Todo, stage, at)
	if s.Progress != nil {
		body += fmt.Sprintf("\n記録済みの達成率: %d%%", *s.Progress)
	}
	if s.RepliesSaved+s.RepliesDelivered+s.RepliesApplied > 0 {
		body += fmt.Sprintf("\n追加指示: 保存 %d件、担当参照の配送 %d件、実反映の証拠 %d件。", s.RepliesSaved, s.RepliesDelivered, s.RepliesApplied)
	}
	if c.Source.Kind == directOwnerChat {
		return body + "\n出所: 本人との直接チャット。Discordの本人投稿URLはありません。"
	}
	body += fmt.Sprintf("\n元の依頼: https://discord.com/channels/%s/%s/%s", guild, c.Source.Thread, c.Source.Message)
	if s.StatusAnswer != "" {
		body += fmt.Sprintf("\n回答の記録: https://discord.com/channels/%s/%s/%s", guild, c.Source.Thread, s.StatusAnswer)
	}
	return body
}

func (r *Runner) reportBody(c ProductReportConfig, s ProductSnapshot, at string) string {
	if c.Source.Kind == stateRequestSource {
		req := r.State.Requests[c.Source.Message]
		if req != nil {
			c.Source.Thread = req.Thread
			c.Source.Message = req.Message
		}
	}
	return productBody(c, s, at, r.State.Destination.Guild)
}
func (r *Runner) isConfiguredRequestReport(todo uint64) bool {
	if validateProductReports(r.Config.ProductReports) != nil {
		return false
	}
	for _, c := range r.Config.ProductReports {
		if c.Todo == todo && c.Source.Kind == stateRequestSource {
			req := r.State.Requests[c.Source.Message]
			row, err := r.todo(todo)
			return err == nil && row.Project == c.Project && req != nil && req.Todo == todo && req.Author == r.State.Destination.Owner && req.Source == "discord-api-v10" && req.Key == requestKey(req.Thread, req.Message)
		}
	}
	return false
}

var errNoticeMissing = errors.New("no verified exact product notice")

// Recovery checks the independent verified ledger AND the live effect. A
// successful command response alone is not a new progress-origin credential.
func (r *Runner) recoverProductNotice(ctx context.Context, id, body, expectedThread string) (ProgressLink, error) {
	var ledger newMessageLedger
	if err := readJSON(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), maxRecoveryMessages, &ledger); err != nil {
		if os.IsNotExist(err) {
			return ProgressLink{}, errNoticeMissing
		}
		return ProgressLink{}, errors.New("product notice ledger unavailable")
	}
	if ledger.Domain != "new-owner-recovery-messages" {
		return ProgressLink{}, errors.New("product notice ledger scope differs")
	}
	for _, e := range ledger.Events {
		if e.Event != "verified" || e.Request != id || e.Phase != "log:0" || e.Receipt == nil {
			continue
		}
		p := e.Receipt
		if p.Author != r.State.Destination.Bot || p.Flags&4096 == 0 || expectedThread != "" && p.Thread != expectedThread {
			return ProgressLink{}, errors.New("product notice receipt differs")
		}
		if _, ok := number(p.Thread); !ok {
			return ProgressLink{}, errors.New("product notice identity invalid")
		}
		if _, ok := number(p.Message); !ok {
			return ProgressLink{}, errors.New("product notice identity invalid")
		}
		var thread Thread
		if r.get(ctx, "/channels/"+p.Thread, &thread) != nil || thread.ID != p.Thread || thread.Type != 11 || thread.Guild != r.State.Destination.Guild || thread.Parent != r.State.Destination.Forum || thread.Owner != p.Author {
			return ProgressLink{}, errors.New("product notice destination differs")
		}
		var m Message
		if r.get(ctx, "/channels/"+p.Thread+"/messages/"+p.Message, &m) != nil || m.ID != p.Message || m.Channel != p.Thread || m.Author.ID != p.Author || !m.Author.Bot || m.Flags&4096 == 0 || m.MentionEveryone || len(m.Mentions) != 0 || m.Timestamp != p.Timestamp || !(m.Content == body || strings.HasPrefix(m.Content, body+"\n")) {
			return ProgressLink{}, errors.New("product notice live effect differs")
		}
		stamp, err := time.Parse(time.RFC3339Nano, p.Timestamp)
		cutoff, _ := time.Parse(time.RFC3339Nano, r.State.Since)
		if err != nil || stamp.Before(cutoff) || stamp.After(r.Now().Add(time.Minute)) {
			return ProgressLink{}, errors.New("product notice timestamp differs")
		}
		sum := sha256.Sum256([]byte(m.Content))
		return ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: id, Guild: thread.Guild, Thread: p.Thread, Message: p.Message}, Author: p.Author, Created: p.Timestamp, Edited: m.Edited, ContentHash: hex.EncodeToString(sum[:])}, nil
	}
	return ProgressLink{}, errNoticeMissing
}

func (r *Runner) reportProducts(ctx context.Context, h *Health) {
	if len(r.Config.ProductReports) == 0 || r.NoticeAttempted || ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if validateProductReports(r.Config.ProductReports) != nil {
		h.Errors = append(h.Errors, "product_report_configuration_deferred")
		return
	}
	if r.State.ProductReports == nil {
		r.State.ProductReports = make(map[uint64]*ProductReportState)
	}
	if r.State.ProgressLinks == nil {
		r.State.ProgressLinks = make(map[string]ProgressLink)
	}
	config := append([]ProductReportConfig(nil), r.Config.ProductReports...)
	sort.Slice(config, func(i, j int) bool { return config[i].Todo < config[j].Todo })
	start := sort.Search(len(config), func(i int) bool { return config[i].Todo > r.State.ProductCursor })
	for i := 0; i < min(2, len(config)); i++ {
		if ctx.Err() != nil {
			return
		}
		c := config[(start+i)%len(config)]
		r.State.ProductCursor = c.Todo
		if r.save() != nil {
			h.Errors = append(h.Errors, "product_report_cursor_deferred")
			return
		}
		row, err := r.todo(c.Todo)
		if err != nil || !r.verifyProductScope(ctx, productOrigin(c), row) {
			h.Errors = append(h.Errors, "product_report_origin_deferred")
			continue
		}
		identity := productIdentity(c)
		state := r.State.ProductReports[c.Todo]
		if state == nil {
			state = &ProductReportState{Identity: identity}
			r.State.ProductReports[c.Todo] = state
		}
		if state.Identity != identity {
			h.Errors = append(h.Errors, "product_report_identity_changed")
			continue
		}
		if state.Pending == nil {
			snapshot, e := r.productSnapshot(row)
			if e != nil {
				h.Errors = append(h.Errors, "product_report_state_deferred")
				continue
			}
			if c.Source.Purpose == "status_question" {
				snapshot = ProductSnapshot{Task: row.Status, Worker: "not_required_for_status_answer", Stage: "status_answer_complete", StatusAnswer: c.Source.AnswerMessage}
			}
			signature := reportSnapshotSignature(c, snapshot)
			heartbeat := false
			if signature == state.Signature {
				last, e := time.Parse(time.RFC3339Nano, state.Sent)
				if c.Source.Kind != stateRequestSource || e != nil || r.Now().Sub(last) < time.Hour || snapshot.Stage == "complete" || snapshot.Stage == "status_answer_complete" {
					continue
				}
				heartbeat = true
			}
			if state.Sent != "" {
				sent, e := time.Parse(time.RFC3339Nano, state.Sent)
				if e != nil || r.Now().Sub(sent) < productReportInterval {
					continue
				}
			}
			state.Generation++
			at := r.Now().UTC().Format(time.RFC3339Nano)
			body := r.reportBody(c, snapshot, at)
			if heartbeat {
				body += "\n定期確認: 前回と同じ状態です。"
			}
			state.Pending = &ProductNotice{ID: fmt.Sprintf("product-report-%d-%s-%d-%s", c.Todo, identity[:16], state.Generation, signature[:16]), Signature: signature, At: at, Body: body, Snapshot: snapshot, Heartbeat: heartbeat}
			if r.save() != nil {
				h.Errors = append(h.Errors, "product_report_pending_deferred")
				return
			}
		}
		pending := state.Pending
		observed, parseErr := time.Parse(time.RFC3339Nano, pending.At)
		cutoff, _ := time.Parse(time.RFC3339Nano, r.State.Since)
		// The durable retry body must still be exactly the fixed metadata-only
		// rendering of its frozen observation, never arbitrary persisted text.
		sig := reportSnapshotSignature(c, pending.Snapshot)
		id := fmt.Sprintf("product-report-%d-%s-%d-%s", c.Todo, identity[:16], state.Generation, sig[:16])
		expectedBody := r.reportBody(c, pending.Snapshot, pending.At)
		if pending.Heartbeat {
			expectedBody += "\n定期確認: 前回と同じ状態です。"
		}
		if (pending.Heartbeat && c.Source.Kind != stateRequestSource) || parseErr != nil || observed.Before(cutoff) || observed.After(r.Now().Add(time.Minute)) || pending.Signature != sig || pending.ID != id || pending.Body != expectedBody {
			h.Errors = append(h.Errors, "product_report_pending_identity_deferred")
			continue
		}
		link, err := r.recoverProductNotice(ctx, pending.ID, pending.Body, state.Thread)
		if errors.Is(err, errNoticeMissing) {
			if ctx.Err() != nil {
				return
			}
			path := filepath.Join(r.Config.Home, "notices", pending.ID+".txt")
			if writeText(path, pending.Body) != nil {
				h.Errors = append(h.Errors, "product_report_body_deferred")
				return
			}
			args := []string{"recovery-log", "--request-id", pending.ID, "--title", "【" + reportLabel(c) + "】作業の進捗", "--body-file", path}
			if state.Thread != "" {
				args = append(args, "--thread", state.Thread)
			}
			if r.reserveNotice("products") != nil {
				h.Errors = append(h.Errors, "product_report_budget_deferred")
				return
			}
			_, _ = r.Run(ctx, "D:/CC/Tools/discord-ask/discord_ask.exe", args...)
			link, err = r.recoverProductNotice(ctx, pending.ID, pending.Body, state.Thread)
		}
		if err != nil {
			h.Errors = append(h.Errors, "product_report_receipt_deferred")
			if r.NoticeAttempted {
				return
			}
			continue
		}
		if state.Thread == "" {
			origin := productOrigin(c)
			origin.RequestID = link.RequestID
			origin.Guild = link.Guild
			origin.Thread = link.Thread
			origin.Message = link.Message
			origin.Job = pending.Snapshot.Job
			link.ProgressOrigin = origin
			if _, exists := r.State.ProgressLinks[link.Thread]; exists {
				h.Errors = append(h.Errors, "product_report_thread_collision")
				return
			}
			r.State.ProgressLinks[link.Thread] = link
		}
		state.Thread = link.Thread
		state.Signature = pending.Signature
		state.Sent = r.Now().UTC().Format(time.RFC3339Nano)
		state.Pending = nil
		if r.save() != nil {
			h.Errors = append(h.Errors, "product_report_commit_deferred")
			return
		}
		h.ProductReports++
		return
	}
}

// Receiver-generated notices already carry an exact request key. Reconstruct
// their origins from the independent verified ledger when no registry was made.
func (r *Runner) derivedRequestLinks(ctx context.Context) []ProgressLink {
	var ledger newMessageLedger
	if readJSON(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), maxRecoveryMessages, &ledger) != nil || ledger.Domain != "new-owner-recovery-messages" {
		return nil
	}
	out := []ProgressLink{}
	ids := make([]string, 0)
	for id, req := range r.State.Requests {
		if req != nil && req.ProgressThread != "" {
			if _, exists := r.State.ProgressLinks[req.ProgressThread]; !exists {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.OriginCursor })
	for i := 0; i < min(2, len(ids)); i++ {
		if ctx.Err() != nil {
			break
		}
		id := ids[(start+i)%len(ids)]
		r.State.OriginCursor = id
		if r.save() != nil {
			break
		}
		req := r.State.Requests[id]
		if req == nil || req.Todo == 0 || req.ProgressThread == "" || req.Origin != nil || req.Key != requestKey(req.Thread, req.Message) || req.Author != r.State.Destination.Owner || req.Source != "discord-api-v10" {
			continue
		}
		if !r.verifyLinkedRequest(ctx, req) {
			continue
		}
		row, err := r.todo(req.Todo)
		if err != nil || row.Project != r.Config.Project {
			continue
		}
		job := latestTodoJob(row)
		if job == "" {
			continue
		}
		for _, e := range ledger.Events {
			if e.Event != "verified" || e.Phase != "log:0" || e.Request != "unattended-"+req.Key+"-"+req.Reported || e.Receipt == nil || e.Receipt.Thread != req.ProgressThread || e.Receipt.Author != r.State.Destination.Bot || e.Receipt.Flags&4096 == 0 {
				continue
			}
			p := e.Receipt
			out = append(out, ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: e.Request, Guild: r.State.Destination.Guild, Thread: p.Thread, Message: p.Message, Todo: req.Todo, Job: job}, Author: p.Author, Created: p.Timestamp})
		}
	}
	return out
}
