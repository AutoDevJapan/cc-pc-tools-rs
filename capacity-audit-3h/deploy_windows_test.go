package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWindowsPinnedSoftwareReplaceRetriesAndRestoresPartialFailure(t *testing.T) {
	root := t.TempDir()
	if !filepath.IsAbs(root) {
		t.Fatal("fixture must be an absolute owned directory")
	}
	script := filepath.Join(root, "acceptance.ps1")
	body := `param([string]$ProductionScript,[string]$OwnedRoot)
$ErrorActionPreference='Stop'
$OwnedRoot=[IO.Path]::GetFullPath($OwnedRoot)
$tokens=$null;$errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($ProductionScript,[ref]$tokens,[ref]$errors)
if($errors.Count){throw 'source syntax error'}
$wanted=@('Assert-Plain','Hash','Install-PinnedRuntime')
$functions=$ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $wanted -contains $node.Name},$true)
if($functions.Count-ne3){throw 'bounded function definitions missing'}
foreach($function in $functions){Invoke-Expression $function.Extent.Text}
function Check([bool]$ok,[string]$reason){if(-not$ok){throw $reason}}
function Fixture([string]$name){
 $dir=Join-Path $OwnedRoot $name
 New-Item -ItemType Directory -Path $dir|Out-Null
 $backup=Join-Path $dir 'backups';New-Item -ItemType Directory -Path $backup|Out-Null
 $src=Join-Path $dir 'source.exe';$dst=Join-Path $dir 'runtime.exe'
 [IO.File]::WriteAllText($src,'new software');[IO.File]::WriteAllText($dst,'old software')
 $acl=Get-Acl -LiteralPath $dst
 $acl.SetAccessRuleProtection($true,$true)
 Set-Acl -LiteralPath $dst -AclObject $acl
 return @{Source=$src;Target=$dst;BackupRoot=$backup;Old=(Hash $dst);New=(Hash $src)}
}
$normal=Fixture 'normal'
$normalAcl=(Get-Acl -LiteralPath $normal.Target).Sddl
$result=Install-PinnedRuntime @normal
Check ($result-eq'updated' -and (Hash $normal.Target)-eq$normal.New) 'normal replace failed'
Check ((Get-Acl -LiteralPath $normal.Target).Sddl-eq$normalAcl) 'normal replace changed the original ACL'
$first=Join-Path $normal.BackupRoot 'runtime.exe.before-3085'
Check ((Hash $first)-eq$normal.Old) 'old software not backed up'
$stage=$normal.Target+'.3085-stage';[IO.File]::Copy($normal.Source,$stage)
$result=Install-PinnedRuntime @normal
Check ($result-eq'already_current' -and -not(Test-Path -LiteralPath $stage)) 'completed retry did not converge'
[IO.File]::WriteAllText($normal.Target,'old software')
$result=Install-PinnedRuntime @normal
Check ($result-eq'updated' -and (Hash $first)-eq$normal.Old) 'existing backup was changed'
Check ((Get-ChildItem -LiteralPath $normal.BackupRoot -File).Count-eq2) 'fresh rollback backup missing'
$failed=Fixture 'failure'
$failedAcl=(Get-Acl -LiteralPath $failed.Target).Sddl
$caught=$false
try{
 Install-PinnedRuntime @failed -ReplaceAction {
  param($s,$t,$b)
  [IO.File]::Move($t,$b)
  throw 'simulated Windows partial replace failure'
 }|Out-Null
}catch{$caught=$true}
Check ($caught -and (Hash $failed.Target)-eq$failed.Old) 'partial failure lost the old destination'
Check ((Get-Acl -LiteralPath $failed.Target).Sddl-eq$failedAcl) 'partial failure lost the original ACL'
Check (@(Get-ChildItem -LiteralPath $failed.BackupRoot -Filter '*.retained.*').Count-eq1) 'rollback byte anchor was not retained'
$result=Install-PinnedRuntime @failed
Check ($result-eq'updated' -and (Hash $failed.Target)-eq$failed.New) 'retry after rollback failed'
$retentionFailure=Fixture 'retention-failure'
$retentionAcl=(Get-Acl -LiteralPath $retentionFailure.Target).Sddl
$caught=$false
try{
 Install-PinnedRuntime @retentionFailure -ReplaceAction {
  param($s,$t,$b)
  [IO.File]::Move($t,$b)
  throw 'simulated Windows partial replace failure'
 } -RetainAction {
  param($s,$t)
  throw 'simulated rollback copy failure'
 }|Out-Null
}catch{$caught=$true}
Check ($caught -and (Test-Path -LiteralPath $retentionFailure.Target)) 'retention failure prevented restoring the destination'
Check ((Hash $retentionFailure.Target)-eq$retentionFailure.Old) 'retention failure changed the old destination'
Check ((Get-Acl -LiteralPath $retentionFailure.Target).Sddl-eq$retentionAcl) 'retention failure lost the original ACL'
$result=Install-PinnedRuntime @retentionFailure
Check ($result-eq'updated' -and (Hash $retentionFailure.Target)-eq$retentionFailure.New) 'retry after retention failure failed'
$unknown=Fixture 'unknown'
$stage=$unknown.Target+'.3085-stage';[IO.File]::WriteAllText($stage,'unowned bytes')
$caught=$false
try{Install-PinnedRuntime @unknown|Out-Null}catch{$caught=$true}
Check ($caught -and (Hash $unknown.Target)-eq$unknown.Old -and [IO.File]::ReadAllText($stage)-eq'unowned bytes') 'unknown stage was changed'
'pinned software replace/retry/rollback acceptance passed'
`
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	production := os.Getenv("CAPACITY_DEPLOY_TEST_SCRIPT")
	if production == "" {
		production = "deploy-3085.ps1"
	}
	source, err := filepath.Abs(production)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// Service-account temporary paths may contain DOS short-name components.
	// Exercise a real alias when this owned temp volume supports short names.
	ownedArgument := root
	longName, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	shortName := make([]uint16, 32768)
	if n, err := syscall.GetShortPathName(longName, &shortName[0], uint32(len(shortName))); err == nil && n > 0 && n < uint32(len(shortName)) {
		ownedArgument = syscall.UTF16ToString(shortName[:n])
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-ProductionScript", source, "-OwnedRoot", ownedArgument)
	// A PowerShell 7 parent must not make Windows PowerShell 5 load its
	// incompatible security module when exercising the real file ACLs.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "PSMODULEPATH=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "PSModulePath="+filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "Modules"))
	hide(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "pinned software replace/retry/rollback acceptance passed") {
		t.Fatalf("owned Windows deployment acceptance failed: %v: %s", err, out)
	}
}
