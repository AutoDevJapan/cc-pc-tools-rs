param(
 [Parameter(Mandatory=$true)][ValidatePattern('^[0-9]{8}-[0-9]{6}-todo-4011$')][string]$ActorJob,
 [Parameter(Mandatory=$true)][ValidatePattern('^[0-9a-f]{40}$')][string]$SourceHead,
 [Parameter(Mandatory=$true)][int]$ReviewTodo,
 [Parameter(Mandatory=$true)][string]$NativeExe
)
$ErrorActionPreference='Stop'
$work=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$ledger=Join-Path $work '.ci-work/4011/os-v2'
$requestPath=Join-Path $ledger 'disk-io-20s-request.private.json'
$resultPath=Join-Path $ledger 'disk-io-20s-result.private.json'
if((Test-Path -LiteralPath $requestPath) -or (Test-Path -LiteralPath $resultPath)){throw 'DIAGNOSTIC_STAGE_EXISTS_RECONCILE_NO_RETRY'}
if(-not [IO.Path]::IsPathRooted($NativeExe) -or (Test-Path -LiteralPath $NativeExe)){throw 'NEW_ABSOLUTE_BUILD_PATH_REQUIRED'}
function Hidden([string]$Exe,[string]$Arguments,[int]$Seconds){
 $s=New-Object Diagnostics.ProcessStartInfo
 $s.FileName=$Exe;$s.Arguments=$Arguments;$s.WorkingDirectory=$work;$s.UseShellExecute=$false;$s.CreateNoWindow=$true
 $s.RedirectStandardOutput=$true;$s.RedirectStandardError=$true
 $p=[Diagnostics.Process]::Start($s);$o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync()
 $timeout=-not $p.WaitForExit($Seconds*1000)
 if($timeout){if(-not $p.HasExited){$p.Kill()};[void]$p.WaitForExit(5000)}
 if(-not $o.Wait(5000) -or -not $e.Wait(5000)){throw 'OWN_PIPE_DRAIN_UNVERIFIED'}
 return [pscustomobject]@{exit=if($p.HasExited){$p.ExitCode}else{$null};timeout=$timeout;exit_confirmed=$p.HasExited;stdout=$o.GetAwaiter().GetResult();stderr=$e.GetAwaiter().GetResult()}
}
function HashStream([IO.Stream]$Stream){
 $r=New-Object IO.StreamReader($Stream,[Text.Encoding]::UTF8,$true,4096,$true)
 try{$text=$r.ReadToEnd().Replace("`r`n","`n")}finally{$r.Dispose()}
 $h=[Security.Cryptography.SHA256]::Create()
 try{return ([BitConverter]::ToString($h.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)))).Replace('-','')}finally{$h.Dispose()}
}
$git=(Get-Command git.exe).Source
$head=Hidden $git 'rev-parse HEAD' 15
$dirty=Hidden $git 'status --porcelain -- tools/vss-4011' 15
if($head.exit -ne 0 -or $head.stdout.Trim() -cne $SourceHead -or $dirty.exit -ne 0 -or $dirty.stdout.Trim()){throw 'SOURCE_HEAD_OR_CLEAN_STATE_DIFFERS'}
$pins=New-Object 'System.Collections.Generic.List[System.IO.FileStream]'
try{
 $sources=@{'tools/vss-4011/common.ps1'='869C87C8DEA7E4A17FC72053126BC86EC6CADFFFDF0137AE793DE52E66F68142';'tools/vss-4011/diskio-once/main_windows.go'='2BF0AF9586BF84CBD939EA6EB95D2BCBDAAE9854EBB6EAAE9941A9EC46615853'}
 foreach($entry in $sources.GetEnumerator()){$f=[IO.File]::Open((Join-Path $work $entry.Key),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read);$pins.Add($f);if((HashStream $f) -cne $entry.Value){throw 'PINNED_SOURCE_DIFFERS'}}
 . (Join-Path $work 'tools/vss-4011/common.ps1')
 $b=Hidden 'C:/Program Files/Go/bin/go.exe' ('build -trimpath -o "'+$NativeExe+'" "'+(Join-Path $PSScriptRoot 'main_windows.go')+'"') 60
 if($b.exit -ne 0 -or $b.timeout){throw 'FRESH_BUILD_FAILED'}
 $f=[IO.File]::Open($NativeExe,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read);$pins.Add($f)
 $request=[ordered]@{todo=4011;actor_job=$ActorJob;utc=[DateTime]::UtcNow.ToString('o');phase='finite_disk_trace_pending';source_head=$SourceHead;source_hashes=$sources;exe_sha256=(Get-Vss4011FileHash $NativeExe);review_todo=$ReviewTodo;duration_seconds=20;etl_files=0;file_names_enabled=$false;retry_allowed=$false}
 Write-Vss4011NewReceipt $requestPath $request
 $result=[ordered]@{todo=4011;phase='native_outcome_unresolved';request_sha256=(Get-Vss4011FileHash $requestPath);utc=[DateTime]::UtcNow.ToString('o');native=$null;launcher_hresult=$null}
 try{
  $call=Hidden $NativeExe '--collect-20s' 65
  $result.call=$call
  if(-not $call.timeout -and $call.exit_confirmed){$result.native=$call.stdout|ConvertFrom-Json;$result.phase=$result.native.phase}else{$result.phase='timeout_reconcile_owned_trace_before_any_next_attempt'}
 }catch{$result.launcher_hresult=$_.Exception.HResult}
 $result.utc=[DateTime]::UtcNow.ToString('o')
 $result|ConvertTo-Json -Depth 12
 Write-Vss4011NewReceipt $resultPath $result
 if($result.native -and $call.exit -eq 0 -and $result.native.SessionAbsent -and $result.native.WatchdogExited){exit 0}else{exit 1}
}finally{foreach($f in $pins){$f.Dispose()}}
