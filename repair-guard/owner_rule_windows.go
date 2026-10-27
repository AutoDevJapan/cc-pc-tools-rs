package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ownerRuleConfig struct {
	dev            string
	infrastructure []string
	records        []string
	tools          []ownerRuleTool
}

type ownerRuleTool struct {
	root    string
	command string
}

func ownerRuleWith(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig, owned func(string) bool) ownerPathVerdict {
	// These are classification facts supplied by the owner, not task grants.
	development := ownerRuleDevelopment(path, config.dev)
	if development == "client" {
		return ownerRuleProtected("owner_rule_other_development_top")
	}
	for _, root := range config.records {
		if within(root, path) {
			return ownerRuleProtected("client_record_store")
		}
	}
	// This routine is also used for an owned resource's original producer.
	// Those internal calls must retain the same exclusions and name checks as
	// the outer normal CLI, rather than inheriting an operations root's result.
	if access.denied(path) {
		return ownerRuleUnknown("existing_exclusion_unavailable_or_matched")
	}
	if ownerCardPath(path) {
		return ownerRuleUnknown("card_name_or_storage_candidate")
	}
	if ownerWindowsAlias(path) {
		return ownerRuleUnknown("windows_alias")
	}
	if ctx.Err() != nil || !ownerRulePlainPath(path, access.plain) {
		return ownerRuleUnknown("path_metadata_unavailable")
	}
	// Check every covering registration before any self evidence. This includes
	// a client worktree whose registered source is outside the operations roots.
	selfRegistration := false
	worktreeRoots := []string{}
	for project, value := range projects {
		root, ok := value.(string)
		if !ok || !filepath.IsAbs(root) || (!within(root, path) && !within(root+"-wt", path)) {
			continue
		}
		membership := access.membership(project, "")
		if membership == "client_project" || ownerRuleDevelopment(root, config.dev) == "client" {
			return ownerRuleProtected("client_registration")
		}
		if !ownerRulePlainPath(root, access.plain) || access.denied(root) {
			return ownerRuleUnknown("registration_metadata_unavailable")
		}
		selfRegistration = selfRegistration || membership == "self" || ownerRuleDevelopment(root, config.dev) == "self"
		if within(root+"-wt", path) {
			worktreeRoots = append(worktreeRoots, root)
		}
	}
	// Department declarations must also win for an unregistered product under
	// an otherwise ordinary operations/self-development location.
	for _, part := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		if access.membership(part, "") == "client_project" {
			return ownerRuleProtected("client_department")
		}
	}
	probe := path
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return ownerRuleUnknown("path_metadata_unavailable")
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return ownerRuleUnknown("path_metadata_unavailable")
		}
		probe = parent
	}
	top, repositoryPresent := ownerRepositoryRoot(probe)
	if repositoryPresent {
		origin, ok := access.git(top, "remote", "get-url", "origin")
		repository, selfOrigin := ownerOriginRepository(origin)
		if !ok {
			return ownerRuleUnknown("repository_metadata_unavailable")
		}
		if access.membership(filepath.Base(top), repository) == "client_project" {
			return ownerRuleProtected("client_department")
		}
		if !selfOrigin {
			if development == "self" {
				return ownerRuleUnknown("owner_location_conflict")
			}
			return ownerRuleProtected("owner_rule_unattributed_repository")
		}
		for _, root := range worktreeRoots {
			registered, ok := access.git(root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
			linked, otherOK := access.git(top, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
			a := strings.Split(strings.ReplaceAll(registered, "\r\n", "\n"), "\n")
			b := strings.Split(strings.ReplaceAll(linked, "\r\n", "\n"), "\n")
			if !ok || !otherOK || len(a) != 2 || len(b) != 2 || !same(a[0], root) || !same(b[0], top) || !same(a[1], b[1]) {
				return ownerRuleUnknown("worktree_identity_mismatch")
			}
		}
		id, actual, owner, verified := access.identity(repository)
		if !verified || id <= 0 || !strings.EqualFold(actual, repository) || !ownerSelfGitHubOwner(owner) || !strings.EqualFold(strings.Split(repository, "/")[0], owner) {
			return ownerRuleUnknown("repository_identity_unavailable")
		}
		if id == 1156778979 {
			return ownerRuleProtected("client_repository_id")
		}
		if ctx.Err() != nil {
			return ownerRuleUnknown("classification_deadline")
		}
		return ownerRuleClear("verified_self_repository")
	}
	if len(worktreeRoots) != 0 {
		return ownerRuleUnknown("worktree_identity_unavailable")
	}
	if ctx.Err() != nil {
		return ownerRuleUnknown("classification_deadline")
	}
	if development == "self" {
		return ownerRuleClear("owner_rule_self_development_top")
	}
	if selfRegistration {
		return ownerRuleClear("self_project_registration")
	}
	for _, root := range config.infrastructure {
		if within(root, path) {
			return ownerRuleClear("owner_rule_operations_infrastructure")
		}
	}
	// Existing PC-tool consumers declare this installed software location. This
	// identifies its operational use; it does not verify the package's bytes or
	// grant access to account/configuration data beside the installation.
	for _, tool := range config.tools {
		if within(tool.root, path) && access.plain(tool.root, true) && access.plain(tool.command, false) && !access.denied(tool.command) {
			return ownerRuleClear("operational_tool_installation")
		}
	}
	if owned != nil && owned(path) {
		return ownerRuleClear("self_owned_temporary_resource")
	}
	return ownerRuleProtected("owner_rule_unattributed_location")
}

func ownerRuleDevelopment(path, root string) string {
	if root == "" || !within(root, path) {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return "client"
	} // mixed container
	top := strings.Split(rel, string(os.PathSeparator))[0]
	for _, name := range []string{"ADJ", "BM1314", "misc"} {
		if strings.EqualFold(top, name) {
			return "self"
		}
	}
	return "client"
}

func ownerRulePlainPath(path string, plain func(string, bool) bool) bool {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			return plain(path, info.IsDir())
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func ownerRuleClear(basis string) ownerPathVerdict {
	return ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}, Basis: basis}
}
func ownerRuleProtected(basis string) ownerPathVerdict {
	v := ownerProtected("client_project")
	v.Basis = basis
	return v
}
func ownerRuleUnknown(basis string) ownerPathVerdict {
	v := ownerUnverified()
	v.Basis = basis
	return v
}

func ownerDefaultRuleConfig() ownerRuleConfig {
	home, _ := os.UserHomeDir()
	npm := filepath.Join(home, "AppData", "Roaming", "npm")
	return ownerRuleConfig{
		dev:            "C:/Users/makhmeto/Desktop/dev",
		infrastructure: []string{"D:/CC", "D:/OMB"},
		records:        []string{"D:/CC/Tools/client-records", "D:/CC/Tools/client-projects.txt"},
		// Normal consumer declarations: cc-jobs/src/platform.rs and pool_retry.rs,
		// cc-scheduler/src/backend.rs. The owner explicitly assigned this existing
		// AI-agent tool's installed location for classification in 4088/2655.
		tools: []ownerRuleTool{{filepath.Join(npm, "node_modules", "@bitkyc08", "opencodex"), filepath.Join(npm, "opencodex.cmd")}},
	}
}

func ownerOrganizationQuery(ctx context.Context, path string, access ownerSourceAccess) ownerPathVerdict {
	config := ownerDefaultRuleConfig()
	if ownerRuleDevelopment(path, config.dev) == "client" {
		return ownerRuleProtected("owner_rule_other_development_top")
	}
	for _, root := range config.records {
		if within(root, path) {
			return ownerRuleProtected("client_record_store")
		}
	}
	snapshot, ok := ownerReadProjectSnapshot(ctx, productStore, legacyMatch)
	if !ok {
		return ownerRuleUnknown("registration_snapshot_unavailable")
	}
	result := ownerRuleWith(ctx, path, snapshot.projects, access, config, func(p string) bool {
		return ownerRuleOwnedTemporary(ctx, p, snapshot.projects, access, config)
	})
	if !snapshot.current() || ctx.Err() != nil {
		return ownerRuleUnknown("registration_snapshot_changed")
	}
	return result
}

func ownerOrganizationPath(path string) ownerPathVerdict {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	refresh := ""
	access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) },
		identity: func(repo string) (int64, string, string, bool) {
			id, name, owner, ok := ownerCachedIdentity(ctx, repo)
			if !ok || ownerIdentityAging(repo, ownerIdentityHome, time.Now()) {
				refresh = repo
			}
			return id, name, owner, ok
		}, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership,
	}
	result := ownerOrganizationQuery(ctx, path, access)
	if refresh != "" && ctx.Err() == nil {
		ownerRequestIdentityRefresh(path, refresh)
	}
	// Existing independently checked operational receipts remain evidence. A
	// positively classified client, conflict or I/O failure never takes this path.
	if result.Basis == "owner_rule_unattributed_location" && repairPathAllowed(path) {
		return ownerRuleClear("verified_operational_receipt")
	}
	return result
}
