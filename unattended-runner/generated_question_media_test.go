package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func generatedFixture(t *testing.T) (generatedQuestionSpec, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(root, "original.png")
	data := []byte("\x89PNG\r\n\x1a\nnew-generated-image")
	if err = os.WriteFile(image, data, 0600); err != nil {
		t.Fatal(err)
	}
	s := generatedQuestionSpec{Scope: generatedQuestion3084, Session: "fixture-session", Proof: filepath.Join(root, "proof.json"), Rollout: filepath.Join(root, "rollout.jsonl"), Read: generatedQuestionRead, Guard: func(context.Context, string) (bool, error) { return true, nil }, Records: generatedQuestionRecords, Task: func() bool { return true }, Now: func() time.Time { return time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) }}
	s.SourceClear = func(string) bool { return true }
	proof := []byte(`{"source":"independently pinned tool proof"}`)
	if err = os.WriteFile(s.Proof, proof, 0600); err != nil {
		t.Fatal(err)
	}
	s.ProofSHA = bytesHash(proof)
	g := map[string]any{"todo": 3084, "option": "a", "revision": 3, "mode": "new_generation", "tool": "image_gen__imagegen", "orchestrator": "functions.exec", "execCellId": "25", "invocationOrder": 1, "inputImages": []any{}, "originalPath": image, "sessionArtifactDirectoryFromTool": s.Session, "request": map[string]any{"prompt": "fictional owner UI mockup", "transparent_background": false}, "response": map[string]any{"output_hint": "Generated images are saved as " + image}}
	b, _ := json.Marshal(g)
	record := filepath.Join(root, "generation.json")
	if err = os.WriteFile(record, b, 0600); err != nil {
		t.Fatal(err)
	}
	s.Images = []generatedImageBound{{Path: image, SHA256: bytesHash(data), Size: int64(len(data)), Record: record, RecordSHA: bytesHash(b), PromptSHA: bytesHash([]byte("fictional owner UI mockup")), Option: "a", Revision: 3, Order: 1}}
	kinds := []string{"custom_tool_call", "custom_tool_call_output", "function_call", "function_call_output"}
	names := []string{"exec", "", "wait", ""}
	calls := []string{"creation", "creation", "wait", "wait"}
	var records bytes.Buffer
	for i, kind := range kinds {
		payload := map[string]any{"type": kind, "name": names[i], "call_id": calls[i]}
		if i == 0 {
			payload["input"] = "tools.image_gen__imagegen({prompt:promptA,transparent_background:false})"
		}
		if i == 3 {
			payload["output"] = []any{map[string]any{"text": "Generated as " + image}}
		}
		raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": payload})
		s.Events = append(s.Events, generatedEventBound{i + 1, bytesHash(raw), kind, names[i], calls[i]})
		records.Write(raw)
		records.WriteByte('\n')
	}
	if err = os.WriteFile(s.Rollout, records.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return s, image
}

func TestGeneratedQuestionQualificationsArePerSourceAndPerQuestion(t *testing.T) {
	s, image := generatedFixture(t)
	p, err := qualifyGeneratedQuestionImage(context.Background(), image, s.Scope, s)
	if err != nil || !p.Qualified || p.Scope != s.Scope || p.Domain != generatedQuestionDomain {
		t.Fatalf("exact new output did not qualify: %v", err)
	}
	for _, field := range []string{"todo", "body", "decision", "owner", "guild", "forum"} {
		t.Run(field, func(t *testing.T) {
			q := s.Scope
			switch field {
			case "todo":
				q.Todo++
			case "body":
				q.Body = "other"
			case "decision":
				q.Decision = "other"
			case "owner":
				q.Owner = "other"
			case "guild":
				q.Guild = "other"
			case "forum":
				q.Forum = "other"
			}
			if _, err := qualifyGeneratedQuestionImage(context.Background(), image, q, s); err == nil {
				t.Fatal("other question or destination qualified")
			}
		})
	}
	copyPath := filepath.Join(filepath.Dir(image), "copy.png")
	b, _ := os.ReadFile(image)
	if err = os.WriteFile(copyPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = qualifyGeneratedQuestionImage(context.Background(), copyPath, s.Scope, s); err == nil {
		t.Fatal("copy path manufactured qualification")
	}
	if err = os.WriteFile(image, []byte("\x89PNG\r\n\x1a\nchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = qualifyGeneratedQuestionImage(context.Background(), image, s.Scope, s); err == nil {
		t.Fatal("changed original qualified")
	}
}

func TestGeneratedQuestionRejectsForgedMissingExpiredOrStoppedEvidence(t *testing.T) {
	for _, kind := range []string{"proof", "record", "native-event", "missing-record", "protected-proof", "protected-source", "expired", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			s, image := generatedFixture(t)
			switch kind {
			case "proof":
				_ = os.WriteFile(s.Proof, []byte(`{"qualified":true}`), 0600)
			case "record":
				_ = os.WriteFile(s.Images[0].Record, []byte(`{"tool":"image_gen__imagegen","todo":3084}`), 0600)
			case "native-event":
				f, _ := os.OpenFile(s.Rollout, os.O_WRONLY, 0600)
				_, _ = f.WriteAt([]byte("forged"), 0)
				_ = f.Close()
			case "missing-record":
				_ = os.Remove(s.Images[0].Record)
			case "protected-proof":
				s.Guard = func(context.Context, string) (bool, error) { return false, nil }
			case "protected-source":
				s.SourceClear = func(string) bool { return false }
			case "expired":
				s.Now = func() time.Time { return time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC) }
			case "stopped":
				s.Task = func() bool { return false }
			}
			if _, err := qualifyGeneratedQuestionImage(context.Background(), image, s.Scope, s); err == nil {
				t.Fatal("unqualified or forged origin accepted")
			}
		})
	}
}

func TestGeneratedQuestionSelectedNativeRecordsSurviveAppend(t *testing.T) {
	s, image := generatedFixture(t)
	f, err := os.OpenFile(s.Rollout, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintln(f, `{"type":"later unrelated turn"}`)
	_ = f.Close()
	if _, err = qualifyGeneratedQuestionImage(context.Background(), image, s.Scope, s); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedQuestionCLIReportsQualificationWithoutReclassifyingSource(t *testing.T) {
	s, image := generatedFixture(t)
	var out bytes.Buffer
	args := append([]string{"--path", image}, generatedQuestionScopeArgs(s.Scope)...)
	if code := generatedQuestionCLIWithSpec(args, &out, s); code != 0 {
		t.Fatalf("new qualified question entry must work: %d %s", code, &out)
	}
	var p map[string]any
	if json.Unmarshal(out.Bytes(), &p) != nil || p["qualified"] != true || p["client_resource"] != nil || p["classification_unverified"] != nil {
		t.Fatal("qualification changed general source classification")
	}
}

func TestGeneratedQuestionQualificationFlowsThroughMediaVerifierButCannotBeUsedForLog(t *testing.T) {
	s, image := generatedFixture(t)
	home := mediaFixture(t)
	calls := 0
	deps := conversationMediaDependencies{Home: home.home, Guard: func(context.Context, string) (bool, error) { return false, nil }, Question: &s.Scope, Qualify: func(ctx context.Context, path string, scope generatedQuestionScope) (*generatedImageQualification, error) {
		calls++
		return qualifyGeneratedQuestionImage(ctx, path, scope, s)
	}}
	proof, err := verifyConversationMedia(context.Background(), image, "conversation-v1-"+strings.Repeat("a", 32), deps)
	if err != nil || proof.QuestionQualification == nil || !proof.Verified || calls != 1 {
		t.Fatalf("qualified question did not reach media proof: %v", err)
	}
	calls = 0
	if _, err = verifyConversationMedia(context.Background(), image, "conversation-log-"+strings.Repeat("a", 32), deps); err == nil || calls != 0 {
		t.Fatal("question qualification was reused for another purpose")
	}
	q := s.Scope
	q.Todo++
	deps.Question = &q
	if _, err = verifyConversationMedia(context.Background(), image, "conversation-v1-"+strings.Repeat("a", 32), deps); err == nil {
		t.Fatal("qualification was reused for another original task")
	}
}
