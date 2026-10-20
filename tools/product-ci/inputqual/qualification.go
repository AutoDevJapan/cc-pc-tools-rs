// Package inputqual qualifies one already requested CI purpose. It does not
// classify the input as public/non-client or recover the original registry.
package inputqual

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const Purpose = "original-3059-current-pr-ci"
const fixedHead = "cbd688871651e46d4821a4535d57eaa408105ff2"
const fixedTree = "73a5d39e6694be7e6e604003dcb4e7bdc087c1c5"
const fixedBase = "41090d19329b81dd1bd076f3336d723823be4b82"
const fixedContext = "6e7ca682c861024b6f08f31382851dab4bceff35"
const originalReviewSHA = "b15cb5b560e3ad65799fe19101c8c986092dd04548661072d32b07f1f2de57b2"

// Report deliberately contains no original text or private paths. A qualified
// purpose is not an execution result and never changes guard-path's verdict.
type Report struct {
	Qualified                         bool   `json:"qualified_for_purpose"`
	Purpose                           string `json:"purpose"`
	Task                              int    `json:"todo_id"`
	OriginTask                        int    `json:"origin_todo_id"`
	AdditionalTask                    int    `json:"additional_todo_id"`
	RepoID                            int64  `json:"repo_id"`
	PR                                int    `json:"pr"`
	Head                              string `json:"head_sha"`
	Tree                              string `json:"head_tree"`
	Run                               int64  `json:"run_id"`
	Job                               int64  `json:"job_id"`
	PinSHA                            string `json:"pin_sha256"`
	ReviewSHA                         string `json:"review_sha256"`
	AcceptanceSHA                     string `json:"acceptance_sha256"`
	ScopeSHA                          string `json:"scope_sha256"`
	IndependentReceiptSHA             string `json:"independent_receipt_sha256"`
	OriginalRegistryRecovered         bool   `json:"original_registry_recovered"`
	GeneralClassificationUnverified   bool   `json:"general_classification_unverified"`
	ConfidentialityEstablished        bool   `json:"confidentiality_established"`
	OriginalCreator                   string `json:"original_creator"`
	IndividualCreationToolEstablished bool   `json:"individual_creation_tool_established"`
	IndividualCreationTimeEstablished bool   `json:"individual_creation_time_established"`
	OfficialCISuccess                 bool   `json:"official_ci_success"`
	InputConstruction                 string `json:"input_construction"`
	OriginalInputFilesRead            bool   `json:"original_input_files_read"`
	ConstructedInputCreator           string `json:"constructed_input_creator,omitempty"`
	ReviewRepresentation              string `json:"review_representation,omitempty"`
	BaseAuthority                     string `json:"base_authority,omitempty"`
	ContextAuthority                  string `json:"context_authority,omitempty"`
}

type Snapshot struct {
	pin, review, acceptance []byte
	report                  Report
}

func (s Snapshot) String() string   { return "[private purpose-qualified CI snapshot]" }
func (s Snapshot) GoString() string { return s.String() }
func (s Snapshot) Report() Report   { return s.report }

// Consume passes the exact verified bytes, never reopens their paths, and
// rechecks identity immediately before the consumer receives them.
func (s Snapshot) Consume(consumer func(pin, review, acceptance []byte) error) error {
	if consumer == nil || !s.report.Qualified || hash(s.pin) != s.report.PinSHA || hash(s.review) != s.report.ReviewSHA || hash(s.acceptance) != s.report.AcceptanceSHA {
		return rejected("snapshot_changed")
	}
	return consumer(s.pin, s.review, s.acceptance)
}

type anchor struct {
	path, sha string
	limit     int64
}
type policy struct {
	root, pin, review, acceptance, db                                                 string
	reviewSHA                                                                         string
	scope, parentSpec, originSpec, predecessor, original, owned, outcome, final, stop anchor
	origin                                                                            func(string) bool
}

// These expectations come from the explicitly supplied, independently retained
// task specifications and receipts, not from a caller flag or the three inputs.
// The source-bound scope is intentionally finite. New tasks or source revisions
// cannot mint qualification by editing their own input/evidence JSON.
func Qualify(path string) (Snapshot, error) {
	// The retained operator wrappers cannot establish independent payload
	// identity. Retire this public reader before opening ANY caller path or
	// locator. Only the independently sourced constructor is executable.
	return Snapshot{}, rejected("legacy_input_route_unavailable")
}
func rejected(stage string) error {
	return fmt.Errorf("current CI purpose qualification rejected: %s", stage)
}
func hash(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func same(a, b string) bool   { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

func qualify(path string, p policy) (Snapshot, error) {
	return qualifyPurpose(path, p, false)
}

// Construct derives typed input from the independent original authorities.
// It never opens, copies or wraps the three rejected operator-created files.
func Construct() (Snapshot, error) {
	p, err := productionPolicy()
	if err != nil {
		return Snapshot{}, err
	}
	return qualifyPurpose(p.pin, p, true)
}

func qualifyPurpose(path string, p policy, construct bool) (Snapshot, error) {
	fail := func(stage string) (Snapshot, error) { return Snapshot{}, rejected(stage) }
	// Membership is checked before opening even one caller-selected resource.
	if !filepath.IsAbs(path) || !same(path, p.pin) || !canonical(path) {
		return fail("unregistered_input")
	}
	if p.origin == nil || !p.origin(p.root) {
		return fail("source_origin")
	}
	checked := map[string][]byte{}
	for label, a := range map[string]anchor{"scope": p.scope, "parent_scope": p.parentSpec, "origin_scope": p.originSpec, "predecessor": p.predecessor, "original": p.original, "owned": p.owned, "outcome": p.outcome, "final": p.final, "stop": p.stop} {
		data, err := readExact(a.path, a.limit)
		if err != nil || hash(data) != a.sha {
			return fail(label + "_authority")
		}
		checked[label] = data
	}
	if !taskScope(p) {
		return fail("original_task_scope")
	}
	var prior struct {
		Task       int    `json:"todo_id"`
		Origin     int    `json:"origin_todo_id"`
		Additional int    `json:"additional_todo_id"`
		Owned      string `json:"owned_id"`
		OwnedPath  string `json:"owned_path"`
		Head       string `json:"head"`
		Tree       string `json:"tree"`
		Review     string `json:"review_sha256"`
		Acceptance string `json:"acceptance_sha256"`
		Original   string `json:"original_receipt_sha256"`
		Count      int    `json:"original_artifacts_verified"`
		Secret     struct {
			Pin     string  `json:"secret_pin"`
			SHA     string  `json:"secret_pin_sha256"`
			Head    string  `json:"head"`
			Run     int64   `json:"run"`
			Attempt int     `json:"attempt"`
			Jobs    []int64 `json:"job_ids"`
		} `json:"current_secret_pin"`
	}
	if decode(checked["predecessor"], &prior, false) != nil || prior.Task != 3059 || prior.Origin != 3056 || prior.Additional != 3068 || prior.Head != fixedHead || prior.Tree != fixedTree || prior.Original != p.original.sha || prior.Count != 36 || prior.Secret.Head != fixedHead || prior.Secret.Run != 39384472279 || prior.Secret.Attempt != 1 || len(prior.Secret.Jobs) != 1 || prior.Secret.Jobs[0] != 118500893552 || !same(prior.Secret.Pin, p.pin) {
		return fail("predecessor_binding")
	}
	var independent struct {
		Task     int    `json:"todo_id"`
		Origin   int    `json:"original_todo"`
		Repo     int64  `json:"repo_id"`
		PR       int    `json:"pr"`
		Head     string `json:"new_head"`
		Tree     string `json:"new_tree"`
		Complete bool   `json:"finite_acceptance_complete"`
		Ref      bool   `json:"final_ref_updated"`
		Review   struct {
			Status    string `json:"status"`
			Must      int    `json:"must"`
			Candidate string `json:"candidate"`
			Job       string `json:"job"`
			SHA       string `json:"response_sha256"`
			Runtime   bool   `json:"runtime_context_verified"`
		} `json:"final_review"`
	}
	if decode(checked["original"], &independent, false) != nil || independent.Task != 3068 || independent.Origin != 3056 || independent.Repo != 1285649124 || independent.PR != 995 || independent.Head != fixedHead || independent.Tree != fixedTree || !independent.Complete || !independent.Ref || independent.Review.Status != "succeeded" || independent.Review.Must != 0 || !independent.Review.Runtime || independent.Review.Candidate != fixedHead || !strings.EqualFold(independent.Review.SHA, p.reviewSHA) || independent.Review.Job != "20261020-160356-todo3068-csv-final-review" {
		return fail("independent_original_result")
	}
	var owned struct{ ID, Path, Project, Content, State string }
	if decode(checked["owned"], &owned, false) != nil || prior.Owned != owned.ID || !same(prior.OwnedPath, filepath.Dir(p.pin)) || !same(owned.Path, filepath.Dir(p.pin)) || owned.Project != "todo-3059" || owned.Content != "current-cbd-evidence-and-ci" || owned.State != "owned" {
		return fail("original_ownership")
	}
	if err := verifyStop(checked, p, prior.Secret.SHA, owned.ID); err != nil {
		return Snapshot{}, err
	}
	if construct {
		return constructSnapshot(checked, p)
	}
	// This is a purpose-authorized bounded read. General classification remains
	// unknown/protected, and the files are neither copied nor reserialized.
	pin, err := readExact(p.pin, 64<<10)
	if err != nil || hash(pin) != prior.Secret.SHA {
		return fail("plan_bytes")
	}
	review, err := readExact(p.review, 8<<20)
	if err != nil || hash(review) != prior.Review {
		return fail("review_bytes")
	}
	acceptance, err := readExact(p.acceptance, 8<<20)
	if err != nil || hash(acceptance) != prior.Acceptance {
		return fail("acceptance_bytes")
	}
	var plan struct {
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
	}
	if decode(pin, &plan, true) != nil || plan.Repo != 1285649124 || plan.PR != 995 || plan.Head != fixedHead || plan.Tree != fixedTree || plan.Base != "41090d19329b81dd1bd076f3336d723823be4b82" || plan.Run != 39384472279 || plan.Attempt != 1 || len(plan.Jobs) != 1 || plan.Jobs[0] != 118500893552 || plan.Kind != "secret" || plan.Context != "6e7ca682c861024b6f08f31382851dab4bceff35" || plan.Review != prior.Review || plan.Acceptance != prior.Acceptance || !same(plan.ReviewFile, p.review) || !same(plan.AcceptanceFile, p.acceptance) {
		return fail("fixed_plan")
	}
	for kind, data := range map[string][]byte{"review": review, "acceptance": acceptance} {
		var v struct {
			Origin   int    `json:"origin_todo_id"`
			Repo     int64  `json:"repo_id"`
			PR       int    `json:"pr"`
			Head     string `json:"head_sha"`
			Tree     string `json:"head_tree"`
			Kind     string `json:"kind"`
			Success  bool   `json:"success"`
			Must     int    `json:"must"`
			SHA      string `json:"original_sha256"`
			Original []byte `json:"original"`
		}
		if decode(data, &v, true) != nil || v.Origin != 3056 || v.Repo != plan.Repo || v.PR != plan.PR || v.Head != plan.Head || v.Tree != plan.Tree || v.Kind != kind || !v.Success || v.Must != 0 || len(v.Original) == 0 || hash(v.Original) != v.SHA {
			return fail("source_evidence")
		}
		expected := p.original.sha
		if kind == "review" {
			expected = strings.ToLower(independent.Review.SHA)
		}
		if v.SHA != expected && !(kind == "review" && sameOriginalReviewRecord(v.Original, checked["original"])) {
			return fail(kind + "_independent_payload")
		}
	}
	r := Report{Qualified: true, Purpose: Purpose, Task: 3059, OriginTask: 3056, AdditionalTask: 3068, RepoID: plan.Repo, PR: plan.PR, Head: plan.Head, Tree: plan.Tree, Run: plan.Run, Job: plan.Jobs[0], PinSHA: hash(pin), ReviewSHA: hash(review), AcceptanceSHA: hash(acceptance), ScopeSHA: p.scope.sha, IndependentReceiptSHA: p.original.sha, GeneralClassificationUnverified: true, OriginalCreator: "prior-original-task3059-operator", InputConstruction: "prior-operator-inputs", OriginalInputFilesRead: true}
	return Snapshot{pin: pin, review: review, acceptance: acceptance, report: r}, nil
}

// Some prior operators retained the original receipt's review RECORD rather
// than the reviewer's raw response. Only exact semantic identity with that
// independently pinned original record is accepted; no new success assertion,
// unknown field or differently sourced response is inferred from a wrapper.
func sameOriginalReviewRecord(payload, receipt []byte) bool {
	var original struct {
		Review json.RawMessage `json:"final_review"`
	}
	if decode(receipt, &original, false) != nil || len(original.Review) == 0 {
		return false
	}
	var a, b any
	if decode(payload, &a, false) != nil || decode(original.Review, &b, false) != nil {
		return false
	}
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}

func taskScope(p policy) bool {
	data, err := readExact(p.db, 64<<20)
	if err != nil {
		return false
	}
	var db struct {
		Rows []json.RawMessage `json:"items"`
	}
	// Historical unselected rows can have case aliases and legacy field types.
	// Enforce case-folded uniqueness when decoding the four selected rows.
	if decodeKeys(data, &db, false, false) != nil {
		return false
	}
	seen := map[int]bool{}
	for _, raw := range db.Rows {
		var identity struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(raw, &identity) != nil {
			return false
		}
		if identity.ID != 3059 && identity.ID != 3056 && identity.ID != 3068 && identity.ID != 3089 {
			continue
		}
		var r struct {
			ID           int    `json:"id"`
			Status       string `json:"status"`
			Spec         string `json:"spec"`
			Dispatchable bool   `json:"dispatchable"`
			WaitOn       []int  `json:"wait_on"`
		}
		if decode(raw, &r, false) != nil {
			return false
		}
		if explicitStop(raw) {
			return false
		}
		if seen[r.ID] {
			return false
		}
		seen[r.ID] = true
		switch r.ID {
		case 3059:
			if !r.Dispatchable || (r.Status != "todo" && r.Status != "doing" && r.Status != "wait") || !same(r.Spec, p.parentSpec.path) {
				return false
			}
		case 3056:
			if r.Status != "wait" || len(r.WaitOn) != 1 || r.WaitOn[0] != 3059 || !same(r.Spec, p.originSpec.path) {
				return false
			}
		case 3068:
			if r.Status != "done" {
				return false
			}
		case 3089:
			if !r.Dispatchable || (r.Status != "doing" && r.Status != "wait" && r.Status != "done") || !same(r.Spec, p.scope.path) {
				return false
			}
		}
	}
	return len(seen) == 4
}

func readExact(path string, limit int64) ([]byte, error) {
	if !plainPath(path) {
		return nil, errors.New("physical identity")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return nil, errors.New("file bounds")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("file open")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) || !singleLink(f) {
		return nil, errors.New("opened identity")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || len(data) == 0 || int64(len(data)) > limit || !os.SameFile(opened, after) || !plainPath(path) || !singleLink(f) {
		return nil, errors.New("read identity")
	}
	return data, nil
}

func canonical(path string) bool {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.Contains(path, "\x00") {
		return false
	}
	return strings.EqualFold(filepath.ToSlash(path), filepath.ToSlash(filepath.Clean(path)))
}
func plainPath(path string) bool {
	if !canonical(path) {
		return false
	}
	for part := filepath.Clean(path); ; part = filepath.Dir(part) {
		info, err := os.Lstat(part)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return false
		}
		if filepath.Dir(part) == part {
			break
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && same(path, resolved)
}

func decode(data []byte, out any, strict bool) error {
	return decodeKeys(data, out, strict, true)
}

func decodeKeys(data []byte, out any, strict, foldKeys bool) error {
	// Reject duplicates before encoding/json can silently replace an authority.
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON depth")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return errors.New("JSON key")
				}
				if foldKeys {
					for prior := range seen {
						if strings.EqualFold(name, prior) {
							return errors.New("JSON key alias")
						}
					}
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("JSON end")
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("JSON end")
			}
		default:
			return errors.New("JSON delimiter")
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("JSON trailing")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if strict {
		d.DisallowUnknownFields()
	}
	return d.Decode(out)
}

func verifiedOrigin(root string) bool {
	if !plainPath(root) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin")
	prepareCommand(cmd)
	data, err := cmd.Output()
	if err != nil {
		return false
	}
	origin := strings.ToLower(strings.TrimSpace(string(data)))
	return origin == "https://github.com/autodevjapan/cc-pc-tools-rs.git" || origin == "git@github.com:autodevjapan/cc-pc-tools-rs.git"
}
