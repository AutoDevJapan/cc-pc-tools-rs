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
	"strings"
	"time"
)

// This plan is consumed with the existing normal todo edit CLI. No launch,
// owner-stop release, wait release, generation change or live-worker interrupt.
type SteeringDeliveryPlan struct {
	Domain     string `json:"domain"`
	ID         string `json:"plan_id"`
	Todo       uint64 `json:"todo_id"`
	Message    string `json:"message_id"`
	Job        string `json:"job_id"`
	Key        string `json:"request_key"`
	ReceiptSHA string `json:"receipt_sha256"`
	ControlSHA string `json:"control_sha256"`
	NoteFile   string `json:"note_file"`
	NoteSHA    string `json:"note_sha256"`
	Phase      string `json:"phase"`
	Prepared   string `json:"prepared_at"`
	Delivered  string `json:"delivered_at,omitempty"`
}
type SteeringDeliveryResult struct {
	Phase         string   `json:"phase"`
	Todo          uint64   `json:"todo_id"`
	Message       string   `json:"message_id"`
	Job           string   `json:"job_id"`
	Plan          string   `json:"plan_id"`
	NoteFile      string   `json:"note_file,omitempty"`
	Executable    string   `json:"edit_executable,omitempty"`
	Args          []string `json:"edit_args,omitempty"`
	WorkerApplied bool     `json:"worker_applied"`
}

func bytesHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (r *Runner) steeringReceiptPath(todo uint64, message string) string {
	return filepath.Join(r.Config.Home, "steering", fmt.Sprintf("%d-%s.json", todo, message))
}
func (r *Runner) publishSteeringSource(q *Request) error {
	workerSource := ordinaryWorkerSource(q)
	return publish(r.steeringReceiptPath(q.Todo, q.Message), &workerSource)
}
func (r *Runner) steeringPlanPath(message string) string {
	return filepath.Join(r.Config.Home, "deliveries", message+".json")
}
func (r *Runner) deliveryRequest(ctx context.Context, message string) (*Request, *TodoRow, string, error) {
	if _, ok := number(message); !ok {
		return nil, nil, "", errors.New("steering message identity invalid")
	}
	req := r.State.Requests[message]
	if req != nil && req.Conversation == "" {
		if err := r.retireOrdinaryWatch(req); err != nil {
			return nil, nil, "", err
		}
	}
	if req == nil || req.Message != message || req.Origin == nil || req.Origin.Scope != productReportScope || req.Todo == 0 || req.Todo != req.Origin.Todo || (req.Phase != "product_reply_saved" && req.Phase != "product_reply_delivered" && req.Phase != "product_reply_applied") {
		return nil, nil, "", errors.New("saved steering association unavailable")
	}
	row, err := r.todo(req.Todo)
	if err != nil || !r.verifyProductScope(ctx, *req.Origin, row) {
		return nil, nil, "", errors.New("steering original scope deferred")
	}
	link, exists := r.State.ProgressLinks[req.Thread]
	if !exists || link.ProgressOrigin != *req.Origin {
		return nil, nil, "", errors.New("steering progress origin differs")
	}
	if _, ok := r.verifyProgressLink(ctx, link); !ok {
		return nil, nil, "", errors.New("steering verified sender origin deferred")
	}
	// The explicit finite report origin proves this bot-thread destination;
	// validateRequest still re-reads the exact owner message and reference.
	if err := r.validateRequest(ctx, req); err != nil {
		return nil, nil, "", err
	}
	bytes, err := os.ReadFile(r.steeringReceiptPath(req.Todo, message))
	if err != nil || len(bytes) > maxState {
		return nil, nil, "", errors.New("private steering receipt unavailable")
	}
	var saved Request
	if json.Unmarshal(bytes, &saved) != nil || saved.Key != req.Key || saved.Todo != req.Todo || saved.Message != req.Message || saved.Author != req.Author || saved.Origin == nil || *saved.Origin != *req.Origin {
		return nil, nil, "", errors.New("private steering receipt differs")
	}
	if saved.Text != req.Text || saved.Edited != req.Edited || saved.HistoryWatch != nil && saved.HistoryWatch.Receipt != "" {
		// validateRequest already retained every previous owner edit. Refresh the
		// private delivery receipt, invalidate the old plan by its byte hash, and
		// require a fresh normal delivery without releasing a wait or owner stop.
		req.Phase = "product_reply_saved"
		if err := r.publishSteeringSource(req); err != nil {
			return nil, nil, "", err
		}
		if err := r.save(); err != nil {
			return nil, nil, "", err
		}
		return nil, nil, "", errors.New("current source receipt refreshed; prepare a new delivery")
	}
	return req, row, bytesHash(bytes), nil
}
func (r *Runner) todoControl(id uint64) (string, error) {
	var db struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := readJSON(r.TodoPath, maxTodo, &db); err != nil {
		return "", err
	}
	var selected map[string]json.RawMessage
	for _, raw := range db.Items {
		var identity struct {
			ID uint64 `json:"id"`
		}
		if json.Unmarshal(raw, &identity) != nil {
			return "", errors.New("task identity invalid")
		}
		if identity.ID == id {
			if selected != nil {
				return "", errors.New("duplicate task identity")
			}
			if json.Unmarshal(raw, &selected) != nil {
				return "", errors.New("task receipt invalid")
			}
		}
	}
	if selected == nil {
		return "", errors.New("task receipt missing")
	}
	control := make(map[string]json.RawMessage)
	for _, key := range []string{"id", "project", "status", "spec", "jobs", "scheduler", "worker_receipt", "resume_generation", "wait_on", "wait_ready", "stop_kind", "stopped", "cancel_requested", "owner_postponement_recorded", "decision_wait", "dispatchable"} {
		if v, ok := selected[key]; ok {
			control[key] = v
		}
	}
	bytes, err := json.Marshal(control)
	if err != nil {
		return "", err
	}
	return bytesHash(bytes), nil
}
func (r *Runner) deliveryBlock(p SteeringDeliveryPlan) string {
	return fmt.Sprintf("unattended-steering-delivery:%s\n本人の追加指示の私的参照: %s\n元ToDo: %d / 対象の担当ジョブ: %s\n担当は作業の区切りでこの参照を読み、元の範囲で反映してください。短いa/bを別質問の承認にしないでください。待機・本人停止・担当世代・費用権限を変える許可ではありません。\n実反映後は %s に todo_id,message_id,job_id,revision,verification,receipt_sha256 を実結果で記録してください。receipt_sha256 は私的参照ファイルのSHA-256です。", p.ID, r.steeringReceiptPath(p.Todo, p.Message), p.Todo, p.Job, filepath.Join(r.Config.Home, "applied", p.Message+".json"))
}
func (r *Runner) prepareSteeringDelivery(ctx context.Context, message string) (SteeringDeliveryResult, error) {
	req, row, receipt, err := r.deliveryRequest(ctx, message)
	if err != nil {
		return SteeringDeliveryResult{}, err
	}
	job := latestTodoJob(row)
	if job == "" {
		return SteeringDeliveryResult{}, errors.New("actual assigned job receipt unavailable")
	}
	control, err := r.todoControl(req.Todo)
	if err != nil {
		return SteeringDeliveryResult{}, err
	}
	id := bytesHash([]byte(req.Key + "\x00" + fmt.Sprint(req.Todo) + "\x00" + job + "\x00" + receipt + "\x00" + control))[:32]
	p := SteeringDeliveryPlan{Domain: "unattended-steering-delivery-v1", ID: id, Todo: req.Todo, Message: message, Job: job, Key: req.Key, ReceiptSHA: receipt, ControlSHA: control, Phase: "prepared", Prepared: r.Now().UTC().Format(time.RFC3339Nano)}
	p.NoteFile = filepath.Join(r.Config.Home, "deliveries", message+"-"+id+".note.txt")
	note := row.Note
	block := r.deliveryBlock(p)
	if !strings.Contains(note, block) {
		note = strings.TrimRight(note, "\r\n") + "\n\n" + block + "\n"
	}
	if len(note) > 1<<20 {
		return SteeringDeliveryResult{}, errors.New("delivery note limit exceeded")
	}
	p.NoteSHA = bytesHash([]byte(note))
	if err := writeText(p.NoteFile, note); err != nil {
		return SteeringDeliveryResult{}, err
	}
	if err := publish(r.steeringPlanPath(message), p); err != nil {
		return SteeringDeliveryResult{}, err
	}
	return SteeringDeliveryResult{Phase: "prepared", Todo: p.Todo, Message: message, Job: job, Plan: id, NoteFile: p.NoteFile, Executable: "D:/CC/Tools/todo/todo.exe", Args: []string{"edit", fmt.Sprint(p.Todo), "--note-file", p.NoteFile}}, nil
}
func (r *Runner) verifySteeringDelivery(ctx context.Context, message string) (SteeringDeliveryResult, error) {
	req, row, receipt, err := r.deliveryRequest(ctx, message)
	if err != nil {
		return SteeringDeliveryResult{}, err
	}
	var p SteeringDeliveryPlan
	if readJSON(r.steeringPlanPath(message), 1<<20, &p) != nil || p.Domain != "unattended-steering-delivery-v1" || p.Todo != req.Todo || p.Message != message || p.Key != req.Key || p.ReceiptSHA != receipt || !validJob(p.Job) {
		return SteeringDeliveryResult{}, errors.New("steering delivery plan differs")
	}
	expected := bytesHash([]byte(req.Key + "\x00" + fmt.Sprint(req.Todo) + "\x00" + p.Job + "\x00" + receipt + "\x00" + p.ControlSHA))[:32]
	if p.ID != expected || p.NoteFile != filepath.Join(r.Config.Home, "deliveries", message+"-"+p.ID+".note.txt") {
		return SteeringDeliveryResult{}, errors.New("steering plan identity differs")
	}
	if !strings.Contains(row.Note, r.deliveryBlock(p)) {
		return SteeringDeliveryResult{}, errors.New("normal todo reference not delivered")
	}
	if p.Phase == "prepared" {
		control, err := r.todoControl(req.Todo)
		if err != nil || control != p.ControlSHA || latestTodoJob(row) != p.Job {
			return SteeringDeliveryResult{}, errors.New("task wait stop or generation changed; prepare again")
		}
		note, err := os.ReadFile(p.NoteFile)
		if err != nil || bytesHash(note) != p.NoteSHA {
			return SteeringDeliveryResult{}, errors.New("delivery note differs")
		}
		p.Phase = "todo_reference_delivered"
		p.Delivered = r.Now().UTC().Format(time.RFC3339Nano)
		if err := publish(r.steeringPlanPath(message), p); err != nil {
			return SteeringDeliveryResult{}, err
		}
	}
	if p.Phase != "todo_reference_delivered" {
		return SteeringDeliveryResult{}, errors.New("steering delivery phase differs")
	}
	req.Phase = "product_reply_delivered"
	var proof struct {
		Todo         uint64 `json:"todo_id"`
		Message      string `json:"message_id"`
		Job          string `json:"job_id"`
		Revision     string `json:"revision"`
		Verification string `json:"verification"`
		ReceiptSHA   string `json:"receipt_sha256"`
	}
	owned := latestTodoJob(row) == p.Job
	for _, raw := range row.Jobs {
		var job string
		if json.Unmarshal(raw, &job) == nil && job == p.Job {
			owned = true
		}
	}
	if readJSON(filepath.Join(r.Config.Home, "applied", message+".json"), 1<<20, &proof) == nil && proof.Todo == req.Todo && proof.Message == message && proof.Job == p.Job && owned && strings.TrimSpace(proof.Revision) != "" && strings.TrimSpace(proof.Verification) != "" && ((req.Edited == "" && len(req.PriorEdits) == 0 && proof.ReceiptSHA == "") || proof.ReceiptSHA == p.ReceiptSHA) {
		req.Phase = "product_reply_applied"
	}
	if err := r.save(); err != nil {
		return SteeringDeliveryResult{}, err
	}
	return SteeringDeliveryResult{Phase: p.Phase, Todo: p.Todo, Message: message, Job: p.Job, Plan: p.ID, WorkerApplied: req.Phase == "product_reply_applied"}, nil
}

func runnerArguments(args []string) (string, string, string, error) {
	config := "D:/CC/Tools/discord-ask/unattended/config.json"
	mode := ""
	message := ""
	seenConfig := false
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return "", "", "", errors.New("argument value missing")
		}
		switch args[i] {
		case "--config":
			if seenConfig {
				return "", "", "", errors.New("duplicate config")
			}
			seenConfig = true
			config = args[i+1]
		case "--prepare-steering-delivery", "--verify-steering-delivery":
			if mode != "" {
				return "", "", "", errors.New("duplicate delivery operation")
			}
			mode = args[i]
			message = args[i+1]
			if _, ok := number(message); !ok {
				return "", "", "", errors.New("message identity invalid")
			}
		default:
			return "", "", "", errors.New("unknown argument")
		}
		i++
	}
	return config, mode, message, nil
}
