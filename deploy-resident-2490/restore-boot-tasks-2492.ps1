$ErrorActionPreference = 'Stop'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490\boot-tasks-before-2492'
$user = (Get-ScheduledTask -TaskName 'CC-ResidentGuard').Principal.UserId
$names = @('CC-ResidentGuard','local-ai-web-autostart-10012','opencodex-proxy','godd-ao-semantic-proxy','CC-OcxHealthWatchdog')
foreach ($name in $names) {
    $path = Join-Path $root ($name + '.xml')
    [xml]$xml = Get-Content -LiteralPath $path -Raw -Encoding Unicode
    $namespace = [Xml.XmlNamespaceManager]::new($xml.NameTable)
    $namespace.AddNamespace('t','http://schemas.microsoft.com/windows/2004/02/mit/task')
    $principal = $xml.SelectSingleNode('/t:Task/t:Principals/t:Principal',$namespace)
    if ($principal.LogonType -ne 'InteractiveToken' -or $principal.RunLevel -ne 'LeastPrivilege') { throw 'boot_backup_principal_mismatch' }
    $current = Get-ScheduledTask -TaskName $name
    if ($current.Principal.UserId -ne $user -or $current.Principal.LogonType -ne 'S4U' -or $current.Principal.RunLevel -ne 'Limited') { throw 'boot_current_principal_changed' }
}
foreach ($name in $names) { Register-ScheduledTask -TaskName $name -Xml (Get-Content -LiteralPath (Join-Path $root ($name + '.xml')) -Raw -Encoding Unicode) -Force | Out-Null }
[ordered]@{ original_interactive_task_definitions_restored=$true; processes_stopped=$false; credentials_supplied=$false } | ConvertTo-Json -Compress
