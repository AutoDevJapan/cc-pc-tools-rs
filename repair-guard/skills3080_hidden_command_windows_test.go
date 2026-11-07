package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSkills3080NewCreationAdvertisesAnExplicitHiddenProcess(t *testing.T) {
	path, cwd := `D:\CC\Work\synthetic\.ci-work\request.json`, `D:\CC\Work\synthetic`
	cmd := skillsForwardCommand(path, cwd)
	if !strings.Contains(cmd.Cmd, "$ccStart.CreateNoWindow = $true") || !strings.Contains(cmd.Cmd, "$ccStart.UseShellExecute = $false") || cmd.Shell != skillsHiddenPowerShell {
		t.Fatal("new canonical creation lacks explicit hidden process contract")
	}
	if !skillsCreationCall(skillsForwardInvocation(path, cwd), path, cwd) {
		t.Fatal("hidden canonical native invocation")
	}
	for _, kind := range []string{"no_window_false", "shell_execute_true", "other_exe", "other_flag", "other_request", "other_inner_cwd", "other_tool_cwd", "extra_effect", "environment_override", "output_reformatted", "exit_lost", "encoding_changed", "other_shell", "yield_changed"} {
		t.Run(kind, func(t *testing.T) {
			bad := cmd
			switch kind {
			case "no_window_false":
				bad.Cmd = strings.Replace(bad.Cmd, "CreateNoWindow = $true", "CreateNoWindow = $false", 1)
			case "shell_execute_true":
				bad.Cmd = strings.Replace(bad.Cmd, "UseShellExecute = $false", "UseShellExecute = $true", 1)
			case "other_exe":
				bad.Cmd = strings.Replace(bad.Cmd, "scrub391.exe", "other.exe", 1)
			case "other_flag":
				bad.Cmd = strings.Replace(bad.Cmd, "--create-skills3080-forward", "--prepare-skills3080-revision", 1)
			case "other_request":
				bad.Cmd = strings.Replace(bad.Cmd, path, path+".copy", 1)
			case "other_inner_cwd":
				bad.Cmd = strings.Replace(bad.Cmd, "WorkingDirectory = '"+cwd, "WorkingDirectory = 'D:\\elsewhere", 1)
			case "other_tool_cwd":
				bad.Workdir = "D:/elsewhere"
			case "extra_effect":
				bad.Cmd += "\nGet-Process"
			case "environment_override":
				bad.Cmd = strings.Replace(bad.Cmd, "$ccProcess =", "$ccStart.Environment['OTHER'] = 'changed'\n$ccProcess =", 1)
			case "output_reformatted":
				bad.Cmd = strings.Replace(bad.Cmd, "[Console]::Out.Write($ccStdout)", "$ccStdout | ConvertFrom-Json | ConvertTo-Json", 1)
			case "exit_lost":
				bad.Cmd = strings.Replace(bad.Cmd, "exit $ccExit", "exit 0", 1)
			case "encoding_changed":
				bad.Cmd = strings.Replace(bad.Cmd, "UTF8Encoding", "ASCIIEncoding", 1)
			case "other_shell":
				bad.Shell = "powershell"
			case "yield_changed":
				bad.Yield++
			}
			if bad == cmd {
				t.Fatal("mutation did not apply")
			}
			b, _ := json.Marshal(bad)
			if skillsCreationCall("text(await tools.exec_command("+string(b)+"));", path, cwd) {
				t.Fatal("changed hidden operation accepted")
			}
		})
	}
	for _, v := range []struct{ p, w string }{{path, cwd}, {strings.ReplaceAll(path, `\`, `\\`), cwd}, {path, strings.ReplaceAll(cwd, `\`, `\\`)}, {strings.ReplaceAll(path, `\`, `\\`), strings.ReplaceAll(cwd, `\`, `\\`)}} {
		actual := skillsForwardCommand(v.p, v.w)
		b, _ := json.Marshal(actual)
		if !skillsCreationCall("text(await tools.exec_command("+string(b)+"));", path, cwd) {
			t.Fatal("only repeated path separators should normalize")
		}
	}
}

const skillsHiddenProbeArgument = "skills3080-hidden-probe-日本語 $cash `tick"

func TestSkills3080HiddenProcessProbeChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != skillsHiddenProbeArgument {
		t.Skip("only the controlled child invocation")
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(20)
	}
	window, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	json.NewEncoder(os.Stdout).Encode(object{"console_window": window != 0, "cwd": cwd, "utf8": "日本語"})
	fmt.Fprint(os.Stderr, "controlled stderr 日本語\n")
	os.Exit(7)
}

func TestSkills3080HiddenProcessPreservesActualConsoleStdioAndExit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(exe+cwd, "'\r\n") {
		t.Fatal("fixture path grammar")
	}
	script := skillsHiddenProcessScript(exe, "-test.run=^TestSkills3080HiddenProcessProbeChild$", skillsHiddenProbeArgument, cwd)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, skillsHiddenPowerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatal("original child exit code was not preserved")
	}
	want, _ := json.Marshal(object{"console_window": false, "cwd": cwd, "utf8": "日本語"})
	want = append(want, '\n')
	if !bytes.Equal(stdout.Bytes(), want) || stderr.String() != "controlled stderr 日本語\n" {
		t.Fatal("hidden CUI child console/stdout/stderr contract changed")
	}
}

func TestSkills3080NewIssuanceCannotUseTheHistoricalDirectContract(t *testing.T) {
	for _, kind := range []string{"new_hidden", "new_direct", "old_direct", "unknown_contract"} {
		t.Run(kind, func(t *testing.T) {
			_, _, e, _ := skillsForwardFixture(t)
			if e.NativeContract != skillsHiddenNativeContract {
				t.Fatal("new issuer did not version its invocation requirement")
			}
			if kind == "old_direct" {
				e.NativeContract = ""
			}
			if kind == "unknown_contract" {
				e.NativeContract = "unrecognized"
			}
			c := skillsConsumeRequest{Creator: e.Writer, RequestPath: e.Writer.Workspace + "/.ci-work/request.json", RequestSHA: e.RequestSHA, CallID: "call_hidden_contract"}
			records := skillsNativeCreationFixture(t, e, c)
			if kind == "new_direct" || kind == "old_direct" {
				b, _ := json.Marshal(skillsLegacyForwardCommand(c.RequestPath, c.Creator.Workspace))
				obj(records[1]["payload"])["input"] = "text(await tools.exec_command(" + string(b) + "));"
			}
			var stream bytes.Buffer
			for _, r := range records {
				json.NewEncoder(&stream).Encode(r)
			}
			start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
			_, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", c.Creator.Workspace, start, start.Add(time.Minute), c)
			if ok != (kind == "new_hidden" || kind == "old_direct") {
				t.Fatalf("issuance contract %s accepted=%v", kind, ok)
			}
			if kind == "old_direct" {
				b, _ := json.Marshal(e)
				if bytes.Contains(b, []byte("native_invocation_contract")) {
					t.Fatal("legacy serialization changed")
				}
			}
		})
	}
}
