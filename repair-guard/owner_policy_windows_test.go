package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ownerFixturePolicy(path string, qualified bool, legacy ownerLegacyState) ownerPathVerdict {
	return ownerPolicyClassify(path, ownerPolicyAccess{
		legacy: func(string) ownerLegacyState { return legacy },
		repair: func(string) bool { return qualified },
		source: func(string) ownerPathVerdict { return ownerUnverified() }, physical: physical,
	})
}

func TestOwnerPolicyCardOverridesSourceQualification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credit-cards.json")
	var output, stderr bytes.Buffer
	code := runGuardCLIWithPath([]string{"--guard-path", path}, &output, &stderr, nil, func(p string) ownerPathVerdict { return ownerFixturePolicy(p, true, ownerLegacyAbsent) })
	var result object
	if json.Unmarshal(output.Bytes(), &result) != nil || code != 0 {
		t.Fatal("normal path classification failed")
	}
	if result["client_resource"] != true || result["access_allowed"] != false || result["protection_status"] != "protected" {
		t.Fatal("card information was accepted as qualified repair source")
	}
	categories, _ := result["protection_categories"].([]any)
	if len(categories) != 1 || categories[0] != "owner_card_information" {
		t.Fatal("card category missing")
	}
}

func TestOwnerPolicyUnknownIsNotAConfirmedClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown")
	var output, stderr bytes.Buffer
	code := runGuardCLIWithPath([]string{"--guard-path", path}, &output, &stderr, nil, func(p string) ownerPathVerdict { return ownerFixturePolicy(p, false, ownerLegacyAbsent) })
	var result object
	if json.Unmarshal(output.Bytes(), &result) != nil || code != 0 {
		t.Fatal("normal path classification failed")
	}
	if result["client_resource"] != true || result["classification_unverified"] != true || result["access_allowed"] != false || result["protection_status"] != "unverified" {
		t.Fatal("unknown classification is still conflated with a confirmed category")
	}
	categories, ok := result["protection_categories"].([]any)
	if !ok || len(categories) != 0 || result["original_registry_recovered"] != false {
		t.Fatal("unknown resource acquired an invented protection category")
	}
	if bytes.Contains(output.Bytes(), []byte(path)) || stderr.Len() != 0 {
		t.Fatal("classification exposed private input")
	}
}

func TestOwnerPolicyCategoriesAndNonSourceBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, status, category string
		legacy                 ownerLegacyState
		allow                  bool
	}{
		{"source.go", "clear", "", ownerLegacyAbsent, true},
		{"source.go", "protected", "client_project", ownerLegacyClient, false},
		{"source.go", "unverified", "", ownerLegacyUnavailable, false},
		{".env.production", "unverified", "", ownerLegacyClear, false},
		{"credentials.json", "unverified", "", ownerLegacyClear, false},
		{"auth.json", "unverified", "", ownerLegacyClear, false},
		{filepath.Join(".git", "config"), "unverified", "", ownerLegacyClear, false},
		{"CREDIT_CARDS.JSON", "protected", "owner_card_information", ownerLegacyAbsent, false},
		{filepath.Join("クレジットカード情報", "scan.png"), "protected", "owner_card_information", ownerLegacyClear, false},
		{"my-credit-card.txt", "protected", "owner_card_information", ownerLegacyClear, false},
		{"credit_card_handler.go", "clear", "", ownerLegacyClear, true},
	} {
		t.Run(tc.name+tc.status, func(t *testing.T) {
			v := ownerFixturePolicy(filepath.Join(root, tc.name), true, tc.legacy)
			if v.AccessAllowed != tc.allow || v.Status != tc.status {
				t.Fatal("classification/qualification precedence differs")
			}
			if tc.category == "" && len(v.Categories) != 0 || tc.category != "" && (len(v.Categories) != 1 || v.Categories[0] != tc.category) {
				t.Fatal("a third or incorrect protection category was emitted")
			}
		})
	}
	// A safe-looking alias must not conceal a card/credential location.
	for _, target := range []string{"credit-cards.json", "credentials.json"} {
		v := ownerPolicyClassify(filepath.Join(root, "alias"), ownerPolicyAccess{
			legacy: func(string) ownerLegacyState { return ownerLegacyAbsent },
			repair: func(string) bool { return true }, source: func(string) ownerPathVerdict { return ownerUnverified() },
			physical: func(string) (string, error) { return filepath.Join(root, target), nil },
		})
		if v.AccessAllowed {
			t.Fatal("resolved non-source accepted")
		}
	}
}

func TestOwnerPolicyLegacyEvidenceIsTypedWithoutDisclosure(t *testing.T) {
	for _, tc := range []struct {
		path, registry string
		want           ownerLegacyState
	}{
		{`D:\fixture\customer-a\file`, "# synthetic exclusions\ncustomer-a\n", ownerLegacyClient},
		{`D:\fixture\customer-a\file`, "\ufeffCUSTOMER-A\r\n", ownerLegacyClient},
		{`D:\fixture\ordinary\file`, "customer-a\n", ownerLegacyClear},
		{`D:\fixture\ordinary\file`, "", ownerLegacyUnavailable},
		{`D:\fixture\ordinary\file`, string([]byte{0xff}), ownerLegacyUnavailable},
	} {
		if ownerLegacyBytes(tc.path, []byte(tc.registry)) != tc.want {
			t.Fatal("legacy classification differs")
		}
	}
	if ownerLegacyBytes("fixture", []byte(strings.Repeat("x", 1024*1024+1))) != ownerLegacyUnavailable {
		t.Fatal("oversized registry accepted")
	}
}

func ownerSourceFixture(t *testing.T) (string, object, ownerSourceAccess) {
	t.Helper()
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "ordinary-source")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/example.git")
	if os.WriteFile(filepath.Join(root, "main.go"), []byte("package fixture\n"), 0600) != nil {
		t.Fatal("fixture source")
	}
	currentFixtureGit(t, root, "add", "--", "main.go")
	currentFixtureGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	db := object{"projects": object{"synthetic": root}, "items": []any{object{"id": 42, "status": "block", "owner_cancelled": true}}}
	access := ownerSourceAccess{git: currentGit, plain: currentPlain, denied: func(string) bool { return false }, identity: func(repository string) (int64, string, string, bool) { return 12345, repository, "AutoDevJapan", true }}
	return root, db, access
}

func TestOwnerRegisteredSourceNeedsNoTaskOrFileException(t *testing.T) {
	root, db, access := ownerSourceFixture(t)
	before, _ := json.Marshal(db)
	for _, path := range []string{root, filepath.Join(root, "main.go")} {
		v := ownerRegisteredSourceWith(path, db, access)
		if !v.AccessAllowed || v.Status != "clear" || len(v.Categories) != 0 {
			t.Fatal("verified registered self source was incorrectly excluded")
		}
	}
	container := root + "-wt"
	if os.Mkdir(container, 0700) != nil {
		t.Fatal("fixture container")
	}
	tree := filepath.Join(container, "ordinary-branch")
	currentFixtureGit(t, root, "worktree", "add", "-qb", "ordinary-fix", tree)
	if !ownerRegisteredSourceWith(filepath.Join(tree, "main.go"), db, access).AccessAllowed {
		t.Fatal("verified same-repository worktree required a task exception")
	}
	after, _ := json.Marshal(db)
	if !bytes.Equal(before, after) {
		t.Fatal("resource classification changed an explicit owner stop")
	}
}

func TestOwnerRegisteredSourceRejectsUnverifiedAndClientIdentity(t *testing.T) {
	root, db, original := ownerSourceFixture(t)
	for _, tc := range []struct {
		name              string
		id                int64
		repository, owner string
		ok                bool
		status            string
	}{
		{"API unavailable", 0, "", "", false, "unverified"},
		{"wrong repository", 12345, "AutoDevJapan/other", "AutoDevJapan", true, "unverified"},
		{"wrong owner", 12345, "AutoDevJapan/example", "OtherOwner", true, "unverified"},
		{"missing ID", 0, "AutoDevJapan/example", "AutoDevJapan", true, "unverified"},
		{"contracted repository ID", 1156778979, "AutoDevJapan/example", "AutoDevJapan", true, "protected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := original
			access.identity = func(string) (int64, string, string, bool) { return tc.id, tc.repository, tc.owner, tc.ok }
			v := ownerRegisteredSourceWith(root, db, access)
			if v.AccessAllowed || v.Status != tc.status {
				t.Fatal("unverified/client identity admitted")
			}
			if tc.status == "protected" && (len(v.Categories) != 1 || v.Categories[0] != "client_project") {
				t.Fatal("contracted identity not classified")
			}
		})
	}
	for _, name := range []string{"untracked.txt", ".env", "credentials.json"} {
		path := filepath.Join(root, name)
		if os.WriteFile(path, []byte("synthetic fixture, no credentials\n"), 0600) != nil {
			t.Fatal("fixture leaf")
		}
		if name != "untracked.txt" {
			currentFixtureGit(t, root, "add", "-f", "--", name)
		}
		if ownerRegisteredSourceWith(path, db, original).AccessAllowed {
			t.Fatal("unknown or credential leaf admitted")
		}
	}
	access := original
	access.denied = func(string) bool { return true }
	if ownerRegisteredSourceWith(root, db, access).AccessAllowed {
		t.Fatal("existing deny lost priority")
	}
	if ownerRegisteredSourceWith(root, object{"projects": []any{root}}, original).AccessAllowed {
		t.Fatal("malformed registration admitted")
	}
	if ownerRegisteredSourceWith(root, object{"projects": object{}}, original).AccessAllowed {
		t.Fatal("unregistered source admitted")
	}

	container := root + "-wt"
	if os.MkdirAll(container, 0700) != nil {
		t.Fatal("fixture container")
	}
	impostor := filepath.Join(container, "unrelated")
	if os.Mkdir(impostor, 0700) != nil {
		t.Fatal("fixture impostor")
	}
	currentFixtureGit(t, impostor, "init", "-q")
	currentFixtureGit(t, impostor, "remote", "add", "origin", "https://github.com/AutoDevJapan/example.git")
	if ownerRegisteredSourceWith(impostor, db, original).AccessAllowed {
		t.Fatal("same remote with unrelated Git management admitted")
	}

	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/OtherOwner/example.git")
	if ownerRegisteredSourceWith(root, db, original).AccessAllowed {
		t.Fatal("other owner's source admitted")
	}
}

func TestOwnerOriginRejectsCredentialURLsAndLookalikes(t *testing.T) {
	for _, origin := range []string{"https://github.com/AutoDevJapan/example.git", "git@github.com:BM1314/example.git", "https://github.com/mashfromband/example.git"} {
		if _, ok := ownerOriginRepository(origin); !ok {
			t.Fatal("self origin rejected")
		}
	}
	for _, origin := range []string{"https://user:secret@github.com/AutoDevJapan/example.git", "https://github.com.evil.invalid/AutoDevJapan/example.git", "https://github.com/OtherOwner/example.git", "https://github.com/AutoDevJapan/example?token=synthetic", "https://github.com/AutoDevJapan/../example", "file:///fixture"} {
		if _, ok := ownerOriginRepository(origin); ok {
			t.Fatal("credential, unknown or lookalike origin accepted")
		}
	}
}
