package inputqual

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// These are independent operational receipts for the answered original scope.
// Locators confer no authority and none of the former rejected inputs is read.
var dependencyLocations = map[string]struct{ pathSHA, contentSHA string }{
	"owner_reply": {"3a4f44adb24e2ba860cdcee45934b97d8a8b055be92635675f0ccb7b8ef9fc8b", "52f5c04cd3568137a3c3d22ba25908431308f367335c22f560275414b0ba284d"},
	"source":      {"5dea3b2168b0890424b2447f9299b88b39021c23ba0887edb4bcd427a04e12ce", "fb9aab3caa2f0cb25565993d53cee27f0c82cf7637b0f0c719c6f06a90151125"},
	"review":      {"71c7aa1ca3f10f3bafad5ece91e1794d7e6c47d5914350dadb2714116642eaa4", "2e0eebe9231960a213503e7afb7325ca155cac70e8b27c3ea658e9ce85dced45"},
}

// ConstructDependency returns only the newly reviewed dependency's evidence.
// The caller must also obtain the unchanged original995 Snapshot separately.
func ConstructDependency() (Snapshot, error) {
	exe, err := os.Executable()
	if err != nil {
		return Snapshot{}, rejected("dependency_locator")
	}
	data, err := readExact(filepath.Join(filepath.Dir(exe), "batch-input-3059.json"), 32<<10)
	if err != nil {
		return Snapshot{}, rejected("dependency_locator")
	}
	var refs map[string]string
	if decode(data, &refs, true) != nil || len(refs) != len(dependencyLocations) {
		return Snapshot{}, rejected("dependency_locator_schema")
	}
	return readDependencyAnchors(refs, dependencyLocations, dependencyApproval)
}

func readDependencyAnchors(refs map[string]string, locations map[string]struct{ pathSHA, contentSHA string }, identity dependencyIdentity) (Snapshot, error) {
	if len(refs) != len(locations) {
		return Snapshot{}, rejected("dependency_locator_schema")
	}
	checked := map[string][]byte{}
	for role, want := range locations {
		path := refs[role]
		if locationDigest(path) != want.pathSHA {
			return Snapshot{}, rejected("dependency_location")
		}
		bytes, err := readExact(path, 1<<20)
		if err != nil || hash(bytes) != want.contentSHA {
			return Snapshot{}, rejected("dependency_" + role + "_authority")
		}
		checked[role] = bytes
	}
	return dependencySnapshotWithIdentity(checked, identity)
}

type dependencyIdentity struct{ Author, Message, Question, Request, Text string }

var dependencyApproval = dependencyIdentity{Author: "20d3bdbe05b1cada8fe78544f42297d7e4a43cae6fa33293a51cedeceafa86ef", Message: "d81a943acd7b39d1b349f9942c5be9a19e50f3d6b30ab589b3d515bd93451b0c", Question: "5d80ff4e65599308ce4e8da50cc71614bf8cd4967946742ec0c0470793c8657c", Request: "228aef6a9cc6b0ca4fda2a424a2c6d2dae9868713a6d6fa0948a53525a91dfb8", Text: "245e054e568823eae46fbbfccf2e773cfc2531b3f5a0a338888ad4bcab5d0ae8"}

func dependencySnapshot(checked map[string][]byte) (Snapshot, error) {
	return dependencySnapshotWithIdentity(checked, dependencyApproval)
}
func dependencySnapshotWithIdentity(checked map[string][]byte, identity dependencyIdentity) (Snapshot, error) {
	var raw struct {
		Message string `json:"message_id"`
		Author  string `json:"author_id"`
		Source  string `json:"source"`
		Text    string `json:"text"`
		Context string `json:"question_context"`
	}
	if decode(checked["owner_reply"], &raw, false) != nil {
		return Snapshot{}, rejected("dependency_owner")
	}
	var context struct {
		Request     string `json:"request_id"`
		Question    string `json:"question_message_id"`
		Fingerprint string `json:"question_fingerprint"`
		Decision    struct {
			Todo  int `json:"todo_id"`
			Scope struct {
				Action string `json:"action"`
				Head   string `json:"additional_pr939_head"`
			} `json:"scope"`
		} `json:"original_decision"`
	}
	if hash([]byte(raw.Message)) != identity.Message || hash([]byte(raw.Author)) != identity.Author || raw.Source != "discord-api-v10" || hash([]byte(raw.Text)) != identity.Text || decode([]byte(raw.Context), &context, false) != nil || hash([]byte(context.Request)) != identity.Request || hash([]byte(context.Question)) != identity.Question || context.Decision.Todo != 3059 || context.Decision.Scope.Action != "extend-finite-ci-execution-scope-to-original-pr939" || context.Decision.Scope.Head != "f1c6e235d93d0719532b592b03037edcbe7bffef" {
		return Snapshot{}, rejected("dependency_owner_scope")
	}
	var source struct {
		Domain, Source, Owner, Head, Tree, Base, Context string
		Repo                                             int64 `json:"repo_id"`
		PR                                               int
		Run                                              int64
		Attempt                                          int
		Job                                              int64
		Workflow                                         string `json:"workflow_sha256"`
		Complete                                         bool   `json:"all_patches_complete"`
		Files                                            []struct{ Filename, Patch string }
	}
	if decode(checked["source"], &source, false) != nil || source.Domain != "original-3059-pr939-source-review-bundle-v1" || source.Source != "github-rest-api" || source.Owner != "AutoDevJapan" || source.Repo != 1285649124 || source.PR != 939 || source.Head != "f1c6e235d93d0719532b592b03037edcbe7bffef" || source.Tree != "df13e3dd2d796812f922868578df2d0ef8beceb5" || source.Base != fixedBase || source.Context != "de71ff5d9a1d5a61328cbcf054347f9686ad7b8e" || source.Run != 39344575918 || source.Attempt != 2 || source.Job != 118550301356 || source.Workflow != "0f9b993f4d00de86a94ad10432123f473daae1c5dfc9d3412149524f5739e273" || !source.Complete || len(source.Files) != 4 || len(checked["review"]) == 0 {
		return Snapshot{}, rejected("dependency_source")
	}
	wantFiles := map[string]bool{"integrations/arps-atrs/package.json": true, "package.json": true, "packages/shared-contracts/package.json": true, "pnpm-lock.yaml": true}
	for _, file := range source.Files {
		if !wantFiles[file.Filename] || file.Patch == "" {
			return Snapshot{}, rejected("dependency_files")
		}
		delete(wantFiles, file.Filename)
	}
	type evidence struct {
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
	}
	makeEvidence := func(kind string, original []byte) []byte {
		v, _ := json.Marshal(evidence{3059, source.Repo, source.PR, source.Head, source.Tree, kind, true, 0, hash(original), original})
		return v
	}
	review, acceptance := makeEvidence("review", checked["review"]), makeEvidence("acceptance", checked["source"])
	pin, _ := json.Marshal(struct {
		Repo          int64   `json:"repo_id"`
		PR            int     `json:"pr"`
		Head          string  `json:"head_sha"`
		Tree          string  `json:"head_tree"`
		Base          string  `json:"base_sha"`
		Run           int64   `json:"run_id"`
		Attempt       int     `json:"attempt"`
		Jobs          []int64 `json:"job_ids"`
		Kind          string  `json:"kind"`
		Context       string  `json:"context_sha"`
		ReviewSHA     string  `json:"review_sha256"`
		AcceptanceSHA string  `json:"acceptance_sha256"`
		ReviewRef     string  `json:"review_file"`
		AcceptanceRef string  `json:"acceptance_file"`
	}{source.Repo, source.PR, source.Head, source.Tree, source.Base, source.Run, source.Attempt, []int64{source.Job}, "secret", source.Context, hash(review), hash(acceptance), "independent-fixed-source-review", "independent-fixed-source-read-acceptance"})
	return Snapshot{pin: pin, review: review, acceptance: acceptance, report: Report{Qualified: true, Purpose: "original-3059-approved-dependency-secret-batch", Task: 3059, OriginTask: 3059, RepoID: source.Repo, PR: source.PR, Head: source.Head, Tree: source.Tree, Run: source.Run, Job: source.Job, PinSHA: hash(pin), ReviewSHA: hash(review), AcceptanceSHA: hash(acceptance), IndependentReceiptSHA: hash(checked["owner_reply"]), GeneralClassificationUnverified: true, InputConstruction: "official-helper-from-independent-owner-source-and-review", ConstructedInputCreator: "official-product-ci-helper", ReviewRepresentation: "original source reviewer response", BaseAuthority: "fixed original dependency source", ContextAuthority: "independent API snapshot and reviewed workflow; requires live match"}}, nil
}
