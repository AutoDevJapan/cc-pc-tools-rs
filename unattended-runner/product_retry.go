package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// These are the original finite guard scope, not new dispatch permissions.
const productRetryID = 1626
const productRetrySession = "01a12690-d332-7140-bc8c-47974cdcd2cb"
const productRetrySourceJob = "20261011-234558-orch-9a5f7b7c6fb45eb2815aa60ea8a5df95"
const productRetryHash = "a600dda0a9dc0f26a3507e081ef5b938cd7de40bad92e500068f3a191b99ecb8"
const productRetryTree = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-1626-1"

type productRetryProof struct {
	ID              uint64          `json:"id"`
	Verified        bool            `json:"verified"`
	Expires         string          `json:"expires_at"`
	SessionVerified bool            `json:"session_receipt_verified"`
	Session         string          `json:"session"`
	Worktree        string          `json:"worktree"`
	SourceJob       string          `json:"job"`
	Request         string          `json:"request"`
	RequestHash     string          `json:"request_sha256"`
	Snapshot        json.RawMessage `json:"snapshot"`
}

type productRetryWait struct {
	Condition string `json:"condition"`
	Since     uint64 `json:"since"`
	NotBefore uint64 `json:"not_before"`
}

// A candidate is advisory. Only a transaction in the normal ToDo/queue APIs
// may authorize a launch or change the original item; callers must not edit
// todo.json from this runner or use Start as an automatic compare-and-set.
type productRetryCandidate struct {
	Key       string
	SourceJob string
	Snapshot  json.RawMessage
	Wait      productRetryWait
	Proof     productRetryProof
}

func finiteProductRetry(at time.Time) bool {
	return !at.Before(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) && at.Before(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
}

func sameProductPath(a, b string) bool {
	// Only spelling equivalence here. The existing guard, not this helper,
	// verifies the physical path, links, original registry and Git owner.
	return strings.EqualFold(strings.ReplaceAll(a, `\`, "/"), strings.ReplaceAll(b, `\`, "/"))
}

func validProductRetryProof(p productRetryProof, at time.Time) bool {
	expires, err := time.Parse(time.RFC3339, p.Expires)
	return err == nil && finiteProductRetry(at) && at.Before(expires) &&
		!expires.After(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) &&
		p.ID == productRetryID && p.Verified && p.SessionVerified &&
		p.Session == productRetrySession && p.SourceJob == productRetrySourceJob &&
		p.RequestHash == productRetryHash && p.Request != "" &&
		sameProductPath(p.Worktree, productRetryTree) && len(p.Snapshot) > 0 && string(p.Snapshot) != "null"
}

// selectProductRetry never releases a wait. Missing, duplicate or malformed
// original metadata fails closed, including unresolved or unknown dependencies.
func selectProductRetry(body []byte, at time.Time) (*productRetryCandidate, error) {
	var db struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &db); err != nil {
		return nil, err
	}
	var original json.RawMessage
	statuses := make(map[uint64]string)
	for _, raw := range db.Items {
		var identity struct {
			ID     uint64 `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, err
		}
		if _, exists := statuses[identity.ID]; exists {
			return nil, errors.New("duplicate todo identity")
		}
		statuses[identity.ID] = identity.Status
		if identity.ID == productRetryID {
			original = raw
		}
	}
	if original == nil || !finiteProductRetry(at) || at.Unix() < 0 {
		return nil, nil
	}
	var row struct {
		Status    string           `json:"status"`
		Dispatch  *bool            `json:"dispatchable"`
		Wait      productRetryWait `json:"wait_ready"`
		On        []uint64         `json:"wait_on"`
		Stopped   bool             `json:"stopped"`
		Cancel    bool             `json:"cancel_requested"`
		Postponed bool             `json:"owner_postponement_recorded"`
		StopKind  string           `json:"stop_kind"`
		Decision  json.RawMessage  `json:"decision_wait"`
		Scheduler struct {
			OwnerStop json.RawMessage `json:"owner_stop_job"`
			Job       string          `json:"job"`
		} `json:"scheduler"`
		Worker struct {
			Job string `json:"job"`
		} `json:"worker_receipt"`
		Qualification struct {
			Receipt productRetryProof `json:"receipt"`
		} `json:"product_resume_qualification"`
	}
	if err := json.Unmarshal(original, &row); err != nil {
		return nil, err
	}
	nonNull := func(raw json.RawMessage) bool { return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" }
	w := row.Wait
	if row.Status != "wait" || row.Dispatch == nil || *row.Dispatch || row.Stopped || row.Cancel || row.Postponed || row.StopKind != "" || nonNull(row.Decision) || nonNull(row.Scheduler.OwnerStop) ||
		w.Condition != "retry-time" || w.NotBefore < w.Since || w.NotBefore-w.Since < 60 || w.NotBefore-w.Since > 86400 || w.NotBefore > uint64(at.Unix()) || !validProductRetryProof(row.Qualification.Receipt, at) {
		return nil, nil
	}
	for _, id := range row.On {
		if id == 0 || id == productRetryID || statuses[id] != "done" {
			return nil, nil
		}
	}
	if row.Worker.Job == "" || row.Worker.Job != row.Scheduler.Job {
		return nil, nil
	}
	// The wait generation, not mutable notes or qualification timestamps, fixes
	// the queue key. Crash recovery must reuse it and never issue a fresh key.
	identity := fmt.Sprintf("product-retry-2709\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s", productRetryID, w.Since, w.NotBefore, row.Worker.Job, productRetryHash, productRetryTree)
	sum := sha256.Sum256([]byte(identity))
	return &productRetryCandidate{Key: hex.EncodeToString(sum[:16]), SourceJob: row.Worker.Job, Snapshot: append(json.RawMessage(nil), original...), Wait: w, Proof: row.Qualification.Receipt}, nil
}
