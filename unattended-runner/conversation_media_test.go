package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type conversationMediaFixture struct {
	root, home, source, snapshot, descriptor, normal, sender string
	data                                                     []byte
	guardPaths                                               []string
	deny                                                     bool
	onGuard                                                  func()
}

func mediaFixture(t *testing.T) *conversationMediaFixture {
	t.Helper()
	root, err := os.MkdirTemp("", "cc-media-fixture-")
	if err != nil {
		t.Fatal("could not create media fixture")
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal("could not resolve media fixture")
	}
	f := &conversationMediaFixture{root: root, home: filepath.Join(root, "tool-home"), source: filepath.Join(root, "original.png"), normal: "normal-log-" + strings.Repeat("a", 64), data: []byte("\x89PNG\r\n\x1a\nfixture-image")}
	f.sender = "conversation-log-" + bytesHash([]byte(f.normal))[:32]
	f.snapshot = filepath.Join(f.home, "conversation-bridge", f.normal, "attachment-0.png")
	f.descriptor = filepath.Join(filepath.Dir(f.snapshot), "attachment-0.source.json")
	if os.MkdirAll(filepath.Dir(f.snapshot), 0700) != nil {
		t.Fatal("could not create media fixture directory")
	}
	mediaJSON(t, filepath.Join(f.home, "conversation-mode.json"), map[string]any{"domain": "discord-conversation-mode-v1", "enabled": true})
	f.write(t)
	return f
}

func mediaJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil || os.WriteFile(path, b, 0600) != nil {
		t.Fatal("could not save private media fixture")
	}
}

func (f *conversationMediaFixture) write(t *testing.T) {
	t.Helper()
	if os.WriteFile(f.source, f.data, 0600) != nil || os.WriteFile(f.snapshot, f.data, 0600) != nil {
		t.Fatal("could not save image fixture")
	}
	mediaJSON(t, f.descriptor, conversationMediaSource{Domain: conversationMediaSourceDomain, SourcePath: f.source, SnapshotPath: f.snapshot, SHA256: bytesHash(f.data), Size: int64(len(f.data))})
}

func (f *conversationMediaFixture) deps() conversationMediaDependencies {
	return conversationMediaDependencies{Home: f.home, Guard: func(ctx context.Context, path string) (bool, error) {
		f.guardPaths = append(f.guardPaths, path)
		if f.onGuard != nil {
			f.onGuard()
		}
		return !f.deny, nil
	}}
}

func (f *conversationMediaFixture) verify(t *testing.T, path string) conversationMediaVerification {
	t.Helper()
	result, err := verifyConversationMedia(context.Background(), path, f.sender, f.deps())
	if err != nil || !result.Verified || result.Domain != conversationMediaDomain || result.SHA256 != bytesHash(f.data) || result.Size != int64(len(f.data)) {
		t.Fatal("qualified image did not return its metadata proof")
	}
	return result
}

func TestConversationMediaDirectAndBridgeSourcesAreCheckedLive(t *testing.T) {
	f := mediaFixture(t)
	f.verify(t, f.source)
	f.verify(t, f.snapshot)
	if len(f.guardPaths) != 2 || !conversationMediaSamePath(f.guardPaths[0], f.source) || !conversationMediaSamePath(f.guardPaths[1], f.source) {
		t.Fatal("snapshot verification lost the original source guard")
	}
	f.deny = true
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("protected source was accepted from an old private receipt")
	}
}

func fixtureVideoMP4(payload int, handler string) []byte {
	box := func(kind string, data []byte) []byte {
		r := make([]byte, 8+len(data))
		binary.BigEndian.PutUint32(r, uint32(len(r)))
		copy(r[4:8], kind)
		copy(r[8:], data)
		return r
	}
	h := make([]byte, 12)
	copy(h[8:], handler)
	data := box("ftyp", []byte("isom\x00\x00\x00\x00isommp42"))
	data = append(data, box("moov", box("trak", box("mdia", box("hdlr", h))))...)
	return append(data, box("mdat", make([]byte, payload))...)
}

func TestConversationMP4PreservesSourceAndQuestionScope(t *testing.T) {
	f := mediaFixture(t)
	f.normal = "normal-ask-" + strings.Repeat("b", 64)
	f.sender = "conversation-v1-" + bytesHash([]byte(f.normal))[:32]
	f.source = filepath.Join(f.root, "original.mp4")
	f.snapshot = filepath.Join(f.home, "conversation-bridge", f.normal, "attachment-0.mp4")
	f.descriptor = filepath.Join(filepath.Dir(f.snapshot), "attachment-0.source.json")
	if err := os.MkdirAll(filepath.Dir(f.snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	f.data = fixtureVideoMP4((10<<20)+1, "vide")
	f.write(t)
	deps := f.deps()
	deps.VideoTitle, deps.VideoOrigin = "Review", "510"
	deps.VideoQuestion = func(context.Context, string, string, conversationMediaSource, *generatedQuestionScope) error {
		return nil
	}
	p, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, deps)
	if err != nil {
		t.Fatal(err)
	}
	if p.MIME != "video/mp4" || p.Extension != "mp4" || len(f.guardPaths) != 1 || f.guardPaths[0] != f.source {
		t.Fatal("MP4 original scope lost")
	}
	f.deny = true
	qualificationCalls := 0
	deps.Question = &generatedQuestionScope{}
	deps.Qualify = func(context.Context, string, generatedQuestionScope) (*generatedImageQualification, error) {
		qualificationCalls++
		return nil, errConversationMedia
	}
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, deps); err == nil || qualificationCalls != 0 {
		t.Fatal("video used image authority")
	}
	f.deny = false
	f.onGuard = func() { _ = os.WriteFile(f.source, fixtureVideoMP4(1, "vide"), 0600) }
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, deps); err == nil {
		t.Fatal("changed original video accepted")
	}
}

func TestConversationMP4FormatSizeAndRetryBoundaries(t *testing.T) {
	valid := fixtureVideoMP4(1, "vide")
	for _, bad := range [][]byte{nil, []byte("ftyp isom"), valid[:len(valid)-1], fixtureVideoMP4(1, "soun"), fixtureVideoMP4(0, "vide")} {
		if conversationVideoMagic(bad) {
			t.Fatal("invalid or audio-only MP4 accepted")
		}
	}
	oversize := make([]byte, conversationVideoLimit+1)
	copy(oversize, valid)
	if _, _, e := conversationAttachmentMagic(oversize, "fixture.mp4"); e == nil {
		t.Fatal("oversize video accepted")
	}
	if _, _, e := conversationAttachmentMagic(valid, "fixture.png"); e == nil {
		t.Fatal("video relabeled as image accepted")
	}
	if _, _, e := conversationAttachmentMagic([]byte("\x89PNG\r\n\x1a\nimage"), "fixture.mp4"); e == nil {
		t.Fatal("image relabeled as video accepted")
	}
	f := mediaFixture(t)
	f.source = filepath.Join(f.root, "direct.mp4")
	f.data = valid
	if e := os.WriteFile(f.source, f.data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := verifyConversationMedia(context.Background(), f.source, f.sender, f.deps()); e == nil {
		t.Fatal("video enabled for silent log")
	}
	paths, hashes, e := conversationQuestionImages([]string{f.source}, func(string) (conversationMediaVerification, error) {
		return conversationMediaVerification{Domain: conversationMediaDomain, Verified: true, MIME: "video/mp4", Extension: "mp4", Size: int64(len(valid)), SHA256: bytesHash(valid)}, nil
	})
	if e != nil || len(paths) != 1 || len(hashes) != 1 || hashes[0] != bytesHash(valid) {
		t.Fatal("question video retry binding lost", e)
	}
}

func TestRegisteredMP4RequiresActualNormalQuestionAndScope(t *testing.T) {
	f := mediaFixture(t)
	f.normal = "normal-ask-" + strings.Repeat("b", 64)
	f.sender = "conversation-v1-" + bytesHash([]byte(f.normal))[:32]
	f.source = filepath.Join(f.root, "original.mp4")
	f.snapshot = filepath.Join(f.home, "conversation-bridge", f.normal, "attachment-0.mp4")
	f.descriptor = strings.TrimSuffix(f.snapshot, ".mp4") + ".source.json"
	f.data = fixtureVideoMP4(1, "vide")
	if err := os.MkdirAll(filepath.Dir(f.snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	f.write(t)
	body := []byte("owned normal question")
	decision := json.RawMessage(`{"todo_id":42,"decision":"direction"}`)
	scope := generatedQuestionScope{Todo: 42, Body: bytesHash(body), Decision: canonicalHash(decision), Owner: "300", Guild: "100", Forum: "200"}
	dir := filepath.Join(f.home, "unattended", "conversations", f.normal)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	q := ConversationQuestion{RequestID: f.normal, SenderID: f.sender, Todo: 42, Project: "fixture", Phase: "reserved", Title: "Review", OriginThread: "510", BodyFile: filepath.Join(dir, "question.txt"), DecisionFile: filepath.Join(dir, "decision.json"), Attachments: []string{f.snapshot}, AttachmentHashes: []string{bytesHash(f.data)}}
	if os.WriteFile(q.BodyFile, body, 0600) != nil || os.WriteFile(q.DecisionFile, decision, 0600) != nil {
		t.Fatal("owned question save")
	}
	base := conversationIndex{Domain: conversationDomain, Destination: Destination{Owner: "300", Guild: "100", Forum: "200"}, Questions: map[string]ConversationQuestion{f.normal: q}}
	for _, fault := range []string{"none", "missing", "phase", "todo", "sender", "hash", "body", "scope", "title", "origin", "unicode-bom", "omitted-title", "omitted-title-mismatch"} {
		if os.WriteFile(q.BodyFile, body, 0600) != nil || os.WriteFile(q.DecisionFile, decision, 0600) != nil {
			t.Fatal("reset owned question inputs")
		}
		current := base
		current.Questions = map[string]ConversationQuestion{f.normal: q}
		changed := q
		switch fault {
		case "missing":
			delete(current.Questions, f.normal)
		case "phase":
			changed.Phase = "posted"
		case "todo":
			changed.Todo++
		case "sender":
			changed.SenderID = "conversation-v1-" + strings.Repeat("c", 32)
		case "hash":
			changed.AttachmentHashes = []string{strings.Repeat("0", 64)}
		case "body":
			changed.BodyFile = filepath.Join(dir, "other.txt")
		case "title":
			changed.Title = "Other context"
		case "origin":
			changed.OriginThread = "511"
		case "omitted-title", "omitted-title-mismatch":
			changed.Title = ""
		}
		if fault != "missing" {
			current.Questions[f.normal] = changed
		}
		mediaJSON(t, filepath.Join(f.home, "unattended", "conversation-questions.json"), current)
		deps := f.deps()
		deps.Question = &scope
		deps.VideoTitle, deps.VideoOrigin = "Review", "510"
		deps.VideoQuestion = registeredConversationVideoQuestionWithContext(f.home, "Review", "510")
		if fault == "omitted-title" {
			deps.VideoTitle = string(body)
			deps.VideoQuestion = registeredConversationVideoQuestionWithContext(f.home, string(body), "510")
		}
		if fault == "unicode-bom" {
			utfBody := append([]byte{0xef, 0xbb, 0xbf}, body...)
			utfDecision := []byte("{\"value\":\"\u2028\u2029\",\"todo_id\":42}")
			if os.WriteFile(q.BodyFile, utfBody, 0600) != nil || os.WriteFile(q.DecisionFile, utfDecision, 0600) != nil {
				t.Fatal("owned unicode fixture")
			}
			unicodeScope := scope
			unicodeScope.Decision = bytesHash([]byte("{\"todo_id\":42,\"value\":\"\u2028\u2029\"}"))
			deps.Question = &unicodeScope
		}
		if fault == "scope" {
			bad := scope
			bad.Body = strings.Repeat("0", 64)
			deps.Question = &bad
		}
		_, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, deps)
		if (err == nil) != (fault == "none" || fault == "unicode-bom" || fault == "omitted-title") {
			t.Fatalf("fault=%s accepted=%v", fault, err == nil)
		}
		if fault == "none" || fault == "unicode-bom" || fault == "omitted-title" {
			args := []string{"--path", f.snapshot, "--request-id", f.sender, "--question-title", deps.VideoTitle, "--question-origin-thread", "510"}
			args = append(args, generatedQuestionScopeArgs(*deps.Question)...)
			var result bytes.Buffer
			if code := conversationMediaCLIWithDependencies(args, &result, f.deps()); code != 0 {
				t.Fatalf("actual CLI video proof failed: %d %s", code, result.String())
			}
			var proof conversationMediaVerification
			if json.Unmarshal(result.Bytes(), &proof) != nil || !proof.Verified || proof.MIME != "video/mp4" {
				t.Fatal("actual CLI proof not bound")
			}
		}
	}
	deps := f.deps()
	deps.Question = &scope
	deps.VideoTitle, deps.VideoOrigin = "Review", "510"
	deps.VideoQuestion = registeredConversationVideoQuestionWithContext(f.home, "Review", "510")
	if _, err := verifyConversationMedia(context.Background(), f.source, f.sender, deps); err == nil {
		t.Fatal("direct arbitrary video source accepted")
	}
}

func TestNormalQuestionDecisionDigestKeepsRustUnicodeAndLiteralEscapesDistinct(t *testing.T) {
	for _, input := range []string{`{"value":"\u2028\u2029"}`, "{\"value\":\"\u2028\u2029\"}"} {
		want := bytesHash([]byte("{\"value\":\"\u2028\u2029\"}"))
		if normalQuestionDecisionHash(json.RawMessage(input)) != want {
			t.Fatal("Rust literal Unicode digest differs")
		}
	}
	if normalQuestionDecisionHash(json.RawMessage(`{"value":"\\u2028\\u2029"}`)) == normalQuestionDecisionHash(json.RawMessage(`{"value":"\u2028\u2029"}`)) {
		t.Fatal("literal slash confused with Unicode escape")
	}
}

func TestNormalQuestionDecisionDigestMatchesSenderNumbers(t *testing.T) {
	for _, tc := range []struct{ raw, sender string }{
		{"3", "3"}, {"3.00", "3.0"}, {"1e-5", "0.00001"},
		{"0.000001", "1e-6"}, {"1e15", "1000000000000000.0"},
		{"1e16", "1e+16"}, {"1.2345678901234568e-10", "1.2345678901234568e-10"},
		{"-0", "-0.0"}, {"-0.00", "-0.0"}, {"0e0", "0.0"},
		{"18446744073709551615", "18446744073709551615"},
		{"-9223372036854775808", "-9223372036854775808"},
		{"18446744073709551616", "1.8446744073709552e+19"},
		{"-9223372036854775809", "-9.223372036854776e+18"},
		{"1e-999", "0.0"}, {"-1e-999", "-0.0"},
		{"5e-324", "5e-324"}, {"1.7976931348623157e308", "1.7976931348623157e+308"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			raw := json.RawMessage(`{"todo_id":42,"scope":{"amount":[` + tc.raw + `]}}`)
			want := bytesHash([]byte(`{"scope":{"amount":[` + tc.sender + `]},"todo_id":42}`))
			if normalQuestionDecisionHash(raw) != want {
				t.Fatal("normal question numeric digest differs from sender")
			}
		})
	}
	if normalQuestionDecisionHash(json.RawMessage(`{"amount":3.0}`)) == normalQuestionDecisionHash(json.RawMessage(`{"amount":4.0}`)) {
		t.Fatal("different numeric scope accepted")
	}
}

func TestNormalQuestionNumericDecisionThroughCLIAndReservedVerifier(t *testing.T) {
	for _, tc := range []struct{ raw, sender string }{
		{"3.00", "3.0"}, {"1e-5", "0.00001"},
		{"18446744073709551615", "18446744073709551615"},
		{"-0.00", "-0.0"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			f := conversationTest(t, false)
			video := fixtureVideoMP4(1, "vide")
			path := normalQuestionVideoPath(t, f, video)
			f.input.Attachments = []string{path}
			decision := []byte(`{"todo_id":42,"decision":"direction","scope":{"amount":` + tc.raw + `}}`)
			if os.WriteFile(f.input.DecisionFile, decision, 0600) != nil {
				t.Fatal("write owned numeric decision")
			}
			base := f.r.Run
			verified := 0
			f.r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
				if len(args) == 0 || args[0] != "recovery-ask" {
					return base(ctx, exe, args...)
				}
				f.posts++
				index, err := f.r.loadConversations()
				if err != nil {
					t.Fatal(err)
				}
				q := index.Questions[f.input.RequestID]
				stored, err := os.ReadFile(q.DecisionFile)
				if err != nil || !bytes.Equal(stored, decision) {
					t.Fatal("normalization changed retained decision")
				}
				body, err := os.ReadFile(q.BodyFile)
				if err != nil {
					t.Fatal(err)
				}
				scope := generatedQuestionScope{Todo: q.Todo, Body: bytesHash(body), Decision: bytesHash([]byte(`{"decision":"direction","scope":{"amount":` + tc.sender + `},"todo_id":42}`)), Owner: f.r.State.Destination.Owner, Guild: f.r.State.Destination.Guild, Forum: f.r.State.Destination.Forum}
				verifyArgs := []string{"--path", path, "--request-id", q.SenderID, "--question-title", q.Title, "--question-origin-thread", q.OriginThread}
				verifyArgs = append(verifyArgs, generatedQuestionScopeArgs(scope)...)
				deps := conversationMediaDependencies{Home: filepath.Dir(f.r.Config.Home), Guard: func(context.Context, string) (bool, error) { return true, nil }}
				var out bytes.Buffer
				if code := conversationMediaCLIWithDependencies(verifyArgs, &out, deps); code != 0 {
					t.Fatalf("same numeric scope rejected by actual verifier CLI: %d", code)
				}
				verified++
				badScope := scope
				badScope.Decision = bytesHash([]byte(`{"decision":"direction","scope":{"amount":4.0},"todo_id":42}`))
				badArgs := append([]string{}, verifyArgs[:8]...)
				badArgs = append(badArgs, generatedQuestionScopeArgs(badScope)...)
				out.Reset()
				if code := conversationMediaCLIWithDependencies(badArgs, &out, deps); code == 0 {
					t.Fatal("changed numeric scope accepted")
				}
				return "", errConversationMedia // synthetic sender stops before HTTP
			}
			args := []string{"ask", "--request-id", f.input.RequestID, "--todo-id", "42", "--origin-thread", f.input.OriginThread, "--body-file", f.input.BodyFile, "--decision-file", f.input.DecisionFile, "--title", f.input.Title, "--attach", path}
			var output bytes.Buffer
			code := conversationCLIWithLoader(args, &output, func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil })
			if code != 1 || f.posts != 1 || verified != 1 {
				t.Fatalf("numeric input did not reach sender verifier: code=%d posts=%d verified=%d", code, f.posts, verified)
			}
			index, err := f.r.loadConversations()
			if err != nil {
				t.Fatal(err)
			}
			q := index.Questions[f.input.RequestID]
			body, err := os.ReadFile(q.BodyFile)
			if err != nil || q.InputSHA != canonicalHash([]any{q.Todo, q.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), q.Title, q.Wait, []string{bytesHash(video)}}) {
				t.Fatal("numeric comparison changed retry identity")
			}
		})
	}
}

func TestConversationMediaQuestionNamespaceKeepsOriginalGuardAndRequestBinding(t *testing.T) {
	f := mediaFixture(t)
	f.normal = "normal-ask-" + strings.Repeat("b", 64)
	f.sender = "conversation-v1-" + bytesHash([]byte(f.normal))[:32]
	f.snapshot = filepath.Join(f.home, "conversation-bridge", f.normal, "attachment-0.png")
	f.descriptor = filepath.Join(filepath.Dir(f.snapshot), "attachment-0.source.json")
	if err := os.MkdirAll(filepath.Dir(f.snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	f.write(t)
	f.verify(t, f.snapshot)
	if len(f.guardPaths) != 1 || !conversationMediaSamePath(f.guardPaths[0], f.source) {
		t.Fatal("question snapshot hid its original source")
	}
	for _, sender := range []string{"conversation-log-" + bytesHash([]byte(f.normal))[:32], "conversation-v1-" + strings.Repeat("c", 32)} {
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, sender, f.deps()); err == nil {
			t.Fatal("question image accepted for another sender")
		}
	}
	f.deny = true
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("question image bypassed source denial")
	}
}

func TestConversationMediaCLIOnlyReturnsFixedMetadataAndNeverUsesRunnerLock(t *testing.T) {
	f := mediaFixture(t)
	unlock, err := acquire(filepath.Join(f.home, "runner.lock"))
	if err != nil {
		t.Fatal("could not hold media fixture runner lock")
	}
	defer unlock()
	before, err := os.ReadDir(f.home)
	if err != nil {
		t.Fatal("could not inspect media fixture")
	}
	var out bytes.Buffer
	code := conversationMediaCLIWithDependencies([]string{"--path", f.snapshot, "--request-id", f.sender}, &out, f.deps())
	var result map[string]any
	if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || len(result) != 6 || result["verified"] != true || result["sha256"] != bytesHash(f.data) {
		t.Fatal("media CLI did not return its fixed proof schema")
	}
	if bytes.Contains(out.Bytes(), []byte(f.root)) || bytes.Contains(out.Bytes(), f.data) || bytes.Contains(out.Bytes(), []byte("fixture-token")) {
		t.Fatal("media CLI exposed a private path, image or credential")
	}
	after, _ := os.ReadDir(f.home)
	if len(after) != len(before) {
		t.Fatal("read-only verification created a state artifact")
	}
}

func TestConversationMediaCLIRejectsInvalidArgumentsWithoutGuard(t *testing.T) {
	for _, args := range [][]string{nil, {"--path", "private-path"}, {"--request-id", "private"}, {"--path=a", "--path=b", "--request-id=x"}, {"--path=a", "--request-id=x", "--request-id=y"}, {"--path=a", "--request-id=x", "extra"}, {"--path=a", "--request-id=x", "--unknown=fixture-token"}} {
		f := mediaFixture(t)
		var out bytes.Buffer
		if conversationMediaCLIWithDependencies(args, &out, f.deps()) != 1 || out.String() != "invalid conversation media arguments\n" || len(f.guardPaths) != 0 {
			t.Fatal("invalid media arguments were echoed or reached the guard")
		}
	}
}

func TestConversationMediaRequiresNewSenderAndStrictEnabledMode(t *testing.T) {
	for _, mode := range []string{`null`, `{}`, `{"domain":"wrong","enabled":true}`, `{"domain":"discord-conversation-mode-v1","enabled":false}`, `{"domain":"discord-conversation-mode-v1","enabled":"true"}`, `{"domain":"discord-conversation-mode-v1","enabled":true,"extra":0}`, `{"domain":"discord-conversation-mode-v1","enabled":false,"enabled":true}`} {
		f := mediaFixture(t)
		if os.WriteFile(filepath.Join(f.home, "conversation-mode.json"), []byte(mode), 0600) != nil {
			t.Fatal("mode fixture write failed")
		}
		if _, err := verifyConversationMedia(context.Background(), f.source, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("invalid or disabled mode authorized an image")
		}
	}
	for _, request := range []string{"legacy-request", "conversation-log-abc", "conversation-log-" + strings.Repeat("A", 32), "conversation-log-" + strings.Repeat("a", 33)} {
		f := mediaFixture(t)
		if _, err := verifyConversationMedia(context.Background(), f.source, request, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("legacy or malformed request entered the new media namespace")
		}
	}
}

func TestConversationMediaBridgeDescriptorIsStrictAndBoundToSnapshotAndSender(t *testing.T) {
	for _, field := range []string{"domain", "source_path", "snapshot_path", "sha256", "size", "extra", "source_is_snapshot", "other_sender"} {
		t.Run(field, func(t *testing.T) {
			f := mediaFixture(t)
			value := map[string]any{"domain": conversationMediaSourceDomain, "source_path": f.source, "snapshot_path": f.snapshot, "sha256": bytesHash(f.data), "size": len(f.data)}
			switch field {
			case "size":
				value[field] = len(f.data) + 1
			case "extra":
				value[field] = true
			case "source_is_snapshot":
				value["source_path"] = f.snapshot
			case "other_sender":
				f.sender = "conversation-log-" + strings.Repeat("b", 32)
			default:
				value[field] = "different"
			}
			mediaJSON(t, f.descriptor, value)
			if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
				t.Fatal("corrupt or mismatched provenance was accepted")
			}
		})
	}
	for _, data := range []string{"null", "{}", "{", `{"domain":"discord-conversation-media-source-v1","domain":"discord-conversation-media-source-v1"}`} {
		f := mediaFixture(t)
		_ = os.WriteFile(f.descriptor, []byte(data), 0600)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
			t.Fatal("malformed source receipt was accepted")
		}
	}
}

func TestConversationMediaChangedOrMissingOriginalAndSnapshotAreRejected(t *testing.T) {
	for _, which := range []string{"source_changed", "snapshot_changed", "source_missing", "receipt_missing", "changed_in_guard"} {
		t.Run(which, func(t *testing.T) {
			f := mediaFixture(t)
			switch which {
			case "source_changed":
				_ = os.WriteFile(f.source, append(f.data, 'x'), 0600)
			case "snapshot_changed":
				_ = os.WriteFile(f.snapshot, append(f.data, 'x'), 0600)
			case "source_missing":
				_ = os.Remove(f.source)
			case "receipt_missing":
				_ = os.Remove(f.descriptor)
			case "changed_in_guard":
				f.onGuard = func() { _ = os.WriteFile(f.source, []byte("changed-current-image"), 0600) }
			}
			if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
				t.Fatal("missing or changed source bytes were authorized")
			}
		})
	}
}

func TestConversationMediaPathBoundariesRejectHiddenOriginsAndAliases(t *testing.T) {
	for _, component := range []string{".env-images", ".ssh", "credentials", "private-secrets", "access-token", "password-images"} {
		f := mediaFixture(t)
		f.source = filepath.Join(f.root, component, "original.png")
		_ = os.Mkdir(filepath.Dir(f.source), 0700)
		f.write(t)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("sensitive original path was hidden by a generic snapshot name")
		}
	}
	f := mediaFixture(t)
	alias := filepath.Dir(f.snapshot) + string(filepath.Separator) + ".." + string(filepath.Separator) + f.normal + string(filepath.Separator) + filepath.Base(f.snapshot)
	if _, err := verifyConversationMedia(context.Background(), alias, f.sender, f.deps()); err == nil {
		t.Fatal("path alias entered the snapshot namespace")
	}
	other := filepath.Join(filepath.Dir(f.snapshot), "different.png")
	_ = os.WriteFile(other, f.data, 0600)
	if _, err := verifyConversationMedia(context.Background(), other, f.sender, f.deps()); err == nil {
		t.Fatal("unknown bridge path fell back to direct source authority")
	}
}

func TestConversationMediaLinksAndNonregularFilesAreRejected(t *testing.T) {
	f := mediaFixture(t)
	link := filepath.Join(f.root, "linked.png")
	if err := os.Symlink(f.source, link); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314)) {
			t.Skip("symbolic links are unavailable for this test account")
		}
		t.Fatal("could not create link fixture")
	}
	if _, err := verifyConversationMedia(context.Background(), link, f.sender, f.deps()); err == nil {
		t.Fatal("symbolic image alias was accepted")
	}
	_ = os.Remove(f.snapshot)
	if os.Link(f.source, f.snapshot) != nil {
		t.Fatal("could not create physical alias fixture")
	}
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("a hard link was treated as an independent source snapshot")
	}
	directory := filepath.Join(f.root, "folder.png")
	_ = os.Mkdir(directory, 0700)
	if _, err := verifyConversationMedia(context.Background(), directory, f.sender, f.deps()); err == nil {
		t.Fatal("directory was accepted as image bytes")
	}
}

func TestConversationMediaSizeAndFormatLimits(t *testing.T) {
	for _, pathKind := range []string{"source", "snapshot"} {
		f := mediaFixture(t)
		path := f.source
		if pathKind == "snapshot" {
			path = f.snapshot
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err != nil || file.Truncate(conversationMediaLimit+1) != nil {
			t.Fatal("could not create oversized image fixture")
		}
		_ = file.Close()
		if _, err := verifyConversationMedia(context.Background(), path, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("oversized input reached image byte processing")
		}
	}
	for _, data := range [][]byte{[]byte("PK\x03\x04code-archive"), []byte("MZexecutable"), []byte("package main"), []byte("RIFFbad")} {
		f := mediaFixture(t)
		f.data = data
		f.write(t)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
			t.Fatal("code or non-image bytes were accepted")
		}
	}
	f := mediaFixture(t)
	f.source = filepath.Join(f.root, "code.zip")
	f.write(t)
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("an archive origin was hidden behind an image suffix")
	}
}

func TestConversationMediaRecognizesOnlyExistingImageMagic(t *testing.T) {
	for _, test := range []struct {
		bytes           []byte
		mime, extension string
	}{
		{[]byte("\x89PNG\r\n\x1a\nimage"), "image/png", "png"}, {[]byte("\xff\xd8\xffimage"), "image/jpeg", "jpg"}, {[]byte("GIF89aimage"), "image/gif", "gif"}, {[]byte("RIFF1234WEBPimage"), "image/webp", "webp"}, {[]byte("BMimage"), "image/bmp", "bmp"}, {[]byte("II\x2a\x00image"), "image/tiff", "tiff"},
	} {
		f := mediaFixture(t)
		f.data = test.bytes
		f.write(t)
		result := f.verify(t, f.snapshot)
		if result.MIME != test.mime || result.Extension != test.extension {
			t.Fatal("image proof format differs from sender magic detection")
		}
	}
}

type conversationMediaReparseInfo struct{ os.FileInfo }

func (conversationMediaReparseInfo) Sys() any { return &struct{ FileAttributes uint32 }{0x400} }

func TestConversationMediaReparseAttributesAreRejectedPortably(t *testing.T) {
	f := mediaFixture(t)
	info, err := os.Stat(f.source)
	if err != nil || !conversationMediaReparse(conversationMediaReparseInfo{info}) {
		t.Fatal("Windows reparse attributes were not detected")
	}
}

func TestConversationMediaGuardFailureAndCancellationHaveFixedOutput(t *testing.T) {
	f := mediaFixture(t)
	deps := f.deps()
	deps.Guard = func(context.Context, string) (bool, error) {
		return false, errors.New("fixture-token private-origin-path")
	}
	var out bytes.Buffer
	if conversationMediaCLIWithDependencies([]string{"--path", f.source, "--request-id", f.sender}, &out, deps) != 1 || out.String() != "conversation media verification unavailable\n" {
		t.Fatal("guard failure exposed private diagnostics")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifyConversationMedia(ctx, f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("canceled media verification succeeded")
	}
}
