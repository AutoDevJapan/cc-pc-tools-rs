package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedSavedReadRejectsMissingDirectoryAndOversize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture")
	if e := os.WriteFile(path, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if b, e := readBounded(path, 7); e != nil || string(b) != "fixture" {
		t.Fatal("ordinary bounded source lost")
	}
	for _, p := range []string{root, filepath.Join(root, "missing")} {
		if _, e := readBounded(p, 100); e == nil {
			t.Fatal("invalid source accepted")
		}
	}
	if _, e := readBounded(path, 3); e == nil {
		t.Fatal("oversize source accepted")
	}
}

func TestDecodeCanonicalSavedChildWithoutExecution(t *testing.T) {
	command := "@'\nSYNTHETIC_PRIVATE_A\nSYNTHETIC_PRIVATE_B\n'@ | Set-Content -LiteralPath 'D:/CC/Tools/client-projects.txt'"
	quoted, _ := json.Marshal(command)
	for _, body := range []string{
		`text(await tools.exec_command({"cmd":` + string(quoted) + `,"max_output_tokens":1000}));`,
		`text(await tools.exec_command({cmd:` + string(quoted) + `,max_output_tokens:1000}));`,
		`notify(await tools.exec_command({cmd:` + string(quoted) + `,prefix_rule:["fixture"]}));`,
		"text(await tools.exec_command({cmd:`" + strings.ReplaceAll(command, "`", "\\`") + "`}));",
	} {
		got, ok := savedChildCommand(Event{Name: "functions.exec", Input: body})
		if !ok || got != command {
			t.Fatal("static escaped command not preserved")
		}
		values := literals(got)
		if len(values) != 1 || string(values[0]) != "SYNTHETIC_PRIVATE_A\nSYNTHETIC_PRIVATE_B" {
			t.Fatal("decoded literal evidence lost")
		}
	}
	args, _ := json.Marshal(map[string]string{"cmd": command})
	got, ok := savedChildCommand(Event{Name: "functions.exec_command", Arguments: string(args)})
	if !ok || got != command {
		t.Fatal("direct canonical tool arguments lost")
	}
	got, ok = savedChildCommand(Event{Name: "exec", Input: `text(await tools.exec_command({cmd:"fixture )) tail"}));`})
	if !ok || got != "fixture )) tail" {
		t.Fatal("parentheses inside command changed wrapper boundary")
	}
}

func TestRejectDynamicOrAmbiguousSavedChild(t *testing.T) {
	for _, body := range []string{
		`if(false){text(await tools.exec_command({cmd:"fixture"}));}`,
		`text(await tools.exec_command({cmd:"fixture"}));text({exit_code:0});`,
		`"text(await tools.exec_command({cmd:\"fixture\"}));"`,
		`text(await tools.exec_command({cmd:variable}));`,
		`text(await tools.exec_command({cmd:"fixture"+variable}));`,
		`text(await tools.exec_command({cmd:"fixture",cmd:"other"}));`,
		`text(await tools.exec_command({cmd:"fixture",...other}));`,
		"text(await tools.exec_command({cmd:`fixture ${value}`}));",
		`text(await tools.exec_command({cmd:"fixture",shell:chooser()}));`,
		`text(await tools.exec_command({/*cmd*/cmd:"fixture"}));`,
		`text(await tools.exec_command({cmd:"fixture"})); text(await tools.exec_command({cmd:"second"}));`,
	} {
		if _, ok := savedChildCommand(Event{Name: "functions.exec", Input: body}); ok {
			t.Fatal("dynamic, quoted or multiple child promoted")
		}
	}
	if _, ok := savedChildCommand(Event{Name: "unknown_tool", Input: `text(await tools.exec_command({cmd:"fixture"}));`}); ok {
		t.Fatal("unknown tool inferred")
	}
}

func TestStaticStringDialectsAndInterpolation(t *testing.T) {
	got, ok := savedChildCommand(Event{Name: "exec", Input: `text(await tools.exec_command({cmd:"fixture\n\u65e5\u672c"}));`})
	if !ok || got != "fixture\n日本" {
		t.Fatal("JSON Unicode escapes lost")
	}
	for _, body := range []string{`text(await tools.exec_command({cmd:'fixture\nsecond'}));`, "text(await tools.exec_command({cmd:`fixture\nsecond`}));"} {
		got, ok := savedChildCommand(Event{Name: "exec", Input: body})
		if !ok || got != "fixture\nsecond" {
			t.Fatal("static literal decoding changed bytes")
		}
	}
	for _, body := range []string{"`a ${value}`", `'a\x41'`, `'a\u0041'`, `'unterminated`, `'a\z'`} {
		if _, _, ok := staticString(body, 0); ok {
			t.Fatal("unresolved string dialect promoted")
		}
	}
}

func TestStaticObjectDoesNotRewriteQuotedKeysOrAcceptDuplicateAliases(t *testing.T) {
	args, ok := staticObject(`{cmd:"text with cmd: and a quote \"", shell:"powershell"}`)
	var cmd string
	if !ok || json.Unmarshal(args["cmd"], &cmd) != nil || !strings.Contains(cmd, "cmd:") {
		t.Fatal("string content rewritten")
	}
	for _, body := range []string{`{cmd:"a","cmd":"b"}`, `{"\u0063md":"a",cmd:"b"}`, `{cmd:"a"} {}`, `{cmd:"unterminated}`, `{cmd:true}`, `{cmd:null}`} {
		event := Event{Name: "exec_command", Arguments: body}
		if _, ok := savedChildCommand(event); ok {
			t.Fatal("duplicate, malformed or non-string cmd accepted")
		}
	}
}
