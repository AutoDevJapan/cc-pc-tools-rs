param([switch]$Apply,[switch]$LibraryOnly)
$askApply=[bool]$Apply;$askLibrary=[bool]$LibraryOnly
$ErrorActionPreference='Stop'
$adapter='D:/CC/Tools/codex-jobs/fourbros4431-ios-native-guard-install.ps1'
if((Get-FileHash -LiteralPath $adapter -Algorithm SHA256).Hash.ToLowerInvariant() -cne '7bf0a58d67bf2742dbac7e93efaf78a16907dcf19b719e0ff7b5a2917eee91b3'){throw 'deployment_adapter_changed'}
. $adapter -LibraryOnly
$caller=Join-Path $PSScriptRoot 'Invoke-AskSoftwareOperation.ps1'
if((Get-FileHash -LiteralPath $caller -Algorithm SHA256).Hash.ToLowerInvariant() -cne 'b74d6ebd050dc04269ad4060e1e5dbdb88ed665bfa9c773869f340a0df9f3925'){throw 'ask_operation_caller_changed'}
. $caller

function Convert-AskSoftwareResponse([int]$Exit,[string]$Output,[string]$ErrorOutput,$Expected){
    if($Output.Length -gt 65536 -or $ErrorOutput.Length){throw 'ask_response_invalid'}
    $v=Read-SyncStrictJson ([Text.Encoding]::UTF8.GetBytes($Output))
    $observation=$null
    if($v.Contains('reconciliation')){
        $observation=$v.reconciliation
        if($Exit -ne 1 -or $v.qualified -isnot [bool] -or $v.qualified -or $v.prepared -isnot [bool] -or $v.prepared -or $observation -isnot [Collections.IDictionary] -or $observation.Count -ne 5 -or $observation.domain -cne 'owner-question-fixed-software-reconcile-v1' -or $observation.switch_allowed -isnot [bool] -or $observation.switch_allowed){throw 'ask_reconciliation_invalid'}
        foreach($key in $observation.Keys){if($key -cnotin @('domain','Target','Stage','Backup','switch_allowed')){throw 'ask_reconciliation_invalid'}}
        foreach($name in @('Target','Stage','Backup')){
            $o=$observation[$name]
            if($o -isnot [Collections.IDictionary] -or $o.state -cnotin @('present','absent','unknown')){throw 'ask_reconciliation_invalid'}
            if($o.state -cne 'present'){if($o.Count -ne 1){throw 'ask_reconciliation_invalid'};continue}
            if($o.Count -ne 4 -or $o.sha256 -isnot [string] -or $o.sha256 -cnotmatch '^[a-f0-9]{64}$' -or $o.acl_sha256 -isnot [string] -or $o.acl_sha256 -cnotmatch '^[a-f0-9]{64}$' -or $o.identity -isnot [Collections.IDictionary] -or $o.identity.Count -ne 3){throw 'ask_reconciliation_invalid'}
            foreach($key in @('volume','file_id','created')){if($o.identity[$key] -isnot [string] -or $o.identity[$key] -cnotmatch '^[0-9]{1,20}$'){throw 'ask_reconciliation_invalid'}}
        }
        $v.Remove('reconciliation')
    }
    $raw=$v|ConvertTo-Json -Depth 10 -Compress
    $r=Convert-GuardSoftwareOperationResponse $Exit $raw '' $Expected ${function:Read-SyncStrictJson}
    if($observation){$r.reconciliation=$observation}
    return $r
}

# The existing controller writes software_prepare_returned with Flush(true)
# before checking qualified/prepared and before its Switch step. Return the
# failed observation through that exact path, not through a parallel journal.
function New-AskDeploymentOperations($Plan,[string]$Receipt,[scriptblock]$Qualify,[scriptblock]$Smoke,[scriptblock]$PrepareSoftware){
    New-GuardDeploymentOperations $Plan $Receipt $Qualify $Smoke $PrepareSoftware
}
if($askLibrary){return}
$repo='C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools'
$reader=$repo+'/codex-owned-b30691d5cc845d615ea1be06bb0d3de3/ask-software-reader-final-schema-v2.exe'
$readerSha='6173f1e960f796e53effb2efc9043407e9a44881a1967f34c183f25749f2cdb4'
$plan=@{target='D:/CC/Tools/discord-ask/discord_ask.exe';beforeSha256='f5dfeaea2013d83f165da7801595a795d71aedba4c93ed9cc24e8492bdf67ad7';beforeIdentity='1215965535:844424946112908';candidate=$repo+'/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-privacy-3f04.exe';candidateSha256='6d22dc15797c821794a56e9d05c432b4a1694369e6d97628925580ec42c29a28';candidateBytes=13416448;source='3f04e69df159bb4f0df1fe80af3b92c611b9f30a';sourceMerged='3f04e69df159bb4f0df1fe80af3b92c611b9f30a';stage='D:/CC/Tools/discord-ask/discord_ask.privacy4431.stage.exe';backup='D:/CC/Tools/discord-ask/discord_ask.privacy4431.before.exe'}
$receipt='D:/CC/Tools/codex-jobs/fourbros4431-ask-privacy-install-result.json'
if(-not $askApply){@{apply=$false;readerPinned=($readerSha.Length -eq 64);targetRead=0}|ConvertTo-Json -Compress;return}
if($readerSha -cnotmatch '^[a-f0-9]{64}$'){throw 'reviewed_reader_build_not_bound'}
foreach($p in @($reader,$plan.candidate,$receipt,($receipt+'.journal'),($receipt+'.pending'))){Assert-SyncGuard $p;Assert-SyncPlain $p}
$pin=[RunnerImageLease]::PinCandidate($reader)
try{
    if((Get-SyncHash $pin) -cne $readerSha){throw 'reviewed_reader_changed'}
    $parser={param($exit,$out,$err)Convert-AskSoftwareResponse $exit $out $err $plan}.GetNewClosure()
    $qualify={Invoke-AskSoftwareOperation $reader '--verify-ask-privacy-update' $parser}.GetNewClosure()
    $prepare={Invoke-AskSoftwareOperation $reader '--prepare-ask-privacy-update' $parser}.GetNewClosure()
    $smoke={param($path)$p=[Diagnostics.Process]::new();$p.StartInfo=[Diagnostics.ProcessStartInfo]::new($path);$p.StartInfo.ArgumentList.Add('--help');$p.StartInfo.UseShellExecute=$false;$p.StartInfo.CreateNoWindow=$true;$p.StartInfo.RedirectStandardOutput=$true;$p.StartInfo.RedirectStandardError=$true;try{[void]$p.Start();$o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync();if(-not $p.WaitForExit(5000)){$p.Kill($true);$p.WaitForExit();throw 'ask_help_timeout'};if($p.ExitCode -ne 0 -or ($o.GetAwaiter().GetResult()+$e.GetAwaiter().GetResult()) -notmatch 'discord_ask'){throw 'ask_help_failed'}}finally{$p.Dispose()}}
    $ops=New-AskDeploymentOperations $plan $receipt $qualify $smoke $prepare
    $result=Invoke-SyncDeploymentController $ops $false
    $result|ConvertTo-Json -Depth 14 -Compress
    if(-not $result.ok){exit 1}
}finally{$pin.Dispose()}
