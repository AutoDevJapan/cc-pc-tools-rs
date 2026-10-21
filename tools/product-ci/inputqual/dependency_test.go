package inputqual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func dependencyFixture(t *testing.T) map[string][]byte {
	t.Helper()
	context, _ := json.Marshal(map[string]any{"request_id": "synthetic-Request", "question_message_id": "synthetic-Question", "original_decision": map[string]any{"todo_id": 3059, "scope": map[string]string{"action": "extend-finite-ci-execution-scope-to-original-pr939", "additional_pr939_head": "f1c6e235d93d0719532b592b03037edcbe7bffef"}}})
	owner, _ := json.Marshal(map[string]any{"message_id": "synthetic-Message", "author_id": "synthetic-Author", "source": "discord-api-v10", "text": "synthetic-Text", "question_context": string(context)})
	source, _ := json.Marshal(map[string]any{"domain": "original-3059-pr939-source-review-bundle-v1", "source": "github-rest-api", "owner": "AutoDevJapan", "repo_id": 1285649124, "pr": 939, "head": "f1c6e235d93d0719532b592b03037edcbe7bffef", "tree": "df13e3dd2d796812f922868578df2d0ef8beceb5", "base": fixedBase, "context": "de71ff5d9a1d5a61328cbcf054347f9686ad7b8e", "run": int64(39344575918), "attempt": 2, "job": int64(118550301356), "workflow_sha256": "0f9b993f4d00de86a94ad10432123f473daae1c5dfc9d3412149524f5739e273", "all_patches_complete": true, "files": []map[string]string{{"filename": "integrations/arps-atrs/package.json", "patch": "synthetic patch"}, {"filename": "package.json", "patch": "synthetic patch"}, {"filename": "packages/shared-contracts/package.json", "patch": "synthetic patch"}, {"filename": "pnpm-lock.yaml", "patch": "synthetic patch"}}})
	return map[string][]byte{"owner_reply": owner, "source": source, "review": []byte("synthetic independent reviewer output; no real approval")}
}

func TestDependencyQualificationBindsOwnerQuestionAndSource(t *testing.T) {
	good := dependencyFixture(t)
	s, err := dependencySnapshotWithIdentity(good, fixtureDependencyIdentity())
	if err != nil || s.Report().PR != 939 || s.Report().OfficialCISuccess || s.Report().OriginalInputFilesRead {
		t.Fatalf("synthetic qualification failed: %v", err)
	}
	calls := 0
	if s.Consume(func(pin, review, acceptance []byte) error {
		calls++
		if len(pin) == 0 || len(review) == 0 || len(acceptance) == 0 {
			t.Fatal("missing evidence bytes")
		}
		return nil
	}) != nil || calls != 1 {
		t.Fatal("qualified evidence did not reach real consumer")
	}
	for role, field := range map[string]string{"owner_reply": "author_id", "source": "head"} {
		bad := dependencyFixture(t)
		var v map[string]any
		if json.Unmarshal(bad[role], &v) != nil {
			t.Fatal("fixture")
		}
		v[field] = "changed"
		bad[role], _ = json.Marshal(v)
		if _, err := dependencySnapshotWithIdentity(bad, fixtureDependencyIdentity()); err == nil {
			t.Fatal("changed independent source accepted")
		}
	}
	s.pin[0] ^= 1
	if s.Consume(func([]byte, []byte, []byte) error { t.Fatal("changed bytes reached consumer"); return nil }) == nil {
		t.Fatal("modified constructed snapshot accepted")
	}
}

func TestDependencyAnchorRejectsChangedAndCopiedReceipts(t *testing.T) {
	good := dependencyFixture(t)
	// The Windows runner exposes its scratch through a directory junction.
	// Fixtures must use the physical path, just like the original qualifier tests;
	// production receipt reads continue rejecting every reparse-point ancestor.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	policy := map[string]struct{ pathSHA, contentSHA string }{}
	for role, data := range good {
		path := filepath.Join(root, role+".json")
		if os.WriteFile(path, data, 0600) != nil {
			t.Fatal("write fixture")
		}
		refs[role] = path
		policy[role] = struct{ pathSHA, contentSHA string }{locationDigest(path), hash(data)}
	}
	if _, err := readDependencyAnchors(refs, policy, fixtureDependencyIdentity()); err != nil {
		t.Fatalf("complete fixture rejected: %v", err)
	}
	copyPath := filepath.Join(root, "copied.json")
	if os.WriteFile(copyPath, good["owner_reply"], 0600) != nil {
		t.Fatal("write copy")
	}
	original := refs["owner_reply"]
	refs["owner_reply"] = copyPath
	if _, err := readDependencyAnchors(refs, policy, fixtureDependencyIdentity()); err == nil {
		t.Fatal("relocated authority accepted")
	}
	refs["owner_reply"] = original
	if os.WriteFile(refs["review"], []byte("substituted reviewer"), 0600) != nil {
		t.Fatal("change fixture")
	}
	if _, err := readDependencyAnchors(refs, policy, fixtureDependencyIdentity()); err == nil {
		t.Fatal("changed independent review accepted")
	}
}

func fixtureDependencyIdentity() dependencyIdentity {
	return dependencyIdentity{Author: hash([]byte("synthetic-Author")), Message: hash([]byte("synthetic-Message")), Question: hash([]byte("synthetic-Question")), Request: hash([]byte("synthetic-Request")), Text: hash([]byte("synthetic-Text"))}
}
