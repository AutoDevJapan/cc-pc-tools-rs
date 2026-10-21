package inputqual

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const runtimeLeaf = "current-ci-handoff-3056.private.json"

type runtimeReference struct {
	File string `json:"file"`
	SHA  string `json:"sha256"`
}
type runtimeHandoff struct {
	Version    int              `json:"version"`
	Task       int              `json:"todo_id"`
	Origin     int              `json:"origin_todo_id"`
	Repo       int64            `json:"repo_id"`
	PR         int              `json:"pr"`
	Head       string           `json:"head_sha"`
	Tree       string           `json:"head_tree"`
	Base       string           `json:"base_sha"`
	Run        int64            `json:"run_id"`
	Attempt    int              `json:"attempt"`
	Jobs       []int64          `json:"job_ids"`
	Kind       string           `json:"kind"`
	Context    string           `json:"context_sha"`
	Review     runtimeReference `json:"review"`
	Acceptance runtimeReference `json:"acceptance"`
}
type runtimeResult struct {
	Origin     int    `json:"origin_todo_id"`
	Repo       int64  `json:"repo_id"`
	PR         int    `json:"pr"`
	Head       string `json:"head_sha"`
	Tree       string `json:"head_tree"`
	Kind       string `json:"kind"`
	Success    bool   `json:"success"`
	Must       *int   `json:"must"`
	OutputFile string `json:"output_file"`
	OutputSHA  string `json:"output_sha256"`
}

// ConstructRuntime consumes the canonical product parent's handoff, not a
// caller-selected pin. Existing retired inputs and finite routes stay retired
// and unchanged. Fresh records must pass the ordinary guard before any read.
func ConstructRuntime() (Snapshot, error) {
	p, e := productionPolicy()
	if e != nil {
		return Snapshot{}, e
	}
	return constructRuntime(p, runtimeReadable)
}

func runtimeReadable(path string) bool {
	exe, e := os.Executable()
	if e != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(filepath.Dir(exe)), "client-records", "scrub391.exe"), "--guard-path", path)
	prepareCommand(cmd)
	data, e := cmd.Output()
	if e != nil || len(data) > 16384 {
		return false
	}
	var result struct {
		OK      bool  `json:"ok"`
		Client  *bool `json:"client_resource"`
		Unknown *bool `json:"classification_unverified"`
	}
	return decode(data, &result, false) == nil && result.OK && result.Client != nil && !*result.Client && result.Unknown != nil && !*result.Unknown
}

func runtimeHex(s string, n int) bool {
	return len(s) == n && strings.Trim(s, "0123456789abcdef") == ""
}

func runtimeScope(p policy) bool {
	if p.origin == nil || !p.origin(p.root) {
		return false
	}
	for _, a := range []anchor{p.parentSpec, p.originSpec} {
		data, e := readExact(a.path, a.limit)
		if e != nil || hash(data) != a.sha {
			return false
		}
	}
	data, e := readExact(p.db, 64<<20)
	if e != nil {
		return false
	}
	var db struct {
		Rows []json.RawMessage `json:"items"`
	}
	if decodeKeys(data, &db, false, false) != nil {
		return false
	}
	seen := map[int]bool{}
	for _, raw := range db.Rows {
		var id struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(raw, &id) != nil {
			return false
		}
		if id.ID != 3059 && id.ID != 3056 {
			continue
		}
		var row struct {
			ID           int    `json:"id"`
			Status       string `json:"status"`
			Spec         string `json:"spec"`
			Dispatchable bool   `json:"dispatchable"`
		}
		if decode(raw, &row, false) != nil || seen[id.ID] || explicitStop(raw) || (row.Status != "doing" && row.Status != "todo" && row.Status != "wait") {
			return false
		}
		spec := p.originSpec.path
		if id.ID == 3059 {
			spec = p.parentSpec.path
			if !row.Dispatchable {
				return false
			}
		}
		if !same(spec, row.Spec) {
			return false
		}
		seen[id.ID] = true
	}
	return len(seen) == 2
}

func constructRuntime(p policy, readable func(string) bool) (Snapshot, error) {
	if !runtimeScope(p) {
		return Snapshot{}, rejected("runtime_task_scope")
	}
	// The parent-selected canonical handoff is the trust boundary. There is no
	// CLI argument or unpinned locator that can substitute a different parent.
	read := func(path string, limit int64) ([]byte, error) {
		if same(path, p.pin) || same(path, p.review) || same(path, p.acceptance) {
			return nil, rejected("runtime_legacy_input")
		}
		if !canonical(path) || readable == nil || !readable(path) {
			return nil, rejected("runtime_read_permission")
		}
		data, e := readExact(path, limit)
		if e != nil {
			return nil, rejected("runtime_record_unavailable")
		}
		return data, nil
	}
	// Specs live in the scheduler's specs store, not the product workspace.
	// Keep the new operational handoff in the independently verified original
	// helper source's ignored records directory, which the ordinary guard can
	// classify without changing the guard's scope or the immutable specs.
	data, e := read(filepath.Join(p.root, ".ci-work", runtimeLeaf), 64<<10)
	if e != nil {
		return Snapshot{}, e
	}
	var h runtimeHandoff
	if decode(data, &h, true) != nil || h.Version != 1 || h.Task != 3059 || h.Origin != 3056 || h.Repo != 1285649124 || h.PR != 995 || !runtimeHex(h.Head, 40) || !runtimeHex(h.Tree, 40) || !runtimeHex(h.Base, 40) || h.Run <= 0 || h.Attempt <= 0 || len(h.Jobs) == 0 || len(h.Jobs) > 8 || (h.Kind != "secret" && h.Kind != "codeql") || (h.Kind == "secret" && (len(h.Jobs) != 1 || !runtimeHex(h.Context, 40))) || (h.Kind == "codeql" && h.Context != "") {
		return Snapshot{}, rejected("runtime_handoff")
	}
	previous := int64(0)
	for _, id := range h.Jobs {
		if id <= previous {
			return Snapshot{}, rejected("runtime_job_set")
		}
		previous = id
	}
	var review, acceptance []byte
	used := map[string]bool{}
	for _, role := range []string{"review", "acceptance"} {
		ref := h.Review
		if role == "acceptance" {
			ref = h.Acceptance
		}
		if !runtimeHex(ref.SHA, 64) {
			return Snapshot{}, rejected("runtime_evidence_hash")
		}
		resultBytes, err := read(ref.File, 64<<10)
		if err != nil {
			return Snapshot{}, err
		}
		var r runtimeResult
		if hash(resultBytes) != ref.SHA || decode(resultBytes, &r, true) != nil || r.Origin != 3056 || r.Repo != h.Repo || r.PR != h.PR || r.Head != h.Head || r.Tree != h.Tree || r.Kind != role || !r.Success || r.Must == nil || *r.Must != 0 || !runtimeHex(r.OutputSHA, 64) {
			return Snapshot{}, rejected("runtime_original_result")
		}
		// Leave space for base64 plus the source-bound envelope under the
		// official parser's existing 8MiB evidence bound.
		output, err := read(r.OutputFile, 4<<20)
		if err != nil {
			return Snapshot{}, err
		}
		if hash(output) != r.OutputSHA || used[locationDigest(r.OutputFile)] || used[r.OutputSHA] {
			return Snapshot{}, rejected("runtime_original_output")
		}
		used[locationDigest(r.OutputFile)], used[r.OutputSHA] = true, true
		envelope, err := json.Marshal(struct {
			Origin      int    `json:"origin_todo_id"`
			Repo        int64  `json:"repo_id"`
			PR          int    `json:"pr"`
			Head        string `json:"head_sha"`
			Tree        string `json:"head_tree"`
			Kind        string `json:"kind"`
			Success     bool   `json:"success"`
			Must        int    `json:"must"`
			OriginalSHA string `json:"original_sha256"`
			Original    []byte `json:"original"`
		}{3056, h.Repo, h.PR, h.Head, h.Tree, role, true, 0, r.OutputSHA, output})
		if err != nil {
			return Snapshot{}, rejected("runtime_envelope")
		}
		if role == "review" {
			review = envelope
		} else {
			acceptance = envelope
		}
	}
	pin, e := json.Marshal(struct {
		Repo           int64   `json:"repo_id"`
		PR             int     `json:"pr"`
		Head           string  `json:"head_sha"`
		Tree           string  `json:"head_tree"`
		Base           string  `json:"base_sha"`
		Run            int64   `json:"run_id"`
		Attempt        int     `json:"attempt"`
		Jobs           []int64 `json:"job_ids"`
		Kind           string  `json:"kind"`
		Context        string  `json:"context_sha"`
		Review         string  `json:"review_sha256"`
		Acceptance     string  `json:"acceptance_sha256"`
		ReviewFile     string  `json:"review_file"`
		AcceptanceFile string  `json:"acceptance_file"`
	}{h.Repo, h.PR, h.Head, h.Tree, h.Base, h.Run, h.Attempt, h.Jobs, h.Kind, h.Context, hash(review), hash(acceptance), "canonical-parent-review", "canonical-parent-acceptance"})
	if e != nil {
		return Snapshot{}, rejected("runtime_pin")
	}
	r := Report{Qualified: true, Purpose: "original-3059-runtime-current-pr-ci", Task: 3059, OriginTask: 3056, RepoID: h.Repo, PR: h.PR, Head: h.Head, Tree: h.Tree, Run: h.Run, Job: h.Jobs[0], PinSHA: hash(pin), ReviewSHA: hash(review), AcceptanceSHA: hash(acceptance), ScopeSHA: p.parentSpec.sha, IndependentReceiptSHA: hash(data), GeneralClassificationUnverified: true, InputConstruction: "official-helper-from-canonical-product-parent-handoff", ConstructedInputCreator: "official-product-ci-helper", ReviewRepresentation: "source-bound original output; parent result and raw output hash verified", BaseAuthority: "canonical-product-parent-handoff", ContextAuthority: "canonical-handoff; requires live workflow match"}
	return Snapshot{pin: pin, review: review, acceptance: acceptance, report: r}, nil
}
