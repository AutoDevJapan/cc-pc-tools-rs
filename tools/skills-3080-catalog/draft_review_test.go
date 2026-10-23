package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
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
	Resolves   int    `json:"resolves_todo"`
	BodyAgain  *bool  `json:"body_reaccepted"`
}

// Anchors are independent of the mutable receipt JSON. A new body needs its
// own independently confirmed response; adding an anchor does not replace the
// old body-review/display-resolution history.
const sqlReviewedAuthoredSHA = "119786b834b0114244e413de9633f49fbda9e66815c626831d0818b27f10beb2"

type draftReviewAnchor struct {
	BodyTodo       int
	BodyHead       string
	BodySHA        string
	BodyMust       int
	ResolutionTodo int
	ResolutionHead string
	ResolutionSHA  string
}

var sqlDraftReviewAnchors = map[string]draftReviewAnchor{
	sqlReviewedAuthoredSHA: {
		BodyTodo: 3368, BodyHead: "b0107b649d5f6a890742aaad926f1b0116018f58",
		BodySHA: "c0f40d79b4386bd5ce7a7d11101e23dbd56d5013b6614d56bfd1caf756ada973", BodyMust: 1,
		ResolutionTodo: 3378, ResolutionHead: "f817c1841abffa97b9be8195c7eb3a5ffc151471",
		ResolutionSHA: "f5925ada6f6042b1e6f16da5b711790310cdbd03f78240fd486367a902260298",
	},
}

func validateDraftReviewHistory(pending bool, reviews []latestDraftReview, latest int, contentSHA string) error {
	return validateAnchoredDraftReviewHistory(pending, reviews, latest, contentSHA, sqlDraftReviewAnchors)
}

func validateAnchoredDraftReviewHistory(pending bool, reviews []latestDraftReview, latest int, contentSHA string, anchors map[string]draftReviewAnchor) error {
	if len(reviews) == 0 || reviews[len(reviews)-1].Todo != latest {
		return errors.New("latest review is not the end of append-only review history")
	}
	seen := map[int]latestDraftReview{}
	for _, r := range reviews {
		if r.Todo < 1 || seen[r.Todo].Todo != 0 {
			return errors.New("missing or repeated review receipt")
		}
		seen[r.Todo] = r
	}
	if pending {
		return nil
	}
	anchor, ok := anchors[contentSHA]
	if !ok {
		return errors.New("authored content has no independently reviewed body anchor")
	}
	r := reviews[len(reviews)-1]
	if err := validateDraftReview(false, r, contentSHA); err != nil {
		return err
	}
	body, ok := seen[anchor.BodyTodo]
	if !ok || body.Head != anchor.BodyHead || body.SHA != anchor.BodySHA || body.Must == nil || *body.Must != anchor.BodyMust || body.ContentSHA != contentSHA {
		return errors.New("body-review identity differs from its independent anchor")
	}
	if anchor.ResolutionTodo == 0 {
		if anchor.BodyMust != 0 || r.Todo != body.Todo || r.Resolves != 0 {
			return errors.New("accepted body lacks its anchored completed review")
		}
		return nil
	}
	// Look up the resolved body by identity, not its position in the history.
	if r.Todo != anchor.ResolutionTodo || r.Head != anchor.ResolutionHead || r.SHA != anchor.ResolutionSHA || r.Resolves != body.Todo || r.BodyAgain == nil || *r.BodyAgain || anchor.BodyMust < 1 || !body.Remaining {
		return errors.New("accepted draft lacks its anchored body-review and display-resolution pair")
	}
	return nil
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
	if err := validateDraftReviewHistory(d.Pending, d.Reviewed.Reviews, d.Reviewed.Latest, contentSHA); err != nil {
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

func TestEveryAuthoredFieldAndArrayElementAffectsContentIdentity(t *testing.T) {
	b := loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", new(any))
	original, err := draftContentSHA(b)
	if err != nil {
		t.Fatal(err)
	}
	var initial struct {
		Drafts []draftContent `json:"drafts"`
		Cases  []draftCase    `json:"static_cases"`
	}
	if err := json.Unmarshal(b, &initial); err != nil {
		t.Fatal(err)
	}
	check := func(name string, mutate func(map[string]any), wantSame bool) {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			mutate(m)
			modified, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			got, err := draftContentSHA(modified)
			if err != nil || (got == original) != wantSame {
				t.Fatalf("authored field coverage: same=%v: %v", got == original, err)
			}
		})
	}
	for i := range initial.Drafts {
		for _, fieldInfo := range reflect.VisibleFields(reflect.TypeOf(draftContent{})) {
			field := fieldInfo.Tag.Get("json")
			if field == "guidance" {
				continue // Every guidance element and its order are checked below.
			}
			check("draft_"+initial.Drafts[i].Name+"_"+field, func(m map[string]any) {
				d := m["drafts"].([]any)[i].(map[string]any)
				old := d[field].(string)
				d[field] = old + " changed"
				if field == "name" {
					for _, c := range m["static_cases"].([]any) {
						row := c.(map[string]any)
						if row["skill"] == old {
							row["skill"] = d[field]
						}
					}
				}
			}, false)
		}
		for j := range initial.Drafts[i].Guidance {
			check("guidance_"+initial.Drafts[i].Name+"_"+initial.Drafts[i].Guidance[j], func(m map[string]any) {
				m["drafts"].([]any)[i].(map[string]any)["guidance"].([]any)[j] = "changed instruction"
			}, false)
		}
		check("guidance_order_"+initial.Drafts[i].Name, func(m map[string]any) {
			a := m["drafts"].([]any)[i].(map[string]any)["guidance"].([]any)
			a[0], a[len(a)-1] = a[len(a)-1], a[0]
		}, false)
	}
	for i := range initial.Cases {
		for _, fieldInfo := range reflect.VisibleFields(reflect.TypeOf(draftCase{})) {
			field := fieldInfo.Tag.Get("json")
			check("case_"+initial.Cases[i].Input+"_"+field, func(m map[string]any) {
				c := m["static_cases"].([]any)[i].(map[string]any)
				if field == "skill" {
					for _, d := range initial.Drafts {
						if d.Name != c[field] {
							c[field] = d.Name
							break
						}
					}
				} else {
					c[field] = c[field].(string) + " changed"
				}
			}, false)
		}
	}
	for _, field := range []string{"drafts", "static_cases"} {
		check(field+"_order", func(m map[string]any) {
			a := m[field].([]any)
			a[0], a[len(a)-1] = a[len(a)-1], a[0]
		}, false)
	}
	check("metadata_outside_authored_projection", func(m map[string]any) {
		for _, v := range m["drafts"].([]any) {
			d := v.(map[string]any)
			for _, field := range []string{"inventory_id", "source_url", "retrieved_at", "status", "installed", "individual_license_review", "renderable_install_plan", "source_original"} {
				d[field] = "metadata changed"
			}
		}
		for _, v := range m["static_cases"].([]any) {
			v.(map[string]any)["real_model_turn"] = true
		}
		m["static_evaluation"].(map[string]any)["real_model_turn"] = true
	}, true)
}

func TestAcceptedSQLDraftRequiresItsHistoricalBodyReviewAndResolution(t *testing.T) {
	var d struct {
		Reviewed struct {
			Latest  int                 `json:"latest_review_todo"`
			Reviews []latestDraftReview `json:"reviews"`
		} `json:"reviewed_draft"`
	}
	loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", &d)
	for _, kind := range []string{"valid", "body_removed", "body_digest_changed", "body_receipt_changed", "resolution_head_changed", "resolution_receipt_changed", "resolves_missing", "resolves_unknown", "body_reaccepted_missing", "body_reaccepted_true", "body_must_erased", "body_relabelled_resolved", "digest_rewritten_with_body", "latest_missing", "duplicate_receipt"} {
		t.Run(kind, func(t *testing.T) {
			reviews := append([]latestDraftReview(nil), d.Reviewed.Reviews...)
			latest := d.Reviewed.Latest
			contentSHA := sqlReviewedAuthoredSHA
			last := len(reviews) - 1
			switch kind {
			case "body_removed":
				reviews = append(reviews[:1], reviews[2:]...)
			case "body_digest_changed":
				reviews[1].ContentSHA = strings.Repeat("c", 64)
			case "body_receipt_changed":
				reviews[1].SHA = strings.Repeat("c", 64)
			case "resolution_head_changed":
				reviews[last].Head = strings.Repeat("c", 40)
			case "resolution_receipt_changed":
				reviews[last].SHA = strings.Repeat("c", 64)
			case "resolves_missing":
				reviews[last].Resolves = 0
			case "resolves_unknown":
				reviews[last].Resolves = 9999
			case "body_reaccepted_missing":
				reviews[last].BodyAgain = nil
			case "body_reaccepted_true":
				v := true
				reviews[last].BodyAgain = &v
			case "body_must_erased":
				v := 0
				reviews[1].Must = &v
			case "body_relabelled_resolved":
				reviews[1].Remaining = false
			case "digest_rewritten_with_body":
				contentSHA = strings.Repeat("c", 64)
				reviews[1].ContentSHA, reviews[last].ContentSHA = contentSHA, contentSHA
			case "latest_missing":
				latest = 9999
			case "duplicate_receipt":
				reviews = append([]latestDraftReview{reviews[0]}, reviews...)
			}
			err := validateDraftReviewHistory(false, reviews, latest, contentSHA)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("body review/resolution conservation %s: %v", kind, err)
			}
		})
	}
}

func TestNewBodyVersionCanUseItsOwnIndependentReviewAnchor(t *testing.T) {
	zero := 0
	contentSHA := strings.Repeat("d", 64)
	// Synthetic evidence only: no new real SQL body has been accepted.
	response := latestDraftReview{Todo: 9001, Head: strings.Repeat("a", 40), SHA: strings.Repeat("b", 64), Must: &zero, ContentSHA: contentSHA}
	anchors := make(map[string]draftReviewAnchor)
	for sha, a := range sqlDraftReviewAnchors {
		anchors[sha] = a
	}
	anchors[contentSHA] = draftReviewAnchor{BodyTodo: response.Todo, BodyHead: response.Head, BodySHA: response.SHA}
	for _, kind := range []string{"valid_new_version", "unanchored", "old_review_relabelled", "new_unresolved_must"} {
		t.Run(kind, func(t *testing.T) {
			r := response
			selected := anchors
			switch kind {
			case "unanchored":
				selected = sqlDraftReviewAnchors
			case "old_review_relabelled":
				r.Head = sqlDraftReviewAnchors[sqlReviewedAuthoredSHA].BodyHead
			case "new_unresolved_must":
				one := 1
				r.Must = &one
			}
			err := validateAnchoredDraftReviewHistory(false, []latestDraftReview{r}, r.Todo, contentSHA, selected)
			if (err != nil) != (kind != "valid_new_version") {
				t.Fatalf("new independent body review: %v", err)
			}
		})
	}
}

func TestSQLDraftKeysAreExplicitlyAuthoredOrOperationalMetadata(t *testing.T) {
	var m map[string]any
	loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", &m)
	check := func(label string, values map[string]any, projected reflect.Type, metadata []string) {
		t.Helper()
		known := map[string]bool{}
		if projected != nil {
			for _, f := range reflect.VisibleFields(projected) {
				known[f.Tag.Get("json")] = true
			}
		}
		for _, k := range metadata {
			if known[k] {
				t.Fatalf("%s: %s is both authored and metadata", label, k)
			}
			known[k] = true
		}
		for k := range values {
			if !known[k] {
				t.Errorf("%s has unclassified key %s; decide its authored identity before use", label, k)
			}
		}
	}
	check("root", m, nil, []string{"todo", "source", "commit", "tree_sha", "status", "not_scope_limit", "drafts", "drafts_count", "installed", "integrated", "excluded", "renderable_plans_added", "source_scripts_executed", "model_turn_tested", "review_pending", "next", "static_cases", "static_evaluation", "usage_evidence", "reviewed_draft", "review_pending_definition"})
	for _, d := range m["drafts"].([]any) {
		check("draft", d.(map[string]any), reflect.TypeOf(draftContent{}), []string{"inventory_id", "source_url", "retrieved_at", "status", "installed", "renderable_install_plan", "individual_license_review", "source_original"})
	}
	for _, c := range m["static_cases"].([]any) {
		check("case", c.(map[string]any), reflect.TypeOf(draftCase{}), []string{"real_model_turn"})
	}
	check("static_evaluation", m["static_evaluation"].(map[string]any), nil, []string{"cases", "positive", "method", "negative_or_boundary", "real_model_turn"})
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
