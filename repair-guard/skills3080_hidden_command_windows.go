package main

import (
	"fmt"
	"regexp"
	"strings"
)

const skillsHiddenPowerShell = "C:/Program Files/PowerShell/7/pwsh.exe"
const skillsHiddenNativeContract = "hidden-process-v1"

// This renderer has two path holes and no caller-selected program, flag,
// environment, script file or output transformation. The internal formatter
// also lets the test binary exercise the real Windows process/stdio contract.
func skillsHiddenProcessScript(exe, flag, path, workspace string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ccStart = [System.Diagnostics.ProcessStartInfo]::new()
$ccStart.FileName = '%s'
$ccStart.WorkingDirectory = '%s'
$ccStart.UseShellExecute = $false
$ccStart.CreateNoWindow = $true
$ccStart.RedirectStandardOutput = $true
$ccStart.RedirectStandardError = $true
$ccStart.StandardOutputEncoding = [System.Text.UTF8Encoding]::new($false, $true)
$ccStart.StandardErrorEncoding = [System.Text.UTF8Encoding]::new($false, $true)
$ccStart.ArgumentList.Add('%s')
$ccStart.ArgumentList.Add('%s')
$ccProcess = [System.Diagnostics.Process]::new()
$ccProcess.StartInfo = $ccStart
if (!$ccProcess.Start()) { throw 'ProcessStartFailed' }
$ccOutput = $ccProcess.StandardOutput.ReadToEndAsync()
$ccError = $ccProcess.StandardError.ReadToEndAsync()
$ccProcess.WaitForExit()
$ccStdout = $ccOutput.GetAwaiter().GetResult()
$ccStderr = $ccError.GetAwaiter().GetResult()
$ccExit = $ccProcess.ExitCode
$ccProcess.Dispose()
[Console]::Out.Write($ccStdout)
[Console]::Error.Write($ccStderr)
exit $ccExit`, exe, workspace, flag, path)
}

var skillsHiddenForwardPattern = func() *regexp.Regexp {
	p := regexp.QuoteMeta(skillsHiddenProcessScript(skillsRuntimeTarget, "--create-skills3080-forward", "{REQUEST}", "{WORKSPACE}"))
	p = strings.Replace(p, regexp.QuoteMeta("{WORKSPACE}"), "([^']+)", 1)
	p = strings.Replace(p, regexp.QuoteMeta("{REQUEST}"), "([^']+)", 1)
	return regexp.MustCompile(`\A` + p + `\z`)
}()

func skillsHiddenForwardMatches(script, path, workspace string) bool {
	m := skillsHiddenForwardPattern.FindStringSubmatch(script)
	return len(m) == 3 && skillsCreationPathEqual(m[1], workspace) && skillsCreationPathEqual(m[2], path)
}
