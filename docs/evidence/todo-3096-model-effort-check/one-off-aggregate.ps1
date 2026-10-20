param([Parameter(Mandatory)][string]$EvidenceDirectory)
$ErrorActionPreference='Stop'
$plan=Get-Content -LiteralPath (Join-Path $EvidenceDirectory 'plan.json') -Raw -Encoding utf8|ConvertFrom-Json
$runs=foreach($p in $plan){
 $path=Join-Path $EvidenceDirectory $p.id
 if(-not(Test-Path -LiteralPath ($path+'.json'))){[pscustomobject]@{id=$p.id;expected_head=$p.head;executed=$false};continue}
 $m=Get-Content -LiteralPath ($path+'.json') -Raw -Encoding utf8|ConvertFrom-Json
 $out=Get-Content -LiteralPath ($path+'.stdout.txt') -Raw -Encoding utf8
 $err=Get-Content -LiteralPath ($path+'.stderr.txt') -Raw -Encoding utf8
 $log=[string]$out+[Environment]::NewLine+[string]$err
 $sm=[regex]::Matches($log,'(?m)^\s*Summary\s+\[\s*([0-9.]+)s\]\s+(\d+) tests? run:\s*([^\r\n]+)')
 $tests=@();$runCount=$null;$passed=$null;$failed=$null;$skipped=$null;$summary=$null
 if($sm.Count){
  $s=$sm[$sm.Count-1];$summary=$s.Value.Trim();$runCount=[int]$s.Groups[2].Value;$passed=0;$failed=0;$skipped=0
  foreach($c in [regex]::Matches($s.Groups[3].Value,'(\d+) (passed|failed|skipped)')){switch($c.Groups[2].Value){passed{$passed=[int]$c.Groups[1].Value}failed{$failed=[int]$c.Groups[1].Value}skipped{$skipped=[int]$c.Groups[1].Value}}}
  $seen=@{}
  foreach($t in [regex]::Matches($log,'(?m)^\s*(PASS|FAIL|TIMEOUT|LEAK|ABORT|EXECFAIL)\s+\[([^\]]+)\]\s+\([^)]*\)\s+(\S+)\s+(\S+)\s*$')){
   $key=$t.Groups[3].Value+' '+$t.Groups[4].Value
   if(-not $seen.ContainsKey($key)){$seen[$key]=$true;$tests+=[pscustomobject]@{status=$t.Groups[1].Value;duration=$t.Groups[2].Value.Trim();binary=$t.Groups[3].Value;name=$t.Groups[4].Value}}
  }
  if($tests.Count -ne $runCount){throw "Parsed test count mismatch for $($p.id): $($tests.Count) vs $runCount"}
 }
 $compileOnly=($p.id -eq 'green-07-examples')
 $classification=if($compileOnly -and $m.exit -eq 0){'compile_pass'}elseif($null -ne $runCount -and $m.exit -eq 0 -and $failed -eq 0){'test_pass'}elseif($null -ne $runCount -and $failed -gt 0){'test_assertion_failure'}elseif($m.status -ne 'exited'){'incomplete'}else{'not_started_or_infrastructure_failure'}
 [pscustomobject][ordered]@{id=$p.id;executed=$true;head=$m.actual_head;expected_head=$p.head;head_match=($m.actual_head -ceq $p.head -and $m.head_after -ceq $p.head);source_unchanged=($m.tracked_changes_before -eq '' -and $m.tracked_changes_after -eq '');args=$m.args;exit=$m.exit;classification=$classification;started_at=$m.started_at;ended_at=$m.ended_at;tests_run=$runCount;passed=$passed;failed=$failed;skipped_by_selection=$skipped;summary=$summary;failure_names=@($tests|Where-Object status -ne PASS|ForEach-Object name);tests=$tests;stdout_file=$m.stdout_file;stderr_file=$m.stderr_file}
}
$runs|ConvertTo-Json -Depth 12|Set-Content -LiteralPath (Join-Path $EvidenceDirectory 'results.json') -Encoding utf8
$runs|Select-Object id,exit,classification,tests_run,passed,failed,head_match,source_unchanged|Format-Table -AutoSize
