package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type unslopProjectFixture struct {
	store, root, key string
	access ownerSourceAccess
	config ownerRuleConfig
}

func newUnslopProjectFixture(t *testing.T) unslopProjectFixture {
	t.Helper()
	base, err := physical(currentShortFixture(t))
	if err != nil {
		t.Fatal("cannot resolve owned fixture directory")
	}
	root := filepath.Join(base, "source")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal("cannot create owned repository fixture")
	}
	f := unslopProjectFixture{store: filepath.Join(base, "store.json"), root: root, key: "writer-fixture"}
	f.access = ownerSourceAccess{
		git: func(_ string, args ...string) (string, bool) {
			if strings.Join(args, " ") == "remote get-url origin" {
				return "https://github.com/" + unslopSavedRepository + ".git", true
			}
			return "", false
		},
		identity: func(string) (int64, string, string, bool) {
			return unslopSavedRepositoryID, unslopSavedRepository, unslopSavedOwner, true
		},
		plain: currentPlain,
		denied: func(string) bool { return false },
		membership: func(string, string) string { return "self" },
	}
	f.write(t, []any{f.row()}, map[string]any{f.key: root, "unrelated-private-key": "Z:/PRIVATE_PATH_CANARY"})
	return f
}

func (f unslopProjectFixture) row() map[string]any {
	return map[string]any{
		"id": 4431, "project": f.key, "cwd": f.root,
		"work_owner": map[string]any{"repository": unslopSavedRepository, "worktree": f.root},
		"title": "PRIVATE_TITLE_CANARY", "note": "PRIVATE_NOTE_CANARY",
		"spec": "PRIVATE_SPEC_CANARY", "history": []string{"PRIVATE_HISTORY_CANARY"},
	}
}

func (f unslopProjectFixture) write(t *testing.T, rows []any, projects map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"projects": projects, "items": rows, "unrelated": "PRIVATE_BODY_CANARY"})
	if err != nil || os.WriteFile(f.store, data, 0600) != nil {
		t.Fatal("cannot write owned registration fixture")
	}
}

func (f unslopProjectFixture) read(t *testing.T) (*ownerProjectSnapshot, ownerRecordMetadata) {
	t.Helper()
	snapshot, record, ok := readUnslopSavedProject(context.Background(), f.store, func(string) bool { return false })
	if !ok || snapshot == nil {
		t.Fatal("normal registration reader did not issue a snapshot")
	}
	return snapshot, record
}

func assertUnslopProjectResult(t *testing.T, result unslopSavedProjectResult, reason string, success bool) {
	t.Helper()
	if result.Reason != reason || result.Qualified != success || result.TodoID != "4431" || result.RepositoryID != 1406909338 {
		t.Fatalf("unexpected fixed outcome: %s", result.Reason)
	}
	if !success && (result.ProjectKey != "" || result.AssociationRevision != "") {
		t.Fatal("refusal exposed association data")
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal("result cannot be encoded")
	}
	for _, forbidden := range []string{"PRIVATE_", "unrelated-private-key", "spec", "note", "history", "cwd", "worktree", "path"} {
		if bytes.Contains(bytes.ToLower(data), []byte(strings.ToLower(forbidden))) {
			t.Fatal("result contains a private field or canary")
		}
	}
}

func TestUnslopProjectResolverNormalReaderPositive(t *testing.T) {
	f := newUnslopProjectFixture(t)
	snapshot, record := f.read(t)
	result := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, result, "unslop_project_resolved", true)
	if result.ProjectKey != f.key || len(result.AssociationRevision) != 64 || !unslopProjectRevisionArgument(result.AssociationRevision) {
		t.Fatal("normal selected association missing")
	}
	again := resolveUnslopProjectSnapshot(context.Background(), result.AssociationRevision, snapshot, record, f.access, f.config)
	if result != again {
		t.Fatal("same snapshot and exact revision changed the result")
	}
	// A returned key/revision is not a path or resource-access grant.
	if bytes.Contains(mustUnslopProjectJSON(t, result), []byte(f.root)) {
		t.Fatal("result exposed the owned root")
	}
}

func mustUnslopProjectJSON(t *testing.T, result unslopSavedProjectResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil { t.Fatal("result cannot be encoded") }
	return data
}

func TestUnslopProjectResolverUnknownAndAmbiguousRecords(t *testing.T) {
	for _, tc := range []struct { name string; rows func(unslopProjectFixture) []any; reason string }{
		{"missing", func(f unslopProjectFixture) []any { return []any{map[string]any{"id": 99, "project": "unrelated-private-key"}} }, "unslop_project_not_found"},
		{"duplicate", func(f unslopProjectFixture) []any { return []any{f.row(), f.row()} }, "unslop_project_record_invalid"},
		{"wrong-id-kind", func(f unslopProjectFixture) []any { row := f.row(); row["id"] = "04431"; return []any{row} }, "unslop_project_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnslopProjectFixture(t)
			f.write(t, tc.rows(f), map[string]any{f.key: f.root})
			snapshot, record := f.read(t)
			result := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
			assertUnslopProjectResult(t, result, tc.reason, false)
		})
	}
}

func TestUnslopProjectResolverRejectsMissingAndMismatchedAssociations(t *testing.T) {
	for _, tc := range []struct { name string; change func(*ownerRecordMetadata, *ownerProjectSnapshot, *ownerSourceAccess); reason string }{
		{"empty-project", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.project = "" }, "unslop_project_metadata_missing"},
		{"path-project", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.project = "C:/PRIVATE_PATH_CANARY" }, "unslop_project_metadata_missing"},
		{"control-project", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.project += "\nPRIVATE_NOTE_CANARY" }, "unslop_project_metadata_missing"},
		{"unregistered-alias", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.project = "writer-fixture-alias" }, "unslop_project_metadata_missing"},
		{"missing-repository", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.repository = "" }, "unslop_project_metadata_missing"},
		{"other-repository", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.repository = "AutoDevJapan/other-product" }, "unslop_project_association_mismatch"},
		{"different-todo", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.todo = "99" }, "unslop_project_record_invalid"},
		{"outside-root", func(r *ownerRecordMetadata, _ *ownerProjectSnapshot, _ *ownerSourceAccess) { r.cwd = "Z:/outside-owned-registration" }, "unslop_project_association_mismatch"},
		{"protected", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.membership = func(string, string) string { return "client_project" } }, "unslop_project_source_protected"},
		{"denied", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.denied = func(string) bool { return true } }, "unslop_project_source_protected"},
		{"identity-unknown", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.identity = func(string) (int64, string, string, bool) { return 0, "", "", false } }, "unslop_project_source_unavailable"},
		{"same-name-other-id", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.identity = func(string) (int64, string, string, bool) { return 99, unslopSavedRepository, unslopSavedOwner, true } }, "unslop_project_association_mismatch"},
		{"identity-owner-mismatch", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.identity = func(string) (int64, string, string, bool) { return unslopSavedRepositoryID, unslopSavedRepository, "other-owner", true } }, "unslop_project_source_unavailable"},
		{"root-remote-mismatch", func(_ *ownerRecordMetadata, _ *ownerProjectSnapshot, a *ownerSourceAccess) { a.git = func(string, ...string) (string, bool) { return "https://github.com/AutoDevJapan/other-product.git", true }; a.identity = func(repository string) (int64, string, string, bool) { return 99, repository, unslopSavedOwner, true } }, "unslop_project_association_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnslopProjectFixture(t)
			snapshot, record := f.read(t)
			positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
			assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
			tc.change(&record, snapshot, &f.access)
			result := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
			assertUnslopProjectResult(t, result, tc.reason, false)
		})
	}
}

func TestUnslopProjectResolverSavedRevisionNeverGrantsProtectedSource(t *testing.T) {
	f := newUnslopProjectFixture(t)
	snapshot, record := f.read(t)
	positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
	f.access.denied = func(string) bool { return true }
	result := resolveUnslopProjectSnapshot(context.Background(), positive.AssociationRevision, snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, result, "unslop_project_source_protected", false)
}

func TestUnslopProjectResolverDoesNotBorrowRootIdentityForNestedRepository(t *testing.T) {
	f := newUnslopProjectFixture(t)
	child := filepath.Join(f.root, "nested")
	if os.MkdirAll(filepath.Join(child, ".git"), 0700) != nil { t.Fatal("cannot make nested fixture") }
	row := f.row(); row["cwd"] = child
	row["work_owner"] = map[string]any{"repository": unslopSavedRepository, "worktree": child}
	f.write(t, []any{row}, map[string]any{f.key: f.root})
	snapshot, record := f.read(t)
	f.access.git = func(root string, args ...string) (string, bool) {
		if same(root, child) { return "https://github.com/AutoDevJapan/other-product.git", true }
		return "https://github.com/" + unslopSavedRepository + ".git", true
	}
	f.access.identity = func(repository string) (int64, string, string, bool) {
		if repository == unslopSavedRepository { return unslopSavedRepositoryID, repository, unslopSavedOwner, true }
		return 99, repository, unslopSavedOwner, true
	}
	result := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, result, "unslop_project_association_mismatch", false)
}

func TestUnslopProjectResolverRevisionAndSnapshotChanges(t *testing.T) {
	t.Run("expected-revision", func(t *testing.T) {
		f := newUnslopProjectFixture(t); snapshot, record := f.read(t)
		positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
		assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
		result := resolveUnslopProjectSnapshot(context.Background(), strings.Repeat("0", 64), snapshot, record, f.access, f.config)
		assertUnslopProjectResult(t, result, "unslop_project_revision_mismatch", false)
	})
	t.Run("new-normal-association", func(t *testing.T) {
		f := newUnslopProjectFixture(t); snapshot, record := f.read(t)
		positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
		assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
		f.key = "writer-renamed-fixture"; f.write(t, []any{f.row()}, map[string]any{f.key: f.root})
		newSnapshot, newRecord := f.read(t)
		result := resolveUnslopProjectSnapshot(context.Background(), positive.AssociationRevision, newSnapshot, newRecord, f.access, f.config)
		assertUnslopProjectResult(t, result, "unslop_project_revision_mismatch", false)
	})
	t.Run("during-identity", func(t *testing.T) {
		f := newUnslopProjectFixture(t); snapshot, record := f.read(t)
		called := 0
		f.access.identity = func(string) (int64, string, string, bool) {
			called++
			if called == 1 { file, err := os.OpenFile(f.store, os.O_APPEND|os.O_WRONLY, 0600); if err != nil { t.Fatal("cannot change fixture") }; _, err = file.WriteString(" "); if file.Close() != nil || err != nil { t.Fatal("cannot seal changed fixture") } }
			return unslopSavedRepositoryID, unslopSavedRepository, unslopSavedOwner, true
		}
		result := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
		assertUnslopProjectResult(t, result, "unslop_project_snapshot_changed", false)
	})
}

func TestUnslopProjectResolverUnavailableCanceledAndCLIRefusal(t *testing.T) {
	f := newUnslopProjectFixture(t); snapshot, record := f.read(t)
	assertUnslopProjectResult(t, resolveUnslopProjectSnapshot(context.Background(), "", nil, record, f.access, f.config), "unslop_project_registration_unavailable", false)
	ctx, cancel := context.WithCancel(context.Background()); cancel()
	assertUnslopProjectResult(t, resolveUnslopProjectSnapshot(ctx, "", snapshot, record, f.access, f.config), "unslop_project_deadline", false)
	if _, _, ok := readUnslopSavedProject(context.Background(), f.store, func(string) bool { return true }); ok { t.Fatal("denied normal store became a snapshot") }
	for _, args := range [][]string{
		{"--resolve-unslop-saved-project", "--association-revision", "PRIVATE_TOKEN_CANARY"},
		{"--resolve-unslop-saved-project", "--path", "C:/PRIVATE_PATH_CANARY"},
		{"--resolve-unslop-saved-project", "--repository-id", "1406909338", "--qualified", "true"},
		{"--resolve-unslop-saved-project", "--association-revision", ""},
		{"--guard-path", "C:/PRIVATE_PATH_CANARY", "--resolve-unslop-saved-project"},
		{"--resolve-unslop-saved-project=PRIVATE_TOKEN_CANARY"},
	} {
		var output bytes.Buffer
		handled, code := runUnslopSavedProjectCLI(args, &output)
		var result unslopSavedProjectResult
		if !handled || code != 2 || json.Unmarshal(output.Bytes(), &result) != nil { t.Fatal("CLI invalid request did not return fixed JSON") }
		assertUnslopProjectResult(t, result, "unslop_project_invalid_request", false)
	}
	var output bytes.Buffer
	if handled, _ := runUnslopSavedProjectCLI([]string{"--guard-path", "C:/ignored"}, &output); handled || output.Len() != 0 { t.Fatal("resolver changed the old CLI route") }
}

// This requires the separate three-line CLI hunk in main_windows.go. On the old
// dispatcher these arguments reach its unknown-flag error, never this JSON API.
func TestUnslopProjectResolverPublicDispatcherRefusesBeforeNormalStoreRead(t *testing.T) {
	var output, diagnostics bytes.Buffer
	called := 0
	code := runGuardCLI([]string{"--resolve-unslop-saved-project", "--association-revision", "PRIVATE_TOKEN_CANARY"}, &output, &diagnostics, func(int) object {
		called++
		return object{}
	})
	var result unslopSavedProjectResult
	if code != 2 || called != 0 || diagnostics.Len() != 0 || json.Unmarshal(output.Bytes(), &result) != nil {
		t.Fatal("public dispatcher did not return the resolver's closed refusal")
	}
	assertUnslopProjectResult(t, result, "unslop_project_invalid_request", false)
}

func TestUnslopProjectResolverMalformedRegistrationIsNotAQualifiedSnapshot(t *testing.T) {
	for _, data := range []string{
		`{"projects":{"writer-fixture":"C:/owned","writer-fixture":"C:/other"},"items":[{"id":4431}]}`,
		`{"projects":{},"projects":{},"items":[{"id":4431}]}`,
		`{"projects":{},"items":[{"id":4431}]`,
	} {
		f := newUnslopProjectFixture(t)
		f.read(t)
		if os.WriteFile(f.store, []byte(data), 0600) != nil { t.Fatal("cannot write malformed owned fixture") }
		if _, _, ok := readUnslopSavedProject(context.Background(), f.store, func(string) bool { return false }); ok {
			t.Fatal("malformed registration issued a snapshot")
		}
	}
}

func TestUnslopProjectResolverCLIValidRequestsAndOutcomes(t *testing.T) {
	f := newUnslopProjectFixture(t)
	snapshot, record := f.read(t)
	positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
	refusal := unslopProjectRefusal("unslop_project_identity_unavailable")
	revision := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name string
		args []string
		expected string
		result unslopSavedProjectResult
		code int
	}{
		{"flag-only-success", []string{"--resolve-unslop-saved-project"}, "", positive, 0},
		{"expected-revision-success", []string{"--resolve-unslop-saved-project", "--association-revision", revision}, revision, positive, 0},
		{"flag-only-refusal", []string{"--resolve-unslop-saved-project"}, "", refusal, 1},
		{"expected-revision-refusal", []string{"--resolve-unslop-saved-project", "--association-revision", revision}, revision, refusal, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			handled, code := runUnslopSavedProjectCLIWithResolver(tc.args, &output, func(ctx context.Context, expected string) unslopSavedProjectResult {
				calls++
				if ctx == nil || ctx.Err() != nil || expected != tc.expected {
					t.Fatal("CLI did not forward its valid request to the resolver")
				}
				return tc.result
			})
			if !handled || code != tc.code || calls != 1 {
				t.Fatal("CLI did not preserve the resolver call count or exit status")
			}
			var result unslopSavedProjectResult
			decoder := json.NewDecoder(&output)
			if decoder.Decode(&result) != nil || result != tc.result {
				t.Fatal("CLI changed the resolver's JSON outcome")
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				t.Fatal("CLI returned more than one JSON result")
			}
			assertUnslopProjectResult(t, result, tc.result.Reason, tc.result.Qualified)
		})
	}
}

type unslopProjectFailingWriter struct { calls int }

func (w *unslopProjectFailingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, io.ErrClosedPipe
}

func TestUnslopProjectResolverCLIOutputFailureIsNotSuccess(t *testing.T) {
	f := newUnslopProjectFixture(t)
	snapshot, record := f.read(t)
	positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
	assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
	for _, result := range []unslopSavedProjectResult{positive, unslopProjectRefusal("unslop_project_source_unavailable")} {
		output := &unslopProjectFailingWriter{}
		calls := 0
		handled, code := runUnslopSavedProjectCLIWithResolver([]string{"--resolve-unslop-saved-project"}, output, func(context.Context, string) unslopSavedProjectResult {
			calls++
			return result
		})
		if !handled || code != 2 || calls != 1 || output.calls != 1 {
			t.Fatal("failed output was reported as a completed CLI outcome")
		}
	}
}

func TestUnslopProjectResolverCLIInvalidRequestDoesNotCallResolver(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		handled bool
	}{
		{"other-operation", []string{"--guard-path", "C:/ignored"}, false},
		{"bad-revision", []string{"--resolve-unslop-saved-project", "--association-revision", "PRIVATE_TOKEN_CANARY"}, true},
		{"uppercase-revision", []string{"--resolve-unslop-saved-project", "--association-revision", strings.Repeat("A", 64)}, true},
		{"extra-authority-input", []string{"--resolve-unslop-saved-project", "--qualified", "true"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			handled, code := runUnslopSavedProjectCLIWithResolver(tc.args, &output, func(context.Context, string) unslopSavedProjectResult {
				calls++
				return unslopProjectRefusal("unslop_project_resolved")
			})
			if handled != tc.handled || calls != 0 {
				t.Fatal("unaccepted request reached a resolver")
			}
			if !tc.handled {
				if code != 0 || output.Len() != 0 { t.Fatal("unrelated CLI operation was changed") }
				return
			}
			var result unslopSavedProjectResult
			if code != 2 || json.Unmarshal(output.Bytes(), &result) != nil { t.Fatal("invalid request was not a fixed refusal") }
			assertUnslopProjectResult(t, result, "unslop_project_invalid_request", false)
		})
	}
}

func TestUnslopProjectResolverCancellationDuringFinalSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name string
		cancelFinal, denyFinal bool
		reason string
		qualified bool
	}{
		{"normal-final", false, false, "unslop_project_resolved", true},
		{"cancel-during-valid-final", true, false, "unslop_project_deadline", false},
		{"changed-final-without-cancel", false, true, "unslop_project_snapshot_changed", false},
		{"cancel-during-changed-final", true, true, "unslop_project_deadline", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnslopProjectFixture(t)
			snapshot, record := f.read(t)
			positive := resolveUnslopProjectSnapshot(context.Background(), "", snapshot, record, f.access, f.config)
			assertUnslopProjectResult(t, positive, "unslop_project_resolved", true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			snapshot.ctx = ctx
			checks := 0
			snapshot.denied = func(string) bool {
				checks++
				if checks == 2 {
					// current() has already checked its context. Cancellation here
					// must still win before the resolver publishes the association.
					if tc.cancelFinal { cancel() }
					return tc.denyFinal
				}
				return false
			}
			result := resolveUnslopProjectSnapshot(ctx, positive.AssociationRevision, snapshot, record, f.access, f.config)
			if checks != 2 || (ctx.Err() != nil) != tc.cancelFinal {
				t.Fatal("final snapshot cancellation control was not reached exactly once")
			}
			assertUnslopProjectResult(t, result, tc.reason, tc.qualified)
			if tc.qualified && result != positive {
				t.Fatal("normal final snapshot changed the saved association")
			}
		})
	}
}
