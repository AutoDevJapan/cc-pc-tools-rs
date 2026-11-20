package main

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	unslopSavedTodoID = "4431"
	unslopSavedRepositoryID int64 = 1406909338
	unslopSavedRepository = "AutoDevJapan/GoDD-Unslop"
	unslopSavedOwner = "AutoDevJapan"
)

// This result identifies one saved association. It grants no file or process access.
type unslopSavedProjectResult struct {
	SchemaVersion int `json:"schema_version"`
	Scope string `json:"scope"`
	TodoID string `json:"todo_id"`
	RepositoryID int64 `json:"repository_id"`
	Qualified bool `json:"qualified"`
	Reason string `json:"reason"`
	ProjectKey string `json:"project_key,omitempty"`
	AssociationRevision string `json:"association_revision,omitempty"`
}

func unslopProjectRefusal(reason string) unslopSavedProjectResult {
	return unslopSavedProjectResult{
		SchemaVersion: 1, Scope: "saved_project_association", TodoID: unslopSavedTodoID,
		RepositoryID: unslopSavedRepositoryID, Reason: reason,
	}
}

func unslopProjectRevisionArgument(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

// The store and all source accessors are fixed here, not supplied by CLI input.
func resolveUnslopSavedProject(parent context.Context, expectedRevision string) unslopSavedProjectResult {
	if !unslopProjectRevisionArgument(expectedRevision) {
		return unslopProjectRefusal("unslop_project_invalid_request")
	}
	ctx, cancel := context.WithTimeout(parent, 750*time.Millisecond)
	defer cancel()
	access := ownerSourceAccess{
		git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) },
		identity: func(repository string) (int64, string, string, bool) { return ownerCachedIdentity(ctx, repository) },
		plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership,
		legacy: ownerLegacyProtection,
	}
	snapshot, record, ok := readUnslopSavedProject(ctx, productStore, legacyMatch)
	if !ok {
		if ctx.Err() != nil {
			return unslopProjectRefusal("unslop_project_deadline")
		}
		return unslopProjectRefusal("unslop_project_registration_unavailable")
	}
	return resolveUnslopProjectSnapshot(ctx, expectedRevision, snapshot, record, access, ownerDefaultRuleConfig())
}

// Reuse the ordinary metadata reader and its single-stream task/registration projection.
// Tests use the same reader on their own file; no alternate installed DB route exists.
func readUnslopSavedProject(ctx context.Context, store string, denied func(string) bool) (*ownerProjectSnapshot, ownerRecordMetadata, bool) {
	var record ownerRecordMetadata
	snapshot, ok := ownerReadProjectSnapshotUsing(ctx, store, denied, func(input io.Reader) (object, bool) {
		projects, selected, valid := ownerTaskRegistrationProjection(ctx, input, unslopSavedTodoID)
		record = selected
		return projects, valid
	})
	return snapshot, record, ok
}

// This owner-internal consumer accepts only a snapshot issued by the normal reader.
// The public operation has no path, repository, identity, callback or qualification input.
func resolveUnslopProjectSnapshot(ctx context.Context, expectedRevision string, snapshot *ownerProjectSnapshot, record ownerRecordMetadata, access ownerSourceAccess, config ownerRuleConfig) unslopSavedProjectResult {
	if !unslopProjectRevisionArgument(expectedRevision) {
		return unslopProjectRefusal("unslop_project_invalid_request")
	}
	if ctx.Err() != nil {
		return unslopProjectRefusal("unslop_project_deadline")
	}
	if snapshot == nil || access.git == nil || access.identity == nil || access.plain == nil || access.denied == nil || access.membership == nil {
		return unslopProjectRefusal("unslop_project_registration_unavailable")
	}
	if !snapshot.current() {
		return unslopProjectRefusal("unslop_project_snapshot_changed")
	}
	if record.id == "" && len(record.ids) == 0 {
		return unslopProjectRefusal("unslop_project_not_found")
	}
	if record.invalid || !record.matchesID(unslopSavedTodoID) || record.todo != "" && record.todo != unslopSavedTodoID {
		return unslopProjectRefusal("unslop_project_record_invalid")
	}
	if record.project == "" || record.project != strings.TrimSpace(record.project) || len(record.project) > 4096 || !utf8.ValidString(record.project) || strings.IndexFunc(record.project, unicode.IsControl) >= 0 || strings.ContainsAny(record.project, "/\\:") {
		return unslopProjectRefusal("unslop_project_metadata_missing")
	}
	root, registered := snapshot.projects[record.project].(string)
	if !registered || !filepath.IsAbs(root) || record.cwd == "" || record.repository == "" {
		return unslopProjectRefusal("unslop_project_metadata_missing")
	}
	if access.membership(record.project, record.repository) == "client_project" || access.denied(root) || access.denied(record.cwd) {
		return unslopProjectRefusal("unslop_project_source_protected")
	}
	if !strings.EqualFold(record.repository, unslopSavedRepository) || !filepath.IsAbs(record.cwd) || !(within(root, record.cwd) || within(root+"-wt", record.cwd)) {
		return unslopProjectRefusal("unslop_project_association_mismatch")
	}
	rootVerdict := ownerRuleWith(ctx, root, snapshot.projects, access, config, nil)
	originVerdict := ownerRuleRecordOriginSource(ctx, record, snapshot.projects, access, config)
	if ctx.Err() != nil {
		return unslopProjectRefusal("unslop_project_deadline")
	}
	if rootVerdict.Status == "protected" || originVerdict.Status == "protected" {
		return unslopProjectRefusal("unslop_project_source_protected")
	}
	if !rootVerdict.AccessAllowed || !originVerdict.AccessAllowed {
		return unslopProjectRefusal("unslop_project_source_unavailable")
	}
	// A registered self location alone is insufficient: bind its actual Git origin
	// and normal sealed identity cache to the fixed numeric repository identity.
	remote, remoteOK := access.git(root, "remote", "get-url", "origin")
	repository, selfOrigin := ownerOriginRepository(remote)
	if !remoteOK {
		return unslopProjectRefusal("unslop_project_repository_unavailable")
	}
	if !selfOrigin || !strings.EqualFold(repository, unslopSavedRepository) {
		return unslopProjectRefusal("unslop_project_association_mismatch")
	}
	id, actual, owner, verified := access.identity(repository)
	if ctx.Err() != nil {
		return unslopProjectRefusal("unslop_project_deadline")
	}
	if !verified {
		return unslopProjectRefusal("unslop_project_identity_unavailable")
	}
	if id != unslopSavedRepositoryID || !strings.EqualFold(actual, unslopSavedRepository) || !strings.EqualFold(owner, unslopSavedOwner) || !ownerSelfGitHubOwner(owner) {
		return unslopProjectRefusal("unslop_project_association_mismatch")
	}
	producerRoot, producerPresent := ownerRepositoryRoot(record.cwd)
	if !producerPresent {
		return unslopProjectRefusal("unslop_project_repository_unavailable")
	}
	producerRemote, producerOK := access.git(producerRoot, "remote", "get-url", "origin")
	producerRepository, producerSelf := ownerOriginRepository(producerRemote)
	if !producerOK {
		return unslopProjectRefusal("unslop_project_repository_unavailable")
	}
	if !producerSelf || !strings.EqualFold(producerRepository, unslopSavedRepository) {
		return unslopProjectRefusal("unslop_project_association_mismatch")
	}
	// Hash only the selected association, never private bodies or the whole store.
	binding, err := json.Marshal(struct {
		Todo, Project, Root, CWD, Worktree, Repository string
		RepositoryID int64
	}{unslopSavedTodoID, record.project, root, record.cwd, record.worktree, unslopSavedRepository, unslopSavedRepositoryID})
	if err != nil {
		return unslopProjectRefusal("unslop_project_metadata_missing")
	}
	revision := hashBytes(append([]byte("unslop-saved-project-association-v1\x00"), binding...))
	if ctx.Err() != nil {
		return unslopProjectRefusal("unslop_project_deadline")
	}
	snapshotCurrent := snapshot.current()
	if ctx.Err() != nil {
		return unslopProjectRefusal("unslop_project_deadline")
	}
	if !snapshotCurrent {
		return unslopProjectRefusal("unslop_project_snapshot_changed")
	}
	if expectedRevision != "" && expectedRevision != revision {
		return unslopProjectRefusal("unslop_project_revision_mismatch")
	}
	result := unslopProjectRefusal("unslop_project_resolved")
	result.Qualified, result.ProjectKey, result.AssociationRevision = true, record.project, revision
	return result
}

// The proposed main hunk calls this before the existing guard dispatcher.
func runUnslopSavedProjectCLI(args []string, output io.Writer) (bool, int) {
	return runUnslopSavedProjectCLIWithResolver(args, output, resolveUnslopSavedProject)
}

// Only the fixed wrapper above is exposed by the CLI. The resolver argument is
// an internal test seam; no command-line input can select a provider or authority.
func runUnslopSavedProjectCLIWithResolver(args []string, output io.Writer, resolve func(context.Context, string) unslopSavedProjectResult) (bool, int) {
	present := false
	for _, arg := range args {
		present = present || arg == "--resolve-unslop-saved-project" || strings.HasPrefix(arg, "--resolve-unslop-saved-project=")
	}
	if !present {
		return false, 0
	}
	expected := ""
	valid := len(args) == 1 && args[0] == "--resolve-unslop-saved-project"
	if len(args) == 3 && args[0] == "--resolve-unslop-saved-project" && args[1] == "--association-revision" && args[2] != "" {
		expected, valid = args[2], unslopProjectRevisionArgument(args[2])
	}
	result := unslopProjectRefusal("unslop_project_invalid_request")
	code := 2
	if valid {
		result = resolve(context.Background(), expected)
		code = 1
		if result.Qualified {
			code = 0
		}
	}
	if json.NewEncoder(output).Encode(result) != nil {
		return true, 2
	}
	return true, code
}
