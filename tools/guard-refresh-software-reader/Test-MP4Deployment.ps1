param([Parameter(Mandatory=$true)][string]$OwnedRoot)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'Install-MP4Software.ps1') -LibraryOnly
Assert-SyncGuard $OwnedRoot
$results=@()
foreach($case in @('normal','before_switch_failure','after_switch_failure','changed_target_acl','published_then_throw')){
    $tag=[Guid]::NewGuid().ToString('N');$prefix=Join-Path $OwnedRoot ('mp4-'+$tag)
    $plan=@{target=$prefix+'-target.exe';candidate=$prefix+'-candidate.exe';stage=$prefix+'-stage.exe';backup=$prefix+'-backup.exe';source=('a'*40);sourceMerged=('a'*40)}
    $receipt=$prefix+'-receipt.json'
    [IO.File]::WriteAllText($plan.candidate,'owned-new')
    # The prepare boundary is synthetic here; the native Go tests separately
    # cover inherited descriptors. Use an explicit owned fixture descriptor so
    # .NET's automatic inheritance bookkeeping is not mistaken for producer I/O.
    $fixtureAcl=[Security.AccessControl.FileSecurity]::new()
    $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
    $fixtureAcl.SetOwner($sid);$fixtureAcl.SetGroup($sid);$fixtureAcl.SetAccessRuleProtection($true,$false)
    $fixtureAcl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid,[Security.AccessControl.FileSystemRights]::FullControl,[Security.AccessControl.AccessControlType]::Allow))
    $original=[IO.FileSystemAclExtensions]::Create([IO.FileInfo]::new($plan.target),[IO.FileMode]::CreateNew,[Security.AccessControl.FileSystemRights]::FullControl,[IO.FileShare]::None,4096,[IO.FileOptions]::None,$fixtureAcl)
    try{$original.Write([Text.Encoding]::UTF8.GetBytes('owned-old'));$original.Flush($true)}finally{$original.Dispose()}
    $held=[RunnerImageLease]::Open($plan.target,$false)
    try{$plan.beforeSha256=Get-SyncHash $held;$plan.beforeIdentity=[RunnerImageLease]::Identity($held);$acl=[IO.FileSystemAclExtensions]::GetAccessControl($held);$plan.beforeAcl=Get-SyncAclHash $acl}finally{$held.Dispose()}
    $held=[RunnerImageLease]::PinCandidate($plan.candidate)
    try{$plan.candidateSha256=Get-SyncHash $held;$plan.candidateBytes=$held.Length}finally{$held.Dispose()}
    $c=@{plan=$plan;case=$case;acl=$acl;hash=${function:Get-SyncHash}}
    $proof=@{qualified=$true;domain='owner-refresh-fixed-software-prepare-v1';target=$plan.target;stage=$plan.stage;backup=$plan.backup;current_sha256=$plan.beforeSha256;candidate_sha256=$plan.candidateSha256;current_identity=@{volume=$plan.beforeIdentity.Split(':')[0];file_id=$plan.beforeIdentity.Split(':')[1];created='1'};data_access_granted=$false}
    $qualify={return $proof}.GetNewClosure()
    $prepare={
        try{
        $r=@{};foreach($key in $proof.Keys){$r[$key]=$proof[$key]}
        foreach($item in @(@{path=$c.plan.backup;raw=[Text.Encoding]::UTF8.GetBytes('owned-old');key='backup_identity'},@{path=$c.plan.stage;raw=[Text.Encoding]::UTF8.GetBytes('owned-new');key='stage_identity'})){
            $file=[IO.FileSystemAclExtensions]::Create([IO.FileInfo]::new($item.path),[IO.FileMode]::CreateNew,[Security.AccessControl.FileSystemRights]::FullControl,[IO.FileShare]::None,4096,[IO.FileOptions]::None,$c.acl)
            try{$file.Write($item.raw);$file.Flush($true);[IO.FileSystemAclExtensions]::SetAccessControl($file,$c.acl);$id=[RunnerImageLease]::Identity($file);$r[$item.key]=@{volume=$id.Split(':')[0];file_id=$id.Split(':')[1];created='1'}}finally{$file.Dispose()}
        }
        $r.prepared=$true;$r.reason='prepared_two_fixed_software_leaves';$r.software_replace_allowed=$false;$r.update_performed=$false;$r.acl_sha256=$c.plan.beforeAcl;$r.source_head=$c.plan.source;$r.source_merged=$c.plan.sourceMerged
        return $r
        }catch{$c.prepareError=$_.Exception.Message;throw}
    }.GetNewClosure()
    $ops=New-MP4DeploymentOperations $plan $receipt $qualify {param($path)} $prepare
    $switch=$ops.Switch;$commit=$ops.VerifyCommit;$publish=$ops.PublishReceipt
    if($case -ceq 'before_switch_failure'){$ops.Switch={param($prepared,$rollback)throw 'owned_before_switch'}}
    elseif($case -ceq 'after_switch_failure'){$ops.VerifyCommit={param($prepared,$rollback)throw 'owned_after_switch'}}
    elseif($case -ceq 'changed_target_acl'){
        $ctx=$commit.Module.SessionState.PSVariable.GetValue('ctx')
        $release=$ops.ReleaseImageGate
        $ops.ReleaseImageGate={
            & $release
            $file=[IO.FileSystemAclExtensions]::Create([IO.FileInfo]::new($c.plan.target),[IO.FileMode]::Open,[Security.AccessControl.FileSystemRights]::FullControl,[IO.FileShare]::Read,4096,[IO.FileOptions]::None,$c.acl)
            try{
            $changed=[IO.FileSystemAclExtensions]::GetAccessControl($file)
            $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
            $changed.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid,[Security.AccessControl.FileSystemRights]::Read,[Security.AccessControl.AccessControlType]::Allow))
            [IO.FileSystemAclExtensions]::SetAccessControl($file,$changed)
            }finally{$file.Dispose()}
            throw 'owned_acl_change'
        }.GetNewClosure()
    }elseif($case -ceq 'published_then_throw'){$ops.PublishReceipt={param($record)& $publish $record;throw 'owned_after_publication'}.GetNewClosure()}
    $result=Invoke-SyncDeploymentController $ops $false
    Write-Output ($result|ConvertTo-Json -Depth 12 -Compress)
    if($c.prepareError){Write-Output ('fixturePrepareError='+$c.prepareError)}
    $hash=(Get-FileHash -LiteralPath $plan.target).Hash.ToLowerInvariant()
    if(-not $result.cleanup.ok){throw 'fixture_cleanup_failed'}
    switch($case){
        'normal' {if(-not $result.ok -or $hash -cne $plan.candidateSha256 -or -not (Test-Path -LiteralPath $receipt)){throw 'normal_switch_failed'}}
        'before_switch_failure' {if($result.ok -or $hash -cne $plan.beforeSha256 -or $result.recovery.restored){throw 'before_switch_changed_target'}}
        'after_switch_failure' {if($result.ok -or $hash -cne $plan.beforeSha256 -or -not $result.recovery.restored){throw 'known_restore_failed'}}
        'changed_target_acl' {if($result.ok -or $hash -cne $plan.candidateSha256 -or $result.recovery.restored){throw 'changed_acl_generation_overwritten'}}
        'published_then_throw' {if($result.ok -or $hash -cne $plan.candidateSha256 -or $result.rollbackApplied -or $result.state -cne 'publication_committed_or_unknown'){throw 'published_target_rolled_back'}}
    }
    $results+=@{case=$case;passed=$true;cleanup=$result.cleanup.ok;state=$result.state;restored=$result.recovery.restored;targetHash=$hash}
}
$results|ConvertTo-Json -Depth 5 -Compress
