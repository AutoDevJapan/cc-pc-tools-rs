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

func TestSkills3080CreationActual64138NativeBinding(t *testing.T) {
	_, _, e, _ := skillsForwardFixture(t)
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
