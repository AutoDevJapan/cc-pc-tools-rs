param(
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9]{8}-[0-9]{6}-todo-4011$')][string]$ActorJob,
    [Parameter(Mandatory=$true)][string]$NativeExe,
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9A-F]{64}$')][string]$NativeExeSha256,
    [Parameter(Mandatory=$true)][ValidatePattern('^[0-9a-f]{40}$')][string]$SourceHead,
    [Parameter(Mandatory=$true)][ValidateRange(1,2147483647)][int]$ReviewTodo
)
$ErrorActionPreference='Stop'
$work=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
. (Join-Path $work 'tools/vss-4011/common.ps1')
$ledger=Join-Path $work '.ci-work/4011/os-v2'
$requestPath=Join-Path $ledger 'positive-cd-request.private.json'
$resultPath=Join-Path $ledger 'positive-cd-result.private.json'
if((Test-Path -LiteralPath $requestPath) -or (Test-Path -LiteralPath $resultPath)){throw 'POSITIVE_CD_ATTEMPT_EXISTS_NO_RETRY'}
$text=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'main_windows.go')).Replace("`r`n","`n")
$sha=[Security.Cryptography.SHA256]::Create()
try{$sourceHash=([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)))).Replace('-','')}finally{$sha.Dispose()}
if($sourceHash -cne '0E204F7562B6091605E37D32F02A1EA588C02A8A5A3A571873817F60640D5B6C'){throw 'REVIEWED_NATIVE_SOURCE_CHANGED'}
$pin=[IO.File]::Open($NativeExe,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
try{
    $sha=[Security.Cryptography.SHA256]::Create()
    try{$actual=([BitConverter]::ToString($sha.ComputeHash($pin))).Replace('-','')}finally{$sha.Dispose()}
    if($actual -cne $NativeExeSha256){throw 'OWN_NATIVE_BYTES_CHANGED'}
    $request=[ordered]@{todo=4011;actor_job=$ActorJob;phase='positive_cd_call_pending_outcome_unknown';utc=[DateTime]::UtcNow.ToString('o');source_head=$SourceHead;source_sha256=$sourceHash;exe_sha256=$actual;review_todo=$ReviewTodo;expected_source='C:';expected_old_storage='C:';target_storage='D:';maximum_bytes=[long](50GB);delete_or_shrink_calls=0;retry_allowed=$false}
    Write-Vss4011NewReceipt $requestPath $request
    $result=[ordered]@{todo=4011;actor_job=$ActorJob;phase='native_call_outcome_unresolved';utc=[DateTime]::UtcNow.ToString('o');process_started=$false;native_exit=$null;timed_out=$false;exit_confirmed=$false;native=$null;stdout='';stderr='';launcher_hresult=$null}
    $exitCode=1
    try{
        $si=New-Object Diagnostics.ProcessStartInfo
        $si.FileName=$NativeExe
        $si.Arguments='--attempt-cd "'+(Join-Path $PSScriptRoot 'preflight.ps1')+'"'
        $si.UseShellExecute=$false;$si.CreateNoWindow=$true
        $si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
        $p=[Diagnostics.Process]::Start($si)
        $result.process_started=$true
        $out=$p.StandardOutput.ReadToEndAsync();$err=$p.StandardError.ReadToEndAsync()
        # Native child deadlines all end at 70s, measured before COM startup.
        if(-not $p.WaitForExit(90000)){
            $result.timed_out=$true
            if(-not $p.HasExited){$p.Kill()}
            [void]$p.WaitForExit(5000)
        }
        $result.exit_confirmed=$p.HasExited
        if(-not $result.exit_confirmed){throw 'OWN_NATIVE_EXIT_UNCONFIRMED'}
        if(-not $out.Wait(5000) -or -not $err.Wait(5000)){throw 'OWN_NATIVE_PIPE_DRAIN_UNCONFIRMED'}
        $result.stdout=$out.GetAwaiter().GetResult();$result.stderr=$err.GetAwaiter().GetResult()
        $result.native_exit=$p.ExitCode
        if($result.timed_out){$result.phase='native_timeout_effect_unresolved_no_retry';$exitCode=124}else{
            $result.native=$result.stdout|ConvertFrom-Json
            if($null -eq $result.native -or [string]::IsNullOrWhiteSpace($result.native.phase)){throw 'NATIVE_RESULT_MISSING'}
            $result.phase=[string]$result.native.phase
            $exitCode=$p.ExitCode
        }
    }catch{$result.launcher_hresult=$_.Exception.HResult;$result.phase='launcher_failed_effect_unresolved_no_retry';$exitCode=1}
    $result.utc=[DateTime]::UtcNow.ToString('o')
    Write-Vss4011NewReceipt $resultPath $result
    [pscustomobject]@{phase=$result.phase;native_exit=$result.native_exit;timed_out=$result.timed_out;exit_confirmed=$result.exit_confirmed;native=$result.native;launcher_hresult=$result.launcher_hresult}|ConvertTo-Json -Depth 12
    exit $exitCode
}finally{$pin.Dispose()}
