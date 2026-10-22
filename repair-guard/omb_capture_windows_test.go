package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

func ombFixture(t *testing.T) (ombCaptureProof, []object) {
	t.Helper()
	var imageBytes bytes.Buffer
	if err := jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	p := ombCaptureProof{Session: "synthetic-session", ImageSHA: hashBytes(imageBytes.Bytes()), ImageSize: imageBytes.Len()}
	args := func(code string) string {
		b, err := json.Marshal(object{"code": code, "title": "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	rows := []object{
		{"type": "session_meta", "payload": object{"id": p.Session}},
		{"type": "response_item", "payload": object{"type": "function_call", "namespace": "mcp__cua_repl", "name": "js", "arguments": args(omb3344OpenCode), "call_id": "open-fixture"}},
		{"type": "response_item", "payload": object{"type": "function_call_output", "call_id": "open-fixture", "output": []object{{"type": "input_text", "text": `Browser tab: fixture, Title: "OpenMausBot", URL: "http://127.0.0.1:38799/".`}}}},
		{"type": "response_item", "payload": object{"type": "function_call", "namespace": "mcp__cua_repl", "name": "js", "arguments": args(omb3344CaptureCode), "call_id": "capture-fixture"}},
		{"type": "response_item", "payload": object{"type": "function_call_output", "call_id": "capture-fixture", "output": []object{{"type": "input_image", "image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imageBytes.Bytes())}}}},
	}
	return p, rows
}

func ombFixtureNative(t *testing.T, p ombCaptureProof, rows []object) (ombCaptureProof, string) {
	t.Helper()
	keys := []string{"session", "open_call", "open_result", "capture_call", "capture_result"}
	p.Events = map[string]ombNativeReference{}
	var result strings.Builder
	for i, row := range rows {
		body, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		result.Write(body)
		result.WriteByte('\n')
		if i < len(keys) {
			var decoded ombNativeEvent
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			p.Events[keys[i]] = ombNativeReference{Line: i + 1, SHA: hashBytes(body), Type: decoded.Type, PayloadType: decoded.Payload.Type, Name: decoded.Payload.Name, Namespace: decoded.Payload.Namespace, CallID: decoded.Payload.CallID}
		}
	}
	return p, result.String()
}

func TestOMBCaptureBindsActualToolReturnAndAllowsLaterNativeAppend(t *testing.T) {
	p, rows := ombFixture(t)
	p, native := ombFixtureNative(t, p, rows)
	image, ok := ombNativeImage(p, strings.NewReader(native+`{"type":"event_msg","payload":{"type":"later_append"}}`+"\n"))
	if !ok || hashBytes(image) != p.ImageSHA {
		t.Fatal("actual original capture bytes refused")
	}
}

func TestOMBCaptureRejectsFalseOrChangedNativeEvidence(t *testing.T) {
	for _, name := range []string{"open URL", "screenshot script", "call ID", "session", "result bytes", "image SHA", "line SHA", "missing result", "question purpose script", "output title", "extra output image"} {
		t.Run(name, func(t *testing.T) {
			p, rows := ombFixture(t)
			switch name {
			case "open URL":
				rows[1]["payload"].(object)["arguments"] = `{"code":"var omb3344Tab = await cua.createBrowserTab(\"chrome\", \"http://127.0.0.1:1/private\");"}`
			case "screenshot script":
				rows[3]["payload"].(object)["arguments"] = `{"code":"await privateTab.getScreenshot();"}`
			case "call ID":
				rows[4]["payload"].(object)["call_id"] = "another-call"
			case "session":
				rows[0]["payload"].(object)["id"] = "another-session"
			case "result bytes":
				rows[4]["payload"].(object)["output"] = []object{{"type": "input_image", "image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("substituted old screenshot"))}}
			case "question purpose script":
				rows[3]["payload"].(object)["arguments"] = `{"code":"const result = await tools.image_gen__imagegen({prompt:\"question\"}); generatedImage(result);"}`
			case "output title":
				rows[2]["payload"].(object)["output"] = []object{{"type": "input_text", "text": `Browser tab: fixture, Title: "Private", URL: "http://127.0.0.1:38799/".`}}
			case "extra output image":
				rows[4]["payload"].(object)["output"] = append(rows[4]["payload"].(object)["output"].([]object), object{"type": "input_image", "image_url": "data:image/jpeg;base64,aA=="})
			}
			p, native := ombFixtureNative(t, p, rows)
			switch name {
			case "image SHA":
				p.ImageSHA = strings.Repeat("a", 64)
			case "line SHA":
				ref := p.Events["capture_result"]
				ref.SHA = strings.Repeat("a", 64)
				p.Events["capture_result"] = ref
			case "missing result":
				native = strings.Join(strings.Split(native, "\n")[:4], "\n") + "\n"
			}
			if _, ok := ombNativeImage(p, strings.NewReader(native)); ok {
				t.Fatal("false capture evidence accepted")
			}
		})
	}
}

func TestOMBCaptureRejectsInterveningCUATabRebinding(t *testing.T) {
	p, rows := ombFixture(t)
	p, native := ombFixtureNative(t, p, rows)
	lines := strings.Split(native, "\n")
	change := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__cua_repl","name":"js","arguments":"{}"}}`
	lines = append(lines[:3], append([]string{change}, lines[3:]...)...)
	for _, key := range []string{"capture_call", "capture_result"} {
		ref := p.Events[key]
		ref.Line++
		p.Events[key] = ref
	}
	if _, ok := ombNativeImage(p, strings.NewReader(strings.Join(lines, "\n"))); ok {
		t.Fatal("rebound persistent tab accepted")
	}
}

func TestOMBCaptureScopeIsNormalReportOnly(t *testing.T) {
	p := ombCaptureProof{Domain: "omb-loopback-runtime-capture-3344-v1", Todo: 3344, Parent: 3338, Purpose: "normal-discord-log", URL: "http://127.0.0.1:38799/", Source: omb3344Image, ImageSHA: strings.Repeat("a", 64), ImageSize: 100, ImageMIME: "image/jpeg", Storage: "first materialization", Job: "synthetic-todo-3344"}
	p.Review.Seen = true
	p.Review.NoSecrets = true
	p.Review.Loopback = true
	p.Review.Reference = "human observed actual tool result"
	if !ombProofScoped(p) {
		t.Fatal("normal-report control refused")
	}
	for _, change := range []string{"purpose", "URL", "path", "todo", "review", "UI approval", "old receipt"} {
		bad := p
		switch change {
		case "purpose":
			bad.Purpose = "owner-generated-question"
		case "URL":
			bad.URL = "http://127.0.0.1:38799/private"
		case "path":
			bad.Source = "D:/OMB/evidence/omb-active-supervision.png"
		case "todo":
			bad.Todo = 3334
		case "review":
			bad.Review.NoSecrets = false
		case "UI approval":
			bad.UIApproval = true
		case "old receipt":
			bad.OldModified = true
		}
		if ombProofScoped(bad) {
			t.Fatalf("report scope broadened: %s", change)
		}
	}
}

func TestOMBCaptureCLIHasNoCallerSuppliedPathOrEvidence(t *testing.T) {
	for _, args := range [][]string{{"--export-omb-runtime-capture", "D:/elsewhere.jpg"}, {"--export-omb-runtime-capture=true"}, {"--export-omb-runtime-capture", "--guard-path", omb3344Image}, {"--export-omb-runtime-capture", "--help"}} {
		var out bytes.Buffer
		called := false
		code := runOMBCaptureCLI(args, &out, func() object { called = true; return object{"exported": true} })
		if code != 1 || called || out.String() != "{\"exported\":false}\n" {
			t.Fatal("caller input reached capture exporter")
		}
	}
	var out bytes.Buffer
	if runOMBCaptureCLI([]string{"--export-omb-runtime-capture"}, &out, func() object { return object{"exported": true} }) != 0 {
		t.Fatal("valid purpose rejected")
	}
}
