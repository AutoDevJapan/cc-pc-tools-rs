package main

import (
	"encoding/json"
	"time"
)

const schemaVersion = 1

// Only these projection fields are decoded. Titles, notes, prompts, account
// information, last_message, and paths are deliberately absent.
type todoStore struct {
	Items []todoItem `json:"items"`
}

type todoItem struct {
	ProductIdentity   json.RawMessage `json:"product_identity"`
	RepositoryID      json.RawMessage `json:"repository_id"`
	RepositoryOwnerID json.RawMessage `json:"repository_owner_id"`
	ID                int64           `json:"id"`
	Project           string          `json:"project"`
	OwnerProject      json.RawMessage `json:"owner_project"`
	Department        string          `json:"department"`
	Status            string          `json:"status"`
	Progress          json.RawMessage `json:"progress"`
	TaskType          json.RawMessage `json:"task_type"`
	Updated           json.RawMessage `json:"updated"`
	Jobs              json.RawMessage `json:"jobs"`
	WaitOn            []int64         `json:"wait_on"`
	WorkWaitOn        []int64         `json:"work_wait_on"`
	WaitReady         *readyWait      `json:"wait_ready"`
	Scheduler         struct {
		Job string `json:"job"`
	} `json:"scheduler"`
	Subwork json.RawMessage `json:"subwork"`
}

type readyWait struct {
	Condition string          `json:"condition"`
	NotBefore json.RawMessage `json:"not_before"`
}

type jobMeta struct {
	ProductIdentity   json.RawMessage `json:"product_identity"`
	RepositoryID      json.RawMessage `json:"repository_id"`
	RepositoryOwnerID json.RawMessage `json:"repository_owner_id"`
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	TaskType          json.RawMessage `json:"task_type"`
	Project           string          `json:"project"`
	OwnerProject      json.RawMessage `json:"owner_project"`
	Department        string          `json:"department"`
	TodoID            int64           `json:"todo_id"`
	RelatedTodoIDs    []int64         `json:"related_todo_ids"`
	WorkWaitOn        []int64         `json:"work_wait_on"`
	CreatedAt         string          `json:"created_at"`
	StartedAt         string          `json:"started_at"`
	FinishedAt        string          `json:"finished_at"`
	LastEventAt       string          `json:"last_event_at"`
	Activity          string          `json:"activity"`
	ContextPhase      string          `json:"context_phase"`
	LastFailureKind   string          `json:"last_failure_kind"`
	RetryNotBefore    json.RawMessage `json:"retry_not_before"`
	CancelRequested   bool            `json:"cancel_requested"`
	PID               uint32          `json:"pid"`
	PIDCreated        *uint64         `json:"pid_created"`
	CodexPID          uint32          `json:"codex_pid"`
	CodexPIDCreated   *uint64         `json:"codex_pid_created"`
	ParentJobID       string          `json:"parent_job_id"`
	Subwork           json.RawMessage `json:"subwork"`
	QueueWaitReason   *queueWait      `json:"queue_wait_reason"`
}

type queueWait struct {
	Code        string  `json:"code"`
	WaitOn      []int64 `json:"wait_on"`
	OwnerTodoID int64   `json:"owner_todo_id"`
	WaitReady   []struct {
		Wait *readyWait `json:"wait"`
	} `json:"wait_ready"`
}

type Clock struct {
	At         *string  `json:"at"`
	AgeSeconds *float64 `json:"age_seconds"`
	State      string   `json:"state"`
}

type Source struct {
	Status      string `json:"status"`
	AcquiredAt  string `json:"acquired_at"`
	Modified    Clock  `json:"modified"`
	BytesRead   int64  `json:"bytes_read"`
	RecordsRead int    `json:"records_read"`
	FailedReads int    `json:"failed_reads"`
	Complete    bool   `json:"complete"`
}

type WaitReason struct {
	Kind      string  `json:"kind"`
	Label     string  `json:"label,omitempty"`
	TodoIDs   []int64 `json:"todo_ids"`
	NotBefore *Clock  `json:"not_before,omitempty"`
}

type Job struct {
	ProductIdentity *productIdentity `json:"product_identity,omitempty"`
	// Only active-only observations certify the canonical predicate after all
	// resource/cancellation/task-wait checks. Diagnostic observations omit it.
	ProcessingVerified *bool        `json:"processing_verified,omitempty"`
	Key                string       `json:"job_key"`
	TodoIDs            []int64      `json:"todo_ids"`
	PublicLabel        string       `json:"public_label"`
	LabelKind          string       `json:"label_kind"`
	Department         string       `json:"department"`
	Priority           int          `json:"priority"`
	RecordedStatus     string       `json:"recorded_status"`
	Execution          string       `json:"execution"`
	WorkerPresence     string       `json:"worker_presence"`
	SupervisorPresence string       `json:"supervisor_presence"`
	Phase              string       `json:"phase"`
	TaskType           string       `json:"task_type"`
	WaitReasons        []WaitReason `json:"wait_reasons"`
	LastActivity       Clock        `json:"last_activity"`
	LastHeartbeat      Clock        `json:"last_heartbeat"`
	Source             Source       `json:"source"`
	rawID              string
	parentID           string
	independent        bool
	cancelRequested    bool
	resourceBlocked    bool
	created            time.Time
}

type Task struct {
	ProductIdentity *productIdentity `json:"product_identity,omitempty"`
	ID              int64            `json:"task_id"`
	DisplayName     string           `json:"display_name"`
	PublicLabel     string           `json:"public_label"`
	LabelKind       string           `json:"label_kind"`
	Department      string           `json:"department"`
	Priority        int              `json:"priority"`
	RecordedStatus  string           `json:"recorded_status"`
	Percent         *int             `json:"percent"`
	ProgressState   string           `json:"progress_state"`
	Execution       string           `json:"execution"`
	Phase           string           `json:"phase"`
	TaskType        string           `json:"task_type"`
	TaskTypeSource  string           `json:"task_type_source"`
	Detail          DetailRef        `json:"detail"`
	JobKeys         []string         `json:"job_keys"`
	WaitReasons     []WaitReason     `json:"wait_reasons"`
	RecordUpdated   Clock            `json:"record_updated"`
	LastActivity    Clock            `json:"last_activity"`
	ParentTodoID    *int64           `json:"parent_task_id"`
}

// This is a numeric local detail route, not a source URL, path, or raw title.
type DetailRef struct {
	Kind   string `json:"kind"`
	TaskID int64  `json:"task_id"`
}

type Snapshot struct {
	ProductBindingsConfigured bool              `json:"product_bindings_configured"`
	ProductBindingsOverridden *bool             `json:"product_bindings_overridden"`
	ProcessingContract        string            `json:"processing_contract,omitempty"`
	SchemaVersion             int               `json:"schema_version"`
	CapturedAt                string            `json:"captured_at"`
	HeartbeatAt               string            `json:"heartbeat_at"`
	Status                    string            `json:"status"`
	Consistency               string            `json:"consistency"`
	Sources                   map[string]Source `json:"sources"`
	Tasks                     []Task            `json:"tasks"`
	Jobs                      []Job             `json:"jobs"`
	TaskTotal                 int               `json:"task_total"`
	JobTotal                  int               `json:"job_total"`
	Truncated                 bool              `json:"truncated"`
	Issues                    []string          `json:"issues"`
}

type Options struct {
	TodoFile, JobsDir, PriorityFile string
	ResourceQueueDir                string
	IncludeDone                     bool
	ActiveOnly                      bool
	TaskIDs                         map[int64]bool
	MaxRows, MaxJobs                int
	TodoBytes, JobBytes, JobsBytes  int64
	StaleAfter, Deadline            time.Duration
}

func defaults() Options {
	return Options{
		TodoFile: "D:/CC/Tools/todo/todo.json", JobsDir: "D:/CC/Tools/codex-jobs/jobs",
		PriorityFile:     "D:/CC/Tools/scheduler/priorities.json",
		ResourceQueueDir: "D:/CC/Tools/civ6-live/queue",
		TaskIDs:          map[int64]bool{}, MaxRows: 1000, MaxJobs: 10000,
		TodoBytes: 64 << 20, JobBytes: 1 << 20, JobsBytes: 128 << 20,
		StaleAfter: 10 * time.Minute, Deadline: 20 * time.Second,
	}
}
