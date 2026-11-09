package main

import (
	"encoding/json"
	"math"
	"path/filepath"
	"time"
)

const taskQuestionReleaseDomain = "owner-task-question-release-v1"

// Written after preserved canonical delivery, before the canonical answer call.
// This immutable intent is not retirement evidence on its own: the canonical
// answer, preserved delivery, terminal job and exact successor must also match.
type taskQuestionRelease struct {
	Domain       string                    `json:"domain"`
	Registration string                    `json:"registration_sha256"`
	Request      string                    `json:"request_id"`
	QuestionSHA  string                    `json:"question_receipt_sha256"`
	Before       taskQuestionState         `json:"original_metadata"`
	WaitSHA      string                    `json:"wait_sha256"`
	DeliverySHA  string                    `json:"preserved_delivery_sha256"`
	Answer       conversationAnswerReceipt `json:"answer"`
	AnswerSHA    string                    `json:"answer_file_sha256"`
	PriorResult  json.RawMessage           `json:"prior_result"`
	Created      time.Time                 `json:"created_at"`
}

func taskQuestionReleaseName(request, answerSHA string) string {
	return bytesHash([]byte(request)) + "-release-" + answerSHA + ".json"
}

func (r taskQuestionRegistry) recordRelease(row *TodoRow, q ConversationQuestion, answer conversationAnswerReceipt, b []byte) error {
	var registration taskQuestionReceipt
	if r.readSigned(taskQuestionName(q.RequestID), &registration) != nil {
		return errTaskQuestion
	}
	var before taskQuestionState
	if json.Unmarshal(row.conversationState, &before) != nil {
		return errTaskQuestion
	}
	before.Root = row.conversationRoot
	if canonicalHash(before) != registration.State || !conversationWaitOwnerUnchanged(row) || !conversationDeliveryRecordedWithMode(row, q, answer, b, true) {
		return errTaskQuestion
	}
	var raw struct {
		Results map[string]json.RawMessage `json:"processed_results"`
	}
	if json.Unmarshal(row.conversationState, &raw) != nil {
		return errTaskQuestion
	}
	witness := taskQuestionRelease{Domain: taskQuestionReleaseDomain, Registration: canonicalHash(registration), Request: q.RequestID, QuestionSHA: q.ReceiptSHA,
		Before: before, WaitSHA: canonicalHash(json.RawMessage(row.QuestionWait)), Answer: answer, AnswerSHA: bytesHash(b), PriorResult: raw.Results[before.Worker.Job], Created: r.Now()}
	for _, delivery := range row.QuestionDeliveries {
		var v struct {
			Preserved bool                      `json:"preserved_wait"`
			Answer    conversationAnswerReceipt `json:"answer"`
			SHA       string                    `json:"answer_file_sha256"`
		}
		if json.Unmarshal(delivery, &v) == nil && v.Preserved && v.Answer == answer && v.SHA == witness.AnswerSHA {
			if witness.DeliverySHA != "" {
				return errTaskQuestion
			}
			witness.DeliverySHA = canonicalHash(delivery)
		}
	}
	if witness.DeliverySHA == "" {
		return errTaskQuestion
	}
	name := taskQuestionReleaseName(q.RequestID, witness.AnswerSHA)
	var old taskQuestionRelease
	if r.readSigned(name, &old) == nil {
		witness.Created = old.Created
		if canonicalHash(witness) != canonicalHash(old) {
			return errTaskQuestion
		}
		return nil
	}
	return r.writeSigned(name, witness)
}

// Return only an authorization projection. Never write the old owner tuple to
// the task or stop the canonical CLI from retiring a finished owner. The caller
// separately binds the observed current state before using this projection.
func (r taskQuestionRegistry) questionState(e taskQuestionEvidence, q *ConversationQuestion, registration *taskQuestionReceipt, current taskQuestionState) (taskQuestionState, error) {
	if q == nil || current.Worker.Job == e.Session {
		return current, nil
	}
	if registration == nil || !q.Wait || q.RequestID != e.Request || q.Todo != current.ID || q.Project != current.Project {
		return current, errTaskQuestion
	}
	var row struct {
		Status    string
		Wait      json.RawMessage `json:"owner_question_wait"`
		Worker    json.RawMessage `json:"worker_receipt"`
		Scheduler struct {
			Job    json.RawMessage
			Key    json.RawMessage
			Last   string `json:"last_job"`
			Phase  string
			Resume bool
		}
		Answers    []json.RawMessage          `json:"owner_question_answer_receipts"`
		Deliveries []json.RawMessage          `json:"owner_question_delivery_receipts"`
		Results    map[string]json.RawMessage `json:"processed_results"`
		WaitOn     []uint64                   `json:"wait_on"`
		WorkOn     []uint64                   `json:"work_owner_wait_on"`
	}
	null := func(b json.RawMessage) bool { return len(b) == 0 || string(b) == "null" }
	if json.Unmarshal(current.current, &row) != nil || row.Status != "todo" || !null(row.Wait) || !null(row.Scheduler.Job) || !null(row.Scheduler.Key) ||
		row.Scheduler.Phase != "resumed" || !row.Scheduler.Resume || row.Scheduler.Last != e.Session || len(row.WaitOn) != 0 || len(row.WorkOn) != 0 {
		return current, errTaskQuestion
	}
	var matched *taskQuestionRelease
	for _, raw := range row.Answers {
		var record struct {
			Domain string
			Wait   json.RawMessage
			Answer conversationAnswerReceipt
			SHA    string    `json:"answer_file_sha256"`
			At     time.Time `json:"applied_at"`
		}
		if json.Unmarshal(raw, &record) != nil {
			return current, errTaskQuestion
		}
		if record.Answer.RequestID != q.SenderID {
			continue
		}
		if matched != nil || record.Domain != "todo-owner-question-answer-receipt-v1" || !shaPattern.MatchString(record.SHA) {
			return current, errTaskQuestion
		}
		var witness taskQuestionRelease
		if r.readSigned(taskQuestionReleaseName(q.RequestID, record.SHA), &witness) != nil || witness.Domain != taskQuestionReleaseDomain ||
			witness.Registration != canonicalHash(*registration) || witness.Request != q.RequestID || witness.QuestionSHA != q.ReceiptSHA ||
			witness.Answer != record.Answer || witness.AnswerSHA != record.SHA || witness.WaitSHA != canonicalHash(record.Wait) || canonicalHash(witness.Before) != registration.State ||
			witness.Created.IsZero() || record.At.Before(witness.Created) || record.At.After(r.Now()) {
			return current, errTaskQuestion
		}
		answerBytes, _ := json.MarshalIndent(witness.Answer, "", "  ")
		acquired, timeErr := time.Parse(time.RFC3339Nano, witness.Answer.Acquired)
		if timeErr != nil || witness.Created.Before(acquired) {
			return current, errTaskQuestion
		}
		if bytesHash(answerBytes) != record.SHA || witness.Answer.Author != e.Scope.Owner || witness.Answer.Source != "discord-api-v10" || witness.Answer.Todo != fmtUint(q.Todo) ||
			witness.Answer.Thread != q.Thread || witness.Answer.Question != q.Message || witness.Answer.Fingerprint != q.Fingerprint || witness.Answer.QuestionSHA != q.ReceiptSHA {
			return current, errTaskQuestion
		}
		before := witness.Before
		var origin struct{ Origin json.RawMessage }
		var owner struct {
			Generation json.RawMessage `json:"resume_generation"`
			Worker     struct {
				Job        string
				Generation uint64
			} `json:"worker_receipt"`
			Job string
			Key json.RawMessage
		}
		if json.Unmarshal(record.Wait, &origin) != nil || json.Unmarshal(origin.Origin, &owner) != nil || owner.Job != e.Session ||
			canonicalHash(owner.Generation) != canonicalHash(before.Generation) || canonicalHash(owner.Worker) != canonicalHash(before.Worker) || canonicalHash(owner.Key) != canonicalHash(before.Scheduler.Key) {
			return current, errTaskQuestion
		}
		var generation uint64
		if json.Unmarshal(before.Generation, &generation) != nil || generation == 0 || generation == math.MaxUint64 || current.Worker.Generation != generation+1 ||
			string(current.Generation) != fmtUint(generation+1) || before.Worker.Job != e.Session || before.Scheduler.Job != e.Session || before.Worker.Generation != generation {
			return current, errTaskQuestion
		}
		worker, _ := json.Marshal(map[string]any{"job": nil, "generation": generation + 1})
		if canonicalHash(row.Worker) != canonicalHash(json.RawMessage(worker)) {
			return current, errTaskQuestion
		}
		preserved := 0
		for _, delivery := range row.Deliveries {
			if canonicalHash(delivery) == witness.DeliverySHA {
				var d struct {
					Domain    string
					Preserved bool `json:"preserved_wait"`
					Wait      json.RawMessage
					Recipient json.RawMessage
					Answer    conversationAnswerReceipt
					SHA       string    `json:"answer_file_sha256"`
					At        time.Time `json:"delivered_at"`
				}
				if json.Unmarshal(delivery, &d) != nil || d.Domain != "todo-owner-question-delivery-receipt-v1" || !d.Preserved || canonicalHash(d.Wait) != witness.WaitSHA ||
					canonicalHash(d.Recipient) != canonicalHash(origin.Origin) || d.Answer != record.Answer || d.SHA != record.SHA || d.At.IsZero() || d.At.Before(acquired) || d.At.After(witness.Created) {
					return current, errTaskQuestion
				}
				preserved++
			}
		}
		if preserved != 1 {
			return current, errTaskQuestion
		}
		expected := witness.PriorResult
		if null(expected) {
			expected, _ = json.Marshal(map[string]any{"generation": generation, "reason": "conversation_answer"})
		}
		var processed struct{ Generation uint64 }
		if json.Unmarshal(expected, &processed) != nil || processed.Generation != generation || canonicalHash(row.Results[e.Session]) != canonicalHash(expected) {
			return current, errTaskQuestion
		}
		if !generatedUUID.MatchString(e.Session) || r.Jobs == "" {
			return current, errTaskQuestion
		}
		jobBytes, err := r.Read(filepath.Join(filepath.FromSlash(r.Jobs), e.Session, "job.json"), 1<<20)
		var job struct {
			ID, Status string
			Todo       uint64   `json:"todo_id"`
			Related    []uint64 `json:"related_todo_ids"`
		}
		var stop nativeQuestionOriginJob
		var rawJob any
		if err != nil || historyStrictJSON(jobBytes, &rawJob) != nil || json.Unmarshal(jobBytes, &job) != nil || json.Unmarshal(jobBytes, &stop) != nil || job.ID != e.Session || nativeQuestionJobStopped(stop) {
			return current, errTaskQuestion
		}
		if job.Status != "succeeded" && job.Status != "failed" && job.Status != "lost" && job.Status != "cancelled" && job.Status != "canceled" {
			return current, errTaskQuestion
		}
		related := job.Todo == 0 || job.Todo == q.Todo
		for _, id := range job.Related {
			related = related || id == q.Todo
		}
		if !related {
			return current, errTaskQuestion
		}
		matched = &witness
	}
	if matched == nil {
		return current, errTaskQuestion
	}
	bound := current
	bound.Generation, bound.Worker, bound.Scheduler = matched.Before.Generation, matched.Before.Worker, matched.Before.Scheduler
	if canonicalHash(bound) != registration.State {
		return current, errTaskQuestion
	}
	// Validate the full question receipt using the original canonical owner tuple.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(current.current, &fields)
	fields["resume_generation"] = bound.Generation
	fields["worker_receipt"], _ = json.Marshal(map[string]any{"job": bound.Worker.Job, "generation": bound.Worker.Generation})
	scheduler := map[string]any{"job": bound.Scheduler.Job, "key": bound.Scheduler.Key}
	fields["scheduler"], _ = json.Marshal(scheduler)
	normalized, _ := json.Marshal(fields)
	var answerWait struct{ Wait json.RawMessage }
	for _, raw := range row.Answers {
		var a struct {
			Answer conversationAnswerReceipt
			Wait   json.RawMessage
		}
		_ = json.Unmarshal(raw, &a)
		if a.Answer == matched.Answer {
			answerWait.Wait = a.Wait
		}
	}
	if !taskQuestionWaitMatches(normalized, answerWait.Wait, *q) {
		return current, errTaskQuestion
	}
	bound.retired = true
	return bound, nil
}
