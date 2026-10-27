package main

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func ownerCompatPolicy(path string, config ownerRuleConfig, access ownerSourceAccess, projects object, owned func(string) ownerPathVerdict) ownerPathVerdict {
	return ownerPolicyClassify(path, ownerPolicyAccess{
		legacy: func(string) ownerLegacyState { return ownerLegacyAbsent }, physical: ownerRulePhysicalPath,
		organization: func(p string) ownerPathVerdict {
			return ownerRuleWith(context.Background(), p, projects, access, config, owned)
		},
	})
}

func TestOwnerRule4088LegacyOwnedQualificationsThroughPolicy(t *testing.T) {
	config, access := ownerRuleFixture(t)
	base := filepath.Dir(config.dev)
	id := "abcdef0123456789abcdef0123456789"
	root, store, jobs := filepath.Join(base, "codex-owned-"+id), filepath.Join(base, "receipts"), filepath.Join(base, "jobs")
	for _, dir := range []string{root, store, jobs} {
		if os.MkdirAll(dir, 0700) != nil {
			t.Fatal("fixture mkdir")
		}
	}
	for _, version := range []int{2, 3} {
		r := ownerOwnedReceipt{ownerOwnedIdentity: ownerOwnedIdentity{Version: version, ID: id, Root: base, Path: root, Project: "self-fixture", Content: "fixture", ProducerKind: "ordinary-only"}, State: "owned"}
		write := func() {
			b, _ := json.Marshal(r.ownerOwnedIdentity)
			r.Integrity = hashBytes(b)
			b, _ = json.Marshal(r)
			if os.WriteFile(filepath.Join(store, id+".json"), b, 0600) != nil {
				t.Fatal("fixture receipt")
			}
		}
		write()
		classify := func() ownerPathVerdict {
			return ownerCompatPolicy(root, config, access, object{}, func(p string) ownerPathVerdict {
				return ownerRuleOwnedVerdictAt(context.Background(), p, store, jobs, object{}, access, config)
			})
		}
		v := classify()
		if v.AccessAllowed {
			t.Fatal("unqualified ordinary receipt admitted itself")
		}
		if got := ownerRuleReceiptFallback(v, root, func(string) bool { return true }); !got.AccessAllowed || got.Basis != "verified_operational_receipt" {
			t.Fatal("old independent receipt lost ordinary root qualification")
		}
		if got := ownerRuleRetirementVerdict(v, root, true); !got.AccessAllowed || got.Basis != "verified_retirement_receipt" {
			t.Fatal("old independent retirement lost ordinary root qualification")
		}
		r.Project = "contracted-fixture"
		write()
		v = classify()
		if v.Status != "protected" || v.Basis != "client_record_producer" || ownerRuleRetirementVerdict(v, root, true).AccessAllowed {
			t.Fatal("client producer lost precedence")
		}
		if ownerRuleReceiptFallback(v, root, func(string) bool { t.Fatal("client reached receipt fallback"); return true }).AccessAllowed {
			t.Fatal("client acquired access")
		}
	}
}

func TestOwnerRule4088QualifiedConversationAndMixedCopiesThroughPolicy(t *testing.T) {
	f := newConversationFixture(t)
	f.publish()
	config, access := ownerRuleFixture(t)
	config.infrastructure = []string{f.a.home}
	config.mixedStores = []string{f.a.home}
	access.conversation = func(p string) bool { return conversationAllowed(p, f.a) }
	for _, path := range []string{f.replyPath, filepath.Join(f.dir, "question.json")} {
		v := ownerCompatPolicy(path, config, access, object{}, nil)
		if !v.AccessAllowed || v.Basis != "verified_conversation_record" {
			t.Fatal("verified individual conversation receipt lost qualification")
		}
	}
	for _, path := range []string{filepath.Join(f.a.home, "conversation-bridge", "request", "body.txt"), filepath.Join(f.a.home, "unattended-state.json"), filepath.Join(f.a.home, "unattended-state.json.bak-fixture"), filepath.Join(f.a.home, "state.json.42.tmp"), f.a.home + ".bak-fixture"} {
		v := ownerCompatPolicy(path, config, access, object{}, nil)
		if v.Status != "protected" || v.Basis != "mixed_client_record_store" {
			t.Fatal("mixed conversation body or copy inherited infrastructure access")
		}
	}
	access.conversation = func(string) bool { return false }
	if ownerCompatPolicy(f.replyPath, config, access, object{}, nil).AccessAllowed {
		t.Fatal("unqualified reply admitted")
	}
	config.mixedStores = []string{filepath.Join(config.infrastructure[0], "todo.json")}
	for _, path := range []string{config.mixedStores[0] + ".bak-fixture", config.mixedStores[0] + ".42.tmp"} {
		if ownerCompatPolicy(path, config, access, object{}, nil).AccessAllowed {
			t.Fatal("mixed table copy admitted")
		}
	}
}

func TestOwnerRule4088MetadataCompatibilityIsLocalToTarget(t *testing.T) {
	for _, body := range []string{
		`{"items":[{"id":9,"spec_history":false,"project":[]},{"id":1,"project":"self","spec":"fixture"}]}`,
		`{"items":[{"id":9,"project":"first","project":"second","note":{"duplicate":1,"duplicate":2}},{"id":1,"project":"self","spec":"fixture"}]}`,
		"{\"items\":[{\"id\":9,\"note\":\"\xff\"},{\"id\":1,\"project\":\"self\",\"spec\":\"fixture\"}]}",
	} {
		m, ok := ownerTaskRecordProjection(context.Background(), strings.NewReader(body), "1")
		if !ok || m.spec != "fixture" {
			t.Fatal("unrelated legacy metadata poisoned target input")
		}
	}
	for _, body := range []string{`{"items":[{"id":1,"project":"first","project":"second"}]}`, `{"items":[{"id":1,"spec_history":false}]}`, `{"items":[{"id":1},{"id":1}]}`} {
		if _, ok := ownerTaskRecordProjection(context.Background(), strings.NewReader(body), "1"); ok {
			t.Fatal("malformed target metadata admitted")
		}
	}
	d := jsontext.NewDecoder(strings.NewReader(`{"id":"fixture-job","todo_id":null,"project":"self"}`))
	m, ok := ownerReadRecordObject(context.Background(), d, "record")
	if !ok || m.invalid || m.todo != "" {
		t.Fatal("non-ToDo producer's null identifier invalidated its metadata")
	}
}

func TestOwnerRule4088ProductionQuerySnapshotBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace int
		during  bool
		want    string
		reads   int
	}{
		{"stable", 0, false, "clear", 1}, {"replace after read", 1, false, "clear", 2}, {"replace during read", 1, true, "clear", 2}, {"replace twice", 2, false, "registration_snapshot_changed", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, access := ownerRuleFixture(t)
			root := filepath.Join(config.dev, "ADJ", "untracked")
			if os.MkdirAll(root, 0700) != nil {
				t.Fatal("fixture mkdir")
			}
			store := filepath.Join(filepath.Dir(config.dev), "registration.json")
			body := []byte(`{"projects":{},"items":[]}`)
			if os.WriteFile(store, body, 0600) != nil {
				t.Fatal("fixture write")
			}
			reads := 0
			read := func(ctx context.Context, project func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
				reads++
				replace := func() {
					if reads <= tc.replace {
						replacement := store + ".pending"
						if err := os.WriteFile(replacement, body, 0600); err != nil {
							t.Fatal("fixture replacement write")
						}
						var replaceErr error
						if tc.during {
							// Match the normal Windows atomic writer. MoveFileEx (used
							// by os.Rename) refuses this open target on Windows.
							dst, _ := syscall.UTF16PtrFromString(store)
							src, _ := syscall.UTF16PtrFromString(replacement)
							ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW").Call(uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(src)), 0, 0, 0, 0)
							if ok == 0 {
								replaceErr = err
							}
						} else {
							replaceErr = os.Rename(replacement, store)
						}
						if err := replaceErr; err != nil {
							var code syscall.Errno
							errors.As(err, &code)
							t.Fatalf("fixture replacement OS code %d", code)
						}
					}
				}
				if tc.during {
					return ownerReadProjectSnapshotUsing(ctx, store, func(string) bool { return false }, func(input io.Reader) (object, bool) { p, ok := project(input); replace(); return p, ok })
				}
				s, ok := ownerReadProjectSnapshotUsing(ctx, store, func(string) bool { return false }, project)
				replace()
				return s, ok
			}
			v := ownerOrganizationQueryWith(context.Background(), root, access, config, read)
			if reads != tc.reads || tc.want == "clear" && !v.AccessAllowed || tc.want != "clear" && (v.AccessAllowed || v.Basis != tc.want) {
				t.Fatal("production snapshot retry differs")
			}
		})
	}
}

func TestOwnerRule4088ProductionInputSinglePassAndNoFallback(t *testing.T) {
	config, access := ownerRuleFixture(t)
	base := filepath.Dir(config.dev)
	config.specStore = filepath.Join(config.infrastructure[0], "specs")
	config.todoStore = filepath.Join(base, "registration.json")
	root := filepath.Join(config.dev, "ADJ", "plain-source")
	for _, dir := range []string{root, config.specStore} {
		if os.MkdirAll(dir, 0700) != nil {
			t.Fatal("fixture mkdir")
		}
	}
	path := filepath.Join(config.specStore, "1-input.md")
	body := []byte(`{"projects":{"self-fixture":"` + filepath.ToSlash(root) + `"},"items":[{"id":1,"project":"self-fixture","spec":"` + filepath.ToSlash(path) + `"}]}`)
	if os.WriteFile(config.todoStore, body, 0600) != nil {
		t.Fatal("fixture table")
	}
	reads := 0
	read := func(ctx context.Context, project func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
		reads++
		return ownerReadProjectSnapshotUsing(ctx, config.todoStore, func(string) bool { return false }, project)
	}
	if v := ownerOrganizationQueryWith(context.Background(), path, access, config, read); !v.AccessAllowed || reads != 1 {
		t.Fatal("input registration was not qualified with one snapshot")
	}
	v := ownerOrganizationQueryWith(context.Background(), filepath.Join(config.specStore, "2-unregistered.md"), access, config, read)
	if v.AccessAllowed || v.Basis != "stored_record_reference_unverified" {
		t.Fatal("unregistered input was misreported as transient failure")
	}
	ownerRuleReceiptFallback(v, path, func(string) bool { t.Fatal("unregistered input reached receipt fallback"); return true })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v := ownerOrganizationQueryWith(ctx, path, access, config, read); v.AccessAllowed || v.Basis != "classification_deadline" {
		t.Fatal("expired production query admitted input")
	}
}

func TestOwnerRule4088ProductionPositivePrecedesSnapshotRetry(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(config.infrastructure[0], "source")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	store := filepath.Join(filepath.Dir(config.dev), "positive-registration.json")
	body := []byte(`{"projects":{"contracted-fixture":"` + filepath.ToSlash(root) + `"}}`)
	if os.WriteFile(store, body, 0600) != nil {
		t.Fatal("fixture metadata")
	}
	reads := 0
	read := func(ctx context.Context, project func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
		reads++
		s, ok := ownerReadProjectSnapshotUsing(ctx, store, func(string) bool { return false }, project)
		next := store + ".pending"
		if os.WriteFile(next, body, 0600) != nil || os.Rename(next, store) != nil {
			t.Fatal("fixture replace")
		}
		return s, ok
	}
	v := ownerOrganizationQueryWith(context.Background(), root, access, config, read)
	if v.Status != "protected" || v.Basis != "client_registration" || reads != 1 {
		t.Fatal("positive client evidence was discarded before retry")
	}
}

func TestOwnerRule4088WorktreeProbeFailureIsNotMismatch(t *testing.T) {
	root, db, source := ownerSourceFixture(t)
	config, _ := ownerRuleFixture(t)
	path := filepath.Join(root+"-wt", "linked")
	currentFixtureGit(t, root, "worktree", "add", "-qb", "probe-fixture", path)
	access := source
	access.git = func(dir string, args ...string) (string, bool) {
		if len(args) > 0 && args[0] == "rev-parse" {
			return "", false
		}
		return currentGit(dir, args...)
	}
	v := ownerRuleWith(context.Background(), path, obj(db["projects"]), access, config, nil)
	if v.Basis != "repository_metadata_unavailable" || !ownerRuleReceiptFallback(v, path, func(string) bool { return true }).AccessAllowed {
		t.Fatal("transient Git failure became an ownership contradiction")
	}
	access.git = func(dir string, args ...string) (string, bool) {
		if len(args) > 0 && args[0] == "rev-parse" {
			return filepath.Join(root, "different") + "\n" + root, true
		}
		return currentGit(dir, args...)
	}
	v = ownerRuleWith(context.Background(), path, obj(db["projects"]), access, config, nil)
	if v.Basis != "worktree_identity_mismatch" || ownerRuleIndependentReceiptEligible(v) {
		t.Fatal("positive Git mismatch acquired transient fallback")
	}
}

func TestOwnerRule4088LocalGitAndProducerExclusion(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(config.dev, "ADJ", "local-only")
	if os.MkdirAll(root, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	currentFixtureGit(t, root, "init", "-q")
	if v := ownerRuleWith(context.Background(), root, object{}, access, config, nil); !v.AccessAllowed || v.Basis != "self_location_without_remotes" {
		t.Fatal("local-only self repository acquired a remote requirement")
	}
	currentFixtureGit(t, root, "remote", "add", "upstream", "https://github.com/OtherOwner/example.git")
	if ownerRuleWith(context.Background(), root, object{}, access, config, nil).AccessAllowed {
		t.Fatal("non-origin remote treated as no remotes")
	}
	access.legacy = func(p string) ownerLegacyState {
		if same(p, root) {
			return ownerLegacyClient
		}
		return ownerLegacyAbsent
	}
	v := ownerRuleRecordOrigin(context.Background(), ownerRecordMetadata{cwd: root, project: "self-fixture"}, object{}, access, config)
	if v.Status != "protected" || v.Basis != "legacy_exclusion" {
		t.Fatal("producer's positive exclusion became unknown")
	}
	access.legacy = nil
	access.identity = func(string) (int64, string, string, bool) { return 0, "", "", false }
	currentFixtureGit(t, root, "remote", "remove", "upstream")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/example.git")
	v = ownerRuleRecordOrigin(context.Background(), ownerRecordMetadata{cwd: root, project: "self-fixture"}, object{}, access, config)
	if v.AccessAllowed || v.Basis != "stored_record_origin_unverified" {
		t.Fatal("producer uncertainty retained an eligible transient basis")
	}
	ownerRuleReceiptFallback(v, root, func(string) bool { t.Fatal("unknown producer reached fallback"); return true })
}

func TestOwnerRule4088FixedHomeAndTempIgnoreCallerEnvironment(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal("fixture physical path")
	}
	plain := filepath.Join(root, "unregistered")
	if os.Mkdir(plain, 0700) != nil {
		t.Fatal("fixture mkdir")
	}
	t.Setenv("USERPROFILE", "")
	t.Setenv("TEMP", filepath.Join(root, "not-temp"))
	t.Setenv("TMP", filepath.Join(root, "not-temp"))
	if _, found := ownerRepositoryRoot(plain); found {
		t.Fatal("caller environment exposed the owner's home repository above ordinary temp data")
	}
}
