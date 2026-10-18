function Invoke-Hidden {
    param(
        [Parameter(Mandatory)][string]$File,
        [string[]]$Arguments = @(),
        [int]$TimeoutSeconds = 180
    )
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $File
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    [void]$process.Start()
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    if (!$process.WaitForExit($TimeoutSeconds * 1000)) {
        $process.Kill($true)
        $process.WaitForExit()
        throw "Finite command timeout: $File"
    }
    $result = [ordered]@{
        command = @($File) + $Arguments
        exit = $process.ExitCode
        stdout = $stdout.GetAwaiter().GetResult()
        stderr = $stderr.GetAwaiter().GetResult()
    }
    $process.Dispose()
    [pscustomobject]$result
}
