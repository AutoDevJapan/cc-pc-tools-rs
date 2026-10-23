package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Hash authored instructions and case expectations, not mutable installation,
// license-investigation or review-status metadata. Field order in source JSON
// and CRLF formatting do not change this versioned semantic projection.
type draftContent struct {
	Name        string   `json:"name"`
	Repo        string   `json:"source_repo"`
	Commit      string   `json:"source_commit"`
	Path        string   `json:"source_path"`
	SourceSHA   string   `json:"source_sha256"`
	Description string   `json:"description"`
	Positive    string   `json:"positive"`
	Negative    string   `json:"negative"`
	Guidance    []string `json:"guidance"`
	Adaptations string   `json:"adaptations"`
	Distinct    string   `json:"distinct_from_existing"`
}

type draftCase struct {
	Reason   string `json:"reason"`
	Input    string `json:"input"`
	Skill    string `json:"skill"`
	Expected string `json:"expected"`
}

func draftContentSHA(b []byte) (string, error) {
	var authored struct {
		Drafts []draftContent `json:"drafts"`
		Cases  []draftCase    `json:"static_cases"`
	}
	if err := json.Unmarshal(b, &authored); err != nil {
		return "", err
	}
	if len(authored.Drafts) == 0 || len(authored.Cases) == 0 {
		return "", errors.New("missing authored drafts or case expectations")
	}
	names := map[string]bool{}
	for _, d := range authored.Drafts {
		if d.Name == "" || names[d.Name] || d.Repo == "" || d.Commit == "" || d.Path == "" || d.SourceSHA == "" || d.Description == "" || d.Positive == "" || d.Negative == "" || len(d.Guidance) == 0 || d.Adaptations == "" || d.Distinct == "" {
			return "", errors.New("incomplete authored instruction identity")
		}
		names[d.Name] = true
	}
	for _, c := range authored.Cases {
		if !names[c.Skill] || c.Input == "" || c.Expected == "" || c.Reason == "" {
			return "", errors.New("case expectation has no authored instruction")
		}
	}
	canonical, err := json.Marshal(authored)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:]), nil
}

type latestDraftReview struct {
	Todo       int    `json:"todo"`
	Head       string `json:"head"`
	Must       *int   `json:"must"`
	SHA        string `json:"raw_sha256"`
	ContentSHA string `json:"parent_computed_authored_content_sha256"`
	Remaining  bool   `json:"remaining_independent_confirmation"`
}

func validateDraftReview(pending bool, r latestDraftReview, contentSHA string) error {
	if pending {
		return nil // Pending is explicitly not acceptance of any body version.
	}
	if r.Todo < 1 || len(r.Head) != 40 || len(r.SHA) != 64 || r.Must == nil || *r.Must != 0 || r.Remaining || len(contentSHA) != 64 || r.ContentSHA != contentSHA {
		return errors.New("draft accepted without a completed review of this content")
	}
	for _, s := range []string{r.SHA, r.Head, r.ContentSHA} {
		if _, err := hex.DecodeString(s); err != nil {
			return err
		}
	}
	return nil
}

func TestDraftContentAndReviewReceiptsReferToTheSameVersion(t *testing.T) {
	var d struct {
		Pending  bool `json:"review_pending"`
		Reviewed struct {
			Algorithm string              `json:"content_identity_algorithm"`
			SHA       string              `json:"authored_content_sha256"`
			Latest    int                 `json:"latest_review_todo"`
			Reviews   []latestDraftReview `json:"reviews"`
		} `json:"reviewed_draft"`
	}
	b := loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", &d)
	contentSHA, err := draftContentSHA(b)
	if err != nil {
		t.Fatal(err)
	}
	if d.Reviewed.Algorithm != "sql-authored-v1" || d.Reviewed.SHA != contentSHA {
		t.Fatalf("authored scope differs from recorded identity: calculated %s", contentSHA)
	}
	if len(d.Reviewed.Reviews) == 0 || d.Reviewed.Reviews[len(d.Reviewed.Reviews)-1].Todo != d.Reviewed.Latest {
		t.Fatal("latest review is not the end of append-only review history")
	}
	seen := map[int]bool{}
	for _, r := range d.Reviewed.Reviews {
		if r.Todo < 1 || seen[r.Todo] {
			t.Fatal("missing or repeated review receipt")
		}
		seen[r.Todo] = true
	}
	if err := validateDraftReview(d.Pending, d.Reviewed.Reviews[len(d.Reviewed.Reviews)-1], contentSHA); err != nil {
		t.Fatal(err)
	}
}

func TestDraftContentIdentityChangesWithInstructionsAndExpectedBehavior(t *testing.T) {
	b := loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", new(any))
	original, err := draftContentSHA(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"metadata_only", "guidance", "description", "source", "case_input", "case_expected"} {
		t.Run(kind, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			draft := m["drafts"].([]any)[0].(map[string]any)
			c := m["static_cases"].([]any)[0].(map[string]any)
			switch kind {
			case "metadata_only":
				m["review_pending"], m["reviewed_draft"], draft["installed"] = true, nil, true
			case "guidance":
				draft["guidance"].([]any)[0] = "different instruction"
			case "description":
				draft["description"] = "different trigger"
			case "source":
				draft["source_sha256"] = strings.Repeat("c", 64)
			case "case_input":
				c["input"] = "different scenario"
			case "case_expected":
				c["expected"] = "different expected behavior"
			}
			modified, err := json.MarshalIndent(m, "", "\t")
			if err != nil {
				t.Fatal(err)
			}
			got, err := draftContentSHA([]byte(strings.ReplaceAll(string(modified), "\n", "\r\n")))
			if err != nil || (got == original) != (kind == "metadata_only") {
				t.Fatalf("content-scope binding %s: %s: %v", kind, got, err)
			}
		})
	}
	for _, invalid := range []string{"{", "null", `{ "drafts": [], "static_cases": [] }`} {
		if _, err := draftContentSHA([]byte(invalid)); err == nil {
			t.Fatal("empty or malformed authored scope accepted")
		}
	}
}

func TestDraftReviewCannotCloseWithAnUnresolvedOrWrongVersionReceipt(t *testing.T) {
	zero, one := 0, 1
	contentSHA := strings.Repeat("d", 64)
	good := latestDraftReview{Todo: 1, Head: strings.Repeat("a", 40), Must: &zero, SHA: strings.Repeat("b", 64), ContentSHA: contentSHA}
	for _, kind := range []string{"valid", "missing", "must_missing", "must_one", "remaining", "todo_zero", "short_head", "nonhex_head", "short_sha", "nonhex_sha", "wrong_body", "nonhex_body"} {
		t.Run(kind, func(t *testing.T) {
			r := good
			wantSHA := contentSHA
			switch kind {
			case "missing":
				r = latestDraftReview{}
			case "must_missing":
				r.Must = nil
			case "must_one":
				r.Must = &one
			case "remaining":
				r.Remaining = true
			case "todo_zero":
				r.Todo = 0
			case "short_head":
				r.Head = "abc"
			case "nonhex_head":
				r.Head = strings.Repeat("z", 40)
			case "short_sha":
				r.SHA = "abc"
			case "nonhex_sha":
				r.SHA = strings.Repeat("z", 64)
			case "wrong_body":
				r.ContentSHA = strings.Repeat("e", 64)
			case "nonhex_body":
				r.ContentSHA, wantSHA = strings.Repeat("z", 64), strings.Repeat("z", 64)
			}
			err := validateDraftReview(false, r, wantSHA)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("review version acceptance %s: %v", kind, err)
			}
		})
	}
	if err := validateDraftReview(true, latestDraftReview{}, contentSHA); err != nil {
		t.Fatal("pending draft should remain explicitly unaccepted", err)
	}
}
