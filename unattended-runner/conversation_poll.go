package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const conversationPollInterval = time.Minute
const conversationPollParallelism = 4
const conversationPollTimeout = 6 * time.Second
const conversationPollStageBudget = 20 * time.Second

type conversationPollResult struct {
	id       string
	question ConversationQuestion
	proof    *conversationProof
	err      error
	checked  string
}

type conversationPollCheck struct {
	CheckedAt  string `json:"checked_at"`
	OK         bool   `json:"verified"`
	DeliveryAt string `json:"delivery_checked_at,omitempty"`
}

type conversationPollSnapshot struct {
	Domain  string                           `json:"domain"`
	Updated string                           `json:"updated_at"`
	Checks  map[string]conversationPollCheck `json:"checks"`
}

// The canonical wait can be committed before the runner's index save fails.
// Read only scheduling metadata once; delivery still guards the selected task
// and validates its exact live owner and wait under the normal mutation path.
func (r *Runner) conversationCanonicalWaits() (map[uint64]*TodoRow, error) {
	var db struct {
		Items []struct {
			ID      uint64          `json:"id"`
			Project string          `json:"project"`
			Wait    json.RawMessage `json:"owner_question_wait"`
		} `json:"items"`
	}
	if err := readJSON(r.TodoPath, maxTodo, &db); err != nil {
		return nil, err
	}
	rows := make(map[uint64]*TodoRow)
	for _, row := range db.Items {
		if _, duplicate := rows[row.ID]; duplicate {
			return nil, errors.New("conversation duplicate task")
		}
		rows[row.ID] = &TodoRow{ID: row.ID, Project: row.Project, QuestionWait: row.Wait}
	}
	return rows, nil
}

func (r *Runner) conversationPollSnapshot() (conversationPollSnapshot, error) {
	snapshot := conversationPollSnapshot{Domain: "discord-conversation-poll-v1", Checks: map[string]conversationPollCheck{}}
	err := readJSON(filepath.Join(r.Config.Home, "conversation-poll-health.json"), maxState, &snapshot)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil || snapshot.Domain != "discord-conversation-poll-v1" || snapshot.Checks == nil {
		return conversationPollSnapshot{Domain: "discord-conversation-poll-v1", Checks: map[string]conversationPollCheck{}}, errors.New("poll history unavailable")
	}
	return snapshot, nil
}

// Each waiting question gets a read-only turn on each existing minute tick.
// Only read-only sender calls run concurrently; captures, ToDo changes and all
// publication still run sequentially under the original runner lock.
func (r *Runner) processConversations(ctx context.Context, health *Health) {
	index, err := r.loadConversations()
	if err != nil {
		health.Errors = append(health.Errors, "conversation_index_deferred")
		return
	}
	snapshot, snapshotErr := r.conversationPollSnapshot()
	if snapshotErr != nil {
		health.Errors = append(health.Errors, "conversation_poll_history_deferred")
	}
	var waiting []ConversationQuestion
	var canonicalWaits map[uint64]*TodoRow
	canonicalRead := false
	for _, id := range orderedConversationKeys(index) {
		q := index.Questions[id]
		if q.Phase == "waiting" || ((q.Wait || q.ExplicitWait != nil) && q.Phase == "answered") {
			waiting = append(waiting, q)
		} else if !q.Wait && q.Phase == "posted" {
			if !canonicalRead {
				canonicalRead = true
				canonicalWaits, err = r.conversationCanonicalWaits()
				if err != nil {
					health.Errors = append(health.Errors, "conversation_wait_readback_deferred")
				}
			}
			if row := canonicalWaits[q.Todo]; row != nil && row.Project == q.Project && r.conversationExactWait(row.QuestionWait, q) {
				var wait struct {
					Origin json.RawMessage `json:"origin"`
				}
				_ = json.Unmarshal(row.QuestionWait, &wait)
				q.ExplicitWait = &conversationExplicitWaitReceipt{WaitSHA: canonicalHash(row.QuestionWait), OriginSHA: canonicalHash(wait.Origin)}
				waiting = append(waiting, q)
			}
		}
	}
	// Oldest checked first if an API outage consumes this finite tick. Healthy
	// later questions do not repeatedly lose their turn to the same failed one.
	sort.SliceStable(waiting, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, snapshot.Checks[waiting[i].RequestID].CheckedAt)
		b, _ := time.Parse(time.RFC3339Nano, snapshot.Checks[waiting[j].RequestID].CheckedAt)
		return a.Before(b)
	})
	health.ConversationWaiting = len(waiting)
	health.ConversationPollSeconds = int(conversationPollInterval / time.Second)
	results := make([]conversationPollResult, len(waiting))
	work := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < min(conversationPollParallelism, len(waiting)); worker++ {
		workers.Go(func() {
			for i := range work {
				q := waiting[i]
				result := conversationPollResult{id: q.RequestID, question: q}
				if ctx.Err() != nil {
					result.err = ctx.Err()
					results[i] = result
					continue
				}
				poll, cancel := context.WithTimeout(ctx, conversationPollTimeout)
				result.proof, result.err = r.conversationProof(poll, q)
				cancel()
				result.checked = r.Now().UTC().Format(time.RFC3339Nano)
				results[i] = result
			}
		})
	}
	for i := range waiting {
		work <- i
	}
	close(work)
	workers.Wait()
	// Newly received/edited text is delivered before old application audits.
	// API-read fairness and delivery fairness have separate clocks.
	sort.SliceStable(results, func(i, j int) bool {
		pending := func(result conversationPollResult) bool {
			if result.err != nil || result.proof == nil {
				return false
			}
			for _, reply := range result.proof.Replies {
				if r.conversationReplyNeedsDelivery(result.question, result.proof, reply) {
					return true
				}
			}
			return false
		}
		a, b := pending(results[i]), pending(results[j])
		if a != b {
			return a
		}
		x, _ := time.Parse(time.RFC3339Nano, snapshot.Checks[results[i].id].DeliveryAt)
		y, _ := time.Parse(time.RFC3339Nano, snapshot.Checks[results[j].id].DeliveryAt)
		return x.Before(y)
	})
	checked := make(map[string]bool, len(waiting))
	for _, result := range results {
		checked[result.id] = true
		if result.checked == "" {
			health.ConversationPollDeferred++
			continue
		}
		health.ConversationChecked++
		q := result.question
		check := snapshot.Checks[result.id]
		check.CheckedAt, check.OK = result.checked, result.err == nil
		snapshot.Checks[result.id] = check
		if result.err != nil {
			health.ConversationPollDeferred++
			continue
		}
		// Captures and delivery remain sequential. An exhausted delivery budget
		// is visible and retried from the same question and source version.
		if ctx.Err() == nil {
			r.processConversationReplies(ctx, index, result.id, q, result.proof, health)
			check.DeliveryAt = r.Now().UTC().Format(time.RFC3339Nano)
			snapshot.Checks[result.id] = check
		} else {
			health.Errors = append(health.Errors, "conversation_delivery_budget_deferred")
		}
	}
	if len(waiting) != 0 {
		snapshot.Updated = r.Now().UTC().Format(time.RFC3339Nano)
		if publish(filepath.Join(r.Config.Home, "conversation-poll-health.json"), snapshot) != nil {
			health.Errors = append(health.Errors, "conversation_poll_record_deferred")
		}
	}
	if health.ConversationPollDeferred != 0 {
		health.Errors = append(health.Errors, "conversation_poll_deferred")
	}
	if ctx.Err() == nil {
		r.processConversationMaintenance(ctx, health, checked)
	}
}
