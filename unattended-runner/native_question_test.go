package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// All records are synthetic and in memory. No temporary folder or HTTP server.
func nativeQuestionMemoryFixture(t *testing.T) (nativeQuestionEvidence, nativeQuestionDeps, map[string][]byte) {
	t.Helper()
	root, _ := filepath.Abs("native-question-fixture")
	now := time.Date(2026, 11, 9, 4, 0, 0, 0, time.UTC)
	body := []byte("画像2枚の構図を確認してください。\r\na. 続ける\r\nb. 修正する\r\n")
	decision := []byte(`{"todo_id":42,"resources":[{"kind":"repository","id":"AutoDevJapan/fixture"}],"decision":"direction","scope":{"view":"fixture"}}`)
	canonical, _ := canonicalJSON(decision)
	e := nativeQuestionEvidence{Domain: nativeEvidenceDomain, Request: "fixture-original-question", Origin: "106", Job: "fixture-job", Worktree: root, Session: "11111111-1111-1111-1111-111111111111", Rollout: filepath.Join(root, "sessions", "rollout-11111111-1111-1111-1111-111111111111.jsonl"), Body: filepath.Join(root, "body.txt"), Decision: filepath.Join(root, "decision.json"), Title: "構図確認", Scope: generatedQuestionScope{Todo: 42, Body: bytesHash(body), Decision: bytesHash(canonical), Owner: "111", Guild: "222", Forum: "333"}, ContentReview: "Synthetic local fixture; no customer or secret content"}
	files := map[string][]byte{e.Body: body, e.Decision: decision}
	for i := 0; i < 2; i++ {
		b := append([]byte("\x89PNG\r\n\x1a\n"), byte(i))
		p := filepath.Join(root, fmt.Sprintf("image-%d.png", i))
		files[p] = b
		e.Images = append(e.Images, nativeQuestionImage{Path: p, SHA: bytesHash(b), Size: int64(len(b))})
	}
	quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	question := "& 'D:/CC/Tools/discord-ask/discord_ask.exe' recovery-ask --request-id '" + e.Request + "' --todo-id 42 --decision-file '" + e.Decision + "' --body-file '" + e.Body + "' --title '" + e.Title + "' --origin-thread 106"
	preview := ""
	for _, im := range e.Images {
		question += " --attach '" + im.Path + "'"
		preview += "image((await tools.view_image({path:" + quote(im.Path) + "})).image_url);\n"
	}
	for _, im := range e.Images {
		question += " --expected-image-sha256 " + im.SHA
	}
	records := map[int]json.RawMessage{}
	add := func(line int, kind, name, call string, output any, input string) nativeQuestionEvent {
		p := map[string]any{"type": kind, "call_id": call}
		if name != "" {
			p["name"] = name
		}
		if input != "" {
			p["input"] = input
		}
		if output != nil {
			p["output"] = output
		}
		b, _ := json.Marshal(map[string]any{"timestamp": now.Add(-time.Hour).Format(time.RFC3339Nano), "type": "response_item", "payload": p})
		records[line] = b
		return nativeQuestionEvent{Line: line, SHA: bytesHash(b)}
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": e.Session, "cwd": root}})
	records[1] = meta
	e.SessionSHA = bytesHash(meta)
	e.Question = add(3, "custom_tool_call", "exec", "ask", nil, "text(await tools.exec_command({cmd:"+quote(question)+",max_output_tokens:1000}));")
	e.Preview = add(4, "custom_tool_call", "exec", "preview", nil, preview)
	blocks := []map[string]any{{"type": "input_text", "text": "Script completed\n"}}
	for _, im := range e.Images {
		blocks = append(blocks, map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(files[im.Path])})
	}
	e.PreviewResult = add(5, "custom_tool_call_output", "", "preview", blocks, "")
	e.Async = add(6, "function_call", "request_user_input_async", "async", nil, "")
	var async map[string]any
	_ = json.Unmarshal(records[6], &async)
	async["payload"].(map[string]any)["arguments"] = `{"questions":[{"title":"表示した2枚目の構図は？","options":["a","b"]}]}`
	records[6], _ = json.Marshal(async)
	e.Async.SHA = bytesHash(records[6])
	e.Accepted = add(7, "function_call_output", "", "async", `{"accepted":true}`, "")
	e.AsyncTitle = "表示した2枚目の構図は？"
	e.AsyncOptions = []string{"a", "b"}
	d := nativeQuestionDeps{Sessions: filepath.Join(root, "sessions"), Now: func() time.Time { return now }, Read: func(p string, max int64) ([]byte, error) {
		b, ok := files[p]
		if !ok || int64(len(b)) > max {
			return nil, errConversationMedia
		}
		return append([]byte(nil), b...), nil
	}, Records: func(_ string, bounds []nativeQuestionEvent) ([]json.RawMessage, error) {
		var out []json.RawMessage
		for _, b := range bounds {
			raw := records[b.Line]
			if bytesHash(raw) != b.SHA {
				return nil, errConversationMedia
			}
			out = append(out, raw)
		}
		return out, nil
	}, Bind: func(context.Context, nativeQuestionEvidence, json.RawMessage) error { return nil }, Clear: func(string) bool { return true }}
	d.Content = func(e nativeQuestionEvidence, b, dec []byte) error {
		if !bytes.Equal(b, body) || !bytes.Equal(dec, canonical) {
			return errConversationMedia
		}
		return nil
	}
	d.Capture = func(nativeQuestionEvidence) error { return nil }
	d.ImageGuard = func(context.Context, string, bool) error { return nil }
	return e, d, files
}

func TestNativeQuestionMemoryExactScopeAndOriginalBytes(t *testing.T) {
	e, d, _ := nativeQuestionMemoryFixture(t)
	p, err := validateNativeQuestion(context.Background(), e, d)
	if err != nil || !p.Validated || p.SendAllowed || p.Request != e.Request || p.AsyncCall != "async" || len(p.Images) != 2 {
		t.Fatal("exact original scope not qualified", err)
	}
}
func TestNativeQuestionMemoryRejectsChanges(t *testing.T) {
	for _, kind := range []string{"request", "body", "decision", "image-order", "image-path", "async", "accepted", "session", "future", "expired", "task", "excluded"} {
		t.Run(kind, func(t *testing.T) {
			e, d, files := nativeQuestionMemoryFixture(t)
			switch kind {
			case "request":
				e.Request = "different"
			case "body":
				files[e.Body] = append(files[e.Body], byte('x'))
			case "decision":
				files[e.Decision] = []byte(`{"todo_id":43}`)
			case "image-order":
				e.Images[0], e.Images[1] = e.Images[1], e.Images[0]
			case "image-path":
				e.Images[1].Path += ".copy.png"
			case "async":
				e.Async.SHA = strings.Repeat("a", 64)
			case "accepted":
				e.Accepted.SHA = strings.Repeat("a", 64)
			case "session":
				e.Session = "22222222-2222-2222-2222-222222222222"
			case "future":
				d.Now = func() time.Time { return time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC) }
			case "expired":
				d.Now = func() time.Time { return time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC) }
			case "task":
				d.Bind = func(context.Context, nativeQuestionEvidence, json.RawMessage) error { return errConversationMedia }
			case "excluded":
				d.Clear = func(string) bool { return false }
			}
			if _, err := validateNativeQuestion(context.Background(), e, d); err == nil {
				t.Fatal("changed input accepted")
			}
		})
	}
}
func TestNativeQuestionMemoryStrictEvidenceAndNonreflectiveCLI(t *testing.T) {
	for _, raw := range []string{`{"domain":"one","domain":"two"}`, `{"domain":"one","DOMAIN":"two"}`, `{"scope":{"todo_id":42,"todo_id":42}}`, `{"unexpected":"private"}`} {
		var e nativeQuestionEvidence
		if historyStrictJSON([]byte(raw), &e) == nil {
			t.Fatal("ambiguous schema accepted")
		}
	}
	e, d, files := nativeQuestionMemoryFixture(t)
	manifest, _ := json.Marshal(e)
	files["evidence"] = manifest
	var out bytes.Buffer
	if nativeQuestionCLIWithDeps([]string{"--evidence", "evidence"}, &out, d) != 0 {
		t.Fatal("CLI validation or redaction", out.String())
	}
	var proof nativeQuestionProof
	if json.Unmarshal(out.Bytes(), &proof) != nil || proof.Images[0].Path != e.Images[0].Path || proof.SendAllowed {
		t.Fatal("purpose proof differs")
	}
	out.Reset()
	if nativeQuestionCLIWithDeps([]string{"--evidence", "missing-private"}, &out, d) != 1 || strings.Contains(out.String(), "missing-private") {
		t.Fatal("CLI failure reflected input")
	}
}

func TestNativeQuestionMemoryLiteralContent(t *testing.T) {
	e, d, _ := nativeQuestionMemoryFixture(t)
	body := []byte("fixture body\r\n")
	dec := []byte(`{"decision":"direction","resources":[{"id":"fixture","kind":"repository"}],"scope":{"view":"fixture"},"todo_id":42}`)
	command := "@'\nfixture body\n'@ | Set-Content -LiteralPath 'body.txt' -Encoding utf8\n$decision=[ordered]@{todo_id=42;resources=@(@{kind='repository';id='fixture'});decision='direction';scope=@{view='fixture'}}\n$decision|ConvertTo-Json -Depth 8|Set-Content -LiteralPath 'decision.json' -Encoding utf8"
	raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "exec", "input": "text(await tools.exec_command({cmd:" + string(mustNativeJSON(command)) + ",max_output_tokens:1000}));"}})
	e.Content = nativeQuestionEvent{Line: 2, SHA: bytesHash(raw)}
	d.Records = func(string, []nativeQuestionEvent) ([]json.RawMessage, error) { return []json.RawMessage{raw}, nil }
	if err := nativeQuestionContent(e, body, dec, d); err != nil {
		t.Fatal("literal source not recovered", err)
	}
	if nativeQuestionContent(e, append(body, 'x'), dec, d) == nil {
		t.Fatal("changed body accepted")
	}
}
func mustNativeJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestNativeQuestionMemoryGuardPurposeNeverClearsReferenceOrExplicitProtection(t *testing.T) {
	raw := []byte(`{"access_allowed":false,"client_resource":true,"classification_unverified":false,"classification_basis":"owner_rule_unattributed_location","protection_status":"protected","protection_categories":["client_project"]}`)
	if nativeQuestionGuardVerdict(raw, true) != nil {
		t.Fatal("native candidate location gap")
	}
	if nativeQuestionGuardVerdict(raw, false) == nil {
		t.Fatal("reference accepted outside normal clear")
	}
	for _, changed := range []string{strings.Replace(string(raw), "owner_rule_unattributed_location", "client_department", 1), strings.Replace(string(raw), `"classification_unverified":false`, `"classification_unverified":true`, 1), strings.Replace(string(raw), `["client_project"]`, `["client_project","owner_card"]`, 1), strings.Replace(string(raw), `"access_allowed":false`, `"access_allowed":false,"access_allowed":true`, 1)} {
		if nativeQuestionGuardVerdict([]byte(changed), true) == nil {
			t.Fatal("explicit/unknown protection accepted")
		}
	}
}
func TestNativeQuestionMemoryMetadataNeverChangesAuditedCommand(t *testing.T) {
	code := `text(await tools.exec_command({cmd:"literal,max_output_tokens:123",max_output_tokens:1000}));`
	got := nativeQuestionMetadata(code)
	if got != `text(await tools.exec_command({cmd:"literal,max_output_tokens:123"}));` {
		t.Fatal("command string changed")
	}
}

func TestNativeQuestionMemoryCanonicalMatchesSenderEscaping(t *testing.T) {
	b, err := canonicalJSON([]byte(`{"z":"<a>&b","a":"日本語"}`))
	if err != nil || string(b) != `{"a":"日本語","z":"<a>&b"}` {
		t.Fatal("sender canonical bytes differ")
	}
}

func TestNativeQuestionMemoryCaptureRequiresCompletedExecutionAndExactCopyReceipt(t *testing.T) {
	for _, kind := range []string{"valid", "failed-run", "fake-command", "different-copy", "different-receipt", "missing-producer", "protected-reference"} {
		t.Run(kind, func(t *testing.T) {
			e, d, _ := nativeQuestionMemoryFixture(t)
			e.Question.Line = 20
			e.Images[1].Producer = filepath.Join(e.Worktree, "test-results", "live-3d-fixture", "live-3d-dark-round-end.png")
			e.Producer = nativeQuestionEvent{Line: 8}
			e.Copy = nativeQuestionEvent{Line: 9}
			e.CopyResult = nativeQuestionEvent{Line: 10}
			exit := 0
			if kind == "failed-run" {
				exit = 1
			}
			script := "$env:E2E_PORT='3137'\n$env:PLAYWRIGHT_BROWSERS_PATH='D:/CC/ToolCache/ms-playwright'\nnode node_modules/@playwright/test/cli.js test --project=chromium --workers=1 --retries=0 --reporter=line e2e/live-3d.spec.ts *> '../result.log'\nexit $LASTEXITCODE"
			if kind == "fake-command" {
				script = "echo " + script
			}
			run := mustNativeJSON(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed", "item": map[string]any{"type": "CommandExecution", "exit_code": exit, "command": []string{"pwsh.exe", "-Command", script}}}})
			src, _ := filepath.Rel(e.Worktree, e.Images[1].Producer)
			dst, _ := filepath.Rel(e.Worktree, e.Images[1].Path)
			command := "Copy-Item -LiteralPath '" + filepath.ToSlash(src) + "' -Destination '" + filepath.ToSlash(dst) + "'"
			if kind == "different-copy" {
				command += "; echo true"
			}
			call := mustNativeJSON(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "exec", "call_id": "copy", "input": "text(await tools.exec_command({cmd:" + string(mustNativeJSON(command)) + "}));"}})
			hash := e.Images[1].SHA
			if kind == "different-receipt" {
				hash = strings.Repeat("a", 64)
			}
			output := string(mustNativeJSON([]map[string]any{{"Path": e.Images[1].Path, "Hash": hash}}))
			result := mustNativeJSON(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output", "call_id": "copy", "output": []map[string]any{{"type": "input_text", "text": string(mustNativeJSON(map[string]any{"exit_code": 0, "output": output}))}}}})
			raw := map[int]json.RawMessage{8: run, 9: call, 10: result}
			e.Producer.SHA = bytesHash(run)
			e.Copy.SHA = bytesHash(call)
			e.CopyResult.SHA = bytesHash(result)
			d.Records = func(_ string, b []nativeQuestionEvent) ([]json.RawMessage, error) {
				var out []json.RawMessage
				for _, v := range b {
					if bytesHash(raw[v.Line]) != v.SHA {
						return nil, errConversationMedia
					}
					out = append(out, raw[v.Line])
				}
				return out, nil
			}
			if kind == "missing-producer" {
				e.Images[1].Producer = ""
			}
			if kind == "protected-reference" {
				e.Images[0].Producer = e.Images[1].Producer
			}
			err := nativeQuestionCapture(e, d)
			if (err == nil) != (kind == "valid") {
				t.Fatal("capture guard", kind, err)
			}
		})
	}
}
