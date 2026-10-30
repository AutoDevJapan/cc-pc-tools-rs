package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080PublicDataIsBoundToPrivateCreatorAndOriginalAuthor(t *testing.T) {
	m, ok := skillsParseManifest(skillsPublicWireFixture())
	if !ok {
		t.Fatal("public data")
	}
	creator := skillsTestManifest().Writer
	author := creator
	author.Job = "20261024-221000-todo-3080"
	bound, ok := skillsBindPublicManifest(m, creator, author)
	if !ok || bound.Writer != creator || bound.proposalAuthor != author || bound.Decision != skillsDecisionSHA {
		t.Fatal("creator and original author lost")
	}
	b, _ := json.Marshal(bound)
	if strings.Contains(string(b), creator.Job) || strings.Contains(string(b), author.Job) || strings.Contains(string(b), skillsDecisionSHA) {
		t.Fatal("private runtime binding leaked into public serialization")
	}
	s := skillsFS{blocked: func(string) bool { return false }}
	defer s.close()
	if n, reason := skillsCreate(m, skillsFixtureRoot(t), &s); n != 0 || reason != "writer" {
		t.Fatal("unbound public data wrote files")
	}
	bad := author
	bad.Task = 3686
	if _, ok := skillsBindPublicManifest(m, creator, bad); ok {
		t.Fatal("different role adopted as author")
	}
}

func TestSkills3080LegacyManifestRetainsItsExactPrivateBinding(t *testing.T) {
	m := skillsTestManifest()
	wrong := m.Writer
	wrong.Job = "20261024-221000-todo-3080"
	if _, ok := skillsBindPublicManifest(m, m.Writer, wrong); ok {
		t.Fatal("legacy author rewritten")
	}
	b, _ := json.Marshal(m)
	parsed, ok := skillsParseManifest(b)
	if !ok || parsed.Writer != m.Writer || parsed.Decision != m.Decision {
		t.Fatal("legacy record altered")
	}
}

func TestSkills3080PublicCreateNewKeepsPrivateAuthorAndSameBuffers(t *testing.T) {
	raw := skillsPublicWireFixture()
	m, ok := skillsParseManifest(raw)
	if !ok {
		t.Fatal("public input")
	}
	creator := skillsTestManifest().Writer
	author := creator
	author.Job = "20261024-221000-todo-3080"
	m, ok = skillsBindPublicManifest(m, creator, author)
	if !ok {
		t.Fatal("normal private binding")
	}
	r := skillsRequest{Domain: "skills-3080-operation-v1", Writer: creator, ManifestCommit: strings.Repeat("a", 40), ManifestPath: "docs/skills-3080-operations/synthetic-public.json", ManifestSHA: hashBytes(raw), Review: &skillsResultRequest{Task: 3800, Job: "20261025-100100-todo-3800", SpecSHA: strings.Repeat("c", 64)}}
	producer := skillsProducer{strings.Repeat("d", 64), strings.Repeat("e", 40)}
	requestSHA := strings.Repeat("f", 64)
	root := skillsFixtureRoot(t)
	createdFS := skillsFS{blocked: func(string) bool { return false }}
	if n, why := skillsCreate(m, root, &createdFS); why != "" || n != len(m.Files) {
		createdFS.close()
		t.Fatalf("CreateNew %d/%s", n, why)
	}
	e, ok := skillsIssueCreated(r, requestSHA, m, root, &createdFS, producer)
	createdFS.close()
	if !ok || e.ManifestAuthor == nil || *e.ManifestAuthor != author || e.Writer != creator || e.DecisionSHA != skillsDecisionSHA || !skillsCreatedBound(e, r, requestSHA, m, producer) {
		t.Fatal("actual creation lost private identity")
	}
	consumerFS := skillsFS{blocked: func(string) bool { return false }}
	defer consumerFS.close()
	c := skillsConsumeRequest{Creator: creator, RequestSHA: requestSHA}
	checks := skillsCreatedChecks{installed: func(*skillsFS) (skillsProducer, bool) { return producer, true }, native: func(*skillsFS, object, skillsConsumeRequest) (skillsCreatedEvidence, bool) { return e, true }, manifest: func(skillsCreatedEvidence, skillsRequest, string, skillsProducer) ([]byte, bool) { return raw, true }, sources: func(skillsManifest, *skillsFS) bool { return true }}
	bound, proof, current, stage := skillsCreatedProvenanceWith(r, c, nil, &consumerFS, checks)
	if stage != "" || bound.Writer != creator || bound.proposalAuthor != author {
		t.Fatalf("private original creation rebind failed: %s", stage)
	}
	result, why := skillsConsumeAcceptedCurrent(bound, proof, root, &consumerFS, []int{0}, current)
	if why != "" || result["same_read_buffer_consumed"] != true || result["files"].([]object)[0]["content_utf8"] != string(m.Files[0].body) {
		t.Fatal("current original object/buffer lost")
	}
	wrong := m
	wrong.proposalAuthor.Job = "20261024-220000-todo-3080"
	if skillsCreatedBound(e, r, requestSHA, wrong, producer) {
		t.Fatal("different original author adopted")
	}
	missing := e
	missing.ManifestAuthor = nil
	checks.native = func(*skillsFS, object, skillsConsumeRequest) (skillsCreatedEvidence, bool) { return missing, true }
	if _, _, _, stage := skillsCreatedProvenanceWith(r, c, nil, &consumerFS, checks); stage == "" {
		t.Fatal("missing private author proof accepted")
	}
}
