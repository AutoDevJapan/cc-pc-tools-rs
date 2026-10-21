package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnerMembershipNeedsPositiveDepartment(t *testing.T) {
	catalog, _ := json.Marshal(object{"domain": "owner-department-classification-v1", "groups": []any{
		object{"kind": "self", "aliases": []string{ownerProductHash("self-fixture"), ownerProductHash("self-alias")}},
		object{"kind": "client_project", "aliases": []string{ownerProductHash("client-fixture-b"), ownerProductHash("client-alias-b")}},
	}})
	for _, tc := range []struct{ project, repo, want string }{
		{"self-fixture", "AutoDevJapan/self-alias", "self"},
		{"resume-self-fixture", "AutoDevJapan/self-alias", "self"},
		{"client-fixture-b", "AutoDevJapan/client-alias-b", "client_project"},
		{"self-fixture", "AutoDevJapan/client-alias-b", "client_project"},
		{"unknown", "AutoDevJapan/unknown", ""},
		{"self-fixture", "AutoDevJapan/unknown", ""},
	} {
		if ownerMembershipWith(tc.project, tc.repo, catalog) != tc.want {
			t.Fatal("department membership differs")
		}
	}
	root, db, access := ownerSourceFixture(t)
	access.membership = func(string, string) string { return "client_project" }
	access.identity = func(string) (int64, string, string, bool) {
		t.Fatal("contracted metadata triggered API")
		return 0, "", "", false
	}
	if v := ownerRegisteredSourceWith(root, db, access); v.AccessAllowed || v.Status != "protected" {
		t.Fatal("a second contracted product was admitted under a self owner")
	}
	access.membership = func(string, string) string { return "" }
	if ownerRegisteredSourceWith(root, db, access).AccessAllowed {
		t.Fatal("unknown department admitted")
	}
}

func TestOwnerProjectProjectionIgnoresItemUpdatesAndRejectsMalformedProjects(t *testing.T) {
	for _, input := range []string{
		`{"projects":{"fixture":"C:/fixture"},"items":[{"note":"old"}]}`,
		`{"items":[{"note":"new","nested":[true,null,2]}],"projects":{"fixture":"C:/fixture"}}`,
	} {
		p, ok := ownerProjectProjection(context.Background(), strings.NewReader(input))
		if !ok || p["fixture"] != "C:/fixture" || len(p) != 1 {
			t.Fatal("item content affected project authority")
		}
	}
	for _, input := range []string{
		`{"projects":{"fixture":"C:/fixture","fixture":"D:/other"}}`,
		`{"projects":{},"projects":{"fixture":"C:/fixture"}}`,
		`{"projects":{"fixture":false}}`,
		`{"projects":{},"items":[`,
		`{"projects":{}} {}`,
	} {
		if _, ok := ownerProjectProjection(context.Background(), strings.NewReader(input)); ok {
			t.Fatal("malformed registration admitted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := ownerProjectProjection(ctx, strings.NewReader(`{"projects":{}}`)); ok {
		t.Fatal("expired projection admitted")
	}
}

func TestOwnerProjectionExceedsOld64MiBLimitWithoutItems(t *testing.T) {
	input := bytes.NewBufferString(`{"projects":{"fixture":"C:/fixture"},"items":[{"note":"`)
	input.WriteString(strings.Repeat("x", 65<<20))
	input.WriteString(`"}]}`)
	started := time.Now()
	p, ok := ownerProjectProjection(context.Background(), input)
	if !ok || len(p) != 1 {
		t.Fatal("large unrelated history disabled registration")
	}
	t.Logf("projects-only projection of 65MiB: %s", time.Since(started))
}

func TestOwnerUnknownNonRepositoryDoesNotUseRegistrationOrGit(t *testing.T) {
	root := t.TempDir()
	if ownerRepositoryCandidate(root) {
		t.Fatalf("owned unknown fixture became a source candidate: %s (temp %s)", root, os.TempDir())
	}
	reads := 0
	read := func(context.Context) (object, bool) {
		reads++
		t.Fatal("unknown non-repository opened registration")
		return nil, false
	}
	if ownerRegisteredSourceAt(context.Background(), root, read, ownerSourceAccess{}).AccessAllowed {
		t.Fatal("unknown directory admitted")
	}
	if reads != 0 {
		t.Fatal("unknown metadata was read")
	}
	known, db, access := ownerSourceFixture(t)
	read = func(context.Context) (object, bool) { reads++; return db["projects"].(object), true }
	if !ownerRegisteredSourceAt(context.Background(), known, read, access).AccessAllowed || reads != 2 {
		t.Fatal("positive source control did not exercise registration reader")
	}
}

func TestOwnerIdentityCacheAvoidsRepeatedAPIAndRefreshesExpiry(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	calls := 0
	fetch := func(context.Context, string) (int64, string, string, bool) {
		calls++
		return 12345, "AutoDevJapan/example", "AutoDevJapan", true
	}
	for n := 0; n < 3; n++ {
		id, _, _, ok := ownerCachedIdentityAt(context.Background(), "AutoDevJapan/example", home, func() time.Time { return now }, fetch)
		if !ok || id != 12345 {
			t.Fatal("normal identity cache query failed")
		}
	}
	if calls != 1 {
		t.Fatal("API was called again for a fresh source identity")
	}
	if _, _, _, ok := ownerCachedIdentityAt(context.Background(), "autodevjapan/EXAMPLE", home, func() time.Time { return now }, fetch); !ok || calls != 1 {
		t.Fatal("case-only repository spelling invalidated identity authority")
	}
	now = now.Add(300 * time.Second)
	if _, _, _, ok := ownerCachedIdentityAt(context.Background(), "AutoDevJapan/example", home, func() time.Time { return now }, fetch); !ok || calls != 2 {
		t.Fatal("expired identity did not recheck API")
	}
}

func TestOwnerRefreshCLIRejectsMixedAndRelativeInput(t *testing.T) {
	for _, args := range [][]string{{"--refresh-owner-source", "relative"}, {"--refresh-owner-source", `C:\fixture`, "--guard-path", `C:\fixture`}, {"--refresh-owner-source", `C:\fixture`, "--verify-current-succession", "1"}} {
		var out, err bytes.Buffer
		if runGuardCLIWithPath(args, &out, &err, nil, func(string) ownerPathVerdict {
			t.Fatal("mixed refresh used path qualification")
			return ownerUnverified()
		}) != 2 {
			t.Fatal("mixed/relative refresh accepted")
		}
	}
}

func TestOwnerSourceGitIgnoresInheritedOverrides(t *testing.T) {
	root, _, _ := ownerSourceFixture(t)
	t.Setenv("GIT_DIR", filepath.Join(root, "absent"))
	t.Setenv("GIT_WORK_TREE", filepath.Dir(root))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "remote.origin.url")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/OtherOwner/example.git")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, ok := ownerSourceGit(ctx, root, "remote", "get-url", "origin")
	if !ok || result != "https://github.com/AutoDevJapan/example.git" {
		t.Fatal("inherited Git state changed source identity")
	}
}

func TestOwnerIdentityCacheSealedAndFinite(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	r := ownerIdentityRecord{"owner-source-identity-v1", hashBytes(ownerDepartmentCatalog), "AutoDevJapan/example", "AutoDevJapan", 12345, now.Unix()}
	data, _ := json.Marshal(r)
	sealed, ok := ownerIdentityDPAPI(data, true)
	if !ok || bytes.Contains(sealed, []byte(r.Repository)) {
		t.Fatal("identity cache was not sealed")
	}
	plain, ok := ownerIdentityDPAPI(sealed, false)
	if !ok || !bytes.Equal(plain, data) {
		t.Fatal("cache integrity roundtrip failed")
	}
	sealed[len(sealed)/2] ^= 1
	if _, ok := ownerIdentityDPAPI(sealed, false); ok {
		t.Fatal("modified cache accepted")
	}
	if !ownerIdentityValid(r, r.Repository, now.Add(299*time.Second)) || ownerIdentityValid(r, r.Repository, now.Add(300*time.Second)) || ownerIdentityValid(r, r.Repository, now.Add(-time.Second)) {
		t.Fatal("cache freshness boundaries differ")
	}
}

func TestOwnerHandleResolvesActualJunctionToCardDirectory(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "credit-cards")
	if os.Mkdir(target, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	leaf := filepath.Join(target, "synthetic.json")
	if os.WriteFile(leaf, []byte(`{"fixture":true}`), 0600) != nil {
		t.Fatal("fixture leaf")
	}
	junction := filepath.Join(root, "safe-alias")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("junction fixture unavailable")
	}
	defer os.Remove(junction)
	path := filepath.Join(junction, "synthetic.json")
	resolved, err := ownerPhysicalPath(path)
	if err != nil || !same(resolved, leaf) {
		t.Fatal("junction target was not resolved by handle")
	}
	v := ownerPolicyClassify(path, ownerPolicyAccess{legacy: func(string) ownerLegacyState { return ownerLegacyAbsent }, repair: func(string) bool { return true }, source: func(string) ownerPathVerdict { return ownerUnverified() }, physical: ownerPhysicalPath})
	if v.AccessAllowed || v.Status != "protected" {
		t.Fatal("junction hid a card directory")
	}
}

// Opt-in acceptance uses only a separately prequalified self source root and
// the normal internal registration/identity path. Never prints project data.
func TestOwnerLiveSourceBudget(t *testing.T) {
	path := os.Getenv("CC_GUARD_LIVE_SELF_SOURCE")
	if path == "" {
		t.Skip("explicit prequalified self-source acceptance only")
	}
	accepted := false
	if !ownerRefreshIdentity(path) {
		t.Fatal("normal bounded metadata refresh failed")
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), time.Second)
	projects, projectionOK := ownerReadProjects(probeCtx, productStore)
	probeCancel()
	match := false
	member := ""
	for project, entry := range projects {
		if root, ok := entry.(string); ok && same(root, path) {
			match = true
			member = ownerDepartmentMembership(project, "")
		}
	}
	t.Logf("registration_projection=%v count=%d matching_root=%v membership=%s plain_store=%v legacy_state=%d", projectionOK, len(projects), match, member, currentPlain(productStore, false), ownerLegacyProtection(productStore))
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		calls := 0
		started := time.Now()
		access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) }, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership, identity: func(repo string) (int64, string, string, bool) {
			return ownerCachedIdentityAt(ctx, repo, ownerIdentityHome, time.Now, func(ctx context.Context, repo string) (int64, string, string, bool) { calls++; return 0, "", "", false })
		}}
		v := ownerRegisteredSourceAt(ctx, path, func(ctx context.Context) (object, bool) { return ownerReadProjects(ctx, productStore) }, access)
		elapsed := time.Since(started)
		cancel()
		t.Logf("attempt=%d allowed=%v status=%s elapsed=%s API_calls=%d", attempt+1, v.AccessAllowed, v.Status, elapsed, calls)
		accepted = accepted || v.AccessAllowed
		if elapsed > 850*time.Millisecond {
			t.Fatal("source check exceeded the consumer's one-second budget")
		}
	}
	if !accepted {
		t.Fatal("normal self-source classification never succeeded within its finite budget")
	}
}
