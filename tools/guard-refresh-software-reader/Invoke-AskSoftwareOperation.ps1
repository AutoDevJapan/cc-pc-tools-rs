function Invoke-AskSoftwareOperation {
    param([string]$Reader,[string]$Operation,[scriptblock]$Parse)
    if($Operation -cnotin @('--verify-ask-privacy-update','--prepare-ask-privacy-update') -or -not $Parse){throw 'ask_software_operation_invalid'}
    $proc=[Diagnostics.Process]::new()
    $proc.StartInfo=[Diagnostics.ProcessStartInfo]::new($Reader)
    $proc.StartInfo.ArgumentList.Add($Operation)
    $proc.StartInfo.UseShellExecute=$false;$proc.StartInfo.CreateNoWindow=$true
    $proc.StartInfo.RedirectStandardOutput=$true;$proc.StartInfo.RedirectStandardError=$true
    try{
        [void]$proc.Start()
        $out=$proc.StandardOutput.ReadToEndAsync();$err=$proc.StandardError.ReadToEndAsync()
        # Producer10s + collection2s + inspection2s + collection2s. A separate
        # outer deadline must not cut off the reader's ordinary recovery result.
        if(-not $proc.WaitForExit(20000)){
            $proc.Kill($true);$proc.WaitForExit()
            # No bytes from an interrupted producer are a successful response.
            # This explicit unknown result is retained by the deployment journal.
            return @{qualified=$false;prepared=$false;reason='qualification_unavailable';software_replace_allowed=$false;data_access_granted=$false;update_performed=$false;reconciliation=@{domain='owner-question-fixed-software-reconcile-v1';Target=@{state='unknown'};Stage=@{state='unknown'};Backup=@{state='unknown'};switch_allowed=$false}}
        }
        $raw=$out.GetAwaiter().GetResult();$errorRaw=$err.GetAwaiter().GetResult()
        return & $Parse $proc.ExitCode $raw $errorRaw
    }finally{$proc.Dispose()}
}
