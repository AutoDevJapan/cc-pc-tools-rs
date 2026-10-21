package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const agentResultDomain = "parent-agent-result-v1"
const agentResultAttempts = 4
const agentResultBudget = 20 * time.Second

var errAgentOwnerQuestionScope = errors.New("owner_question_scope_invalid")

// Public, non-secret text from the parent's existing owner question.
// This result post points back to that question; it does not create another ask.
type AgentOwnerQuestion struct {
	Question string `json:"question"`
	ChoiceA  string `json:"choice_a"`
	ChoiceB  string `json:"choice_b"`
	URL      string `json:"url"`
}

// Public explanations are a closed vocabulary. Private source material is
// referenced, never opened or copied into Discord or command diagnostics.
type AgentResultEvent struct {
	RequestID     string             `json:"request_id"`
	Todo          uint64             `json:"todo_id"`
	Project       string             `json:"project"`
	Thread        string             `json:"thread_id"`
	Source        string             `json:"source"`
	Parent        string             `json:"parent_id,omitempty"`
	Child         string             `json:"child_id,omitempty"`
	Attempt       string             `json:"attempt_id"`
	Transition    string             `json:"transition_id"`
	State         string             `json:"state"`
	Reason        string             `json:"reason"`
	Remaining     string             `json:"remaining"`
	Next          string             `json:"next_action"`
	OwnerRequired bool               `json:"owner_required"`
	OwnerReply    bool               `json:"owner_reply,omitempty"`
	OwnerQuestion AgentOwnerQuestion `json:"owner_question,omitzero"`
	PrivateRef    string             `json:"private_ref,omitempty"`
	ItemID        string             `json:"item_id,omitempty"`
	ItemKind      string             `json:"item_kind,omitempty"`
	CapacityBytes uint64             `json:"capacity_bytes,omitempty"`
	CapacityScope string             `json:"capacity_scope,omitempty"`
}
type AgentResultRecord struct {
	Domain         string           `json:"domain"`
	ID             string           `json:"id"`
	Event          AgentResultEvent `json:"event"`
	ParentReceived string           `json:"parent_received_at"`
	Attempts       int              `json:"attempts"`
	NextAttempt    string           `json:"next_attempt_at,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
	Message        string           `json:"message_id,omitempty"`
	URL            string           `json:"url,omitempty"`
	Verified       string           `json:"verified_at,omitempty"`
	Body           string           `json:"body"`
	NotifyOwner    bool             `json:"notify_owner,omitempty"`
}
type agentResultWatch struct {
	Domain              string           `json:"domain"`
	Event               AgentResultEvent `json:"event"`
	Signature           string           `json:"signature"`
	Closed              bool             `json:"closed"`
	Generation          uint64           `json:"generation"`
	Deadline            string           `json:"result_deadline"`
	BaselineItems       []string         `json:"baseline_item_transitions,omitempty"`
	WaitReported        bool             `json:"wait_limit_reported,omitempty"`
	ObservationReported bool             `json:"observation_reported,omitempty"`
	StableWaitSignature bool             `json:"stable_wait_signature,omitempty"`
}

var agentStateLabels = map[string]string{
	"completed": "完了", "stopped": "未完了で停止", "rejected": "実行を拒否", "start_failed": "起動できず停止",
	"failed": "異常終了", "timeout": "時間切れ", "capacity_wait": "利用枠の回復待ち", "dependency_wait": "依存する作業の完了待ち",
	"cancelled": "取り消し", "lost": "結果がないまま終了", "parent_stopped": "親担当が未完了で停止",
	"unverified":    "担当の状態は未確認",
	"item_deferred": "対象1件の清掃を保留（担当全体の終了とは別）",
}
var agentReasonLabels = map[string]string{
	"completed": "担当が作業の完了を報告しました。", "work_incomplete": "担当の実行は終わりましたが、依頼の完了は確認できていません。",
	"policy_rejected": "実行環境が操作を拒否しました。", "initialization_rejected": "実行環境が担当の起動または初期化を拒否しました。",
	"execution_failed": "担当の実行中にエラーが発生しました。", "timeout": "実行時間の上限に達しました。",
	"capacity": "利用枠またはモデルの空きを待っています。", "dependency": "先に完了する必要のある作業を待っています。",
	"cancelled": "担当の実行が取り消されました。", "result_missing": "担当が終了結果を残さず停止しました。",
	"parent_stopped":                  "親担当が処理を続けられず、既存の監督処理へ結果を保存しました。",
	"validation_stop":                 "通知経路の確認のため、検証担当を予定どおり停止しました。",
	"authentication":                  "実行に必要な認証を確認できませんでした。",
	"observation_unavailable":         "登録済み担当の状態を安全に確認できません。担当の成否は未確認です。",
	"result_wait_limit":               "登録した結果待ちの期限に達しました。担当の成否は未確認です。",
	"cleanup_busy":                    "対象が使用中のため、清掃を保留しました。",
	"cleanup_ownership_unverified":    "対象の所有と作業終了を確認できず、清掃を保留しました。",
	"cleanup_measurement_unavailable": "対象の容量を計測できず、清掃を保留しました。",
	"cleanup_budget":                  "今回の作業時間内に対象の清掃を終えられず、保留しました。",
	"cleanup_protected":               "実行環境の確認で対象を清掃できず、保留しました。",
	"item_progress_invalid":           "清掃保留の記録に不足または不正な情報があり、親担当が内容を確認します。",
}
var agentRemainingLabels = map[string]string{"none": "なし", "verification": "元の依頼に必要な検証と配送結果の確認が残っています。", "work": "元の依頼の実作業が残っています。", "unverified": "元の依頼の残作業はまだ確認できていません。"}
var agentNextLabels = map[string]string{"none": "追加作業なし", "review_result": "親担当が保存済みの結果を確認して続行します。", "retry": "親担当が同じ依頼の通常の再開手順を進めます。", "wait_dependency": "既存の担当管理で待ち先の完了後に再開します。", "owner_decision": "保存済みの質問と本人の回答を確認して進めます。"}
var agentItemKinds = map[string]string{"build_output": "終了済みの検証生成物", "deletion_request": "削除希望へ移した対象", "recycle_bin": "ごみ箱の対象", "unused_model": "未使用候補のAIモデル", "scratch": "担当が作成した一時ファイル"}

// One actual item/transition, supplied by the worker's structured progress.
// Paths and raw error text stay private. No category-level counts are expanded
// into invented item results.
type AgentDeferredItem struct {
	ID            string             `json:"item_id"`
	Kind          string             `json:"item_kind"`
	Transition    string             `json:"transition_id"`
	Reason        string             `json:"reason"`
	Bytes         uint64             `json:"capacity_bytes"`
	Scope         string             `json:"capacity_scope"`
	Next          string             `json:"next_action"`
	OwnerRequired bool               `json:"owner_required"`
	OwnerQuestion AgentOwnerQuestion `json:"owner_question,omitzero"`
	PrivateRef    string             `json:"private_ref,omitempty"`
}

func agentDeferredEvent(base AgentResultEvent, item AgentDeferredItem) AgentResultEvent {
	e := base
	e.Transition = "item-" + canonicalHash([]string{item.ID, item.Transition})[:40]
	e.State = "item_deferred"
	e.Reason = item.Reason
	e.Remaining = "verification"
	e.Next = item.Next
	e.OwnerRequired = item.OwnerRequired
	e.OwnerReply = false
	e.OwnerQuestion = item.OwnerQuestion
	e.ItemID = item.ID
	e.ItemKind = item.Kind
	e.CapacityBytes = item.Bytes
	e.CapacityScope = item.Scope
	e.PrivateRef = item.PrivateRef
	return e
}

func validateAgentEvent(e AgentResultEvent) error {
	if !conversationID.MatchString(e.RequestID) || !conversationID.MatchString(e.Attempt) || !conversationID.MatchString(e.Transition) || e.Todo == 0 || e.Project != "cc-pc-tools-rs" {
		return errors.New("agent result scope invalid")
	}
	if _, ok := number(e.Thread); !ok {
		return errors.New("agent result destination invalid")
	}
	for _, id := range []string{e.Parent, e.Child} {
		if id != "" && !conversationID.MatchString(id) {
			return errors.New("agent result identity invalid")
		}
	}
	if e.Source != "multi_agent_v1" && e.Source != "codex_jobs" && e.Source != "launcher" && e.Source != "parent" {
		return errors.New("agent result source invalid")
	}
	if agentStateLabels[e.State] == "" || agentReasonLabels[e.Reason] == "" || agentRemainingLabels[e.Remaining] == "" || agentNextLabels[e.Next] == "" {
		return errors.New("agent result vocabulary invalid")
	}
	if e.State == "completed" && (e.Reason != "completed" || e.Remaining != "none" || e.Next != "none" || e.OwnerRequired) || e.State != "completed" && (e.Reason == "completed" || e.Remaining == "none" || e.Next == "none") || e.OwnerRequired != (e.Next == "owner_decision") {
		return errors.New("agent result outcome inconsistent")
	}
	if e.OwnerRequired && e.OwnerQuestion != (AgentOwnerQuestion{}) {
		q := e.OwnerQuestion
		for _, text := range []string{q.Question, q.ChoiceA, q.ChoiceB} {
			if strings.TrimSpace(text) == "" || len(text) > 300 || strings.Contains(text, "<@") || strings.Contains(text, "@everyone") || strings.Contains(text, "@here") || strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
				return errors.New("concrete owner question and choices required")
			}
		}
		parts := strings.Split(strings.TrimPrefix(q.URL, "https://discord.com/channels/"), "/")
		if !strings.HasPrefix(q.URL, "https://discord.com/channels/") || len(parts) != 3 || q.ChoiceA == q.ChoiceB {
			return errors.New("separate owner question destination required")
		}
		for _, id := range parts {
			if _, ok := number(id); !ok {
				return errors.New("owner question destination invalid")
			}
		}
	} else if !e.OwnerRequired && e.OwnerQuestion != (AgentOwnerQuestion{}) {
		return errors.New("unneeded owner question is not permitted")
	}
	if e.PrivateRef != "" && (!filepath.IsAbs(e.PrivateRef) || len(e.PrivateRef) > 4096) {
		return errors.New("agent result private reference invalid")
	}
	if e.State == "item_deferred" {
		if !conversationID.MatchString(e.ItemID) || agentItemKinds[e.ItemKind] == "" || !strings.HasPrefix(e.Reason, "cleanup_") || (e.CapacityScope != "complete" && e.CapacityScope != "partial" && e.CapacityScope != "unknown") || e.CapacityScope == "unknown" && e.CapacityBytes != 0 {
			return errors.New("agent deferred item invalid")
		}
	} else if e.ItemID != "" || e.ItemKind != "" || e.CapacityScope != "" || e.CapacityBytes != 0 {
		return errors.New("item details require an item transition")
	}
	return nil
}
func agentResultID(e AgentResultEvent) string {
	return canonicalHash([]any{agentResultDomain, e.RequestID, e.Todo, e.Source, e.Parent, e.Child, e.Attempt, e.Transition, e.ItemID})
}
func (r *Runner) agentResultPath(id string) string {
	archived := filepath.Join(r.Config.Home, "agent-results", "verified", id+".json")
	if _, err := os.Stat(archived); err == nil {
		return archived
	}
	return filepath.Join(r.Config.Home, "agent-results", "records", id+".json")
}
func (r *Runner) archiveAgentFile(kind, archive, id string) error {
	to := filepath.Join(r.Config.Home, "agent-results", archive, id+".json")
	if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(r.Config.Home, "agent-results", kind, id+".json"), to)
}
func (r *Runner) recordAgentResult(e AgentResultEvent) (AgentResultRecord, error) {
	return r.recordAgentResultWithin(context.Background(), e)
}
func agentResultNotifies(e AgentResultEvent) bool {
	// The owner's latest correction requires action, not merely a reply or stop.
	// Every result is still returned and delivered, including silent holds.
	return e.OwnerRequired
}
func (r *Runner) recordAgentResultWithin(ctx context.Context, e AgentResultEvent) (AgentResultRecord, error) {
	if err := validateAgentEvent(e); err != nil {
		return AgentResultRecord{}, err
	}
	// All producers, including the periodic observer, share this short lock.
	// The long-running runner lock is never needed for parent receipt ingestion.
	unlock, err := r.agentReceiveLock(ctx)
	if err != nil {
		return AgentResultRecord{}, err
	}
	defer unlock()
	id := agentResultID(e)
	p := AgentResultRecord{Domain: agentResultDomain, ID: id, Event: e, ParentReceived: r.Now().UTC().Format(time.RFC3339Nano), NotifyOwner: agentResultNotifies(e)}
	p.Body = agentResultBody(p)
	path := r.agentResultPath(id)
	var old AgentResultRecord
	if err := readJSON(path, 1<<20, &old); err == nil {
		if old.Domain != agentResultDomain || old.ID != id || old.Event != e || old.ParentReceived == "" {
			return p, errors.New("agent result retry differs")
		}
		return old, nil
	} else if !os.IsNotExist(err) {
		return p, errors.New("agent result receipt unavailable")
	}
	// The periodic sender may have atomically archived this very receipt after
	// the first path lookup. A retry never replaces its verified result.
	if err := readJSON(filepath.Join(r.Config.Home, "agent-results", "verified", id+".json"), 1<<20, &old); err == nil {
		if old.Domain != agentResultDomain || old.ID != id || old.Event != e {
			return p, errors.New("agent result retry differs")
		}
		return old, nil
	} else if !os.IsNotExist(err) {
		return p, errors.New("agent result receipt unavailable")
	}
	// This file is the parent's durable inbox and the supervisor's outbox.
	// Old receipts can lack question details; never invalidate their history.
	// New action-required input must supply the concrete, public question.
	if e.OwnerRequired && e.OwnerQuestion == (AgentOwnerQuestion{}) {
		return p, errors.New("concrete owner question and choices required")
	}
	if err := publish(path, p); err != nil {
		return p, err
	}
	return p, nil
}
func agentResultBody(p AgentResultRecord) string {
	if p.Body != "" {
		return p.Body
	}
	state := "継続中"
	if p.Event.State == "completed" {
		state = "完了"
	}
	decision := "不要"
	if p.Event.OwnerRequired {
		decision = "必要（元の質問で扱います）"
	}
	body := fmt.Sprintf("状態: %s\n【PCの道具】担当の結果を親へ返しました（元ToDo %d）。\n結果: %s\n理由: %s\n残作業: %s\n次の処置: %s\n本人の判断: %s", state, p.Event.Todo, agentStateLabels[p.Event.State], agentReasonLabels[p.Event.Reason], agentRemainingLabels[p.Event.Remaining], agentNextLabels[p.Event.Next], decision)
	if p.Event.OwnerRequired {
		q := p.Event.OwnerQuestion
		body = fmt.Sprintf("回答が必要：%s\n a: %s\n b: %s\n回答先: <%s>\nこの対象だけ回答を待ち、AI側はほかの作業を続けます。\n\n%s", q.Question, q.ChoiceA, q.ChoiceB, q.URL, body)
	} else {
		next := map[string]string{"none": "結果の記録を終えました。", "review_result": "保存した結果を確認して作業を続けます。", "retry": "同じ依頼の再開を進めます。", "wait_dependency": "先行作業が終わり次第、作業を再開します。"}[p.Event.Next]
		body = "対応不要です。AI側で" + next + "返信やPC操作は要りません。\n\n" + body
	}
	if p.Event.State == "item_deferred" {
		capacity := "未計測"
		if p.Event.CapacityScope == "complete" {
			capacity = fmt.Sprintf("%d bytes（対象全体の実測）", p.Event.CapacityBytes)
		}
		if p.Event.CapacityScope == "partial" {
			capacity = fmt.Sprintf("%d bytes（確認できた範囲のみ）", p.Event.CapacityBytes)
		}
		body += fmt.Sprintf("\n保留対象: %s（対象 %s）\n確認できた容量: %s\n対象の場所と元の指示は親の私的記録に保持しています。", agentItemKinds[p.Event.ItemKind], p.Event.ItemID, capacity)
	}
	if len(p.ID) >= 40 {
		// Ordinary logs deduplicate presentation, not retry IDs. The real
		// parent receipt distinguishes separate events with the same prose.
		body += "\n\n-# 結果記録: " + p.ID[:40]
	}
	return body
}
func agentResultFiles(home, kind string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(home, "agent-results", kind))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if !e.IsDir() && len(id) == 64 && strings.Trim(id, "0123456789abcdef") == "" && e.Name() == id+".json" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
func (r *Runner) agentResultSlice(kind string, limit int) ([]string, error) {
	ids, err := agentResultFiles(r.Config.Home, kind)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	path := filepath.Join(r.Config.Home, "agent-results", kind+"-cursor.json")
	var cursor string
	if err = readJSON(path, 1024, &cursor); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	start := sort.SearchStrings(ids, cursor)
	if start < len(ids) && ids[start] == cursor {
		start++
	}
	selected := make([]string, 0, min(limit, len(ids)))
	for i := 0; i < min(limit, len(ids)); i++ {
		selected = append(selected, ids[(start+i)%len(ids)])
	}
	return selected, nil
}
func (r *Runner) agentResultCursor(kind, id string) error {
	return publish(filepath.Join(r.Config.Home, "agent-results", kind+"-cursor.json"), id)
}

func (r *Runner) deliverAgentResult(ctx context.Context, p *AgentResultRecord) error {
	if p.Event.OwnerRequired && p.NotifyOwner && !strings.HasPrefix(p.Event.OwnerQuestion.URL, "https://discord.com/channels/"+r.State.Destination.Guild+"/") {
		return errAgentOwnerQuestionScope
	}
	body := agentResultBody(*p)
	path := filepath.Join(r.Config.Home, "agent-results", "bodies", p.ID+".txt")
	if err := writeText(path, body); err != nil {
		return err
	}
	// The ordinary sender owns durable reservations, nonces and lost-response recovery.
	// Notifying results use an ordinary product topic. The original thread may
	// be a question's silent progress thread; never relax that sender restriction.
	destination := p.Event.Thread
	if p.NotifyOwner {
		destination = ""
	}
	response, err := r.conversationLog(ctx, "agent-result-"+p.ID[:40], "【PCの道具】担当の結果", path, destination, p.NotifyOwner, nil)
	if err != nil || response["delivery"] != "verified" {
		return errors.New("delivery_unverified")
	}
	thread, _ := response["thread_id"].(string)
	message, _ := response["message_id"].(string)
	if _, ok := number(thread); !ok || destination != "" && thread != destination {
		return errors.New("delivery_unverified")
	}
	if _, ok := number(message); !ok {
		return errors.New("delivery_unverified")
	}
	var live struct {
		Message
		MentionRoles *[]string `json:"mention_roles"`
	}
	if r.get(ctx, "/channels/"+thread+"/messages/"+message, &live) != nil {
		return errors.New("delivery_unverified")
	}
	expectedBody, expectedFlags := body, uint64(4096|4)
	mentionsOK := len(live.Mentions) == 0
	if p.NotifyOwner {
		expectedBody += "\n<@" + r.State.Destination.Owner + ">"
		expectedFlags = 4
		mentionsOK = len(live.Mentions) == 1 && live.Mentions[0].ID == r.State.Destination.Owner
	}
	rolesOK := live.MentionRoles != nil && len(*live.MentionRoles) == 0
	// Old silent receipts predate this extra readback field. Their immutable
	// request stays valid; new owner notifications require the explicit field.
	if !p.NotifyOwner && live.MentionRoles == nil {
		rolesOK = true
	}
	if live.ID != message || live.Channel != thread || live.Author.ID != r.State.Destination.Bot || !live.Author.Bot || !agentDeliveredBody(live.Content, expectedBody) || live.Flags&(4096|4) != expectedFlags || live.MentionEveryone || !rolesOK || !mentionsOK {
		return errors.New("delivery_unverified")
	}
	p.Message = message
	p.URL = "https://discord.com/channels/" + r.State.Destination.Guild + "/" + thread + "/" + message
	p.Verified = r.Now().UTC().Format(time.RFC3339Nano)
	p.LastError = ""
	p.NextAttempt = ""
	return nil
}
func agentDeliveredBody(content, body string) bool {
	marker, ok := strings.CutPrefix(content, body+"\n\n-# recovery:")
	return ok && len(marker) == 30 && strings.Trim(marker[:24], "0123456789abcdef") == "" && marker[24:] == ":log:0"
}
func (r *Runner) agentResultPhase(ctx context.Context, h *Health) {
	work, cancel := context.WithTimeout(ctx, agentResultBudget)
	defer cancel()
	r.observeAgentResults(work, h)
	ids, err := r.agentResultSlice("records", 16)
	if err != nil {
		h.Errors = append(h.Errors, "agent_result_queue_deferred")
		return
	}
	for _, id := range ids {
		if work.Err() != nil {
			return
		}
		var p AgentResultRecord
		if err := readJSON(r.agentResultPath(id), 1<<20, &p); err != nil || p.Domain != agentResultDomain || p.ID != id || agentResultID(p.Event) != id || validateAgentEvent(p.Event) != nil || p.ParentReceived == "" {
			h.Errors = append(h.Errors, "agent_result_receipt_deferred")
			continue
		}
		if r.agentResultCursor("records", id) != nil {
			h.Errors = append(h.Errors, "agent_result_cursor_deferred")
			return
		}
		if p.Verified != "" {
			if r.archiveAgentFile("records", "verified", id) != nil {
				h.Errors = append(h.Errors, "agent_result_archive_deferred")
			}
			continue
		}
		if p.Attempts >= agentResultAttempts || p.LastError == "owner_question_scope_invalid" {
			continue
		}
		if p.NextAttempt != "" {
			at, err := time.Parse(time.RFC3339Nano, p.NextAttempt)
			if err != nil || r.Now().Before(at) {
				continue
			}
		}
		p.Attempts++
		p.LastError = "delivery_unverified"
		p.NextAttempt = r.Now().Add(time.Duration(1<<uint(p.Attempts-1)) * time.Minute).UTC().Format(time.RFC3339Nano)
		// Persist the attempt before any external operation. An interrupted parent
		// leaves the same delivery ID and a bounded, pending retry.
		if publish(r.agentResultPath(id), p) != nil {
			h.Errors = append(h.Errors, "agent_result_attempt_deferred")
			return
		}
		if err := r.deliverAgentResult(work, &p); err != nil {
			h.Errors = append(h.Errors, "agent_result_delivery_deferred")
			if errors.Is(err, errAgentOwnerQuestionScope) {
				p.LastError, p.NextAttempt = "owner_question_scope_invalid", ""
			}
		}
		if publish(r.agentResultPath(id), p) != nil {
			h.Errors = append(h.Errors, "agent_result_commit_deferred")
		} else if p.Verified != "" {
			if r.archiveAgentFile("records", "verified", id) != nil {
				h.Errors = append(h.Errors, "agent_result_archive_deferred")
			}
		}
		return // At most one ordinary delivery attempt per periodic tick.
	}
}
func (r *Runner) resumeAgentResult(id string) error {
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("agent result identity invalid")
	}
	var p AgentResultRecord
	if readJSON(r.agentResultPath(id), 1<<20, &p) != nil || p.ID != id || p.Domain != agentResultDomain || agentResultID(p.Event) != id {
		return errors.New("agent result unavailable")
	}
	if p.Verified != "" {
		return nil
	}
	p.Attempts = 0
	p.NextAttempt = ""
	p.LastError = ""
	return publish(r.agentResultPath(id), p)
}

func agentJobOutcome(job map[string]json.RawMessage) (string, string) {
	text := func(key string) string { var v string; _ = json.Unmarshal(job[key], &v); return v }
	status := text("status")
	failure := text("last_failure_kind")
	switch status {
	case "succeeded":
		line := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(text("last_message"), "\ufeff")), "\n", 2)[0]
		if strings.TrimSpace(line) == "状態: 完了" {
			return "completed", "completed"
		}
		return "stopped", "work_incomplete"
	case "cancelled", "canceled":
		return "cancelled", "cancelled"
	case "lost", "missing":
		return "lost", "result_missing"
	case "failed":
		switch failure {
		case "policy":
			return "rejected", "policy_rejected"
		case "timeout":
			return "timeout", "timeout"
		case "capacity", "quota":
			return "capacity_wait", "capacity"
		case "auth":
			return "start_failed", "authentication"
		}
		return "failed", "execution_failed"
	case "queued":
		var wait []json.RawMessage
		_ = json.Unmarshal(job["work_wait_on"], &wait)
		if len(wait) > 0 {
			return "dependency_wait", "dependency"
		}
		if failure == "capacity" || failure == "quota" || failure == "rate_limit" {
			return "capacity_wait", "capacity"
		}
	}
	return "", ""
}
func (r *Runner) agentWatchSnapshot(parent context.Context, e AgentResultEvent) (string, string, string, []AgentDeferredItem, bool, error) {
	root := r.JobsPath
	if root == "" {
		root = "D:/CC/Tools/codex-jobs/jobs"
	}
	path := filepath.Join(root, e.Child, "job.json")
	guard := r.AgentResultGuard
	if guard == nil {
		guard = historyReadGuard
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	if !guard(ctx, path) {
		return "", "", "", nil, false, errors.New("agent result source protected")
	}
	var job map[string]json.RawMessage
	if readJSON(path, 1<<20, &job) != nil {
		return "", "", "", nil, false, errors.New("agent result source unavailable")
	}
	var id string
	_ = json.Unmarshal(job["id"], &id)
	if id != e.Child {
		return "", "", "", nil, false, errors.New("agent result source differs")
	}
	var items []AgentDeferredItem
	badItems := false
	if raw, ok := job["item_deferred"]; ok {
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil {
			badItems = true
		} else {
			if len(rows) > 32 {
				badItems = true
				rows = rows[:32]
			}
			for _, row := range rows {
				var item AgentDeferredItem
				if json.Unmarshal(row, &item) != nil || !conversationID.MatchString(item.Transition) || validateAgentEvent(agentDeferredEvent(e, item)) != nil || item.OwnerRequired && item.OwnerQuestion == (AgentOwnerQuestion{}) {
					badItems = true
					continue
				}
				items = append(items, item)
			}
		}
	}
	state, reason := agentJobOutcome(job)
	if state == "" {
		return "", state, reason, items, badItems, nil
	}
	// Only explicit state fields determine the transition. Body text is neither
	// rejection evidence nor public report material.
	if state == "capacity_wait" || state == "dependency_wait" {
		// Scheduler retry clocks/counters do not end one continuous wait.
		return canonicalHash([]string{state, reason}), state, reason, items, badItems, nil
	}
	signature := canonicalHash([]any{state, reason, job["status"], job["finished_at"], job["attempt"], job["attempts"], job["work_wait_on"], job["capacity_retry_at"]})
	return signature, state, reason, items, badItems, nil
}
func (r *Runner) agentWatchPath(id string) string {
	closed := filepath.Join(r.Config.Home, "agent-results", "closed-watches", id+".json")
	if _, err := os.Stat(closed); err == nil {
		return closed
	}
	return filepath.Join(r.Config.Home, "agent-results", "watches", id+".json")
}
func agentWatchID(e AgentResultEvent) string {
	return canonicalHash([]any{e.RequestID, e.Todo, e.Parent, e.Child, e.Attempt})
}
func (r *Runner) watchAgentResult(e AgentResultEvent) error {
	if err := validateAgentEvent(e); err != nil {
		return err
	}
	if e.Source != "codex_jobs" || !validJob(e.Child) {
		return errors.New("external watch identity invalid")
	}
	path := r.agentWatchPath(agentWatchID(e))
	var prior agentResultWatch
	if err := readJSON(path, 1<<20, &prior); err == nil {
		if prior.Domain != agentResultDomain || prior.Event != e {
			return errors.New("agent watch retry differs")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errors.New("agent watch unavailable")
	}
	sig, state, _, items, _, err := r.agentWatchSnapshot(context.Background(), e)
	if err != nil {
		return err
	}
	if state != "" {
		return errors.New("register an active external job before its next transition")
	}
	w := agentResultWatch{Domain: agentResultDomain, Event: e, Signature: sig, Deadline: r.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), StableWaitSignature: true}
	for _, item := range items {
		w.BaselineItems = append(w.BaselineItems, agentDeferredEvent(e, item).Transition)
	}
	return publish(path, w)
}
func (r *Runner) observeAgentResults(ctx context.Context, h *Health) {
	ids, err := r.agentResultSlice("watches", 4)
	if err != nil {
		h.Errors = append(h.Errors, "agent_watch_queue_deferred")
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		path := filepath.Join(r.Config.Home, "agent-results", "watches", id+".json")
		var w agentResultWatch
		if readJSON(path, 1<<20, &w) != nil || w.Domain != agentResultDomain || validateAgentEvent(w.Event) != nil || agentWatchID(w.Event) != id {
			h.Errors = append(h.Errors, "agent_watch_receipt_deferred")
			continue
		}
		if r.agentResultCursor("watches", id) != nil {
			return
		}
		if w.Closed {
			if r.archiveAgentFile("watches", "closed-watches", id) != nil {
				h.Errors = append(h.Errors, "agent_watch_archive_deferred")
			}
			continue
		}
		deadline, deadlineErr := time.Parse(time.RFC3339Nano, w.Deadline)
		if deadlineErr != nil {
			h.Errors = append(h.Errors, "agent_watch_deadline_deferred")
			continue
		}
		sig, state, reason, items, badItems, err := r.agentWatchSnapshot(ctx, w.Event)
		if badItems {
			h.Errors = append(h.Errors, "agent_item_progress_invalid")
			notice := w.Event
			notice.Transition, notice.State, notice.Reason = "invalid-item-progress", "unverified", "item_progress_invalid"
			notice.Remaining, notice.Next = "unverified", "review_result"
			notice.OwnerRequired, notice.OwnerReply, notice.OwnerQuestion = false, false, AgentOwnerQuestion{}
			if _, noticeErr := r.recordAgentResultWithin(ctx, notice); noticeErr != nil {
				h.Errors = append(h.Errors, "agent_item_diagnostic_deferred")
			}
		}
		unreadable := err != nil
		if unreadable {
			h.Errors = append(h.Errors, "agent_watch_source_deferred")
			if ctx.Err() != nil {
				return
			}
			// A refusal to observe is not evidence that the child failed.
			sig, state, reason = canonicalHash("observation_unavailable"), "unverified", "observation_unavailable"
		}
		if !unreadable && !w.StableWaitSignature {
			// The signature scheme changing is not evidence of a new stop.
			if w.Signature != "" && (state == "capacity_wait" || state == "dependency_wait") {
				sameOrUnknown := true
				if len(w.Signature) >= 32 {
					prior := w.Event
					prior.Transition = fmt.Sprintf("observed-%d-%s", w.Generation, w.Signature[:32])
					priorID := agentResultID(prior)
					var saved AgentResultRecord
					if readJSON(r.agentResultPath(priorID), 1<<20, &saved) == nil && saved.ID == priorID && saved.Domain == agentResultDomain && agentResultID(saved.Event) == priorID {
						sameOrUnknown = saved.Event.State == state && saved.Event.Reason == reason
					}
				}
				if sameOrUnknown {
					w.Signature = sig
				}
			}
			w.StableWaitSignature = true
			if publish(path, w) != nil {
				h.Errors = append(h.Errors, "agent_watch_baseline_deferred")
				continue
			}
		}
		for _, item := range items {
			if ctx.Err() != nil {
				return
			}
			e := agentDeferredEvent(w.Event, item)
			baseline := false
			for _, prior := range w.BaselineItems {
				if e.Transition == prior {
					baseline = true
					break
				}
			}
			if baseline {
				continue
			}
			if _, itemErr := r.recordAgentResultWithin(ctx, e); itemErr != nil {
				h.Errors = append(h.Errors, "agent_item_return_deferred")
			}
		}
		expired := !w.WaitReported && !r.Now().Before(deadline) && (state == "" || state == "unverified" || state == "dependency_wait" || state == "capacity_wait")
		if expired {
			sig, state, reason = canonicalHash("result_wait_limit"), "unverified", "result_wait_limit"
		}
		// Observation failures and the wait deadline are parent notices, not
		// transitions of the worker itself. Never replace its saved signature.
		if !expired && (unreadable && w.ObservationReported || !unreadable && sig == w.Signature) {
			continue
		}
		if !expired && !unreadable {
			w.Generation++
		}
		if state != "" {
			e := w.Event
			e.Transition = fmt.Sprintf("observed-%d-%s", w.Generation, sig[:32])
			if expired {
				e.Transition = "result-wait-limit"
			} else if unreadable {
				e.Transition = fmt.Sprintf("observation-%d-%s", w.Generation, canonicalHash(w.Signature)[:24])
			}
			e.State = state
			e.Reason = reason
			e.Remaining = "unverified"
			e.Next = "review_result"
			e.OwnerRequired = false
			e.OwnerReply = false
			e.OwnerQuestion = AgentOwnerQuestion{}
			if state == "completed" {
				e.Remaining = "none"
				e.Next = "none"
			}
			if state == "dependency_wait" {
				e.Next = "wait_dependency"
			}
			if state == "capacity_wait" {
				e.Next = "retry"
			}
			if _, err := r.recordAgentResultWithin(ctx, e); err != nil {
				h.Errors = append(h.Errors, "agent_watch_return_deferred")
				continue
			}
			if expired {
				w.WaitReported = true
			} else if unreadable {
				w.ObservationReported = true
			}
			w.Closed = state != "dependency_wait" && state != "capacity_wait" && state != "unverified"
		}
		if !expired && !unreadable {
			w.Signature = sig
			w.ObservationReported = false
		}
		if publish(path, w) != nil {
			h.Errors = append(h.Errors, "agent_watch_commit_deferred")
		} else if w.Closed {
			if r.archiveAgentFile("watches", "closed-watches", id) != nil {
				h.Errors = append(h.Errors, "agent_watch_archive_deferred")
			}
		}
	}
}
