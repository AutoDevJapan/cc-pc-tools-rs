# One-off CLI launcher. Every child is created without a console window.
function Invoke-Hidden {
    param([string]$Exe, [string[]]$Argv, [string]$WorkingDirectory = (Get-Location).Path)
    $si = [Diagnostics.ProcessStartInfo]::new()
    $si.FileName = $Exe
    $si.WorkingDirectory = $WorkingDirectory
    $si.UseShellExecute = $false
    $si.CreateNoWindow = $true
    $si.RedirectStandardOutput = $true
    $si.RedirectStandardError = $true
    foreach ($a in $Argv) { [void]$si.ArgumentList.Add($a) }
    $p = [Diagnostics.Process]::Start($si)
    $stdout = $p.StandardOutput.ReadToEndAsync()
    $stderr = $p.StandardError.ReadToEndAsync()
    $p.WaitForExit()
    [pscustomobject]@{exit=$p.ExitCode; stdout=$stdout.Result; stderr=$stderr.Result}
}
