package main

import (
	"encoding/json"
	"math"
	"path/filepath"
	"regexp"
	"time"
)

// These are canonical Task records written under todo.lock by normal start or
// scheduler preparation/link. They establish no rights without the independently
// signed original purpose and the complete release/answer proof.
type taskQuestionControl struct {
	Status     string   `json:"status"`
	Generation uint64   `json:"resume_generation"`
	Jobs       []string `json:"jobs"`
	Worker     struct {
		Job        *string `json:"job"`
		Generation uint64  `json:"generation"`
	} `json:"worker_receipt"`
	Scheduler struct {
		Job    *string `json:"job"`
		Key    *string `json:"key"`
		Last   *string `json:"last_job"`
		Phase  string  `json:"phase"`
		Resume bool    `json:"resume"`
	} `json:"scheduler"`
}

type taskQuestionMetadata struct {
	ID      uint64          `json:"id"`
	Project string          `json:"project"`
	Root    string          `json:"root"`
	Title   string          `json:"title"`
	Created float64         `json:"created_s"`
	Spec    json.RawMessage `json:"spec"`
}

func taskQuestionMetadataOf(s taskQuestionState) taskQuestionMetadata {
	return taskQuestionMetadata{s.ID, s.Project, s.Root, s.Title, s.Created, s.Spec}
}

func taskQuestionString(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func (r taskQuestionRegistry) continuationCurrent(current taskQuestionState, release taskQuestionRelease, applied time.Time) error {
	var row struct {
		Transitions []json.RawMessage `json:"owner_question_continuation_receipts"`
	}
	var originalGeneration uint64
	if json.Unmarshal(current.current, &row) != nil || len(row.Transitions) > 256 || json.Unmarshal(release.Before.Generation, &originalGeneration) != nil || originalGeneration == math.MaxUint64 {
		return errTaskQuestion
	}
	anchor := taskQuestionControl{Status: "todo", Generation: originalGeneration + 1, Jobs: release.Before.Jobs}
	anchor.Worker.Generation = anchor.Generation
	anchor.Scheduler.Phase, anchor.Scheduler.Resume, anchor.Scheduler.Last = "resumed", true, &release.Before.Worker.Job
	lastAt := applied
	for _, raw := range row.Transitions {
		var edge struct {
			Domain    string               `json:"domain"`
			Request   string               `json:"request_id"`
			Question  string               `json:"question_receipt_sha256"`
			Answer    string               `json:"answer_file_sha256"`
			Metadata  taskQuestionMetadata `json:"metadata"`
			Operation string               `json:"operation"`
			Before    taskQuestionControl  `json:"before"`
			After     taskQuestionControl  `json:"after"`
			At        time.Time            `json:"recorded_at"`
		}
		if historyStrictJSON(raw, &edge) != nil {
			return errTaskQuestion
		}
		if edge.Request != release.Answer.RequestID {
			continue
		}
		if edge.Domain != "todo-owner-question-continuation-v1" || edge.Question != release.QuestionSHA || edge.Answer != release.AnswerSHA ||
			canonicalHash(edge.Metadata) != canonicalHash(taskQuestionMetadataOf(release.Before)) || canonicalHash(edge.Before) != canonicalHash(anchor) ||
			edge.At.Before(lastAt) || edge.At.After(r.Now()) || r.continuationEdge(edge.Operation, edge.Before, edge.After, current.ID, applied, edge.At) != nil {
			return errTaskQuestion
		}
		anchor, lastAt = edge.After, edge.At
	}
	// Compare exactly the current native tuple; arbitrary generation/worker/key
	// edits with no contiguous canonical transition remain ineligible.
	var live taskQuestionControl
	if json.Unmarshal(current.current, &live) != nil || canonicalHash(live) != canonicalHash(anchor) {
		return errTaskQuestion
	}
	return nil
}

func (r taskQuestionRegistry) continuationEdge(operation string, before, after taskQuestionControl, todo uint64, applied, recorded time.Time) error {
	if before.Generation == 0 || before.Worker.Generation != before.Generation || after.Worker.Generation != after.Generation ||
		(before.Status != "todo" && before.Status != "doing") || !before.Scheduler.Resume || !after.Scheduler.Resume {
		return errTaskQuestion
	}
	expected := before
	switch operation {
	case "scheduler_prepare":
		if before.Status != "todo" || before.Scheduler.Job != nil || before.Worker.Job != nil || before.Scheduler.Key != nil || before.Scheduler.Phase != "resumed" ||
			after.Scheduler.Key == nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(*after.Scheduler.Key) {
			return errTaskQuestion
		}
		expected.Scheduler.Key, expected.Scheduler.Phase = after.Scheduler.Key, "preparing"
	case "scheduler_prepared":
		if before.Status != "todo" || before.Scheduler.Job != nil || before.Worker.Job != nil || before.Scheduler.Key == nil || before.Scheduler.Phase != "preparing" {
			return errTaskQuestion
		}
		expected.Scheduler.Phase = "prepared"
	case "canonical_start", "scheduler_link":
		job := taskQuestionString(after.Scheduler.Job)
		if job == "" || !validJob(job) || len(job) > 200 || job == taskQuestionString(before.Scheduler.Job) {
			return errTaskQuestion
		}
		expected.Status, expected.Scheduler.Job, expected.Scheduler.Phase = "doing", after.Scheduler.Job, "running"
		expected.Jobs = append(append([]string{}, before.Jobs...), job)
		for _, old := range before.Jobs {
			if old == job {
				return errTaskQuestion
			}
		}
		if operation == "canonical_start" {
			if before.Generation == math.MaxUint64 {
				return errTaskQuestion
			}
			expected.Generation++
			expected.Worker.Job, expected.Worker.Generation, expected.Scheduler.Key = after.Scheduler.Job, expected.Generation, nil
			if before.Scheduler.Job != nil {
				expected.Scheduler.Last = before.Scheduler.Job
			}
		} else if before.Status != "todo" || (before.Scheduler.Phase != "preparing" && before.Scheduler.Phase != "prepared") || before.Scheduler.Job != nil || before.Scheduler.Key == nil {
			return errTaskQuestion
		}
		if r.continuationJob(job, todo, taskQuestionString(expected.Scheduler.Key), applied, recorded) != nil {
			return errTaskQuestion
		}
	default:
		return errTaskQuestion
	}
	if canonicalHash(expected) != canonicalHash(after) {
		return errTaskQuestion
	}
	return nil
}

func (r taskQuestionRegistry) continuationJob(id string, todo uint64, key string, applied, recorded time.Time) error {
	b, err := r.Read(filepath.Join(filepath.FromSlash(r.Jobs), id, "job.json"), 1<<20)
	var job struct {
		ID, Status string
		Todo       uint64          `json:"todo_id"`
		Key        string          `json:"launch_key"`
		Created    json.RawMessage `json:"created_at"`
	}
	var stop nativeQuestionOriginJob
	var raw any
	if err != nil || historyStrictJSON(b, &raw) != nil || json.Unmarshal(b, &job) != nil || json.Unmarshal(b, &stop) != nil || job.ID != id || job.Todo != todo || nativeQuestionJobStopped(stop) || (key != "" && job.Key != key) {
		return errTaskQuestion
	}
	switch job.Status {
	case "queued", "running", "succeeded", "failed", "lost", "cancelled", "canceled":
	default:
		return errTaskQuestion
	}
	var date string
	var created time.Time
	if json.Unmarshal(job.Created, &date) == nil {
		created, err = time.Parse(time.RFC3339Nano, date)
	} else {
		var seconds float64
		err = json.Unmarshal(job.Created, &seconds)
		if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 && seconds < float64(math.MaxInt64)/1e9 {
			created = time.Unix(0, int64(seconds*1e9))
		}
	}
	if err != nil || created.IsZero() || created.Before(applied) || created.After(recorded) {
		return errTaskQuestion
	}
	return nil
}
