package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerRule4088PlannedAndLinkedWorktrees(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(config.dev, "ADJ", "self-fixture")
	if os.MkdirAll(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/example.git")
	currentFixtureGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	projects := object{"self-fixture": root}
	planned := filepath.Join(root+"-wt", "todo-1-fixture")
	if v := ownerRuleWith(context.Background(), planned, projects, access, config, nil); !v.AccessAllowed || v.Basis != "verified_planned_worktree" {
		t.Fatal("planned self worktree denied")
	}
	if v := ownerRuleWith(context.Background(), filepath.Join(planned, "deeper"), projects, access, config, nil); v.AccessAllowed {
		t.Fatal("deeper uncreated worktree admitted")
	}
	if v := ownerRuleWith(context.Background(), planned, object{"contracted-fixture": root}, access, config, nil); v.Status != "protected" {
		t.Fatal("planned client worktree admitted")
	}
	currentFixtureGit(t, root, "worktree", "add", "-qb", "fixture-branch", planned)
	if !ownerRuleWith(context.Background(), planned, projects, access, config, nil).AccessAllowed {
		t.Fatal("same linked worktree denied")
	}
	ordinary := filepath.Join(root+"-wt", "existing-unrelated")
	if os.Mkdir(ordinary, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if ownerRuleWith(context.Background(), ordinary, projects, access, config, nil).AccessAllowed {
		t.Fatal("existing unrelated worktree admitted")
	}
	nonGit := filepath.Join(config.dev, "BM1314", "plain-registration")
	if os.MkdirAll(nonGit, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if !ownerRuleWith(context.Background(), filepath.Join(nonGit+"-wt", "ordinary"), object{"self-fixture": nonGit}, access, config, nil).AccessAllowed {
		t.Fatal("non-Git registered work area required Git identity")
	}
}

func TestOwnerRule4088DepartmentBranchesAndBoundary(t *testing.T) {
	config, access := ownerRuleFixture(t)
	client := filepath.Join(config.dev, "ADJ", "contracted-fixture", "child")
	if os.MkdirAll(client, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if v := ownerRuleWith(context.Background(), client, object{}, access, config, nil); v.Basis != "client_department" || v.Status != "protected" {
		t.Fatal("unregistered client product not recognized")
	}
	root := filepath.Join(config.dev, "misc", "ordinary")
	if os.MkdirAll(filepath.Join(root, "data", "contracted-fixture"), 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if !ownerRuleWith(context.Background(), filepath.Join(root, "data", "contracted-fixture"), object{}, access, config, nil).AccessAllowed {
		t.Fatal("deep data directory acquired product identity")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/contracted-fixture.git")
	if v := ownerRuleWith(context.Background(), root, object{}, access, config, nil); v.Basis != "client_department" || v.Status != "protected" {
		t.Fatal("repository client membership branch not recognized")
	}
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/OtherOwner/example.git")
	if v := ownerRuleWith(context.Background(), root, object{"self-fixture": root}, access, config, nil); v.Basis != "owner_location_conflict" {
		t.Fatal("location conflict classified as client fact")
	}
}

func TestOwnerRule4088ReceiptFallbackIsFiniteAndTyped(t *testing.T) {
	for _, basis := range []string{"registration_snapshot_unavailable", "registration_snapshot_changed", "classification_deadline", "repository_identity_unavailable", "repository_metadata_unavailable", "owner_rule_unattributed_location"} {
		calls := 0
		v := ownerRuleReceiptFallback(ownerRuleUnknown(basis), "fixture", func(string) bool { calls++; return true })
		if !v.AccessAllowed || v.Basis != "verified_operational_receipt" || calls != 1 {
			t.Fatal("independent receipt lost to transient classification")
		}
		if ownerRuleReceiptFallback(ownerRuleUnknown(basis), "fixture", func(string) bool { return false }).AccessAllowed {
			t.Fatal("failed receipt became classification")
		}
	}
	for _, basis := range []string{"client_registration", "client_department", "client_repository_id", "owner_rule_other_development_top", "owner_location_conflict", "stored_record_origin_unverified", "physical_identity_mismatch"} {
		v := ownerRuleReceiptFallback(ownerRuleUnknown(basis), "fixture", func(string) bool { t.Fatal("positive protection/conflict reached receipt fallback"); return true })
		if v.AccessAllowed {
			t.Fatal("positive protection/conflict overridden")
		}
	}
}

func TestOwnerRule4088MixedRecordsUseProducerMetadata(t *testing.T) {
	config, access := ownerRuleFixture(t)
	config.jobStore = filepath.Join(config.infrastructure[0], "jobs")
	config.specStore = filepath.Join(config.infrastructure[0], "specs")
	config.promptStore = filepath.Join(config.infrastructure[0], "prompts")
	config.todoStore = filepath.Join(config.infrastructure[0], "registration.json")
	job := "20261027-fixture-todo-42"
	jobRoot := filepath.Join(config.jobStore, job)
	root := filepath.Join(config.dev, "ADJ", "plain-self")
	for _, dir := range []string{jobRoot, root, config.specStore, config.promptStore} {
		if os.MkdirAll(dir, 0700) != nil {
			t.Fatal("fixture mkdir")
		}
	}
	write := func(path, body string) {
		t.Helper()
		if os.WriteFile(path, []byte(body), 0600) != nil {
			t.Fatal("fixture metadata")
		}
	}
	// Use forward slashes for JSON fixture metadata; no real client data exists.
	cwd := filepath.ToSlash(root)
	write(filepath.Join(jobRoot, "job.json"), `{"id":"`+job+`","project":"self-fixture","cwd":"`+cwd+`","prompt":"ignored body","work_owner":{"repository":"AutoDevJapan/example","worktree":"`+cwd+`"}}`)
	path := filepath.Join(jobRoot, "last_message.md")
	if v := ownerRuleWith(context.Background(), path, object{}, access, config, nil); !v.AccessAllowed || v.Basis != "verified_self_record_producer" {
		t.Fatal("self producer record denied")
	}
	write(filepath.Join(jobRoot, "job.json"), `{"id":"`+job+`","project":"contracted-fixture","cwd":"`+cwd+`"}`)
	if v := ownerRuleWith(context.Background(), path, object{}, access, config, nil); v.Status != "protected" || v.Basis != "client_record_producer" {
		t.Fatal("client record inherited infrastructure classification")
	}
	write(filepath.Join(jobRoot, "job.json"), `{"id":"wrong","project":"self-fixture","cwd":"`+cwd+`"}`)
	if ownerRuleWith(context.Background(), path, object{}, access, config, nil).AccessAllowed {
		t.Fatal("unbound producer metadata admitted")
	}
	spec := filepath.Join(config.specStore, "42-fixture.md")
	prompt := filepath.Join(config.promptStore, "42-generation.md")
	write(config.todoStore, `{"items":[{"id":42,"project":"self-fixture","spec":"`+filepath.ToSlash(spec)+`","scheduler":{"prompt":"`+filepath.ToSlash(prompt)+`"},"note":"ignored private body"}]}`)
	for _, p := range []string{spec, prompt} {
		if !ownerRuleWith(context.Background(), p, object{"self-fixture": root}, access, config, nil).AccessAllowed {
			t.Fatal("registered self input denied")
		}
	}
	write(config.todoStore, `{"items":[{"id":42,"project":"contracted-fixture","spec":"`+filepath.ToSlash(spec)+`"}]}`)
	if v := ownerRuleWith(context.Background(), spec, object{"contracted-fixture": root}, access, config, nil); v.Status != "protected" {
		t.Fatal("client input inherited infrastructure classification")
	}
	if ownerRuleWith(context.Background(), filepath.Join(config.specStore, "43-other.md"), object{}, access, config, nil).AccessAllowed {
		t.Fatal("unregistered input admitted")
	}
	config.mixedStores = []string{filepath.Join(config.infrastructure[0], "conversations")}
	if v := ownerRuleWith(context.Background(), filepath.Join(config.mixedStores[0], "reply.json"), object{}, access, config, nil); v.Status != "protected" {
		t.Fatal("mixed client conversation store admitted")
	}
}

func TestOwnerRule4088MetadataProjectionSkipsBodiesAndRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{`{"items":[{"id":1,"project":"self","note":"ignored","spec":"fixture"}]}`, `{"items":[{"note":{"nested":[1,true]},"id":1,"project":"self","spec_history":[{"spec":"old"}],"spec":"fixture"}]}`} {
		m, ok := ownerTaskRecordProjection(ctx, strings.NewReader(body), "1")
		if !ok || m.project != "self" || m.spec != "fixture" {
			t.Fatal("metadata projection differs")
		}
	}
	for _, body := range []string{`{"items":[{"id":1,"project":"self","project":"client"}]}`, `{"items":[{"id":1},{"id":1}]}`, `{"items":[{"id":1,"note":[}]}`, `{"items":[{"id":2}]}`} {
		if _, ok := ownerTaskRecordProjection(ctx, strings.NewReader(body), "1"); ok {
			t.Fatal("invalid or ambiguous metadata accepted")
		}
	}
}

func TestOwnerRule4088HardLinkAndEnvironment(t *testing.T) {
	base := currentShortFixture(t)
	a, b := filepath.Join(base, "source.txt"), filepath.Join(base, "alias.txt")
	if os.WriteFile(a, []byte("synthetic"), 0600) != nil || os.Link(a, b) != nil {
		t.Fatal("fixture links")
	}
	if _, err := ownerRulePhysicalPath(a); err == nil {
		t.Fatal("multiply named file admitted")
	}
	before := ownerDefaultRuleConfig().tools[0].root
	t.Setenv("USERPROFILE", "")
	t.Setenv("APPDATA", "")
	if after := ownerDefaultRuleConfig().tools[0].root; !filepath.IsAbs(after) || !same(before, after) {
		t.Fatal("consumer environment changed tool ownership")
	}
}

func TestOwnerRule4088LegacyRetirementKeepsPurposeQualification(t *testing.T) {
	v := ownerRuleRetirementVerdict(ownerRuleProtected("owner_rule_unattributed_location"), "ordinary", true)
	if !v.AccessAllowed || v.Basis != "verified_retirement_receipt" {
		t.Fatal("independent old retirement receipt lost qualification")
	}
	for _, basis := range []string{"client_record_producer", "client_department", "client_registration", "client_repository_id", "physical_identity_mismatch", "windows_alias", "owner_location_conflict"} {
		if ownerRuleRetirementVerdict(ownerRuleUnknown(basis), "ordinary", true).AccessAllowed {
			t.Fatal("retirement erased protection evidence")
		}
	}
	if ownerRuleRetirementVerdict(ownerRuleUnknown(""), "credit-cards.json", true).AccessAllowed {
		t.Fatal("retirement erased card candidate")
	}
}
