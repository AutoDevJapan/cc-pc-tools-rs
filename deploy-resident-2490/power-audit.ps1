$ErrorActionPreference = 'Stop'
$updatePath = 'HKLM:\SOFTWARE\Microsoft\WindowsUpdate\UX\Settings'
$updateValues = [ordered]@{}
if (Test-Path -LiteralPath $updatePath) {
    $settings = Get-ItemProperty -LiteralPath $updatePath
    foreach ($name in @('ActiveHoursStart', 'ActiveHoursEnd', 'IsActiveHoursEnabled', 'PauseUpdatesExpiryTime', 'PauseQualityUpdatesEndTime', 'PauseFeatureUpdatesEndTime', 'EngagedRestartDeadline', 'EngagedRestartLastTime', 'RebootDeadline', 'ScheduledRebootTime')) {
        if ($null -ne $settings.PSObject.Properties[$name]) { $updateValues[$name] = $settings.$name }
    }
}
$updateTasks = @(Get-ScheduledTask | Where-Object { $_.TaskPath -eq '\Microsoft\Windows\UpdateOrchestrator\' })
$rebootTasks = @($updateTasks | Where-Object { $_.TaskName -match 'Reboot' } | ForEach-Object {
    $info = Get-ScheduledTaskInfo -InputObject $_
    [ordered]@{ name = $_.TaskName; state = [string]$_.State; next_run = [string]$info.NextRunTime; last_result = $info.LastTaskResult }
})
$power = & "$env:SystemRoot\System32\powercfg.exe" /query SCHEME_CURRENT SUB_SLEEP
if ($LASTEXITCODE -ne 0) { throw 'power_query_failed' }
$winlogon = Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon' -Name AutoAdminLogon -ErrorAction SilentlyContinue
$report = [ordered]@{
    checked_at = [DateTime]::UtcNow.ToString('o')
    update_reboot_required = (Test-Path -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired')
    servicing_reboot_pending = (Test-Path -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')
    update_settings = $updateValues
    update_task_count = $updateTasks.Count
    reboot_tasks = $rebootTasks
    automatic_logon_configured = ($winlogon.AutoAdminLogon -eq '1')
    last_boot = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime().ToString('o')
    sleep_configuration = ($power -join "`n")
    changed_auth_security_settings = $false
}
$report | ConvertTo-Json -Depth 5
