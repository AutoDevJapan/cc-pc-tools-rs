package main

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func skillsBootstrapFixture(cwd, body string, readonly bool) string {
	mode := `<permission_profile type="disabled"><file_system type="unrestricted" /></permission_profile>`
	if readonly {
		mode = `<permission_profile type="managed"><file_system type="restricted"><entry access="read"><special>:root</special></entry></file_system></permission_profile>`
	}
	return "# AGENTS.md instructions for " + cwd + "\n\n<INSTRUCTIONS>\n" + body + "\n</INSTRUCTIONS>\n<environment_context><cwd>" + cwd + "</cwd><shell>powershell</shell><current_date>2026-10-26</current_date><timezone>Asia/Tokyo</timezone><filesystem><workspace_roots><root>" + cwd + "</root><root>D:/CC</root></workspace_roots>" + mode + "</filesystem></environment_context>"
}

func TestSkills3080StoredScopeAcceptsOnlyNormalRelationAndGuidance(t *testing.T) {
	for _, kind := range []string{"readonly", "implementation", "already_in_spec", "relation", "no_marker", "private_tail", "before_guide", "after_guide", "other_parent", "other_kind", "missing_guide", "quoted_other_spec"} {
		t.Run(kind, func(t *testing.T) {
			spec := []byte(`{"domain":"synthetic-typed"}`)
			sandbox := "read-only"
			guide := skillsRunnerReadGuidance
			var rel []skillsSubwork
			if kind == "implementation" {
				sandbox = "danger-full-access"
				guide = skillsRunnerWriteGuidance
			}
			if kind == "already_in_spec" {
				spec = append(spec, []byte(guide)...)
				guide = ""
			}
			tail := guide
			if kind == "relation" || kind == "other_parent" || kind == "other_kind" {
				rel = []skillsSubwork{{Parent: 3080, Kind: "review"}}
				tail = "\nこの作業はToDo 3080の独立したreviewです。保存済み仕様の有限な範囲だけを扱い、主担当の完了を前提にせず結果を返してください。主担当のソースを編集したり、独立したPRの作成やマージを担当したりしないでください。\n" + guide
				if kind == "other_parent" {
					rel[0].Parent = 3999
				}
				if kind == "other_kind" {
					rel[0].Kind = "review private"
				}
			}
			switch kind {
			case "private_tail":
				tail = "private input"
			case "before_guide":
				tail = "private input" + guide
			case "after_guide":
				tail = guide + "private input"
			case "missing_guide":
				tail = ""
			}
			prompt := append(append([]byte("registered scheduler prefix\n依頼:\n"), spec...), []byte(tail)...)
			if kind == "no_marker" {
				prompt = []byte("other request")
			}
			if kind == "quoted_other_spec" {
				prompt = append([]byte("registered\n依頼:\nprivate spec then quoted: "), prompt...)
			}
			ok := skillsStoredPromptScope(prompt, spec, sandbox, rel...)
			want := kind == "readonly" || kind == "implementation" || kind == "already_in_spec" || kind == "relation"
			if ok != want {
				t.Fatalf("stored scope %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080BootstrapIdentityBindsInstructionsAndEnvironment(t *testing.T) {
	for _, kind := range []string{"readonly", "implementation", "changed_body", "wrong_cwd", "wrong_header", "extra_text", "private_element", "extra_attribute", "write_grant", "duplicate_cwd", "other_root", "malformed", "empty_cwd", "empty_special"} {
		t.Run(kind, func(t *testing.T) {
			cwd, body := "D:/CC/Work/synthetic", "fixed owner and project operating instructions"
			text := skillsBootstrapFixture(cwd, body, kind != "implementation")
			switch kind {
			case "changed_body":
				text = strings.Replace(text, body, "private input", 1)
			case "wrong_cwd":
				text = strings.Replace(text, "<cwd>"+cwd, "<cwd>D:/other", 1)
			case "wrong_header":
				text = "private instruction\n" + text
			case "extra_text":
				text += "private instruction"
			case "private_element":
				text = strings.Replace(text, "</environment_context>", "<private>private input</private></environment_context>", 1)
			case "extra_attribute":
				text = strings.Replace(text, "<environment_context>", `<environment_context scope="private">`, 1)
			case "write_grant":
				text = strings.Replace(text, `access="read"`, `access="write"`, 1)
			case "duplicate_cwd":
				text = strings.Replace(text, "</cwd>", "</cwd><cwd>"+cwd+"</cwd>", 1)
			case "other_root":
				text = strings.Replace(text, "<root>D:/CC</root>", "<root>D:/other</root>", 1)
			case "malformed":
				text += "<"
			case "empty_cwd":
				text = strings.Replace(text, "<cwd>"+cwd+"</cwd>", "<cwd/>", 1)
			case "empty_special":
				text = strings.Replace(text, "<special>:root</special>", "<special/>", 1)
			}
			sha, ok := skillsBootstrapIdentity(text, cwd)
			accepted := ok && sha == hashBytes([]byte(body))
			if accepted != (kind == "readonly" || kind == "implementation") {
				t.Fatalf("bootstrap %s accepted=%v", kind, accepted)
			}
		})
	}
}

func TestSkills3080TypedNativeRejectsEveryUnknownEarlierUser(t *testing.T) {
	for _, kind := range []string{"missing_bootstrap", "known_origin_bootstrap", "plain_prior_user", "fake_bootstrap", "private_bootstrap_tail", "prior_with_delimiter", "later_user", "unsupported_user_block"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
			id := "11111111-1111-7111-8111-111111111111"
			cwd := "D:/CC/Work/synthetic"
			body := "fixed owner operating instructions"
			prompt := "exact execution input"
			at := start.Add(time.Second)
			records := []object{{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}}
			user := func(s string) object {
				return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": s}}}}
			}
			switch kind {
			case "known_origin_bootstrap":
				records = append(records, user(skillsBootstrapFixture(cwd, body, true)))
			case "plain_prior_user":
				records = append(records, user("private earlier input"))
			case "fake_bootstrap":
				records = append(records, user(skillsBootstrapFixture(cwd, "private input", true)))
			case "private_bootstrap_tail":
				records = append(records, user(skillsBootstrapFixture(cwd, body, true)+"private input"))
			case "prior_with_delimiter":
				records = append(records, user("prior\n依頼:\nprivate"))
			}
			input := user(prompt)
			if kind == "unsupported_user_block" {
				input["payload"].(object)["content"] = []object{{"type": "input_text", "text": prompt}, {"type": "input_image", "image_url": "private image"}}
			}
			records = append(records, input)
			if kind == "later_user" {
				records = append(records, user("private later input"))
			}
			var stream bytes.Buffer
			for _, r := range records {
				b, _ := json.Marshal(r)
				stream.Write(b)
				stream.WriteByte('\n')
			}
			p := &skillsNativeResultProof{prompt: []byte(prompt), inputOnly: true, exactInput: true, bootstrapSHA: hashBytes([]byte(body)), cwd: cwd}
			_, ok := skillsNativeProjectionBound(&stream, id, cwd, start, start.Add(time.Minute), p)
			if ok != (kind == "known_origin_bootstrap") {
				t.Fatalf("typed prior input %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080CallerBootstrapIsIndependentOfTaskWindow(t *testing.T) {
	for _, kind := range []string{"valid_before_job", "missing_bootstrap", "task_input_only", "changed_bootstrap", "other_session", "other_cwd"} {
		t.Run(kind, func(t *testing.T) {
			cwd, id, body := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111", "fixed operating instructions"
			metaID, metaCwd := id, cwd
			if kind == "other_session" {
				metaID = "22222222-2222-7222-8222-222222222222"
			}
			if kind == "other_cwd" {
				metaCwd = "D:/other"
			}
			input := skillsBootstrapFixture(cwd, body, true)
			if kind == "task_input_only" {
				input = "exact task input"
			}
			if kind == "changed_bootstrap" {
				input = skillsBootstrapFixture(cwd, "private", true)
			}
			records := []object{{"type": "session_meta", "timestamp": "2026-10-24T00:00:00Z", "payload": object{"id": metaID, "cwd": metaCwd}}}
			if kind != "missing_bootstrap" {
				records = append(records, object{"type": "response_item", "timestamp": "2026-10-24T00:00:01Z", "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": input}}}})
			}
			var stream bytes.Buffer
			for _, r := range records {
				b, _ := json.Marshal(r)
				stream.Write(b)
				stream.WriteByte('\n')
			}
			sha, ok := skillsBootstrapProjection(&stream, id, cwd)
			if (ok && sha == hashBytes([]byte(body))) != (kind == "valid_before_job") {
				t.Fatalf("independent bootstrap %s not bound", kind)
			}
		})
	}
}

func TestSkills3080RunnerGuidanceMatchesLocalPublicSource(t *testing.T) {
	b, e := os.ReadFile("../cc-job-policy/src/lib.rs")
	if e != nil {
		t.Fatal(e)
	}
	for key, want := range map[string]string{"READ_GUIDANCE": skillsRunnerReadGuidance, "WRITE_GUIDANCE": skillsRunnerWriteGuidance, "REGISTRAR_GUIDANCE": skillsRunnerRegistrarGuidance} {
		r := regexp.MustCompile(`pub const ` + key + `: &str = ("(?:\\.|[^"\\])*");`)
		m := r.FindSubmatch(b)
		if len(m) != 2 {
			t.Fatal("runner guidance source changed")
		}
		v, e := strconv.Unquote(string(m[1]))
		if e != nil || v != want {
			t.Fatal("normal runner protocol drifted from fixed verifier")
		}
	}
}
