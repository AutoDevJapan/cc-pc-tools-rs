param(
 [Parameter(Mandatory=$true)][ValidatePattern('^[0-9]{8}-[0-9]{6}-todo-4011$')][string]$ActorJob,
 [Parameter(Mandatory=$true)][ValidatePattern('^[0-9a-f]{40}$')][string]$SourceHead,
 [Parameter(Mandatory=$true)][ValidateRange(1,2147483647)][int]$ReviewTodo,
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
 $r=[ordered]@{started=$false;exit=$null;timeout=$false;exit_confirmed=$false;stdout_complete=$false;stderr_complete=$false;stdout='';stderr='';error_type=$null;error_code=$null;hresult=$null}
 $p=$null;$o=$null;$e=$null
 try{
  $s=New-Object Diagnostics.ProcessStartInfo
  $s.FileName=$Exe;$s.Arguments=$Arguments;$s.WorkingDirectory=$work;$s.UseShellExecute=$false;$s.CreateNoWindow=$true
  $s.RedirectStandardOutput=$true;$s.RedirectStandardError=$true
  $p=[Diagnostics.Process]::Start($s);$r.started=$true
  $o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync()
  $r.timeout=-not $p.WaitForExit($Seconds*1000)
  if($r.timeout){if(-not $p.HasExited){$p.Kill()};[void]$p.WaitForExit(5000)}
  $r.exit_confirmed=$p.HasExited
  if($p.HasExited){$r.exit=$p.ExitCode}
  $r.stdout_complete=$o.Wait(5000);$r.stderr_complete=$e.Wait(5000)
  if($r.stdout_complete){$r.stdout=$o.GetAwaiter().GetResult()}
  if($r.stderr_complete){$r.stderr=$e.GetAwaiter().GetResult()}
  if(-not $r.exit_confirmed){$r.error_code='OWN_EXIT_UNVERIFIED'}
  elseif(-not $r.stdout_complete -or -not $r.stderr_complete){$r.error_code='OWN_PIPE_DRAIN_UNVERIFIED'}
 }catch{
  $r.error_type=$_.Exception.GetType().FullName;$r.hresult=$_.Exception.HResult
  $r.error_code=if(-not $r.started){'OWN_PROCESS_START_FAILED'}else{'OWN_PROCESS_CONTROL_FAILED'}
 }finally{
  if($p){
   try{if(-not $p.HasExited){$p.Kill();[void]$p.WaitForExit(5000)};$r.exit_confirmed=$p.HasExited;if($p.HasExited){$r.exit=$p.ExitCode}}catch{if(-not $r.error_code){$r.error_code='OWN_FINALLY_EXIT_UNVERIFIED'}}
   $p.Dispose()
  }
 }
 return [pscustomobject]$r
}
function CallComplete($Call){return $Call.started -and $Call.exit_confirmed -and -not $Call.timeout -and $Call.stdout_complete -and $Call.stderr_complete -and -not $Call.error_code}
function HashStream([IO.Stream]$Stream){
 $reader=New-Object IO.StreamReader($Stream,[Text.Encoding]::UTF8,$true,4096,$true)
 try{$text=$reader.ReadToEnd().Replace("`r`n","`n")}finally{$reader.Dispose()}
 $h=[Security.Cryptography.SHA256]::Create()
 try{return ([BitConverter]::ToString($h.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)))).Replace('-','')}finally{$h.Dispose()}
}
function Observation {
 $r=[ordered]@{utc=[DateTime]::UtcNow.ToString('o');complete=$false;storage=$null;shadows=@();point_sequences=@();free=@();hresult=$null;error_type=$null}
 try{
  $c=Get-Vss4011CVolume;$r.storage=Get-Vss4011Storage $c.DeviceID
  $r.shadows=@(Get-Vss4011Shadows $c.DeviceID)
  $r.point_sequences=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Property SequenceNumber|ForEach-Object {$_.SequenceNumber})
  $r.free=@(Get-CimInstance Win32_Volume -Filter "DriveLetter='C:' OR DriveLetter='D:' OR DriveLetter='E:'" -Property DriveLetter,FreeSpace|ForEach-Object {[ordered]@{volume=$_.DriveLetter;bytes=[uint64]$_.FreeSpace}})
  $r.complete=$true
 }catch{$r.hresult=$_.Exception.HResult;$r.error_type=$_.Exception.GetType().FullName}
 return [pscustomobject]$r
}
$git=(Get-Command git.exe).Source
$head=Hidden $git 'rev-parse HEAD' 15
$dirty=Hidden $git 'status --porcelain -- tools/vss-4011' 15
if(-not (CallComplete $head) -or $head.exit -ne 0 -or $head.stdout.Trim() -cne $SourceHead -or -not (CallComplete $dirty) -or $dirty.exit -ne 0 -or $dirty.stdout.Trim()){throw 'SOURCE_HEAD_OR_CLEAN_STATE_DIFFERS'}
$pins=New-Object 'System.Collections.Generic.List[System.IO.FileStream]'
try{
 $sources=@{'tools/vss-4011/common.ps1'='869C87C8DEA7E4A17FC72053126BC86EC6CADFFFDF0137AE793DE52E66F68142';'tools/vss-4011/diskio-once/main_windows.go'='2FE3AA14BF782576B1593FB7F7D84BFC971D0CBEE3BC2F09A9F02DEEE96A0EA3'}
 foreach($entry in $sources.GetEnumerator()){$f=[IO.File]::Open((Join-Path $work $entry.Key),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read);$pins.Add($f);if((HashStream $f) -cne $entry.Value){throw 'PINNED_SOURCE_DIFFERS'}}
 . (Join-Path $work 'tools/vss-4011/common.ps1')
 $b=Hidden 'C:/Program Files/Go/bin/go.exe' ('build -trimpath -o "'+$NativeExe+'" "'+(Join-Path $PSScriptRoot 'main_windows.go')+'"') 60
 if(-not (CallComplete $b) -or $b.exit -ne 0){throw 'FRESH_BUILD_FAILED'}
 $f=[IO.File]::Open($NativeExe,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read);$pins.Add($f)
 $ownedGUID=([BitConverter]::ToString(([guid]::NewGuid()).ToByteArray())).Replace('-','').ToLowerInvariant()
 $request=[ordered]@{todo=4011;actor_job=$ActorJob;utc=[DateTime]::UtcNow.ToString('o');phase='finite_disk_trace_pending';purpose='instrument_acceptance_and_current_baseline';owned_guid_bytes=$ownedGUID;source_head=$SourceHead;source_hashes=$sources;exe_sha256=(Get-Vss4011FileHash $NativeExe);review_todo=$ReviewTodo;duration_seconds=20;etl_files=0;file_names_enabled=$false;retry_allowed=$false}
 # The ownership value is durable before native trace start, including a timeout.
 Write-Vss4011NewReceipt $requestPath $request
 $result=[ordered]@{todo=4011;owned_guid_bytes=$ownedGUID;phase='native_outcome_unresolved';request_sha256=(Get-Vss4011FileHash $requestPath);utc=[DateTime]::UtcNow.ToString('o');before=$null;call=$null;native=$null;after=$null;post_query_call=$null;post_query=$null;launcher_hresult=$null;launcher_error_type=$null;launcher_error_code=$null}
 $result.before=Observation
 try{
  $result.call=Hidden $NativeExe ('--collect-20s '+$ownedGUID) 65
  if(CallComplete $result.call){
   $result.native=$result.call.stdout|ConvertFrom-Json
   if(-not $result.native -or [string]::IsNullOrWhiteSpace([string]$result.native.phase) -or $result.native.owned_guid_bytes -cne $ownedGUID){throw 'NATIVE_RESULT_OR_OWNERSHIP_MISSING'}
   $result.phase=$result.native.phase
  }elseif($result.call.timeout){$result.phase='timeout_reconcile_owned_trace_before_any_next_attempt'}
  elseif(-not $result.call.started){$result.phase='native_not_started'}
  else{$result.phase='native_result_unresolved_no_retry'}
 }catch{$result.launcher_hresult=$_.Exception.HResult;$result.launcher_error_type=$_.Exception.GetType().FullName;$result.launcher_error_code='NATIVE_RESULT_OR_OWNERSHIP_UNVERIFIED'}
 # Query only, even after timeout/start/drain failure. Never start or stop here.
 try{
  $result.post_query_call=Hidden $NativeExe ('--query-owned '+$ownedGUID) 10
  if((CallComplete $result.post_query_call) -and $result.post_query_call.exit -eq 0){$result.post_query=$result.post_query_call.stdout|ConvertFrom-Json}
 }catch{$result.launcher_error_code='POST_QUERY_UNVERIFIED'}
 $result.after=Observation
 $result.utc=[DateTime]::UtcNow.ToString('o')
 $result|ConvertTo-Json -Depth 14
 Write-Vss4011NewReceipt $resultPath $result
 if($result.native -and $result.call.exit -eq 0 -and (CallComplete $result.call) -and $result.native.SessionAbsent -and $result.native.WatchdogExited -and $result.post_query.state -eq 'absent' -and $result.before.complete -and $result.after.complete){exit 0}else{exit 1}
}finally{foreach($f in $pins){$f.Dispose()}}
