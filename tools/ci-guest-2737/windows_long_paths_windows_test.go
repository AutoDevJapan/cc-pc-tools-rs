package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsOfflineLongPathsConfiguration(t *testing.T) {
	// Execute the actual generated registry-edit block, redirecting its two
	// paths to private HKCU fixtures. Never mount a disk or change host policy.
	stage := windowsOfflineStageScript(`C:\public\unused`)
	start := strings.Index(stage, "\n Set-ItemProperty -LiteralPath $service")
	end := strings.Index(stage, "\n $value=$null")
	if start < 0 || end <= start {
		t.Fatal("offline registry edit block absent")
	}
	fragment := stage[start:end]
	for _, mode := range []string{"absent", "disabled", "enabled", "wrong-type", "missing-parent", "write-error", "dropped-write"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			key := `HKCU:\Software\CC2737PublicTests\` + filepath.Base(root) + "-" + mode
			setup := ""
			switch mode {
			case "disabled", "write-error", "dropped-write":
				setup = "New-ItemProperty -LiteralPath $fs -Name LongPathsEnabled -PropertyType DWord -Value 0 | Out-Null\n"
			case "enabled":
				setup = "New-ItemProperty -LiteralPath $fs -Name LongPathsEnabled -PropertyType DWord -Value 1 | Out-Null\n"
			case "wrong-type":
				setup = "New-ItemProperty -LiteralPath $fs -Name LongPathsEnabled -PropertyType String -Value 'public-fixture' | Out-Null\n"
			}
			shim := ""
			if mode == "write-error" || mode == "dropped-write" {
				body := "throw 'PUBLIC_WRITE_ERROR'"
				if mode == "dropped-write" {
					body = "return"
				}
				shim = "function New-ItemProperty { [CmdletBinding()]param($LiteralPath,$Name,$PropertyType,$Value,[switch]$Force) " + body + " }\n"
			}
			want := "$true"
			if mode == "missing-parent" || mode == "write-error" || mode == "dropped-write" {
				want = "$false"
			}
			script := "$ErrorActionPreference='Stop'\n$controlSet=" + psQuote(key) + "\n" + `
if(Test-Path -LiteralPath $controlSet){throw 'PUBLIC_FIXTURE_COLLISION'}
try {
 $service=$controlSet+'\Services\PublicFixture'
 $fs=$controlSet+'\Control\FileSystem'
 New-Item -Path $service -Force | Out-Null
` + " if('" + mode + "' -ne 'missing-parent'){New-Item -Path $fs -Force | Out-Null}\n" + setup + shim + `
 $applied=$false
 try {
` + fragment + `
  $applied=$true
 } catch {}
 if($applied -ne ` + want + `){throw 'PUBLIC_CONFIGURATION_RESULT_MISMATCH'}
 if($applied){
  $observed=Get-Item -LiteralPath $fs -ErrorAction Stop
  try {
   if($observed.GetValueKind('LongPathsEnabled') -ne [Microsoft.Win32.RegistryValueKind]::DWord -or $observed.GetValue('LongPathsEnabled') -ne 1){throw 'PUBLIC_CONFIGURATION_VALUE_MISMATCH'}
  } finally {$observed.Close()}
 }
} finally {Remove-Item -LiteralPath $controlSet -Recurse -Force -ErrorAction Stop}
`
			// Store only public test code/output, not production hives or logs.
			if err := os.WriteFile(filepath.Join(root, "fixture.ps1"), []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			for _, engine := range []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `C:\Program Files\PowerShell\7\pwsh.exe`} {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				c := command(ctx, engine, "-NoProfile", "-NonInteractive", "-File", filepath.Join(root, "fixture.ps1"))
				out, err := c.CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("offline registry fixture failed (%s): %v\n%s", engine, err, out)
				}
			}
		})
	}
}
