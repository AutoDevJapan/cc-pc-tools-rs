package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const skillsRunnerOwnerHeader = "\n\n## 最新の保存済みオーナー指示\n以下はPCの保存済み運用指示です。元の作業対象・閲覧専用指定・承認審査の範囲を広げません。GitHub所有者は実際の所有者を確認し、保護対象はscrub391 --guard-pathで除外してください。稼働中の担当は途中成果を保存し、次の区切りで反映してください。\n"
const skillsRunnerSiteHeader = "\n\n## 実行時のサイト制限\n"

// Validate every record before filtering type/role, including outside-window
// records, without retaining their message or tool-result bodies in a tree.
func skillsNativeUniqueJSON(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		t, err := d.Token()
		if err != nil {
			return false
		}
		delim, composite := t.(json.Delim)
		if !composite {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func skillsStoredPromptScope(prompt, spec []byte, sandbox string, subwork ...skillsSubwork) bool {
	const marker = "\n依頼:\n"
	at := bytes.Index(prompt, []byte(marker))
	if at < 0 || !bytes.HasPrefix(prompt[at+len(marker):], spec) {
		return false
	}
	tail := prompt[at+len(marker)+len(spec):]
	guide := skillsRunnerWriteGuidance
	if sandbox == "read-only" {
		guide = skillsRunnerReadGuidance
	}
	want := ""
	if len(subwork) == 1 && subwork[0].Parent > 0 {
		relation := subwork[0]
		if relation.Kind != "review" && relation.Kind != "check" && relation.Kind != "research" {
			return false
		}
		want = fmt.Sprintf("\nこの作業はToDo %dの独立した%sです。保存済み仕様の有限な範囲だけを扱い、主担当の完了を前提にせず結果を返してください。主担当のソースを編集したり、独立したPRの作成やマージを担当したりしないでください。\n", relation.Parent, relation.Kind)
	}
	if !bytes.Contains(append(append([]byte{}, prompt[:at+len(marker)+len(spec)]...), []byte(want)...), []byte(strings.TrimSpace(guide))) {
		want += guide
	}
	return bytes.Equal(tail, []byte(want))
}

// The normal launcher saves execution-prompt.md separately. Its only added
// text is the independently read current owner policy and fixed site framing.
// Neither that file nor its native copy may add arbitrary text to the spec.
func skillsTypedLaunchInputAt(s *skillsFS, root string, job object, spec []byte) ([]byte, bool) {
	base, ok := skillsReviewLaunchPromptAt(s, root, job, spec)
	if !ok {
		return nil, false
	}
	var relation skillsSubwork
	if b, e := json.Marshal(job["subwork"]); e != nil || json.Unmarshal(b, &relation) != nil {
		return nil, false
	}
	if !skillsStoredPromptScope(base, spec, str(job["sandbox"]), relation) || job["fixed_read_only_review"] != nil || job["finite_research_approval"] != nil {
		return nil, false
	}
	registeredPath := filepath.Join("D:/CC/Tools/scheduler/prompts", fmt.Sprintf("%d-%s.md", conversationNumber(job["todo_id"]), str(job["launch_key"])))
	if !same(registeredPath, str(job["prompt_source"])) {
		return nil, false
	}
	registered, ok := s.read(registeredPath, 4<<20)
	if !ok {
		return nil, false
	}
	guide := skillsRunnerWriteGuidance
	if job["sandbox"] == "read-only" {
		guide = skillsRunnerReadGuidance
	}
	wantBase := append([]byte{}, registered...)
	if !bytes.Contains(registered, []byte(strings.TrimSpace(guide))) {
		wantBase = append(wantBase, []byte(guide)...)
	}
	if !bytes.Equal(base, wantBase) {
		return nil, false
	}
	policy, ok := s.read("D:/CC/AGENTS.md", 1<<20)
	if !ok || len(bytes.TrimSpace(policy)) == 0 {
		return nil, false
	}
	execution, ok := s.read(filepath.Join(root, str(job["id"]), "execution-prompt.md"), 4<<20)
	if !ok {
		return nil, false
	}
	want := append(append([]byte{}, base...), []byte(skillsRunnerOwnerHeader)...)
	want = append(want, policy...)
	want = append(want, []byte(skillsRunnerSiteHeader+skillsRunnerRegistrarGuidance+"\n")...)
	return execution, bytes.Equal(execution, want)
}

// Bootstrap is operating-instruction metadata supplied by the normal runtime,
// not the task's user input. Accept only the exact instructions digest from the
// real registered origin's same-repository session, with a strict cwd envelope.
// This does not qualify or read the global AGENTS.md file through another path.
func skillsBootstrapIdentity(text, cwd string) (string, bool) {
	body, ok := skillsBootstrapBody(text, cwd)
	return hashBytes([]byte(body)), ok
}

func skillsBootstrapBody(text, cwd string) (string, bool) {
	return skillsBootstrapBodyWithSandbox(text, cwd, "")
}

func skillsBootstrapBodyWithSandbox(text, cwd, sandbox string) (string, bool) {
	const header = "# AGENTS.md instructions for "
	line, rest, ok := strings.Cut(text, "\n")
	if !ok || !strings.HasPrefix(line, header) || !same(strings.TrimSuffix(strings.TrimPrefix(line, header), "\r"), cwd) {
		return "", false
	}
	if !strings.HasPrefix(rest, "\n<INSTRUCTIONS>\n") {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "\n<INSTRUCTIONS>\n")
	at := strings.LastIndex(rest, "\n</INSTRUCTIONS>")
	if at < 0 {
		return "", false
	}
	body, tail := rest[:at], strings.TrimSpace(rest[at+len("\n</INSTRUCTIONS>"):])
	if body == "" || len(body) > 256<<10 || !skillsBootstrapEnvironmentWithSandbox(tail, cwd, sandbox) {
		return "", false
	}
	return body, true
}

func skillsBootstrapEnvironment(raw, cwd string) bool {
	return skillsBootstrapEnvironmentWithSandbox(raw, cwd, "")
}

func skillsBootstrapEnvironmentWithSandbox(raw, cwd, sandbox string) bool {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	stack := []string{}
	counts := map[string]int{}
	values := map[string]int{}
	roots := 0
	parents := map[string]string{"environment_context": "", "cwd": "environment_context", "shell": "environment_context", "current_date": "environment_context", "timezone": "environment_context", "filesystem": "environment_context", "workspace_roots": "filesystem", "root": "workspace_roots", "permission_profile": "filesystem", "file_system": "permission_profile", "entry": "file_system", "special": "entry"}
	profile, mode := "", ""
	seenRoots := map[string]bool{}
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return false
		}
		switch v := token.(type) {
		case xml.StartElement:
			parent, allowed := parents[v.Name.Local]
			actualParent := ""
			if len(stack) > 0 {
				actualParent = stack[len(stack)-1]
			}
			if !allowed || parent != actualParent || v.Name.Space != "" {
				return false
			}
			if len(stack) == 0 && v.Name.Local != "environment_context" {
				return false
			}
			counts[v.Name.Local]++
			if v.Name.Local != "root" && counts[v.Name.Local] > 1 {
				return false
			}
			for _, a := range v.Attr {
				valid := a.Name.Space == "" && ((v.Name.Local == "permission_profile" && a.Name.Local == "type" && (a.Value == "disabled" || a.Value == "managed")) || (v.Name.Local == "file_system" && a.Name.Local == "type" && (a.Value == "unrestricted" || a.Value == "restricted")) || (v.Name.Local == "entry" && a.Name.Local == "access" && a.Value == "read"))
				if !valid {
					return false
				}
				if v.Name.Local == "permission_profile" {
					profile = a.Value
				}
				if v.Name.Local == "file_system" {
					mode = a.Value
				}
			}
			if (v.Name.Local == "permission_profile" || v.Name.Local == "file_system" || v.Name.Local == "entry") && len(v.Attr) != 1 {
				return false
			}
			stack = append(stack, v.Name.Local)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1] != v.Name.Local {
				return false
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			value := strings.TrimSpace(string(v))
			if value == "" {
				continue
			}
			if len(stack) == 0 {
				return false
			}
			name := stack[len(stack)-1]
			values[name]++
			if name != "root" && values[name] != 1 {
				return false
			}
			switch stack[len(stack)-1] {
			case "cwd":
				if !same(value, cwd) {
					return false
				}
			case "shell":
				if value != "powershell" {
					return false
				}
			case "current_date":
				if _, e := time.Parse("2006-01-02", value); e != nil {
					return false
				}
			case "timezone":
				if value != "Asia/Tokyo" {
					return false
				}
			case "root":
				if !same(value, cwd) && !same(value, "D:/CC") {
					return false
				}
				key := strings.ToLower(filepath.Clean(value))
				if seenRoots[key] {
					return false
				}
				seenRoots[key] = true
				roots++
			case "special":
				if value != ":root" {
					return false
				}
			default:
				return false
			}
		default:
			return false
		}
	}
	fsMode := (profile == "disabled" && mode == "unrestricted" && counts["entry"] == 0 && counts["special"] == 0) || (profile == "managed" && mode == "restricted" && counts["entry"] == 1 && counts["special"] == 1 && values["special"] == 1)
	if sandbox != "" && ((sandbox == "read-only") != (profile == "managed")) {
		return false
	}
	return len(stack) == 0 && counts["environment_context"] == 1 && counts["cwd"] == 1 && counts["shell"] == 1 && counts["current_date"] == 1 && counts["timezone"] == 1 && values["cwd"] == 1 && values["shell"] == 1 && values["current_date"] == 1 && values["timezone"] == 1 && roots == 2 && fsMode
}

func skillsBootstrapProjection(input io.Reader, id, cwd string) (string, bool) {
	body, ok := skillsBootstrapBodyProjection(input, id, cwd, "")
	return hashBytes([]byte(body)), ok
}

func skillsBootstrapBodyProjection(input io.Reader, id, cwd, sandbox string) (string, bool) {
	scan := bufio.NewScanner(io.LimitReader(input, 4<<20))
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	session := false
	for scan.Scan() {
		// Validate duplicates before type/role filtering can hide an input.
		if !skillsNativeUniqueJSON(scan.Bytes()) {
			return "", false
		}
		var e struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if !skillsCreationHeaderKeys(scan.Bytes(), "type", "timestamp", "payload") || json.Unmarshal(scan.Bytes(), &e) != nil {
			return "", false
		}
		if e.Type != "session_meta" && e.Type != "response_item" {
			continue
		}
		var p struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			ID      string          `json:"id"`
			Cwd     string          `json:"cwd"`
			Content json.RawMessage `json:"content"`
		}
		if !skillsCreationHeaderKeys(e.Payload, "type", "role", "id", "cwd", "content") || json.Unmarshal(e.Payload, &p) != nil {
			return "", false
		}
		if e.Type == "session_meta" {
			if _, strict := conversationJSON(scan.Bytes()); !strict || session || p.ID != id || !same(p.Cwd, cwd) {
				return "", false
			}
			session = true
			continue
		}
		if e.Type != "response_item" || p.Type != "message" || p.Role != "user" {
			continue
		}
		if !session {
			return "", false
		}
		var blocks []json.RawMessage
		if _, strict := conversationJSON(scan.Bytes()); !strict || json.Unmarshal(p.Content, &blocks) != nil || len(blocks) < 1 || len(blocks) > 2 {
			return "", false
		}
		var text strings.Builder
		for _, raw := range blocks {
			var block struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if !skillsCreationHeaderKeys(raw, "type", "text") || json.Unmarshal(raw, &block) != nil || block.Type != "input_text" {
				return "", false
			}
			text.WriteString(block.Text)
		}
		return skillsBootstrapBodyWithSandbox(text.String(), cwd, sandbox)
	}
	return "", false
}

func skillsBootstrapForJob(s *skillsFS, job object) (string, bool) {
	input, ok := skillsNativeSnapshot(s, job)
	if !ok {
		return "", false
	}
	return skillsBootstrapProjection(input, str(job["thread_id"]), str(job["cwd"]))
}
