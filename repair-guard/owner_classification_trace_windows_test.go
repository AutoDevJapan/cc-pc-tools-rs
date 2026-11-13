package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestClassificationDiagnosticCLIIsOptInAndDoesNotChangeVerdict(t *testing.T) {
	calls := 0
	classify := func(string) (ownerPathVerdict, *ownerClassificationTrace) {
		calls++
		return ownerRuleUnknown("classification_deadline"), &ownerClassificationTrace{Stage: "classification_deadline", RegistrationReads: 1, GitCalls: 1, DeadlineStage: "git"}
	}
	var out, err bytes.Buffer
	exit := runClassificationDiagnosticCLI([]string{"--guard-path", "C:/owned/source", "--classification-diagnostic"}, &out, &err, classify)
	var v map[string]any
	if exit != 0 || err.Len() != 0 || calls != 1 || json.Unmarshal(out.Bytes(), &v) != nil || v["access_allowed"] != false || v["classification_basis"] != "classification_deadline" {
		t.Fatal("trace changed failed classification", exit, out.String(), err.String())
	}
	for _, args := range [][]string{{"--classification-diagnostic"}, {"--guard-path", "relative", "--classification-diagnostic"}, {"--refresh-owner-source", "C:/owned/source", "--classification-diagnostic"}, {"--guard-path", "C:/owned/source", "--classification-diagnostic", "--diagnostic"}, {"--guard-path", "C:/owned/source", "--classification-diagnostic", "unexpected"}} {
		out.Reset()
		err.Reset()
		before := calls
		if runClassificationDiagnosticCLI(args, &out, &err, classify) != 2 || calls != before || out.Len() != 0 {
			t.Fatal("invalid trace args invoked classification")
		}
	}
}

func TestClassificationDiagnosticIdentityTimesNeverRenewTTL(t *testing.T) {
	now := time.Unix(10000, 0)
	record := ownerIdentityRecord{Domain: "owner-source-identity-v1", Catalog: hashBytes(ownerDepartmentCatalog), Repository: "AutoDevJapan/owned", Owner: "AutoDevJapan", ID: 12345, Checked: now.Unix() - 299}
	trace := &ownerClassificationTrace{}
	ctx := context.WithValue(context.Background(), ownerClassificationTraceKey{}, trace)
	ownerClassificationIdentity(ctx, record, record.Repository, now)
	if trace.Checked == nil || *trace.Checked != record.Checked || trace.Expires == nil || *trace.Expires != record.Checked+300 || trace.IdentityValid == nil || !*trace.IdentityValid {
		t.Fatal("cache metadata changed boundary")
	}
	ownerClassificationIdentity(ctx, record, record.Repository, now.Add(time.Second))
	if trace.IdentityValid == nil || *trace.IdentityValid || record.Checked != 9701 {
		t.Fatal("trace renewed expired identity")
	}
	b, _ := json.Marshal(trace)
	if bytes.Contains(b, []byte("AutoDevJapan")) || bytes.Contains(b, []byte("owned")) {
		t.Fatal("identity name exposed")
	}
	ownerClassificationIdentity(ctx, ownerIdentityRecord{}, "AutoDevJapan/unknown", now)
	if trace.Checked == nil || *trace.Checked != 9701 {
		t.Fatal("invalid record changed qualified metadata")
	}
}

func TestClassificationDiagnosticDeadlineKeepsFailureAndFixedPhase(t *testing.T) {
	trace := &ownerClassificationTrace{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ownerClassificationTraceKey{}, trace))
	done := ownerClassificationPhase(ctx, "registration")
	cancel()
	done()
	if trace.DeadlineStage != "registration" || trace.RegistrationReads != 1 {
		t.Fatal("deadline phase discarded")
	}
	ownerClassificationPhase(ctx, "sensitive-owned-placeholder")()
	b, _ := json.Marshal(trace)
	if strings.Contains(string(b), "sensitive-owned-placeholder") {
		t.Fatal("unknown phase escaped")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("observer changed cancellation")
	}
}

func TestClassificationDiagnosticDoesNotChangeOrdinaryOutput(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runGuardCLIWithPath([]string{"--guard-path", "C:/owned/source"}, &out, &stderr, nil, func(string) ownerPathVerdict { return ownerRuleClear("verified_self_repository") })
	if code != 0 || stderr.Len() != 0 || bytes.Contains(out.Bytes(), []byte("classification_diagnostic")) {
		t.Fatal("ordinary output changed")
	}
}

func TestClassificationOwnedGitLaunchTiming(t *testing.T) {
	if os.Getenv("F4431_CLASSIFY_TIMING") != "1" {
		t.Skip("finite workstation performance diagnostic")
	}
	root := diagnosticHome(t)
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/owned-timing-fixture.git")
	for _, path := range []string{"C:/Program Files/Git/cmd/git.exe", "C:/Program Files/Git/mingw64/bin/git.exe"} {
		var samples []int64
		for i := 0; i < 4; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			cmd := exec.CommandContext(ctx, path, "-C", root, "remote", "get-url", "origin")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			cmd.Env = civ6GitEnvironment(os.Environ())
			started := time.Now()
			raw, err := cmd.Output()
			elapsed := time.Since(started).Milliseconds()
			cancel()
			if err != nil || strings.TrimSpace(string(raw)) != "https://github.com/AutoDevJapan/owned-timing-fixture.git" {
				t.Fatal("owned Git semantic control failed")
			}
			samples = append(samples, elapsed)
		}
		t.Logf("known_installation_entry=%s elapsed_ms=%v", filepath.ToSlash(strings.TrimPrefix(path, "C:/Program Files/Git/")), samples)
	}
}

func TestClassificationNestedWorktreeReusesOnlySameAttemptTopProbe(t *testing.T) {
	config, access := ownerRuleFixture(t)
	registered := filepath.Join(config.dev, "ADJ", "registered")
	top := registered + "-wt/owned"
	if os.MkdirAll(filepath.Join(top, ".git"), 0700) != nil {
		t.Fatal("fixture repo")
	}
	projects := object{"self-fixture": registered, "second-self": registered}
	queries, topQueries := 0, 0
	access.git = func(root string, args ...string) (string, bool) {
		queries++
		if len(args) == 3 && args[0] == "remote" {
			return "https://github.com/AutoDevJapan/owned", true
		}
		if len(args) > 0 && args[0] == "remote" {
			return "https://github.com/AutoDevJapan/owned", true
		}
		if same(root, top) {
			topQueries++
		}
		return root + "\nC:/owned-common", true
	}
	v := ownerRuleWith(context.Background(), top, projects, access, config, nil)
	if !v.AccessAllowed {
		t.Fatal("normal nested worktree rejected", v.Basis, queries)
	}
	if topQueries != 1 {
		t.Fatal("same current worktree metadata probed repeatedly", topQueries, queries)
	}
	before := queries
	v = ownerRuleWith(context.Background(), top, projects, access, config, nil)
	if !v.AccessAllowed || queries == before {
		t.Fatal("probe cache leaked into another attempt")
	}
}

func TestClassificationMalformedIdentityNeverPublishesValidMetadata(t *testing.T) {
	home := diagnosticHome(t)
	repo := "AutoDevJapan/owned-trace"
	now := time.Now()
	valid := ownerIdentityRecord{Domain: "owner-source-identity-v1", Catalog: hashBytes(ownerDepartmentCatalog), Repository: repo, Owner: "AutoDevJapan", ID: 12345, Checked: now.Unix()}
	raw, e := json.Marshal(valid)
	if e != nil {
		t.Fatal(e)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"id":"not-an-integer"}`)...)
	sealed, ok := ownerIdentityDPAPI(raw, true)
	clear(raw)
	if !ok {
		t.Fatal("owned DPAPI fixture")
	}
	if os.WriteFile(filepath.Join(home, hashBytes([]byte(strings.ToLower(repo)))+".dpapi"), sealed, 0600) != nil {
		t.Fatal("owned sealed write")
	}
	clear(sealed)
	trace := &ownerClassificationTrace{}
	ctx := context.WithValue(context.Background(), ownerClassificationTraceKey{}, trace)
	_, _, _, verified := ownerCachedIdentityAt(ctx, repo, home, func() time.Time { return now }, func(context.Context, string) (int64, string, string, bool) { return 0, "", "", false })
	if verified {
		t.Fatal("malformed record allowed")
	}
	if trace.IdentityValid != nil || trace.Checked != nil || trace.Expires != nil {
		t.Fatal("decode failure published valid identity metadata")
	}
}
