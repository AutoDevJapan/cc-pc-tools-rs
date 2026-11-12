param([Parameter(Mandatory=$true)][string]$OwnedRoot)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'Install-AskPrivacySoftware.ps1') -LibraryOnly
Assert-SyncGuard $OwnedRoot
$stopResults=@()
foreach($case in @('normal','exit_race','kill_failed','wait_unknown')){
    $c=@{case=$case;kills=0;waits=0;exited=$false}
    $p=[pscustomobject]@{}
    $p|Add-Member ScriptProperty HasExited {$this.Context.exited}
    $p|Add-Member NoteProperty Context $c
    $p|Add-Member ScriptMethod Kill {param($tree)$this.Context.kills++;if($this.Context.case -ceq 'kill_failed'){throw [ComponentModel.Win32Exception]::new(5)};if($this.Context.case -ceq 'exit_race'){$this.Context.exited=$true;throw [InvalidOperationException]::new()};$this.Context.exited=$true}
    $p|Add-Member ScriptMethod WaitForExit {param($ms)$this.Context.waits++;return $this.Context.case -cne 'wait_unknown'}
    $ok=Stop-AskSoftwareOperation $p
    if($ok -ne ($case -in @('normal','exit_race'))){throw 'stop_collection_oracle_failed'}
    $stopResults+=@{case=$case;passed=$true;collected=$ok;kills=$c.kills;waits=$c.waits}
}
$results=@()
$invocation=[Guid]::NewGuid().ToString('N')
foreach($case in @('absent','partial','unknown')){
    $case=$case+'-'+$invocation
    $target=Join-Path $OwnedRoot ('recover-'+$case+'-old.exe')
    $candidate=Join-Path $OwnedRoot ('recover-'+$case+'-candidate.exe')
    $stage=Join-Path $OwnedRoot ('recover-'+$case+'-stage.exe')
    $backup=Join-Path $OwnedRoot ('recover-'+$case+'-backup.exe')
    $receipt=Join-Path $OwnedRoot ('recover-'+$case+'-receipt.json')
    [IO.File]::WriteAllText($target,'owned-original');[IO.File]::WriteAllText($candidate,'owned-candidate')
    $old=[RunnerImageLease]::Open($target,$false)
    try{$oldId=[RunnerImageLease]::Identity($old);$oldHash=Get-SyncHash $old;$acl=[IO.FileSystemAclExtensions]::GetAccessControl($old);$aclHash=Get-SyncAclHash $acl}finally{$old.Dispose()}
    $new=[RunnerImageLease]::Open($candidate,$false)
    try{$newHash=Get-SyncHash $new;$newBytes=$new.Length}finally{$new.Dispose()}
    $plan=@{target=$target;candidate=$candidate;beforeSha256=$oldHash;beforeIdentity=$oldId;candidateSha256=$newHash;candidateBytes=$newBytes;stage=$stage;backup=$backup;source=('a'*40);sourceMerged=('a'*40)}
    $proof=@{qualified=$true;domain='owner-refresh-fixed-software-prepare-v1';target=$target;stage=$stage;backup=$backup;current_sha256=$oldHash;candidate_sha256=$newHash;current_identity=@{volume=$oldId.Split(':')[0];file_id=$oldId.Split(':')[1];created='1'};data_access_granted=$false}
    $ctx=@{case=$case.Split('-')[0];plan=$plan;proof=$proof;acl=$acl;aclHash=$aclHash;hash=${function:Get-SyncHash};parse=${function:Convert-AskSoftwareResponse}}
    $qualify={return $ctx.proof}.GetNewClosure()
    $prepare={
        $r=@{qualified=$false;prepared=$false;reason='qualification_unavailable';software_replace_allowed=$false;data_access_granted=$false;update_performed=$false;reconciliation=@{domain='owner-question-fixed-software-reconcile-v1';Target=@{state='unknown'};Stage=@{state='absent'};Backup=@{state='absent'};switch_allowed=$false}}
        if($ctx.case -ceq 'unknown'){$r.reconciliation.Stage.state='unknown';$r.reconciliation.Backup.state='unknown'}
        if($ctx.case -ceq 'partial'){
            [IO.File]::WriteAllText($ctx.plan.backup,'owned-original')
            $f=[RunnerImageLease]::Open($ctx.plan.backup,$false)
            try{$id=[RunnerImageLease]::Identity($f);$sha=& $ctx.hash $f}finally{$f.Dispose()}
            $r.reconciliation.Backup=@{state='present';identity=@{volume=$id.Split(':')[0];file_id=$id.Split(':')[1];created='1'};sha256=$sha;acl_sha256=$ctx.aclHash}
        }
        $raw=$r|ConvertTo-Json -Depth 12 -Compress
        return & $ctx.parse 1 $raw '' $ctx.plan
    }.GetNewClosure()
    $ops=New-AskDeploymentOperations $plan $receipt $qualify {param($p)} $prepare
    $counter=@{switches=0};$ops.Switch={param($p,$rollback)$counter.switches++;throw 'unexpected_switch'}.GetNewClosure()
    $result=Invoke-SyncDeploymentController $ops $false
    if($result.ok -or $counter.switches -ne 0 -or (Get-FileHash -LiteralPath $target).Hash.ToLowerInvariant() -cne $oldHash -or -not $result.cleanup.preparation.reconciliation){throw 'failure_recovery_contract_lost'}
    $records=@([IO.File]::ReadAllLines($receipt+'.journal')|ForEach-Object{$_|ConvertFrom-Json -AsHashtable})
    $saved=@($records|Where-Object stage -ceq 'software_prepare_returned')
    if($saved.Count -ne 1 -or $saved[0].data.reconciliation.Backup.state -cne $result.cleanup.preparation.reconciliation.Backup.state){throw 'durable_preparation_observation_missing'}
    $results+=@{case=$case;passed=$true;targetUnchanged=$true;switches=0;resultState=$result.cleanup.preparation.reconciliation.Backup.state;journalFlushedReadback=$true;cleanup=$result.cleanup.ok}
}
$results|ConvertTo-Json -Depth 8 -Compress
$stopResults|ConvertTo-Json -Depth 8 -Compress
