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
		`{"projects":{"fixtur\ufffd":"C:/fixture"}}`,
		`{"projects":{"fixture":"C:/fixtur\ufffd"}}`,
		`{"projects":{},"items":[`,
		`{"projects":{}} {}`,
		`{"projects":{},"items":[{"bad":[1,]}]}`,
		`{"projects":{},"items":[` + strings.Repeat("[", 130) + "null" + strings.Repeat("]", 130) + `]}`,
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

func TestOwnerProjectionManySmallItems(t *testing.T) {
	item := `{"id":1,"status":"done","project":"fixture","jobs":["fixture-a","fixture-b"],"history":[{"note":"old"},{"note":"new"}],"note":"synthetic","at":12345},`
	input := `{"items":[` + strings.Repeat(item, 400000) + `null],"projects":{"fixture":"C:/fixture"}}`
	started := time.Now()
	p, ok := ownerProjectProjection(context.Background(), strings.NewReader(input))
	if !ok || len(p) != 1 || p["fixture"] != "C:/fixture" {
		t.Fatal("many small items affected source authority")
	}
	t.Logf("many-small-items bytes=%d elapsed=%s", len(input), time.Since(started))
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
	if !ownerRegisteredSourceAt(context.Background(), known, read, access).AccessAllowed || reads != 1 {
		t.Fatal("positive source control did not exercise registration reader")
	}
}

func TestOwnerIdentityCacheAvoidsRepeatedAPIAndRefreshesExpiry(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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

func TestOwnerRegistrationSnapshotDetectsReplacementWithSameBytesAndTime(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "registration.json")
	data := []byte(`{"projects":{"fixture":"C:/fixture"},"items":[]}`)
	if os.WriteFile(path, data, 0600) != nil {
		t.Fatal("fixture write")
	}
	s, ok := ownerReadProjectSnapshot(context.Background(), path, func(string) bool { return false })
	if !ok {
		t.Fatal("normal registration snapshot rejected")
	}
	if !s.current() {
		t.Fatal("fresh file generation rejected")
	}
	replacement := filepath.Join(root, "replacement.json")
	if os.WriteFile(replacement, data, 0600) != nil {
		t.Fatal("fixture replacement")
	}
	if os.Chtimes(replacement, s.info.ModTime(), s.info.ModTime()) != nil {
		t.Fatal("fixture timestamp")
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("reader prevented the ordinary atomic writer: %v", err)
	}
	if s.current() {
		t.Fatal("replacement was hidden by equal bytes, size and timestamp")
	}
}

func TestOwnerContractPriorityDoesNotDependOnRegistrationOrder(t *testing.T) {
	root, _, access := ownerSourceFixture(t)
	access.membership = func(project, repository string) string {
		if project == "client-fixture" {
			return "client_project"
		}
		return "self"
	}
	for i := 0; i < 40; i++ {
		db := object{"projects": object{"synthetic": root, "client-fixture": root}}
		v := ownerRegisteredSourceWith(root, db, access)
		if v.AccessAllowed || v.Status != "protected" {
			t.Fatal("self map entry bypassed a covering contracted declaration")
		}
	}
}

func TestOwnerUntrackedSourceDoesNotRefreshIdentity(t *testing.T) {
	root, db, access := ownerSourceFixture(t)
	path := filepath.Join(root, "untracked-fixture.json")
	if os.WriteFile(path, []byte(`{"fixture":true}`), 0600) != nil {
		t.Fatal("fixture write")
	}
	calls := 0
	access.identity = func(repo string) (int64, string, string, bool) { calls++; return 12345, repo, "AutoDevJapan", true }
	if ownerRegisteredSourceWith(path, db, access).AccessAllowed || calls != 0 {
		t.Fatal("untracked data requested identity authority")
	}
	if !ownerRegisteredSourceWith(filepath.Join(root, "main.go"), db, access).AccessAllowed || calls != 1 {
		t.Fatal("positive tracked control did not exercise identity")
	}
}

func TestOwnerIdentityRefreshesBeforeExpiryWithoutExtendingStaleAuthority(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	calls := 0
	repo := "AutoDevJapan/example"
	fetch := func(context.Context, string) (int64, string, string, bool) {
		calls++
		return 12345, repo, "AutoDevJapan", true
	}
	if _, _, _, ok := ownerCachedIdentityAt(context.Background(), repo, home, func() time.Time { return now }, fetch); !ok {
		t.Fatal("initial identity failed")
	}
	if ownerIdentityAging(repo, home, now.Add(239*time.Second)) || !ownerIdentityAging(repo, home, now.Add(240*time.Second)) {
		t.Fatal("proactive refresh window differs")
	}
	now = now.Add(240 * time.Second)
	if _, _, _, ok := ownerCachedIdentityAt(context.Background(), repo, home, func() time.Time { return now }, fetch, true); !ok || calls != 2 {
		t.Fatal("proactive refresh reused the old timestamp")
	}
	if ownerIdentityAging(repo, home, now) {
		t.Fatal("successful refresh did not reset age")
	}
	if r, ok := ownerReadIdentity(home, repo, now.Add(299*time.Second)); !ok || r.Checked != now.Unix() {
		t.Fatal("fresh identity was lost")
	}
	if _, ok := ownerReadIdentity(home, repo, now.Add(300*time.Second)); ok {
		t.Fatal("expired identity remained authority")
	}
}

func TestOwnerRefreshFailureCooldownAndClaimOwnership(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	a := "AutoDevJapan/example-a"
	b := "AutoDevJapan/example-b"
	first, ok := ownerClaimRefresh(home, a, now)
	if !ok {
		t.Fatal("first finite refresh was not admitted")
	}
	if _, ok := ownerClaimRefresh(home, a, now.Add(time.Second)); ok {
		t.Fatal("in-flight request was duplicated")
	}
	if ownerDropRefreshClaim(home, strings.Repeat("b", 32), now) {
		t.Fatal("foreign claim identity removed the active request")
	}
	if !ownerWriteSealed(home, ownerRefreshFailureName(a), ownerRefreshFailure{"owner-source-refresh-failure-v1", a, hashBytes(ownerDepartmentCatalog), now.Unix()}) {
		t.Fatal("failure state not preserved")
	}
	if !ownerDropRefreshClaim(home, first.ID, now) {
		t.Fatal("owned finished request could not close")
	}
	if _, ok := ownerClaimRefresh(home, a, now.Add(299*time.Second)); ok {
		t.Fatal("failed identity restarted before its finite cooldown")
	}
	second, ok := ownerClaimRefresh(home, b, now.Add(time.Second))
	if !ok {
		t.Fatal("one failed repository blocked independent metadata work")
	}
	if ownerDropRefreshClaim(home, first.ID, now) {
		t.Fatal("late cleanup removed a newer claim")
	}
	if !ownerDropRefreshClaim(home, second.ID, now) {
		t.Fatal("second owned request could not close")
	}
	third, ok := ownerClaimRefresh(home, a, now.Add(300*time.Second))
	if !ok {
		t.Fatal("finite retry did not reopen after cooldown")
	}
	recovered, ok := ownerClaimRefresh(home, b, now.Add(321*time.Second))
	if !ok {
		t.Fatal("interrupted stale request was not recovered")
	}
	if recovered.ID == third.ID || ownerDropRefreshClaim(home, third.ID, now.Add(322*time.Second)) {
		t.Fatal("stale cleanup removed the recovery generation")
	}
	if !ownerDropRefreshClaim(home, recovered.ID, now.Add(322*time.Second)) {
		t.Fatal("recovered request could not close")
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

func TestOwnerRefreshCannotMixWithCapacityRuntimeQualification(t *testing.T) {
	for _, flag := range []string{"--refresh-owner-source", "--refresh-request"} {
		var out, stderr bytes.Buffer
		calls := 0
		code := runGuardCLIWithRuntime([]string{"--verify-capacity-runtime-update", `C:\fixture.exe`, flag, `C:\fixture`}, &out, &stderr, nil, func(string) object {
			calls++
			return object{"qualified": true}
		})
		if code != 1 || calls != 0 || strings.TrimSpace(out.String()) != `{"qualified":false}` {
			t.Fatal("mixed entry points reached a runtime verifier")
		}
	}
}

func TestOwnerRefreshLocalFailureDoesNotCoolUnattemptedIdentity(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	repository := "AutoDevJapan/example"
	claim, ok := ownerClaimRefresh(home, repository, now)
	if !ok {
		t.Fatal("claim fixture")
	}
	fetches := 0
	fetch := func(context.Context, string) (int64, string, string, bool) { fetches++; return 0, "", "", false }
	query := func(context.Context, string, ownerSourceAccess) ownerPathVerdict { return ownerUnverified() }
	if ownerRefreshIdentityRequestWith("unused", strings.Repeat("f", 32), home, func() time.Time { return now }, query, fetch) {
		t.Fatal("unowned refresh request accepted")
	}
	if _, ok := ownerReadRefreshClaim(home); !ok {
		t.Fatal("wrong request removed another claim")
	}
	if ownerRefreshIdentityRequestWith("unused", claim.ID, home, func() time.Time { return now }, query, fetch) {
		t.Fatal("unverified local input reported refreshed")
	}
	if fetches != 0 || ownerRefreshCooling(home, repository, now) {
		t.Fatal("local registration failure cooled an API request that never happened")
	}
	if _, ok := ownerReadRefreshClaim(home); ok {
		t.Fatal("finished owned claim remained")
	}
	claim, ok = ownerClaimRefresh(home, repository, now)
	if !ok {
		t.Fatal("local failure disabled a valid next attempt")
	}
	query = func(ctx context.Context, _ string, access ownerSourceAccess) ownerPathVerdict {
		access.identity(repository)
		return ownerUnverified()
	}
	if ownerRefreshIdentityRequestWith("unused", claim.ID, home, func() time.Time { return now }, query, fetch) || fetches != 1 || !ownerRefreshCooling(home, repository, now) {
		t.Fatal("actual fetch failure did not create finite cooldown")
	}
}

func TestOwnerIdentityRefreshRequiresDurableCache(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-directory")
	if os.WriteFile(home, []byte("fixture"), 0600) != nil {
		t.Fatal("fixture write")
	}
	_, _, _, ok := ownerCachedIdentityAt(context.Background(), "AutoDevJapan/example", home, time.Now, func(context.Context, string) (int64, string, string, bool) {
		return 12345, "AutoDevJapan/example", "AutoDevJapan", true
	})
	if ok {
		t.Fatal("refresh reported success without a reusable identity")
	}
}

func TestOwnerAbandonedRefreshClaimCoolsOnlyItsRepository(t *testing.T) {
	for _, skew := range []time.Duration{21 * time.Second, -time.Hour} {
		home, err := physical(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		a, b := "AutoDevJapan/example", "AutoDevJapan/another"
		claim, ok := ownerClaimRefresh(home, a, now)
		if !ok {
			t.Fatal("claim fixture")
		}
		if _, ok := ownerClaimRefresh(home, a, now.Add(skew)); ok || !ownerRefreshCooling(home, a, now.Add(skew)) {
			t.Fatal("abandoned/skewed helper immediately restarted")
		}
		next, ok := ownerClaimRefresh(home, b, now.Add(skew))
		if !ok || next.ID == claim.ID {
			t.Fatal("abandoned repository disabled another request")
		}
		if ownerDropRefreshClaim(home, claim.ID, now.Add(skew)) || !ownerDropRefreshClaim(home, next.ID, now.Add(skew)) {
			t.Fatal("cleanup removed another generation")
		}
	}
}

func TestOwnerUnicodeFailureIsNotAnUnnormalizedName(t *testing.T) {
	if _, ok := ownerUnicodeKC("credit\x00card"); ok {
		t.Fatal("failed normalization returned unchecked name")
	}
}

func TestOwnerRepositoryDirectoryDoesNotHideOutsideCardLocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "credit-cards", "source-fixture")
	if os.MkdirAll(filepath.Join(root, ".git"), 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if !ownerCardPath(root) {
		t.Fatal("repository marker hid its outside card location")
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
	if v.AccessAllowed || v.Status != "unverified" {
		t.Fatal("junction hid a card directory")
	}
}

// Opt-in acceptance uses only a separately prequalified self source root and
// the normal internal registration/identity path. Never prints project data.
func TestOwnerLiveSourceBudget(t *testing.T) {
	root := os.Getenv("CC_GUARD_LIVE_SELF_SOURCE")
	if root == "" {
		t.Skip("explicit prequalified self-source metadata acceptance only")
	}
	if !ownerRefreshIdentity(root) {
		t.Fatal("normal bounded metadata refresh failed")
	}
	targets := []struct{ kind, path string }{{"registered-root", root}, {"root-tracked-file", filepath.Join(root, "Cargo.toml")}}
	if tree := os.Getenv("CC_GUARD_LIVE_SELF_WORKTREE"); tree != "" {
		targets = append(targets, struct{ kind, path string }{"worktree-tracked-file", filepath.Join(tree, "Cargo.toml")})
	}
	for _, target := range targets {
		t.Run(target.kind, func(t *testing.T) {
			accepted := false
			for attempt := 0; attempt < 3; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
				calls := 0
				started := time.Now()
				access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) }, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership, identity: func(repo string) (int64, string, string, bool) {
					return ownerCachedIdentityAt(ctx, repo, ownerIdentityHome, time.Now, func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false })
				}}
				v := ownerSourceQuery(ctx, target.path, access)
				elapsed := time.Since(started)
				cancel()
				t.Logf("attempt=%d allowed=%v status=%s elapsed=%s API_calls=%d", attempt+1, v.AccessAllowed, v.Status, elapsed, calls)
				accepted = accepted || v.AccessAllowed
				if v.AccessAllowed && elapsed >= 750*time.Millisecond {
					t.Fatal("successful qualification exceeded its admission deadline")
				}
				if elapsed > time.Second {
					t.Fatal("source check exceeded the consumer's one-second budget")
				}
			}
			if !accepted {
				t.Fatal("tracked self source never qualified within the finite budget")
			}
		})
	}
}
