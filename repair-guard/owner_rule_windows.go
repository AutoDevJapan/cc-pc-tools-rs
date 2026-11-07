package main

import (
	"context"
	"io"
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
	jobStore       string
	specStore      string
	promptStore    string
	todoStore      string
	mixedStores    []string
	taskMetadata   *ownerRecordMetadata // only the current validated snapshot
	taskMetadataID string
	recordProducer bool // a producer receipt, not qualification of source bytes
}

type ownerRuleTool struct {
	root    string
	command string
}

func ownerRuleWith(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig, owned func(string) ownerPathVerdict) ownerPathVerdict {
	// These are classification facts supplied by the owner, not task grants.
	development := ownerRuleDevelopment(path, config.dev)
	if development == "client" {
		return ownerRuleProtected("owner_rule_other_development_top")
	}
	for _, root := range config.records {
		if ownerRuleMixedStorePath(root, path) {
			return ownerRuleProtected("client_record_store")
		}
	}
	// This routine is also used for an owned resource's original producer.
	// Those internal calls must retain the same exclusions and name checks as
	// the outer normal CLI, rather than inheriting an operations root's result.
	if access.legacy != nil {
		switch access.legacy(path) {
		case ownerLegacyClient:
			return ownerRuleProtected("legacy_exclusion")
		case ownerLegacyUnavailable:
			return ownerRuleUnknown("existing_exclusion_unavailable")
		}
	} else if access.denied(path) {
		return ownerRuleProtected("legacy_exclusion")
	}
	if ownerCardPath(path) {
		return ownerRuleUnknown("card_name_or_storage_candidate")
	}
	if ownerWindowsAlias(path) {
		return ownerRuleUnknown("windows_alias")
	}
	if ctx.Err() != nil {
		return ownerRuleUnknown("classification_deadline")
	}
	if !ownerRulePlainPath(path, access.plain) {
		return ownerRuleUnknown("path_metadata_unavailable")
	}
	// Check every covering registration before any self evidence. This includes
	// a client worktree whose registered source is outside the operations roots.
	selfRegistration := false
	worktreeRoots := []string{}
	registrationUnavailable := false
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
			registrationUnavailable = true
			continue
		}
		selfRegistration = selfRegistration || membership == "self" || ownerRuleDevelopment(root, config.dev) == "self"
		if within(root+"-wt", path) {
			worktreeRoots = append(worktreeRoots, root)
		}
	}
	// Department declarations must also win for an unregistered product under
	// an otherwise ordinary operations/self-development location.
	if registrationUnavailable {
		return ownerRuleUnknown("registration_metadata_unavailable")
	}
	for _, part := range ownerRuleProductNames(path, config) {
		if access.membership(part, "") == "client_project" {
			return ownerRuleProtected("client_department")
		}
	}
	if result, handled := ownerRuleStoredContent(ctx, path, projects, access, config); handled {
		return result
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
			remotes, listed := access.git(top, "remote")
			if listed && strings.TrimSpace(remotes) == "" && (development == "self" || selfRegistration || ownerRuleInfrastructure(path, config)) {
				return ownerRuleClear("self_location_without_remotes")
			}
			return ownerRuleUnknown("repository_metadata_unavailable")
		}
		if access.membership(filepath.Base(top), repository) == "client_project" {
			return ownerRuleProtected("client_department")
		}
		if !selfOrigin {
			if development == "self" || selfRegistration || ownerRuleInfrastructure(path, config) {
				return ownerRuleUnknown("owner_location_conflict")
			}
			return ownerRuleProtected("owner_rule_unattributed_repository")
		}
		for _, root := range worktreeRoots {
			// For stored output, the bound original producer and real GitHub
			// identity establish provenance. It does not admit this source tree
			// for editing, which still needs the normal common-directory proof.
			if config.recordProducer {
				break
			}
			registered, ok := access.git(root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
			linked, otherOK := access.git(top, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
			a := strings.Split(strings.ReplaceAll(registered, "\r\n", "\n"), "\n")
			b := strings.Split(strings.ReplaceAll(linked, "\r\n", "\n"), "\n")
			if !ok || !otherOK {
				return ownerRuleUnknown("repository_metadata_unavailable")
			}
			if len(a) != 2 || len(b) != 2 || !same(a[0], root) || !same(b[0], top) || !same(a[1], b[1]) {
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
		for _, root := range worktreeRoots {
			// A non-Git registration still owns its ordinary sibling work area.
			if !ownerRepositoryCandidate(root) {
				continue
			}
			rel, err := filepath.Rel(root+"-wt", path)
			_, absent := os.Lstat(path)
			if err != nil || rel == "." || filepath.Dir(rel) != "." || !os.IsNotExist(absent) {
				return ownerRuleUnknown("worktree_identity_unavailable")
			}
			v := ownerRuleWith(ctx, root, projects, access, config, nil)
			if !v.AccessAllowed {
				return v
			}
			return ownerRuleClear("verified_planned_worktree")
		}
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
	if owned != nil {
		return owned(path)
	}
	return ownerRuleProtected("owner_rule_unattributed_location")
}

func ownerRuleInfrastructure(path string, config ownerRuleConfig) bool {
	for _, root := range config.infrastructure {
		if within(root, path) {
			return true
		}
	}
	return false
}

// Only product/container boundaries name a product. A similarly named data
// directory deep inside a self repository must not acquire client membership.
func ownerRuleProductNames(path string, config ownerRuleConfig) []string {
	roots := []string{}
	for _, name := range []string{"ADJ", "BM1314", "misc"} {
		roots = append(roots, filepath.Join(config.dev, name))
	}
	for _, root := range config.infrastructure {
		roots = append(roots, root, filepath.Join(root, "Work"), filepath.Join(root, "RuntimeSources"))
	}
	names := []string{}
	for _, root := range roots {
		if !within(root, path) {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != "." {
			names = append(names, strings.TrimSuffix(strings.Split(rel, string(os.PathSeparator))[0], "-wt"))
		}
	}
	return names
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
	// Same fixed owner as the development root. Some regular consumers clear
	// USERPROFILE/APPDATA; process environment must not change this identity.
	npm := "C:/Users/makhmeto/AppData/Roaming/npm"
	return ownerRuleConfig{
		dev:            "C:/Users/makhmeto/Desktop/dev",
		infrastructure: []string{"D:/CC", "D:/OMB"},
		records:        []string{"D:/CC/Tools/client-records", "D:/CC/Tools/client-projects.txt"},
		jobStore:       "D:/CC/Tools/codex-jobs/jobs", specStore: "D:/CC/Tools/todo/specs", promptStore: "D:/CC/Tools/scheduler/prompts", todoStore: productStore,
		mixedStores: []string{productStore, "D:/CC/Tools/todo/history", "D:/CC/Tools/todo/note-history-v1", "D:/CC/Tools/todo/stall_summary.json", "D:/CC/Tools/todo/capacity-decisions", discordAskDirectory, "D:/OMB/workspace"},
		// Normal consumer declarations: cc-jobs/src/platform.rs and pool_retry.rs,
		// cc-scheduler/src/backend.rs. The owner explicitly assigned this existing
		// AI-agent tool's installed location for classification in 4088/2655.
		tools: []ownerRuleTool{{filepath.Join(npm, "node_modules", "@bitkyc08", "opencodex"), filepath.Join(npm, "opencodex.cmd")}},
	}
}

func ownerOrganizationQuery(ctx context.Context, path string, access ownerSourceAccess) ownerPathVerdict {
	access.legacy = ownerLegacyProtection
	access.conversation = conversationPathAllowed
	access.operational = ownerRuleOperationalPathAllowed
	return ownerOrganizationQueryWith(ctx, path, access, ownerDefaultRuleConfig(), func(ctx context.Context, project func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
		return ownerReadProjectSnapshotUsing(ctx, productStore, legacyMatch, project)
	})
}

func ownerOrganizationQueryWith(ctx context.Context, path string, access ownerSourceAccess, config ownerRuleConfig, read func(context.Context, func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool)) (result ownerPathVerdict) {
	defer func() {
		if !result.AccessAllowed && ownerRuleRecordedPath(path, config) {
			result.ReceiptRestricted = true
		}
	}()
	if ownerRuleDevelopment(path, config.dev) == "client" {
		return ownerRuleProtected("owner_rule_other_development_top")
	}
	for _, root := range config.records {
		if ownerRuleMixedStorePath(root, path) {
			return ownerRuleProtected("client_record_store")
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		config.taskMetadata = nil
		config.taskMetadataID = ""
		var snapshot *ownerProjectSnapshot
		var ok bool
		inputID := ""
		for _, store := range []string{config.specStore, config.promptStore} {
			if same(filepath.Dir(path), store) {
				inputID, _, _ = strings.Cut(filepath.Base(path), "-")
			}
		}
		if inputID != "" {
			snapshot, ok = read(ctx, func(input io.Reader) (object, bool) {
				projects, m, valid := ownerTaskRegistrationProjection(ctx, input, inputID)
				if valid {
					config.taskMetadata = &m
					config.taskMetadataID = inputID
				}
				return projects, valid && projects != nil
			})
		} else {
			snapshot, ok = read(ctx, func(input io.Reader) (object, bool) { return ownerProjectProjection(ctx, input) })
		}
		if ctx.Err() != nil {
			return ownerRuleUnknown("classification_deadline")
		}
		if !ok {
			if attempt == 0 && snapshot != nil && snapshot.replaced() {
				continue
			}
			return ownerRuleUnknown("registration_snapshot_unavailable")
		}
		result := ownerRuleWith(ctx, path, snapshot.projects, access, config, func(p string) ownerPathVerdict {
			return ownerRuleOwnedTemporary(ctx, p, snapshot.projects, access, config)
		})
		if result.Status == "protected" && result.Basis != "owner_rule_unattributed_location" {
			return result
		}
		if ctx.Err() != nil {
			return ownerRuleUnknown("classification_deadline")
		}
		if snapshot.current() {
			return result
		}
		if attempt == 1 || !snapshot.replaced() {
			return ownerRuleUnknown("registration_snapshot_changed")
		}
	}
	return ownerRuleUnknown("registration_snapshot_changed")
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
	return ownerRuleFinalize(result, path, conversationPathAllowed, ownerRuleOperationalPathAllowed, repairPathAllowed)
}

func ownerRuleOperationalPathAllowed(path string) bool {
	return discordAskRuntime(path, legacyMatch)
}

// Exact independent proofs survive a transient registration failure. A mixed
// body or a positive client classification never acquires the generic fallback.
func ownerRuleFinalize(result ownerPathVerdict, path string, conversation, operational, receipt func(string) bool) ownerPathVerdict {
	transient := result
	transient.ReceiptRestricted = false
	if result.ReceiptRestricted && result.Status == "unverified" && ownerRuleIndependentReceiptEligible(transient) {
		if conversation(path) {
			return ownerRuleClear("verified_conversation_record")
		}
		if operational(path) {
			return ownerRuleClear("verified_operational_receipt")
		}
	}
	return ownerRuleReceiptFallback(result, path, receipt)
}

// A transient metadata failure does not erase an independently checked old
// operational receipt. Positive client evidence, conflicts and mixed data
// stores never acquire this fallback.
func ownerRuleReceiptFallback(result ownerPathVerdict, path string, receipt func(string) bool) ownerPathVerdict {
	if ownerRuleIndependentReceiptEligible(result) {
		if receipt(path) {
			return ownerRuleClear("verified_operational_receipt")
		}
	}
	return result
}

func ownerRuleIndependentReceiptEligible(v ownerPathVerdict) bool {
	if v.ReceiptRestricted {
		return false
	}
	if v.Status == "protected" {
		return v.Basis == "owner_rule_unattributed_location" && len(v.Categories) == 1 && v.Categories[0] == "client_project"
	}
	if v.Status != "unverified" || len(v.Categories) != 0 {
		return false
	}
	switch v.Basis {
	case "", "owner_rule_unattributed_location", "registration_snapshot_unavailable", "registration_snapshot_changed", "classification_deadline", "repository_identity_unavailable", "repository_metadata_unavailable", "owned_resource_metadata_unavailable":
		return true
	}
	return false
}
