param([Parameter(Mandatory)][string]$File,[Parameter(Mandatory)][string[]]$CommandArgs,[Parameter(Mandatory)][string]$Receipt,[string]$WorkingDirectory='D:/CC/Work/unattended-operations-20261013-wt/todo-3096-3081',[string]$ExpectedHead='',[switch]$Cargo)
$ErrorActionPreference='Stop'
function Read-Git([string[]]$GitArgs) {
 $s=[System.Diagnostics.ProcessStartInfo]::new();$s.FileName='C:/Program Files/Git/cmd/git.exe';$s.WorkingDirectory=$WorkingDirectory;$s.UseShellExecute=$false;$s.CreateNoWindow=$true;$s.RedirectStandardOutput=$true;$s.RedirectStandardError=$true
 foreach($a in $GitArgs){[void]$s.ArgumentList.Add($a)}
 $p=[System.Diagnostics.Process]::new();$p.StartInfo=$s;[void]$p.Start();$o=$p.StandardOutput.ReadToEndAsync();$e=$p.StandardError.ReadToEndAsync();$p.WaitForExit();$code=$p.ExitCode;$out=$o.GetAwaiter().GetResult();$err=$e.GetAwaiter().GetResult();$p.Dispose();if($code -ne 0){throw "git failed: $err"};return $out.Trim()
}
$head=Read-Git @('rev-parse','HEAD');$branch=Read-Git @('branch','--show-current')
if($ExpectedHead -and $head -cne $ExpectedHead){throw "HEAD mismatch: $head"}
$tracked=Read-Git @('status','--porcelain','--untracked-files=no')
if($Cargo -and $tracked){throw "Tracked worktree changed before test: $tracked"}
$psi=[System.Diagnostics.ProcessStartInfo]::new();$psi.FileName=$File;$psi.WorkingDirectory=$WorkingDirectory;$psi.UseShellExecute=$false;$psi.CreateNoWindow=$true;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true
foreach($a in $CommandArgs){[void]$psi.ArgumentList.Add($a)}
if($Cargo){
 $psi.Environment['CARGO_TARGET_DIR']=(Join-Path $WorkingDirectory 'codex-owned-d9b4cd0278e9c17d3ecd99906b2a88a4/target')
 $psi.Environment['RUSTC_WRAPPER']='D:/CC/Tools/sccache/sccache.exe'
 $psi.Environment['SCCACHE_DIR']='D:/CC/BuildCache/sccache';$psi.Environment['SCCACHE_CACHE_SIZE']='30G';$psi.Environment['SCCACHE_SERVER_PORT']='42259'
 $psi.Environment['CARGO_BUILD_JOBS']='10';$psi.Environment['NEXTEST_TEST_THREADS']='10';$psi.Environment['RUST_TEST_THREADS']='10';$psi.Environment['CARGO_TERM_COLOR']='never';$psi.Environment['NEXTEST_COLOR']='never'
 $psi.Environment['TEMP']=(Join-Path $WorkingDirectory 'codex-owned-d9b4cd0278e9c17d3ecd99906b2a88a4/fixture-temp');$psi.Environment['TMP']=$psi.Environment['TEMP'];[void][System.IO.Directory]::CreateDirectory($psi.Environment['TEMP'])
}
[void][System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Receipt))
$meta=[ordered]@{file=$File;args=$CommandArgs;cwd=$WorkingDirectory;actual_head=$head;branch=$branch;tracked_changes_before=$tracked;started_at=[DateTimeOffset]::Now.ToString('o');create_no_window=$true;status='starting';pid=$null;exit=$null;ended_at=$null;stdout_file=([System.IO.Path]::GetFileName($Receipt)+'.stdout.txt');stderr_file=([System.IO.Path]::GetFileName($Receipt)+'.stderr.txt')}
if($Cargo){$meta['target_dir']=$psi.Environment['CARGO_TARGET_DIR'];$meta['sccache']='D:/CC/Tools/sccache/sccache.exe';$meta['max_build_jobs']=10;$meta['max_test_threads']=10}
$meta|ConvertTo-Json -Depth 6|Set-Content -LiteralPath ($Receipt+'.json') -Encoding utf8
$outFile=[System.IO.File]::Open($Receipt+'.stdout.txt',[System.IO.FileMode]::CreateNew,[System.IO.FileAccess]::Write,[System.IO.FileShare]::ReadWrite);$errFile=[System.IO.File]::Open($Receipt+'.stderr.txt',[System.IO.FileMode]::CreateNew,[System.IO.FileAccess]::Write,[System.IO.FileShare]::ReadWrite)
$p=[System.Diagnostics.Process]::new();$p.StartInfo=$psi
try {
 [void]$p.Start();$meta['pid']=$p.Id;$meta['status']='running';$meta|ConvertTo-Json -Depth 6|Set-Content -LiteralPath ($Receipt+'.json') -Encoding utf8
 $outTask=$p.StandardOutput.BaseStream.CopyToAsync($outFile);$errTask=$p.StandardError.BaseStream.CopyToAsync($errFile)
 $p.WaitForExit();$meta['exit']=$p.ExitCode
 [System.Threading.Tasks.Task]::WaitAll([System.Threading.Tasks.Task[]]@($outTask,$errTask))
 $outFile.Flush();$errFile.Flush();$meta['status']='exited';$meta['ended_at']=[DateTimeOffset]::Now.ToString('o');$meta['head_after']=Read-Git @('rev-parse','HEAD');$meta['tracked_changes_after']=Read-Git @('status','--porcelain','--untracked-files=no')
 $meta|ConvertTo-Json -Depth 6|Set-Content -LiteralPath ($Receipt+'.json') -Encoding utf8
 [pscustomobject]@{receipt=$Receipt;head=$meta.actual_head;exit=$meta.exit;status=$meta.status;started_at=$meta.started_at;ended_at=$meta.ended_at}|ConvertTo-Json -Compress
} finally {$outFile.Dispose();$errFile.Dispose();$p.Dispose()}

