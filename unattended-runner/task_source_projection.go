package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const taskSourceProjectionDomain = "owner-task-source-projection-v1"

type taskSourceProjectionRequest struct {
	Todo                          uint64
	Root, Body, Decision, Image    string
	BodySHA, DecisionSHA, ImageSHA string
}

type taskSourceProjectionDependencies struct {
	Task  func(context.Context, uint64) (taskQuestionState, error)
	Guard func(context.Context, string) bool
}

type taskSourceProjectedWorker struct {
	Job        *string `json:"job"`
	Generation *uint64 `json:"generation"`
}

// No title, spec, note, other Task, or raw store value is exported.
type taskSourceProjectedTask struct {
	ID               uint64                    `json:"id"`
	Project          string                    `json:"project"`
	Root             string                    `json:"root"`
	Status           string                    `json:"status"`
	ResumeGeneration *uint64                   `json:"resume_generation"`
	Worker           taskSourceProjectedWorker `json:"worker_receipt"`
	SchedulerJob     *string                   `json:"scheduler_job"`
	SnapshotSHA      string                    `json:"snapshot_sha256"`
}

type taskSourceProjectedInputs struct {
	SourceRoot  string `json:"source_root"`
	BodySHA     string `json:"body_sha256"`
	DecisionSHA string `json:"decision_sha256"`
	ImageSHA    string `json:"image_sha256"`
	ImageBytes  int64  `json:"image_bytes"`
	ImageMIME   string `json:"image_mime"`
}

type taskSourceProjection struct {
	Domain                 string                     `json:"domain"`
	MetadataOnly           bool                       `json:"metadata_only"`
	Registered             bool                       `json:"registered"`
	SendAllowed            bool                       `json:"send_allowed"`
	TaskToRepositoryProved bool                       `json:"task_to_repository_proved"`
	SourceQuiescenceProved bool                       `json:"source_quiescence_proved"`
	Stage                  string                     `json:"stage"`
	Task                   *taskSourceProjectedTask   `json:"task"`
	Inputs                 *taskSourceProjectedInputs `json:"inputs"`
}

type taskSourceProjectionFailure string

func (e taskSourceProjectionFailure) Error() string { return string(e) }

func taskSourceProjectionEmpty(stage string) taskSourceProjection {
	return taskSourceProjection{Domain: taskSourceProjectionDomain, MetadataOnly: true, Stage: stage}
}

func taskSourceProjectionProduction() taskSourceProjectionDependencies {
	return taskSourceProjectionDependencies{
		Task: func(ctx context.Context, id uint64) (taskQuestionState, error) {
			if ctx.Err() != nil {
				return taskQuestionState{}, errTaskQuestion
			}
			// Reuse the same fixed normal provider as taskQuestionProduction.
			return taskQuestionReadState("D:/CC/Tools/todo/todo.json", id)
		},
		Guard: func(ctx context.Context, path string) bool {
			raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
			var proof struct {
				OK, Access, Client, Unknown *bool
			}
			var fields map[string]json.RawMessage
			if err != nil || len(raw) > 64<<10 || historyStrictJSON([]byte(raw), &fields) != nil {
				return false
			}
			if json.Unmarshal(fields["ok"], &proof.OK) != nil || json.Unmarshal(fields["access_allowed"], &proof.Access) != nil ||
				json.Unmarshal(fields["client_resource"], &proof.Client) != nil || json.Unmarshal(fields["classification_unverified"], &proof.Unknown) != nil {
				return false
			}
			return proof.OK != nil && *proof.OK && proof.Access != nil && *proof.Access && proof.Client != nil && !*proof.Client && proof.Unknown != nil && !*proof.Unknown
		},
	}
}

type taskSourceHeldFile struct {
	path  string
	file  *os.File
	info  os.FileInfo
	bytes []byte
	sha   string
}

func taskSourceHoldFile(ctx context.Context, path, expected string, limit int64, guard func(context.Context, string) bool) (*taskSourceHeldFile, error) {
	if ctx.Err() != nil || !filepath.IsAbs(path) || !shaPattern.MatchString(expected) || !guard(ctx, path) {
		return nil, taskSourceProjectionFailure("input_unqualified")
	}
	physical, named, err := conversationMediaPhysical(path)
	if err != nil || !named.Mode().IsRegular() || named.Size() < 1 || named.Size() > limit {
		return nil, taskSourceProjectionFailure("input_unavailable")
	}
	f, err := os.Open(physical)
	if err != nil {
		return nil, taskSourceProjectionFailure("input_unavailable")
	}
	h := &taskSourceHeldFile{path: path, file: f}
	failed := true
	defer func() {
		if failed {
			clear(h.bytes)
			_ = f.Close()
		}
	}()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(named, opened) || opened.Size() != named.Size() {
		return nil, taskSourceProjectionFailure("input_changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	h.bytes = b
	if err != nil || int64(len(b)) != opened.Size() || int64(len(b)) > limit || bytesHash(b) != expected {
		return nil, taskSourceProjectionFailure("input_changed")
	}
	h.info, h.sha = opened, expected
	if h.recheck(ctx, guard) != nil {
		return nil, taskSourceProjectionFailure("input_changed")
	}
	failed = false
	return h, nil
}

func (h *taskSourceHeldFile) recheck(ctx context.Context, guard func(context.Context, string) bool) error {
	if ctx.Err() != nil || !guard(ctx, h.path) {
		return taskSourceProjectionFailure("input_unqualified")
	}
	_, named, err := conversationMediaPhysical(h.path)
	opened, statErr := h.file.Stat()
	if err != nil || statErr != nil || !os.SameFile(h.info, named) || !os.SameFile(h.info, opened) ||
		named.Size() != h.info.Size() || opened.Size() != h.info.Size() || !opened.ModTime().Equal(h.info.ModTime()) {
		return taskSourceProjectionFailure("input_changed")
	}
	b := make([]byte, len(h.bytes))
	defer clear(b)
	n, readErr := h.file.ReadAt(b, 0)
	after, statErr := h.file.Stat()
	_, namedAfter, pathErr := conversationMediaPhysical(h.path)
	if readErr != nil || n != len(b) || bytesHash(b) != h.sha || statErr != nil || pathErr != nil ||
		after.Size() != h.info.Size() || !after.ModTime().Equal(h.info.ModTime()) || !os.SameFile(h.info, namedAfter) {
		return taskSourceProjectionFailure("input_changed")
	}
	return nil
}

func taskSourceProjectState(s taskQuestionState, id uint64) (*taskSourceProjectedTask, error) {
	var metadata struct {
		Status           string                    `json:"status"`
		ResumeGeneration *uint64                   `json:"resume_generation"`
		Worker           taskSourceProjectedWorker `json:"worker_receipt"`
		Scheduler        struct {
			Job *string `json:"job"`
		} `json:"scheduler"`
	}
	var unique map[string]json.RawMessage
	if s.ID != id || s.Project == "" || strings.EqualFold(s.Project, "owner") || !filepath.IsAbs(filepath.FromSlash(s.Root)) ||
		historyStrictJSON(s.current, &unique) != nil || json.Unmarshal(s.current, &metadata) != nil ||
		(metadata.Status != "doing" && metadata.Status != "todo" && metadata.Status != "block") || len(s.Project) > 256 || len(s.Root) > 4096 ||
		strings.ContainsAny(s.Project, "\r\n\x00") || strings.ContainsAny(s.Root, "\r\n\x00") {
		return nil, taskSourceProjectionFailure("task_unavailable")
	}
	for _, job := range []*string{metadata.Worker.Job, metadata.Scheduler.Job} {
		if job != nil && (len(*job) > 256 || strings.ContainsAny(*job, "\r\n\x00")) {
			return nil, taskSourceProjectionFailure("task_unavailable")
		}
	}
	p := &taskSourceProjectedTask{ID: s.ID, Project: s.Project, Root: s.Root, Status: metadata.Status, SnapshotSHA: bytesHash(s.current),
		ResumeGeneration: metadata.ResumeGeneration, Worker: metadata.Worker, SchedulerJob: metadata.Scheduler.Job}
	return p, nil
}

// The normal provider and original input handles stay in this invocation. The
// returned projection is an observation, never an issuer, media or send grant.
func taskSourceProject(ctx context.Context, q taskSourceProjectionRequest, d taskSourceProjectionDependencies) (result taskSourceProjection, err error) {
	result = taskSourceProjectionEmpty("unavailable")
	if ctx == nil || ctx.Err() != nil || q.Todo == 0 || d.Task == nil || d.Guard == nil || !filepath.IsAbs(q.Root) {
		return result, taskSourceProjectionFailure("invalid_input")
	}
	if !d.Guard(ctx, q.Root) {
		return result, taskSourceProjectionFailure("source_root_unqualified")
	}
	_, rootBefore, rootErr := conversationMediaPhysical(q.Root)
	if rootErr != nil || !rootBefore.IsDir() {
		return result, taskSourceProjectionFailure("source_root_unavailable")
	}
	var held []*taskSourceHeldFile
	defer func() {
		for _, h := range held {
			clear(h.bytes)
			if closeErr := h.file.Close(); closeErr != nil {
				err = errors.Join(err, taskSourceProjectionFailure("input_close_unavailable"))
			}
		}
		if err != nil {
			result = taskSourceProjectionEmpty("unavailable")
		}
	}()
	before, readErr := d.Task(ctx, q.Todo)
	if readErr != nil {
		return result, taskSourceProjectionFailure("task_unavailable")
	}
	p, projectErr := taskSourceProjectState(before, q.Todo)
	if projectErr != nil {
		return result, projectErr
	}
	if !d.Guard(ctx, p.Root) {
		return result, taskSourceProjectionFailure("task_root_unqualified")
	}
	_, taskRootBefore, rootErr := conversationMediaPhysical(p.Root)
	if rootErr != nil || !taskRootBefore.IsDir() {
		return result, taskSourceProjectionFailure("task_root_unavailable")
	}
	for _, input := range []struct {
		path, sha string
		limit     int64
	}{{q.Body, q.BodySHA, 128 << 10}, {q.Decision, q.DecisionSHA, 128 << 10}, {q.Image, q.ImageSHA, conversationMediaLimit}} {
		h, holdErr := taskSourceHoldFile(ctx, input.path, input.sha, input.limit, d.Guard)
		if holdErr != nil {
			return result, holdErr
		}
		held = append(held, h)
	}
	var decision struct {
		Todo      uint64 `json:"todo_id"`
		Decision  string
		Resources []struct{ Kind, ID string }
	}
	var decisionUnique map[string]json.RawMessage
	if !utf8.Valid(held[0].bytes) || len(strings.TrimSpace(string(held[0].bytes))) == 0 ||
		historyStrictJSON(held[1].bytes, &decisionUnique) != nil || json.Unmarshal(held[1].bytes, &decision) != nil ||
		decision.Todo != q.Todo || decision.Decision != "direction" {
		return result, taskSourceProjectionFailure("question_scope_mismatch")
	}
	repositories := 0
	for _, resource := range decision.Resources {
		if resource.Kind == "repository" && resource.ID != "" {
			repositories++
		}
	}
	if repositories != 1 {
		return result, taskSourceProjectionFailure("question_scope_mismatch")
	}
	mime, _, imageErr := conversationMediaMagic(held[2].bytes)
	if imageErr != nil || (mime != "image/png" && mime != "image/jpeg") {
		return result, taskSourceProjectionFailure("image_format_unavailable")
	}
	for _, h := range held {
		if err = h.recheck(ctx, d.Guard); err != nil {
			return result, err
		}
	}
	_, taskRootAfter, rootErr := conversationMediaPhysical(p.Root)
	if rootErr != nil || !os.SameFile(taskRootBefore, taskRootAfter) {
		return result, taskSourceProjectionFailure("task_root_changed")
	}
	if !d.Guard(ctx, q.Root) || !d.Guard(ctx, p.Root) {
		return result, taskSourceProjectionFailure("source_root_unqualified")
	}
	_, rootAfter, rootErr := conversationMediaPhysical(q.Root)
	if rootErr != nil || !os.SameFile(rootBefore, rootAfter) {
		return result, taskSourceProjectionFailure("source_root_changed")
	}
	after, readErr := d.Task(ctx, q.Todo)
	if readErr != nil {
		return result, taskSourceProjectionFailure("task_unavailable")
	}
	current, projectErr := taskSourceProjectState(after, q.Todo)
	if projectErr != nil || current.SnapshotSHA != p.SnapshotSHA || current.Root != p.Root || canonicalHash(current) != canonicalHash(p) || ctx.Err() != nil {
		return result, taskSourceProjectionFailure("task_changed")
	}
	// The last provider read may itself take time. Retain and recheck the same
	// original handles afterward; do not treat an earlier sample as a lease.
	for _, h := range held {
		if err = h.recheck(ctx, d.Guard); err != nil {
			return result, err
		}
	}
	_, taskRootLast, taskRootErr := conversationMediaPhysical(p.Root)
	_, rootLast, sourceRootErr := conversationMediaPhysical(q.Root)
	if taskRootErr != nil || !os.SameFile(taskRootBefore, taskRootLast) {
		return result, taskSourceProjectionFailure("task_root_changed")
	}
	if sourceRootErr != nil || !os.SameFile(rootBefore, rootLast) || !d.Guard(ctx, q.Root) || !d.Guard(ctx, p.Root) || ctx.Err() != nil {
		return result, taskSourceProjectionFailure("source_root_changed")
	}
	result = taskSourceProjectionEmpty("complete")
	result.Task = current
	result.Inputs = &taskSourceProjectedInputs{SourceRoot: q.Root, BodySHA: q.BodySHA, DecisionSHA: q.DecisionSHA, ImageSHA: q.ImageSHA, ImageBytes: int64(len(held[2].bytes)), ImageMIME: mime}
	return result, nil
}

func taskSourceProjectionCLI(args []string, out io.Writer, d taskSourceProjectionDependencies) int {
	f := flag.NewFlagSet("task-source-projection", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var q taskSourceProjectionRequest
	var id string
	for _, v := range []struct {
		name   string
		target *string
	}{
		{"todo-id", &id}, {"cwd", &q.Root}, {"body-file", &q.Body}, {"decision-file", &q.Decision}, {"attach", &q.Image},
		{"body-sha256", &q.BodySHA}, {"decision-sha256", &q.DecisionSHA}, {"image-sha256", &q.ImageSHA},
	} {
		conversationStringFlag(f, v.name, v.target)
	}
	result := taskSourceProjectionEmpty("invalid_input")
	var err error = taskSourceProjectionFailure("invalid_input")
	if f.Parse(args) == nil && f.NArg() == 0 && id != "" {
		parsed, parseErr := strconv.ParseUint(id, 10, 64)
		if parseErr == nil && parsed != 0 && strconv.FormatUint(parsed, 10) == id {
			q.Todo = parsed
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			result, err = taskSourceProject(ctx, q, d)
			cancel()
		}
	}
	if err != nil {
		result = taskSourceProjectionEmpty("unavailable")
		var fixed taskSourceProjectionFailure
		if errors.As(err, &fixed) {
			result.Stage = string(fixed)
		}
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil || len(encoded) > 64<<10 {
		return 1
	}
	encoded = append(encoded, '\n')
	n, writeErr := out.Write(encoded)
	if writeErr != nil || n != len(encoded) || err != nil {
		return 1
	}
	return 0
}
