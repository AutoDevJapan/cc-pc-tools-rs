package main

import (
	"strings"
	"testing"
)

func TestSkills3080AcceptedPR525IssuerSurvivesLaterRuntime(t *testing.T) {
	_, r, e, _ := skillsForwardFixture(t)
	e.Producer = skillsProducer{"ccb5e797e6b5627d98ade2a562f2f98f3c121a5403e1523071afc07f73af4556", "038bc2570014132321919bf6758d68979506b741"}
	current := skillsProducer{strings.Repeat("1", 64), strings.Repeat("2", 40)}
	if !skillsCreationHeaderBound(e, r, e.RequestSHA, current) {
		t.Fatal("independently accepted PR525 native issuer was lost after runtime update")
	}
	e.Producer.Revision = strings.Repeat("3", 40)
	if skillsCreationHeaderBound(e, r, e.RequestSHA, current) {
		t.Fatal("same digest with another revision accepted")
	}
}

func TestSkills3080LaterOfficialRuntimeRemainsAnExactSoftwarePair(t *testing.T) {
	p := skillsProducer{"f3927b8850258098d54dae87196ae382960dc83a5b6e7dd11354ab97ffd07cdf", "f5f846a8470b544846e4c9ce43306061bf5668f9"}
	current := skillsProducer{strings.Repeat("1", 64), strings.Repeat("2", 40)}
	if !skillsAcceptedProducer(p, current) {
		t.Fatal("independently reproduced current software pair lost")
	}
	p.Revision = strings.Repeat("3", 40)
	if skillsAcceptedProducer(p, current) {
		t.Fatal("other source revision accepted for the same observed digest")
	}
	p.SHA = strings.Repeat("4", 64)
	p.Revision = "f5f846a8470b544846e4c9ce43306061bf5668f9"
	if skillsAcceptedProducer(p, current) {
		t.Fatal("unobserved build with a claimed accepted revision admitted")
	}
}

func TestSkills3080CreatedPublicationNeedsNativeIssuingIdentityBeforeBlob(t *testing.T) {
	for _, kind := range []string{"valid_network_disabled", "private_root", "unknown_producer", "different_producer_revision", "other_manifest", "other_commit", "other_request", "other_writer", "no_review", "other_review", "empty_creation", "old_receipt", "changed_bytes", "missing_local_object", "alias_manifest"} {
		t.Run(kind, func(t *testing.T) {
			m, r, e, _ := skillsForwardFixture(t)
			body := []byte("fixed public manifest bytes already verified at native CreateNew")
			r.ManifestSHA = hashBytes(body)
			e.ManifestSHA = r.ManifestSHA
			originalRequestSHA := e.RequestSHA
			e.Producer = skillsProducer{"29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb", "e1a4c21dc6241c140d4d7364d34f42b16d7005be"}
			current := skillsProducer{strings.Repeat("a", 64), strings.Repeat("b", 40)}
			switch kind {
			case "unknown_producer":
				e.Producer.SHA = strings.Repeat("c", 64)
			case "different_producer_revision":
				e.Producer.Revision = strings.Repeat("d", 40)
			case "other_manifest":
				e.ManifestSHA = strings.Repeat("d", 64)
			case "other_commit":
				e.ManifestCommit = strings.Repeat("d", 40)
			case "other_request":
				e.RequestSHA = strings.Repeat("d", 64)
			case "other_writer":
				e.Writer.Job = "20261026-110000-todo-3080"
			case "no_review":
				r.Review = nil
			case "other_review":
				e.Review.FinalSHA = strings.Repeat("e", 64)
			case "empty_creation":
				e.Files = nil
			case "old_receipt":
				e.Domain = "skills-3080-created-v1"
			case "changed_bytes":
				body = append(body, '!')
			case "alias_manifest":
				r.ManifestPath = "docs/skills-3080-operations/../private.json"
				e.ManifestPath = r.ManifestPath
			}
			reads := 0
			got, ok := skillsCreatedPublicManifestWith(e, r, originalRequestSHA, current, func(root string) bool { return kind != "private_root" && same(root, m.Writer.Workspace) }, func(root, commit, path string) ([]byte, bool) { reads++; return body, kind != "missing_local_object" })
			if ok != (kind == "valid_network_disabled") {
				t.Fatalf("publication %s accepted=%v", kind, ok)
			}
			if ok && (reads != 1 || hashBytes(got) != r.ManifestSHA) {
				t.Fatal("positive never consumed exact public object")
			}
			if kind != "valid_network_disabled" && kind != "changed_bytes" && kind != "missing_local_object" && reads != 0 {
				t.Fatal("unqualified declaration opened a manifest")
			}
		})
	}
}
