package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type nativePartialFixture struct {
	c        *conversationFixture
	f        *taskQuestionFixture
	v        nativePartialInput
	q        ConversationQuestion
	reply    conversationReply
	items    map[string]map[string]any
	child    map[string]any
	parent   map[string]any
	rpcCalls int
}

func nativePartialTest(t *testing.T) *nativePartialFixture {
	t.Helper()
	c, f := taskQuestionConversationTest(t)
	q := c.ask(t)
	reply := c.reply(t, 8, "a: keep this exact palette and lyric timing scope")
	c.r.ConversationReceiverOnly = true
	h := Health{}
	c.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 {
		t.Fatal(h.Errors)
	}
	proof, err := c.r.conversationProof(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	stamp, ready, err := c.r.conversationReplySource(context.Background(), q, proof, reply)
	if err != nil || !ready {
		t.Fatal("source fixture", err)
	}
	uuid := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }
	v := nativePartialInput{Domain: nativePartialDomain, Kind: nativePartialKind, Request: q.RequestID, Message: reply.Message, Origin: "42", Project: q.Project, Revision: strings.Repeat("e", 40), Parent: f.e.Session, Child: uuid(2), SourceSHA: stamp.SourceSHA, ReplySHA: stamp.ReplySHA, Assignment: nativePartialSelector{Turn: uuid(4), Item: "assignment"}, Artifact: nativePartialArtifact{Path: filepath.Join(f.home, "binding.json")}, Evidence: conversationImplementationEvidence{Completion: "Appearance verified; GUI acceptance pending", Execution: "Native verification commands; no product restart", Origin: "All original stops and dependencies preserved", Identity: "Current original API source and owner matched", AppliedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	artifact, _ := json.Marshal(map[string]any{"owner_reply": v.Message, "revision": v.Revision, "acceptance": "pending"})
	if os.WriteFile(v.Artifact.Path, artifact, 0600) != nil {
		t.Fatal("artifact")
	}
	v.Artifact.SHA = bytesHash(artifact)
	prompt := fmt.Sprintf("Origin 42 / question %s / reply %s / source %s / replySHA %s / %s", q.Message, v.Message, v.SourceSHA, v.ReplySHA, reply.Body)
	v.Assignment.PromptSHA = bytesHash([]byte(prompt))
	command := "Get-FileHash -LiteralPath '" + filepath.ToSlash(v.Artifact.Path) + "' -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json -Compress"
	rawCommand := `"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe" -Command "` + command + `"`
	outputJSON, _ := json.Marshal(map[string]string{"Path": v.Artifact.Path, "Hash": strings.ToUpper(v.Artifact.SHA)})
	output := string(outputJSON)
	v.Commands = []nativePartialCommand{{Turn: uuid(5), Item: "verification", CommandSHA: bytesHash([]byte(rawCommand)), OutputSHA: bytesHash([]byte(output))}}
	n := &nativePartialFixture{c: c, f: f, v: v, q: q, reply: reply, items: map[string]map[string]any{
		"assignment":   {"id": "assignment", "type": "collabAgentToolCall", "tool": "sendInput", "status": "completed", "senderThreadId": v.Parent, "receiverThreadIds": []string{v.Child}, "prompt": prompt},
		"verification": {"id": "verification", "type": "commandExecution", "status": "completed", "source": "unifiedExecStartup", "command": rawCommand, "commandActions": []any{map[string]string{"type": "unknown", "command": command}}, "aggregatedOutput": output, "cwd": f.home, "exitCode": 0, "startedAtMs": time.Now().Add(-time.Minute).UnixMilli(), "completedAtMs": time.Now().Add(-time.Second).UnixMilli()},
	}, parent: map[string]any{"id": v.Parent, "cwd": f.home, "source": "vscode"}, child: map[string]any{"id": v.Child, "cwd": f.home, "parentThreadId": v.Parent, "source": map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": v.Parent, "depth": 1}}}}}
	c.r.NativePartialGuard = func(context.Context, string) bool { return true }
	c.r.NativePartialRPC = n.rpc
	return n
}

func (n *nativePartialFixture) rpc(_ context.Context, method string, args map[string]any) (json.RawMessage, error) {
	n.rpcCalls++
	var result any
	switch method {
	case "thread/read":
		if args["threadId"] == n.v.Parent {
			result = map[string]any{"thread": n.parent}
		} else if args["threadId"] == n.v.Child {
			result = map[string]any{"thread": n.child}
		} else {
			return nil, errors.New("unexpected thread")
		}
	case "thread/items/read":
		ids, ok := args["itemIds"].([]string)
		if !ok || len(ids) != 1 || n.items[ids[0]] == nil {
			return nil, errors.New("unexpected selector")
		}
		if (ids[0] == "assignment" && (args["threadId"] != n.v.Parent || args["turnId"] != n.v.Assignment.Turn)) || (ids[0] == "verification" && (args["threadId"] != n.v.Child || args["turnId"] != n.v.Commands[0].Turn)) {
			return nil, errors.New("unexpected scope")
		}
		result = map[string]any{"data": []any{map[string]any{"turnId": args["turnId"], "item": n.items[ids[0]]}}}
	default:
		return nil, errors.New("mutation attempted")
	}
	b, err := json.Marshal(result)
	return b, err
}

func (n *nativePartialFixture) verify(t *testing.T) (nativePartialVerification, error) {
	t.Helper()
	b, _ := json.Marshal(n.v)
	state, root, err := n.c.r.nativePartialState(context.Background(), n.v)
	if err != nil {
		return nativePartialVerification{}, err
	}
	return n.c.r.nativePartialVerify(context.Background(), n.v, b, state, root)
}

func TestNativePartialAuthenticatesEvidenceWithoutChangingStopsOrState(t *testing.T) {
	n := nativePartialTest(t)
	n.c.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true; row["wait_on"] = []uint64{17} })
	before, _ := os.ReadFile(n.f.todos)
	stateBefore, _ := json.Marshal(n.c.r.State)
	got, err := n.verify(t)
	after, _ := os.ReadFile(n.f.todos)
	stateAfter, _ := json.Marshal(n.c.r.State)
	if err != nil || !got.Verified || got.Acceptance != "pending" || got.Applied || got.Closed || n.rpcCalls != 4 || string(before) != string(after) || string(stateBefore) != string(stateAfter) {
		t.Fatal("native evidence verification changed ownership or inferred completion", err)
	}
	if got.InputSHA == "" || got.OriginSHA == "" || got.NativeSHA == "" || got.SourceSHA != n.v.SourceSHA || got.ReplySHA != n.v.ReplySHA {
		t.Fatal("evidence scope lost")
	}
}

func TestNativePartialRejectsIndependentEvidenceMismatches(t *testing.T) {
	cases := map[string]func(*nativePartialFixture){
		"child_parent":     func(n *nativePartialFixture) { n.child["parentThreadId"] = n.v.Child },
		"parent_cwd":       func(n *nativePartialFixture) { n.parent["cwd"] = filepath.Dir(n.f.home) },
		"wrong_receiver":   func(n *nativePartialFixture) { n.items["assignment"]["receiverThreadIds"] = []string{n.v.Parent} },
		"assignment_hash":  func(n *nativePartialFixture) { n.v.Assignment.PromptSHA = strings.Repeat("0", 64) },
		"command_hash":     func(n *nativePartialFixture) { n.v.Commands[0].CommandSHA = strings.Repeat("0", 64) },
		"output_hash":      func(n *nativePartialFixture) { n.v.Commands[0].OutputSHA = strings.Repeat("0", 64) },
		"null_exit":        func(n *nativePartialFixture) { n.items["verification"]["exitCode"] = nil },
		"nonzero_exit":     func(n *nativePartialFixture) { n.items["verification"]["exitCode"] = 1 },
		"unfinished":       func(n *nativePartialFixture) { n.items["verification"]["status"] = "inProgress" },
		"artifact_changed": func(n *nativePartialFixture) { _ = os.WriteFile(n.v.Artifact.Path, []byte(`{}`), 0600) },
		"artifact_guard": func(n *nativePartialFixture) {
			n.c.r.NativePartialGuard = func(context.Context, string) bool { return false }
		},
		"different_source": func(n *nativePartialFixture) { n.v.SourceSHA = strings.Repeat("0", 64) },
		"different_reply":  func(n *nativePartialFixture) { n.v.ReplySHA = strings.Repeat("0", 64) },
		"generation_changed": func(n *nativePartialFixture) {
			n.c.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 2 })
		},
		"missing_admission": func(n *nativePartialFixture) {
			_ = os.Remove(filepath.Join(n.f.r.Keys.Home, taskQuestionAdmissionName(n.v.Request, true)))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			n := nativePartialTest(t)
			change(n)
			before, _ := os.ReadFile(n.f.todos)
			if _, err := n.verify(t); err == nil {
				t.Fatal("unqualified native evidence accepted")
			}
			after, _ := os.ReadFile(n.f.todos)
			if string(before) != string(after) {
				t.Fatal("failed verification changed task")
			}
		})
	}
}

func TestNativePartialCannotCloseThroughLegacyAppliedFallback(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "partial native work only", true)
	row, err := f.r.todo(42)
	if err != nil {
		t.Fatal(err)
	}
	stamp, _, err := f.r.conversationReplySource(context.Background(), f.q, f.current(), reply)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(nativePartialReceipt{Domain: nativePartialReceiptDomain, Kind: nativePartialKind, InputSHA: strings.Repeat("f", 64), Input: nativePartialInput{Origin: "42", Project: f.q.Project, Request: f.q.RequestID, Message: reply.Message, SourceSHA: stamp.SourceSHA}})
	row.NativePartialRecords = []json.RawMessage{b}
	_, ready, err := f.r.conversationCloseReply(context.Background(), f.q, f.current(), reply, row)
	if err != nil || ready {
		t.Fatal("partial native evidence became applied", err)
	}
	f.check(t, 0)
}

func TestNativePartialCLIAndSchemaRejectAuthorityOverrides(t *testing.T) {
	for _, args := range [][]string{{"native-implementation-record", "--input-file", "input.json"}, {"verify-native-implementation", "--input-file", "input.json", "--origin-state-file", "state.json"}} {
		if _, err := parseConversationCLI(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"native-implementation-record", "--input-file", "input.json", "--actor-todo-id", "1"}, {"--config", "custom.json", "verify-native-implementation", "--input-file", "i", "--origin-state-file", "s"}, {"verify-native-implementation", "--input-file", "i"}, {"native-implementation-record", "--input-file", "i", "--input-file", "j"}} {
		if _, err := parseConversationCLI(args); err == nil {
			t.Fatal("authority override accepted")
		}
	}
	n := nativePartialTest(t)
	b, _ := json.Marshal(n.v)
	if _, err := nativePartialDecode(b); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append([]byte(`{"domain":"duplicate",`), b[1:]...), append(b[:len(b)-1], []byte(`,"job_id":"fake"}`)...)} {
		if _, err := nativePartialDecode(bad); err == nil {
			t.Fatal("ambiguous/legacy authority input accepted")
		}
	}
}

func TestNativePartialResultRequiresExplicitPendingBooleans(t *testing.T) {
	v := map[string]any{"domain": nativePartialReceiptDomain, "kind": nativePartialKind, "recorded": true, "changed": false, "input_sha256": strings.Repeat("a", 64), "receipt_sha256": strings.Repeat("b", 64), "acceptance": "pending", "application_inferred": false, "question_closed_get_verified": false}
	b, _ := json.Marshal(v)
	if _, err := nativePartialResultDecode(b); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"recorded", "changed", "application_inferred", "question_closed_get_verified"} {
		t.Run(key, func(t *testing.T) {
			saved := v[key]
			delete(v, key)
			b, _ := json.Marshal(v)
			if _, err := nativePartialResultDecode(b); err == nil {
				t.Fatal("missing boolean accepted")
			}
			v[key] = nil
			b, _ = json.Marshal(v)
			if _, err := nativePartialResultDecode(b); err == nil {
				t.Fatal("null boolean accepted")
			}
			v[key] = saved
		})
	}
}

func TestNativePartialDetectsSourceAndArtifactChangeDuringNativeRead(t *testing.T) {
	for _, change := range []string{"reply", "artifact", "snapshot"} {
		t.Run(change, func(t *testing.T) {
			n := nativePartialTest(t)
			base := n.c.r.NativePartialRPC
			mutated := false
			n.c.r.NativePartialRPC = func(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
				b, err := base(ctx, method, args)
				if !mutated && method == "thread/items/read" {
					mutated = true
					switch change {
					case "reply":
						n.c.replies[0].Body = "edited while reading"
					case "artifact":
						_ = os.WriteFile(n.v.Artifact.Path, []byte(`{}`), 0600)
					case "snapshot":
						n.c.mutateTodo(t, func(row map[string]any) { row["note"] = "concurrent writer" })
					}
				}
				return b, err
			}
			if _, err := n.verify(t); err == nil || !mutated {
				t.Fatal("concurrent source change accepted")
			}
		})
	}
}

func TestNativePartialHashProofRejectsEchoCommentAndForeignPaths(t *testing.T) {
	n := nativePartialTest(t)
	item := n.items["verification"]
	action := item["commandActions"].([]any)[0].(map[string]string)["command"]
	raw := item["command"].(string)
	output := item["aggregatedOutput"].(string)
	if !nativePartialHashCommand(raw, action, output, n.v.Artifact) {
		t.Fatal("valid hash proof rejected")
	}
	for _, bad := range []string{"Write-Output '" + n.v.Artifact.SHA + "' # " + action, action + "; Write-Output done", strings.Replace(action, filepath.ToSlash(n.v.Artifact.Path), "C:/foreign/binding.json", 1), "$p='" + filepath.ToSlash(n.v.Artifact.Path) + "'; " + action} {
		if nativePartialHashCommand(strings.Replace(raw, action, bad, 1), bad, output, n.v.Artifact) {
			t.Fatal("nonliteral or foreign hash accepted")
		}
	}
	if nativePartialHashCommand(raw+"; Write-Output extra", action, output, n.v.Artifact) {
		t.Fatal("outer script accepted extra statements")
	}
	foreign, _ := json.Marshal(map[string]string{"Path": "C:/foreign/binding.json", "Hash": n.v.Artifact.SHA})
	if nativePartialHashCommand(raw, action, string(foreign), n.v.Artifact) {
		t.Fatal("foreign output path accepted")
	}
}

func TestNativePartialRejectsFutureTimeAndMisleadingArtifactFields(t *testing.T) {
	for _, kind := range []string{"future", "after_applied", "prose_revision", "wrong_reply", "corrupt_index"} {
		t.Run(kind, func(t *testing.T) {
			n := nativePartialTest(t)
			switch kind {
			case "future":
				n.items["verification"]["startedAtMs"] = time.Now().Add(time.Hour).UnixMilli()
				n.items["verification"]["completedAtMs"] = time.Now().Add(2 * time.Hour).UnixMilli()
			case "after_applied":
				n.items["verification"]["completedAtMs"] = time.Now().Add(time.Minute).UnixMilli()
			case "corrupt_index":
				_ = os.WriteFile(n.c.r.conversationPath(), []byte(`{broken`), 0600)
			case "prose_revision", "wrong_reply":
				artifact := map[string]string{"owner_reply": n.v.Message, "revision": n.v.Revision}
				if kind == "prose_revision" {
					delete(artifact, "revision")
					artifact["note"] = n.v.Revision
				} else {
					artifact["owner_reply"] = "999"
				}
				b, _ := json.Marshal(artifact)
				_ = os.WriteFile(n.v.Artifact.Path, b, 0600)
				n.v.Artifact.SHA = bytesHash(b)
			}
			if _, err := n.verify(t); err == nil {
				t.Fatal("false evidence accepted")
			}
		})
	}
}

func TestNativePartialWriterKeepsFixedVerificationBudget(t *testing.T) {
	if commandBudget(conversationTodo, []string{"native-implementation-record"}) != 50*time.Second || nativePartialCLIDeadline != 180*time.Second {
		t.Fatal("native re-verification transaction lost its bounded budget")
	}
	if commandBudget(conversationTodo, []string{"implementation-record"}) != 20*time.Second || conversationCLIDeadline != 60*time.Second {
		t.Fatal("native budget changed ordinary command deadlines")
	}
}
