package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ownerRuleFixture(t *testing.T) (ownerRuleConfig, ownerSourceAccess) {
	t.Helper()
	base, err := physical(currentShortFixture(t))
	if err != nil {
		t.Fatal("fixture metadata")
	}
	config := ownerRuleConfig{dev: filepath.Join(base, "dev"), infrastructure: []string{filepath.Join(base, "operations")}, records: []string{filepath.Join(base, "operations", "private-records")}}
	for _, dir := range []string{config.dev, config.infrastructure[0], config.records[0]} {
		if os.MkdirAll(dir, 0700) != nil {
			t.Fatal("fixture mkdir")
		}
	}
	access := ownerSourceAccess{git: currentGit, plain: currentPlain, denied: func(string) bool { return false },
		identity: func(repo string) (int64, string, string, bool) {
			owner, _, _ := strings.Cut(repo, "/")
			return 12345, repo, owner, true
		},
		membership: func(project, repository string) string {
			if project == "contracted-fixture" || strings.HasSuffix(repository, "/contracted-fixture") {
				return "client_project"
			}
			if project == "self-fixture" {
				return "self"
			}
			return ""
		},
	}
	return config, access
}

func TestOwnerRule4088UntrackedAndUnknown(t *testing.T) {
	config, access := ownerRuleFixture(t)
	for _, tc := range []struct{ label, relative, status string }{
		{"unlisted top", "dev/unknown-fixture/child", "protected"},
		{"ADJ untracked", "dev/ADJ/untracked/child", "clear"},
		{"BM1314 untracked", "dev/BM1314/untracked/child", "clear"},
		{"misc untracked", "dev/misc/untracked/child", "clear"},
		{"case insensitive", "dev/adj/untracked/child", "clear"},
		{"similar top", "dev/ADJ-other/child", "protected"},
		{"mixed development root", "dev", "protected"},
		{"unknown outside", "unattributed/child", "protected"},
		{"operations", "operations/tool/child", "clear"},
		{"record store", "operations/private-records/child", "protected"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			path := filepath.Join(filepath.Dir(config.dev), filepath.FromSlash(tc.relative))
			if os.MkdirAll(path, 0700) != nil {
				t.Fatal("fixture mkdir")
			}
			v := ownerRuleWith(context.Background(), path, object{}, access, config, nil)
			if v.Status != tc.status || v.AccessAllowed != (tc.status == "clear") || v.Basis == "" {
				t.Fatal("owner rule classification differs")
			}
			if v.Status == "protected" && (len(v.Categories) != 1 || v.Categories[0] != "client_project") {
				t.Fatal("owner rule lacks client category")
			}
		})
	}
}

func TestOwnerRule4088ProtectionsPrecedeSelf(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(config.dev, "ADJ", "untracked")
	if os.MkdirAll(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	for _, tc := range []struct {
		label, leaf string
		legacy      ownerLegacyState
	}{
		{"card", "credit-cards.json", ownerLegacyClear},
		{"exclusion", "ordinary.txt", ownerLegacyClient},
		{"alias", "ordinary.txt:stream", ownerLegacyClear},
	} {
		t.Run(tc.label, func(t *testing.T) {
			path := filepath.Join(root, tc.leaf)
			v := ownerPolicyClassify(path, ownerPolicyAccess{legacy: func(string) ownerLegacyState { return tc.legacy }, repair: func(string) bool { return true }, physical: physical,
				organization: func(p string) ownerPathVerdict {
					return ownerRuleWith(context.Background(), p, object{"contracted-fixture": root}, access, config, nil)
				},
			})
			if v.AccessAllowed {
				t.Fatal("protection lost to self location or repair qualification")
			}
		})
	}
	alias := filepath.Join(root, "safe-alias")
	v := ownerPolicyClassify(alias, ownerPolicyAccess{legacy: func(string) ownerLegacyState { return ownerLegacyClear }, repair: func(string) bool { return true },
		physical: func(string) (string, error) { return root, nil }, organization: func(p string) ownerPathVerdict {
			return ownerRuleWith(context.Background(), p, object{}, access, config, nil)
		},
	})
	if v.AccessAllowed {
		t.Fatal("different physical identity admitted")
	}
}

func TestOwnerRule4088RepositoryAndCoveringClient(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(config.dev, "misc", "repository")
	if os.MkdirAll(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/mashfromband/example.git")
	path := filepath.Join(root, "untracked")
	if os.Mkdir(path, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if !ownerRuleWith(context.Background(), path, object{}, access, config, nil).AccessAllowed {
		t.Fatal("verified self repository required tracking or catalogue membership")
	}
	for _, tc := range []struct {
		label    string
		id       int64
		verified bool
		status   string
	}{
		{"contracted ID", 1156778979, true, "protected"},
		{"distinct alpha ID", 1154066087, true, "clear"},
		{"unknown identity", 0, false, "unverified"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			probe := access
			probe.identity = func(repo string) (int64, string, string, bool) { return tc.id, repo, "mashfromband", tc.verified }
			if v := ownerRuleWith(context.Background(), path, object{}, probe, config, nil); v.Status != tc.status {
				t.Fatal("repository identity precedence differs")
			}
		})
	}
	projects := object{"self-fixture": config.infrastructure[0], "contracted-fixture": root}
	if v := ownerRuleWith(context.Background(), path, projects, access, config, nil); v.Status != "protected" {
		t.Fatal("covering client registration lost priority")
	}
	clientRoot := filepath.Join(config.dev, "unknown-fixture", "repository")
	projects = object{"unknown": clientRoot}
	worktree := clientRoot + "-wt"
	if os.MkdirAll(worktree, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if v := ownerRuleWith(context.Background(), worktree, projects, access, config, nil); v.Status != "protected" {
		t.Fatal("client worktree lost source classification")
	}
	cardDirectory := filepath.Join(root, "credit-cards")
	if os.Mkdir(cardDirectory, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	if !ownerCardPath(cardDirectory) || !ownerCardPath(filepath.Join(cardDirectory, "handler.go")) {
		t.Fatal("repository marker concealed nested card storage")
	}
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/OtherOwner/example.git")
	if v := ownerRuleWith(context.Background(), path, object{}, access, config, nil); v.Status != "unverified" || v.Basis != "owner_location_conflict" {
		t.Fatal("self location conflict was silently resolved")
	}
}

func TestOwnerRule4088ExternalRepositoryAndDeadline(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(filepath.Dir(config.dev), "external-repository")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/BM1314/example.git")
	if !ownerRuleWith(context.Background(), root, object{}, access, config, nil).AccessAllowed {
		t.Fatal("actual self owner outside development root rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ownerRuleWith(ctx, root, object{}, access, config, nil).AccessAllowed {
		t.Fatal("expired classification admitted")
	}
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/OtherOwner/example.git")
	if v := ownerRuleWith(context.Background(), root, object{}, access, config, nil); v.Status != "protected" {
		t.Fatal("unattributed external repository admitted")
	}
}

func TestOwnerRule4088OperationalInstallationUsesDeclaredCommand(t *testing.T) {
	config, access := ownerRuleFixture(t)
	base := filepath.Join(filepath.Dir(config.dev), "installed-tools")
	tool := ownerRuleTool{filepath.Join(base, "package"), filepath.Join(base, "command.cmd")}
	if os.MkdirAll(tool.root, 0700) != nil || os.WriteFile(tool.command, []byte("synthetic command registration"), 0600) != nil {
		t.Fatal("fixture install")
	}
	config.tools = []ownerRuleTool{tool}
	if !ownerRuleWith(context.Background(), tool.root, object{}, access, config, nil).AccessAllowed {
		t.Fatal("declared operational software rejected")
	}
	if ownerRuleWith(context.Background(), base, object{}, access, config, nil).AccessAllowed {
		t.Fatal("operational installation widened to parent/account data")
	}
	if v := ownerRuleWith(context.Background(), tool.root, object{"contracted-fixture": tool.root}, access, config, nil); v.Status != "protected" {
		t.Fatal("tool declaration overrode client registration")
	}
	config.tools[0].command = filepath.Join(base, "missing-command.cmd")
	if ownerRuleWith(context.Background(), tool.root, object{}, access, config, nil).AccessAllowed {
		t.Fatal("unregistered installed command admitted")
	}
}

func TestOwnerRule4088ExternalEvidenceAndRedaction(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(filepath.Dir(config.dev), "outside")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	for _, tc := range []struct {
		label    string
		projects object
		owned    bool
		status   string
	}{
		{"registered self", object{"self-fixture": root}, false, "clear"},
		{"unattributed registration", object{"unknown": root}, false, "protected"},
		{"owned self", object{}, true, "clear"},
		{"owned client", object{"contracted-fixture": root}, true, "protected"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			classify := func(p string) ownerPathVerdict {
				return ownerRuleWith(context.Background(), p, tc.projects, access, config, func(string) ownerPathVerdict {
					if tc.owned {
						return ownerRuleClear("self_owned_temporary_resource")
					}
					return ownerRuleProtected("owner_rule_unattributed_location")
				})
			}
			var out, errout bytes.Buffer
			code := runGuardCLIWithPath([]string{"--guard-path", root}, &out, &errout, nil, classify)
			var result object
			if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || result["protection_status"] != tc.status || result["original_registry_recovered"] != false || result["classification_policy"] != "owner-client-rule-20261027" || result["classification_basis"] == "" {
				t.Fatal("normal CLI contract differs")
			}
			if bytes.Contains(out.Bytes(), []byte(filepath.Base(root))) || bytes.Contains(out.Bytes(), []byte("contracted-fixture")) || errout.Len() != 0 {
				t.Fatal("private classification evidence disclosed")
			}
		})
	}
}
