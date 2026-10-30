package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const skillsActual64138Workspace = `D:\CC\Work\unattended-operations-20261013-wt\todo-3080-16-skills-3048`
const skillsActual64138Request = skillsActual64138Workspace + `\.ci-work\todo3080/next-four-39ea-current-forward-request175645.json`

func skillsActual64138(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-native-path-actual64138-175645.txt"))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	if len(b) != 450 || hex.EncodeToString(h[:]) != "d8439880cc85a444f34f695a968de73761310b014782a2c819d1e72e6c436ce0" {
		t.Fatal("original native call bytes changed")
	}
	return string(b)
}

func TestSkills3080CreationActual64138(t *testing.T) {
	if !skillsCreationCall(skillsForwardInvocation(skillsActual64138Request, skillsActual64138Workspace), skillsActual64138Request, skillsActual64138Workspace) {
		t.Fatal("normal renderer control rejected")
	}
	if !skillsCreationCall(skillsActual64138(t), skillsActual64138Request, skillsActual64138Workspace) {
		t.Fatal("original successfully issued 450-byte native call rejected")
	}
}

func TestSkills3080CreationActual64138RejectsOtherEffects(t *testing.T) {
	actual := skillsActual64138(t)
	for _, tc := range []struct{ name, old, replacement string }{
		{"exe", "scrub391.exe", "other.exe"},
		{"flag", "--create-skills3080-forward", "--prepare-skills3080-revision"},
		{"request", "next-four-39ea-current", "next-four-other"},
		{"workspace", `"workdir":"D:`, `"workdir":"C:`},
		{"login", `"login":false`, `"login":true`},
		{"shell", `"shell":"powershell"`, `"shell":"pwsh"`},
		{"yield", `"yield_time_ms":30000`, `"yield_time_ms":30001`},
		{"tokens", `"max_output_tokens":30000`, `"max_output_tokens":30001`},
		{"duplicate_key", `"login":false`, `"login":false,"login":false`},
		{"unknown_field", `"login":false`, `"env":{},"login":false`},
		{"redirect", `.json'"`, `.json' > out.txt"`},
		{"extra_command", `.json'"`, `.json'; Get-Process"`},
		{"extra_argument", `.json'"`, `.json' --other"`},
		{"unquoted_path", `forward '`, `forward `},
		{"script", `& 'D:/CC/Tools/client-records/scrub391.exe'`, `& 'D:/CC/Tools/client-records/script.ps1'`},
		{"extra_JS", `text(await`, `text(1);text(await`},
		{"other_result", `text(await`, `text(JSON.stringify(await`},
		{"extra_await", `));`, `)); text(await tools.exec_command({}));`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(actual, tc.old, tc.replacement, 1)
			if input == actual {
				t.Fatal("test mutation did not apply")
			}
			if skillsCreationCall(input, skillsActual64138Request, skillsActual64138Workspace) {
				t.Fatal("changed action, option or result accepted")
			}
		})
	}
}

func TestSkills3080CreationPathOnlyRepeatsBackslashes(t *testing.T) {
	for _, tc := range []struct {
		actual, expected string
		want             bool
	}{
		{`D:\\CC\\Work\request.json`, `D:\CC\Work\request.json`, true},
		{`D:\\\CC/Work\\request.json`, `D:\CC/Work\request.json`, true},
		{`D:\CC\Work\.\request.json`, `D:\CC\Work\request.json`, false},
		{`D:\CC\Work\child\..\request.json`, `D:\CC\Work\request.json`, false},
		{`D:/CC/Work/request.json`, `D:\CC\Work\request.json`, false},
		{`D:\cc\Work\request.json`, `D:\CC\Work\request.json`, false},
		{`D:\CC\Work\request.json.`, `D:\CC\Work\request.json`, false},
		{`D:\CC\Work\request.json `, `D:\CC\Work\request.json`, false},
		{`D:\CC\Work\request.json:stream`, `D:\CC\Work\request.json`, false},
		{`D:\CC\Work\request.json\`, `D:\CC\Work\request.json`, false},
	} {
		if got := skillsCreationPathEqual(tc.actual, tc.expected); got != tc.want {
			t.Errorf("path %q=%v, want %v", tc.actual, got, tc.want)
		}
	}
	for _, path := range []string{`request.json`, `D:request.json`, `\CC\Work\request.json`, `\\server\share\request.json`, `\\?\D:\CC\request.json`, `D://CC/request.json`, `D:\/CC\request.json`, `D:/\CC/request.json`, `D:\CC\.\request.json`, `D:\CC\..\request.json`, `D:\CC\CON.txt`, `D:\CC\NUL`, `D:\CC\COM1.log`, `D:\CC\LPT9.log`, `D:\CC\file:stream`, "D:\\CC\\file\n.json"} {
		if skillsCreationPathEqual(path, path) {
			t.Errorf("unknown path form accepted: %q", path)
		}
	}
}

func TestSkills3080CreationCanonicalCallStillRequiresPathGrammar(t *testing.T) {
	for _, tc := range []struct{ path, workspace string }{
		{`D:/work/.ci-work/./request.json`, `D:/work`},
		{`D:/work/.ci-work/child/../request.json`, `D:/work`},
		{`D:/work/.ci-work/request.json.`, `D:/work`},
		{`D:/work/.ci-work/request.json `, `D:/work`},
		{`D:/work/.ci-work/request.json:stream`, `D:/work`},
		{`D:/work/.ci-work/CON.txt`, `D:/work`},
		{`D:/work/.ci-work/request.json`, `D:/work/.`},
		{`D:/work/.ci-work/request.json`, `D:/work/`},
		{`D:/work/area/.ci-work/request.json`, `D:/work/./area`},
		{`\\server\share\work\request.json`, `\\server\share\work`},
		{`D:work\request.json`, `D:work`},
	} {
		// Construct the exact expected command rather than calling the renderer;
		// a renderer that now refuses the grammar must not make this vacuous.
		b, err := json.Marshal(skillsForwardCommand(tc.path, tc.workspace))
		if err != nil {
			t.Fatal(err)
		}
		input := "text(await tools.exec_command(" + string(b) + "));"
		if skillsCreationCall(input, tc.path, tc.workspace) {
			t.Errorf("exact command bypassed path grammar: %q / %q", tc.path, tc.workspace)
		}
		if skillsForwardInvocation(tc.path, tc.workspace) != "" {
			t.Errorf("renderer advertised an unconsumable path: %q / %q", tc.path, tc.workspace)
		}
	}
}

func TestSkills3080CreationRejectsUnicodeReservedDevicesAtBothEntrypoints(t *testing.T) {
	for _, name := range []string{"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³", "com¹.txt", "lpt².tar.gz", "CONIN$", "CONOUT$", "CON .txt", "com¹ .log"} {
		path := `D:/work/` + name + `/request.json`
		workspace := `D:/work`
		b, _ := json.Marshal(skillsForwardCommand(path, workspace))
		if skillsForwardInvocation(path, workspace) != "" || skillsCreationCall("text(await tools.exec_command("+string(b)+"));", path, workspace) {
			t.Errorf("Unicode reserved device accepted: %s", name)
		}
	}
	for _, name := range []string{"COM⁴", "COM10", "日本語"} {
		path := `D:/work/` + name + `/request.json`
		inv := skillsForwardInvocation(path, `D:/work`)
		if inv == "" || !skillsCreationCall(inv, path, `D:/work`) {
			t.Errorf("ordinary Unicode name rejected: %s", name)
		}
	}
}

func TestSkills3080CreationActual64138NativeBinding(t *testing.T) {
	_, _, e, _ := skillsForwardFixture(t)
	e.NativeContract = "" // The original producer predates the hidden contract.
	e.Writer.Workspace = skillsActual64138Workspace
	c := skillsConsumeRequest{Creator: e.Writer, RequestPath: skillsActual64138Request, RequestSHA: e.RequestSHA, CallID: "call_64138b14df4e483885c35a84ad2a2741"}
	start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
	for _, kind := range []string{"original", "request_hash", "writer", "thread", "cwd", "time", "duplicate_call"} {
		t.Run(kind, func(t *testing.T) {
			records := skillsNativeCreationFixture(t, e, c)
			obj(records[1]["payload"])["input"] = skillsActual64138(t)
			check := c
			switch kind {
			case "request_hash":
				check.RequestSHA = strings.Repeat("a", 64)
			case "writer":
				check.Creator.Job += "x"
			case "thread":
				obj(records[0]["payload"])["id"] = "other"
			case "cwd":
				obj(records[0]["payload"])["cwd"] = "D:/other"
			case "time":
				records[1]["timestamp"] = start.Add(-time.Second).Format(time.RFC3339)
			case "duplicate_call":
				records = append(records, records[1])
			}
			var stream bytes.Buffer
			for _, r := range records {
				if err := json.NewEncoder(&stream).Encode(r); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", skillsActual64138Workspace, start, start.Add(time.Minute), check)
			if ok != (kind == "original") || (ok && skillsProjectionSHA(got) != skillsProjectionSHA(e)) {
				t.Fatalf("native binding %s=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080CreationRejectsDuplicateSessionMetadata(t *testing.T) {
	_, _, e, _ := skillsForwardFixture(t)
	c := skillsConsumeRequest{Creator: e.Writer, RequestPath: e.Writer.Workspace + "/.ci-work/forward-request.json", RequestSHA: e.RequestSHA, CallID: "call_meta_duplicates"}
	start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
	for _, name := range []string{"type", "id", "cwd", "payload", "timestamp", "nested"} {
		t.Run(name, func(t *testing.T) {
			records := skillsNativeCreationFixture(t, e, c)
			meta, err := json.Marshal(records[0])
			if err != nil {
				t.Fatal(err)
			}
			old, replacement := "", ""
			switch name {
			case "type":
				old, replacement = `"type":"session_meta"`, `"type":"ignored","type":"session_meta"`
			case "id":
				old, replacement = `"id":"synthetic-thread"`, `"id":"other","id":"synthetic-thread"`
			case "cwd":
				value, _ := json.Marshal(e.Writer.Workspace)
				old = `"cwd":` + string(value)
				replacement = `"cwd":"D:/other",` + old
			case "payload":
				old, replacement = `"payload":`, `"payload":{},"payload":`
			case "timestamp":
				old, replacement = `"timestamp":`, `"timestamp":"ignored","timestamp":`
			case "nested":
				old, replacement = `"payload":`, `"extra":{"x":1,"x":2},"payload":`
			}
			bad := bytes.Replace(meta, []byte(old), []byte(replacement), 1)
			if bytes.Equal(meta, bad) {
				t.Fatal("duplicate mutation did not apply")
			}
			var stream bytes.Buffer
			stream.Write(bad)
			stream.WriteByte('\n')
			for _, record := range records[1:] {
				if json.NewEncoder(&stream).Encode(record) != nil {
					t.Fatal("record encoding")
				}
			}
			if _, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", e.Writer.Workspace, start, start.Add(time.Minute), c); ok {
				t.Fatal("duplicate session metadata accepted")
			}
		})
	}
}

func TestSkills3080CreationRejectsCaseAliasedNativeHeaders(t *testing.T) {
	_, _, e, _ := skillsForwardFixture(t)
	c := skillsConsumeRequest{Creator: e.Writer, RequestPath: e.Writer.Workspace + "/.ci-work/forward-request.json", RequestSHA: e.RequestSHA, CallID: "call_meta_case_alias"}
	start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
	for _, kind := range []string{"id", "cwd", "id_only", "cwd_only", "type", "timestamp", "payload", "call_id", "block_type", "block_text_first", "block_text_result"} {
		t.Run(kind, func(t *testing.T) {
			records := skillsNativeCreationFixture(t, e, c)
			index := 0
			if kind == "call_id" {
				index = 1
			}
			if strings.HasPrefix(kind, "block_") {
				index = 2
			}
			raw, err := json.Marshal(records[index])
			if err != nil {
				t.Fatal(err)
			}
			old, replacement := "", ""
			switch kind {
			case "id":
				old, replacement = `"id":"synthetic-thread"`, `"id":"other","ID":"synthetic-thread"`
			case "id_only":
				old, replacement = `"id":"synthetic-thread"`, `"ID":"synthetic-thread"`
			case "cwd":
				v, _ := json.Marshal(e.Writer.Workspace)
				old = `"cwd":` + string(v)
				replacement = `"cwd":"D:/other","CWD":` + string(v)
			case "cwd_only":
				old, replacement = `"cwd":`, `"CWD":`
			case "type":
				old, replacement = `"type":"session_meta"`, `"type":"ignored","Type":"session_meta"`
			case "timestamp":
				old, replacement = `"timestamp":`, `"Timestamp":"ignored","timestamp":`
			case "payload":
				old, replacement = `"payload":`, `"Payload":{},"payload":`
			case "call_id":
				old, replacement = `"call_id":"call_meta_case_alias"`, `"call_id":"other","CALL_ID":"call_meta_case_alias"`
			case "block_type":
				old, replacement = `"type":"input_text"`, `"type":"ignored","Type":"input_text"`
			case "block_text_first", "block_text_result":
				block := 0
				if kind == "block_text_result" {
					block = 1
				}
				value, _ := json.Marshal(obj(records[2]["payload"])["output"].([]object)[block]["text"])
				old = `"text":` + string(value)
				replacement = `"text":"ignored","Text":` + string(value)
			}
			bad := bytes.Replace(raw, []byte(old), []byte(replacement), 1)
			if bytes.Equal(raw, bad) {
				t.Fatal("case alias mutation missing")
			}
			var stream bytes.Buffer
			for i, r := range records {
				if i == index {
					stream.Write(bad)
					stream.WriteByte('\n')
				} else if json.NewEncoder(&stream).Encode(r) != nil {
					t.Fatal("fixture encoding")
				}
			}
			if _, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", e.Writer.Workspace, start, start.Add(time.Minute), c); ok {
				t.Fatal("case-aliased native header accepted")
			}
		})
	}
}
