param(
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9]{8}-[0-9]{6}-todo-4011$')][string]$ActorJob,
    [Parameter(Mandatory=$true)][string]$NativeExe,
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9a-f]{40}$')][string]$SourceHead,
    [Parameter(Mandatory=$true)][ValidateRange(1,2147483647)][int]$ReviewTodo,
    [ValidateSet('Inspect','Attempt')][string]$Mode='Inspect',
    [string]$GoExe='C:/Program Files/Go/bin/go.exe'
)
$ErrorActionPreference='Stop'
$work=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
function RunHidden([string]$Exe,[string]$Arguments,[int]$Seconds=30){
    $si=New-Object Diagnostics.ProcessStartInfo
    $si.FileName=$Exe;$si.Arguments=$Arguments;$si.WorkingDirectory=$work
    $si.UseShellExecute=$false;$si.CreateNoWindow=$true;$si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
    $p=[Diagnostics.Process]::Start($si);$o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync()
    if(-not $p.WaitForExit($Seconds*1000)){
        if(-not $p.HasExited){$p.Kill()};[void]$p.WaitForExit(5000)
        throw 'OWN_PREPARATION_TIMEOUT_NO_WRITE'
    }
    if(-not $o.Wait(5000) -or -not $e.Wait(5000)){throw 'OWN_PREPARATION_DRAIN_UNVERIFIED'}
    return [pscustomobject]@{exit=$p.ExitCode;stdout=$o.GetAwaiter().GetResult();stderr=$e.GetAwaiter().GetResult()}
}
function CanonicalHash([IO.Stream]$Stream){
    $reader=New-Object IO.StreamReader($Stream,[Text.Encoding]::UTF8,$true,4096,$true)
    try{$text=$reader.ReadToEnd().Replace("`r`n","`n")}finally{$reader.Dispose()}
    $sha=[Security.Cryptography.SHA256]::Create()
    try{return ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)))).Replace('-','')}finally{$sha.Dispose()}
}
$git=(Get-Command git.exe -ErrorAction Stop).Source
$head=RunHidden $git 'rev-parse HEAD'
if($head.exit -ne 0 -or $head.stdout.Trim() -cne $SourceHead){throw 'ACTUAL_SOURCE_HEAD_DIFFERS'}
$dirty=RunHidden $git 'status --porcelain -- tools/vss-4011'
if($dirty.exit -ne 0 -or $dirty.stdout.Trim()){throw 'TOOLS_SOURCE_NOT_CLEAN'}
$ledger=Join-Path $work '.ci-work/4011/os-v2'
$prefix=if($Mode -eq 'Attempt'){'positive-cd'}else{'cd-runner-inspection'}
$requestPath=Join-Path $ledger ($prefix+'-request.private.json')
$resultPath=Join-Path $ledger ($prefix+'-result.private.json')
if((Test-Path -LiteralPath $requestPath) -or (Test-Path -LiteralPath $resultPath)){throw 'STAGE_EXISTS_NO_RETRY'}
if(Test-Path -LiteralPath $NativeExe){throw 'NEW_BUILD_PATH_REQUIRED'}
$pins=New-Object 'System.Collections.Generic.List[System.IO.FileStream]'
try{
    $expected=@{
        'tools/vss-4011/common.ps1'='869C87C8DEA7E4A17FC72053126BC86EC6CADFFFDF0137AE793DE52E66F68142'
        'tools/vss-4011/cd-attempt/main_windows.go'='33869F2ACDEA3F1AA8459A9962BE43946496EEA0B376250F924AA279B2E83EFC'
        'tools/vss-4011/cd-attempt/preflight.ps1'='3C0E26FD7C7B93056AEBB64A7221760BC144218A5E88877DAF8B5BBD9C664E16'
    }
    foreach($entry in $expected.GetEnumerator()){
        $pin=[IO.File]::Open((Join-Path $work $entry.Key),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        $pins.Add($pin)
        if((CanonicalHash $pin) -cne $entry.Value){throw 'PINNED_REVIEWED_SOURCE_DIFFERS'}
    }
    . (Join-Path $work 'tools/vss-4011/common.ps1')
    $build=RunHidden $GoExe ('build -trimpath -o "'+$NativeExe+'" "'+(Join-Path $PSScriptRoot 'main_windows.go')+'"') 60
    if($build.exit -ne 0){throw 'FRESH_VERIFIED_SOURCE_BUILD_FAILED'}
    $exePin=[IO.File]::Open($NativeExe,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    $pins.Add($exePin)
    $actual=Get-Vss4011FileHash $NativeExe
    $request=[ordered]@{todo=4011;actor_job=$ActorJob;phase=if($Mode -eq 'Attempt'){'positive_cd_call_pending_outcome_unknown'}else{'cd_runner_inspection_pending_no_write'};utc=[DateTime]::UtcNow.ToString('o');mode=$Mode;source_head=$SourceHead;source_hashes=$expected;exe_sha256=$actual;build_from_verified_source=$true;review_todo=$ReviewTodo;expected_source='C:';expected_old_storage='C:';target_storage='D:';maximum_bytes=[long](50GB);delete_or_shrink_calls=0;retry_allowed=$false}
    Write-Vss4011NewReceipt $requestPath $request
    $result=[ordered]@{todo=4011;actor_job=$ActorJob;phase='native_call_outcome_unresolved';utc=[DateTime]::UtcNow.ToString('o');process_started=$false;native_exit=$null;timed_out=$false;exit_confirmed=$false;native=$null;stdout='';stderr='';launcher_hresult=$null}
    $exitCode=1
    try{
        $si=New-Object Diagnostics.ProcessStartInfo
        $si.FileName=$NativeExe
        $si.Arguments=if($Mode -eq 'Attempt'){'--attempt-cd "'+(Join-Path $PSScriptRoot 'preflight.ps1')+'" "'+$ledger+'"'}else{'--inspect-cd "'+(Join-Path $PSScriptRoot 'preflight.ps1')+'"'}
        $si.UseShellExecute=$false;$si.CreateNoWindow=$true;$si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
        $p=[Diagnostics.Process]::Start($si);$result.process_started=$true
        $out=$p.StandardOutput.ReadToEndAsync();$err=$p.StandardError.ReadToEndAsync()
        if(-not $p.WaitForExit(90000)){
            $result.timed_out=$true
            if(-not $p.HasExited){$p.Kill()};[void]$p.WaitForExit(5000)
        }
        $result.exit_confirmed=$p.HasExited
        if(-not $result.exit_confirmed){throw 'OWN_NATIVE_EXIT_UNCONFIRMED'}
        if(-not $out.Wait(5000) -or -not $err.Wait(5000)){throw 'OWN_NATIVE_PIPE_DRAIN_UNCONFIRMED'}
        $result.stdout=$out.GetAwaiter().GetResult();$result.stderr=$err.GetAwaiter().GetResult();$result.native_exit=$p.ExitCode
        if($result.timed_out){$result.phase='native_timeout_effect_unresolved_no_retry';$exitCode=124}else{
            $result.native=$result.stdout|ConvertFrom-Json
            if($null -eq $result.native -or [string]::IsNullOrWhiteSpace($result.native.phase)){throw 'NATIVE_RESULT_MISSING'}
            $result.phase=[string]$result.native.phase;$exitCode=$p.ExitCode
        }
    }catch{$result.launcher_hresult=$_.Exception.HResult;$result.phase='launcher_failed_effect_unresolved_no_retry';$exitCode=1}
    $result.utc=[DateTime]::UtcNow.ToString('o')
    # If saving fails the normal Broker stdout still retains the native response.
    [pscustomobject]@{phase=$result.phase;native_exit=$result.native_exit;timed_out=$result.timed_out;exit_confirmed=$result.exit_confirmed;native=$result.native;launcher_hresult=$result.launcher_hresult}|ConvertTo-Json -Depth 14
    Write-Vss4011NewReceipt $resultPath $result
    exit $exitCode
}finally{foreach($pin in $pins){$pin.Dispose()}}
