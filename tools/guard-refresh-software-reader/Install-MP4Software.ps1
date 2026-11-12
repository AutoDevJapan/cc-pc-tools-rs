param([ValidateSet('ask','runner')][string]$Kind='ask',[switch]$Apply,[switch]$LibraryOnly)
$mp4Apply=[bool]$Apply;$mp4Library=[bool]$LibraryOnly
$ErrorActionPreference='Stop'
$privacy=Join-Path $PSScriptRoot 'Install-AskPrivacySoftware.ps1'
if((Get-FileHash -LiteralPath $privacy -Algorithm SHA256).Hash.ToLowerInvariant() -cne 'b0d07cc6248255a749008129290c062b3ef70561220cbe591c8fa9171bc44bc8'){throw 'reviewed_deployment_library_changed'}
. $privacy -LibraryOnly

function Get-MP4DeploymentPlan([string]$Selected){
    $root='C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools'
    $source='cfecca6b5dc4c769697e4d7d17dffdcc73912601'
    switch -CaseSensitive ($Selected){
        'ask' {return @{target='D:/CC/Tools/discord-ask/discord_ask.exe';beforeSha256='6d22dc15797c821794a56e9d05c432b4a1694369e6d97628925580ec42c29a28';beforeIdentity='1215965535:281474995436467';beforeAcl='6064032359202b73f60c351b40ceb9d609c84d4205d745321ede80762345587c';candidate=$root+'/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-mp4-cfecca.exe';candidateSha256='';candidateBytes=0;source=$source;sourceMerged=$source;stage='D:/CC/Tools/discord-ask/discord_ask.mp4cfecca.stage.exe';backup='D:/CC/Tools/discord-ask/discord_ask.mp4cfecca.before.exe'}}
        'runner' {return @{target='D:/CC/Tools/discord-ask/unattended-runner.exe';beforeSha256='385f4207dd5491cc0c88060f71f1e270b543e42a1c45898b1de15348afb7efb4';beforeIdentity='1215965535:844424946531910';beforeAcl='44dc0d97e005be11daca911fc4677371297dc38382d44d6151c1b8679f181b08';candidate=$root+'/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/unattended-runner-mp4-cfecca.exe';candidateSha256='';candidateBytes=0;source=$source;sourceMerged=$source;stage='D:/CC/Tools/discord-ask/unattended-runner.mp4cfecca.stage.exe';backup='D:/CC/Tools/discord-ask/unattended-runner.mp4cfecca.before.exe'}}
        default {throw 'unknown_fixed_software'}
    }
}

function Invoke-MP4SoftwareOperation([string]$Reader,[string]$Operation,[scriptblock]$Parse){
    if($Operation -cnotin @('--verify-mp4-ask-update','--prepare-mp4-ask-update','--verify-mp4-runner-update','--prepare-mp4-runner-update') -or -not $Parse){throw 'mp4_software_operation_invalid'}
    $proc=[Diagnostics.Process]::new();$proc.StartInfo=[Diagnostics.ProcessStartInfo]::new($Reader)
    $proc.StartInfo.ArgumentList.Add($Operation);$proc.StartInfo.UseShellExecute=$false;$proc.StartInfo.CreateNoWindow=$true
    $proc.StartInfo.RedirectStandardOutput=$true;$proc.StartInfo.RedirectStandardError=$true
    try{
        [void]$proc.Start();$out=$proc.StandardOutput.ReadToEndAsync();$err=$proc.StandardError.ReadToEndAsync()
        if(-not $proc.WaitForExit(20000)){
            if(-not (Stop-AskSoftwareOperation $proc)){throw 'mp4_software_tree_collection_unknown'}
            return @{qualified=$false;prepared=$false;reason='qualification_unavailable';software_replace_allowed=$false;data_access_granted=$false;update_performed=$false;reconciliation=@{domain='owner-question-fixed-software-reconcile-v1';Target=@{state='unknown'};Stage=@{state='unknown'};Backup=@{state='unknown'};switch_allowed=$false}}
        }
        return & $Parse $proc.ExitCode ($out.GetAwaiter().GetResult()) ($err.GetAwaiter().GetResult())
    }finally{$proc.Dispose()}
}

function New-MP4DeploymentOperations($Plan,[string]$Receipt,[scriptblock]$Qualify,[scriptblock]$Smoke,[scriptblock]$PrepareSoftware){
    $ops=New-AskDeploymentOperations $Plan $Receipt $Qualify $Smoke $PrepareSoftware
    $observe=$ops.ObserveState;$same=$ops.SameGeneration;$restore=$ops.Restore
    # The hash-pinned deployment library retains this exact writer handle.
    # Opening a new read/share-read pin here would conflict with that writer.
    $context=$observe.Module.SessionState.PSVariable.GetValue('ctx')
    if(-not $context -or $context.plan.target -cne $Plan.target){throw 'reviewed_deployment_context_unavailable'}
    $aclHash={param($Acl)Get-SyncAclHash $Acl}
    $ops.ObserveState={
        $actual=& $observe
        $held=$context.recoveryTarget
        if(-not $held -or [RunnerImageLease]::Identity($held) -cne $actual.targetIdentity){throw 'recovery_named_identity_changed'}
        $actual.targetAclHash=& $aclHash ([IO.FileSystemAclExtensions]::GetAccessControl($held))
        return $actual
    }.GetNewClosure()
    $ops.SameGeneration={param($prepared,$actual)return (& $same $prepared $actual) -and $actual.targetAclHash -ceq $Plan.beforeAcl}.GetNewClosure()
    $ops.Restore={
        param($prepared,$actual,$rollback)
        $held=$context.recoveryTarget
        if(-not $held -or [RunnerImageLease]::Identity($held) -cne $prepared.targetIdentity -or (& $aclHash ([IO.FileSystemAclExtensions]::GetAccessControl($held))) -cne $Plan.beforeAcl){throw 'recovery_target_acl_or_generation_changed'}
        & $restore $prepared $actual $rollback
    }.GetNewClosure()
    return $ops
}

if($mp4Library){return}
$plan=Get-MP4DeploymentPlan $Kind
$repo='C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools'
$reader=$repo+'/codex-owned-b30691d5cc845d615ea1be06bb0d3de3/mp4-software-reader-final.exe'
$readerSha=''
$receipt='D:/CC/Tools/codex-jobs/fourbros4431-mp4-'+$Kind+'-installed.json'
if(-not $mp4Apply){@{apply=$false;kind=$Kind;targetRead=0;candidateBound=($plan.candidateSha256.Length -eq 64);readerBound=($readerSha.Length -eq 64)}|ConvertTo-Json -Compress;return}
if($readerSha -cnotmatch '^[a-f0-9]{64}$' -or $plan.candidateSha256 -cnotmatch '^[a-f0-9]{64}$' -or $plan.candidateBytes -le 0){throw 'reviewed_mp4_binary_not_bound'}
foreach($p in @($reader,$plan.candidate,$receipt,($receipt+'.journal'),($receipt+'.pending'))){Assert-SyncGuard $p;Assert-SyncPlain $p}
$pin=[RunnerImageLease]::PinCandidate($reader)
try{
    if((Get-SyncHash $pin) -cne $readerSha){throw 'reviewed_reader_changed'}
    $parser={param($exit,$out,$err)Convert-AskSoftwareResponse $exit $out $err $plan}.GetNewClosure()
    $verifyFlag='--verify-mp4-'+$Kind+'-update';$prepareFlag='--prepare-mp4-'+$Kind+'-update'
    $qualify={Invoke-MP4SoftwareOperation $reader $verifyFlag $parser}.GetNewClosure()
    $prepare={Invoke-MP4SoftwareOperation $reader $prepareFlag $parser}.GetNewClosure()
    $smoke={param($path)
        $p=[Diagnostics.Process]::new();$p.StartInfo=[Diagnostics.ProcessStartInfo]::new($path)
        $p.StartInfo.ArgumentList.Add('--help');$p.StartInfo.UseShellExecute=$false;$p.StartInfo.CreateNoWindow=$true
        $p.StartInfo.RedirectStandardOutput=$true;$p.StartInfo.RedirectStandardError=$true
        try{[void]$p.Start();$o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync();if(-not $p.WaitForExit(5000)){$p.Kill($true);$p.WaitForExit();throw 'mp4_help_timeout'};if($p.ExitCode -ne 0 -or $e.GetAwaiter().GetResult().Length -or $o.GetAwaiter().GetResult().Length -eq 0){throw 'mp4_help_failed'}}finally{$p.Dispose()}
    }
    $ops=New-MP4DeploymentOperations $plan $receipt $qualify $smoke $prepare
    $result=Invoke-SyncDeploymentController $ops $false
    $result|ConvertTo-Json -Depth 14 -Compress
    if(-not $result.ok){exit 1}
}finally{$pin.Dispose()}
