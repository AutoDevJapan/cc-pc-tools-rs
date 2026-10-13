#!/usr/bin/env pwsh
# opencodex codex autostart shim
$hadApiAuthToken = Test-Path Env:\OPENCODEX_API_AUTH_TOKEN
$priorApiAuthToken = $env:OPENCODEX_API_AUTH_TOKEN
try {
if (-not $env:OPENCODEX_API_AUTH_TOKEN -and (Test-Path -LiteralPath 'C:\Users\makhmeto\.opencodex\service-api-token')) {
  $env:OPENCODEX_API_AUTH_TOKEN = (Get-Content -Raw -LiteralPath 'C:\Users\makhmeto\.opencodex\service-api-token').Trim()
}
$internalCommands = @('app-server', 'archive', 'apply', 'cloud', 'completion', 'debug', 'delete', 'doctor', 'exec-server', 'features', 'fork', 'help', 'login', 'logout', 'mcp', 'plugin', 'sandbox', 'unarchive', 'update')
$valueOptions = @('-c', '--config', '--enable', '--disable', '--remote', '--remote-auth-token-env', '-i', '--image', '-m', '--model', '--local-provider', '-p', '--profile', '-s', '--sandbox', '-C', '--cd', '--add-dir', '-a', '--ask-for-approval')
$subcommand = ""
$skipNext = $false
foreach ($argValue in $args) {
  $argText = [string]$argValue
  if ($skipNext) { $skipNext = $false; continue }
  if ($argText -eq "--") { break }
  if ($valueOptions -contains $argText) { $skipNext = $true; continue }
  if (@("--help", "-h", "--version", "-V") -contains $argText) { $subcommand = $argText; break }
  if ($argText.StartsWith("-")) { continue }
  $subcommand = $argText
  break
}
$skipEnsure = $env:OCX_SHIM_BYPASS -or $internalCommands -contains $subcommand -or @("--help", "-h", "--version", "-V") -contains $subcommand
if (-not $skipEnsure) {
  $priorRuntimeSource = $env:OCX_BUN_RUNTIME_SOURCE
  $priorRuntimePath = $env:OCX_BUN_RUNTIME_PATH
  $env:OCX_BUN_RUNTIME_SOURCE = 'bundled'
  $env:OCX_BUN_RUNTIME_PATH = 'C:\Users\makhmeto\AppData\Roaming\npm\node_modules\@bitkyc08\opencodex\node_modules\bun\bin\bun.exe'
  $ocxEnsureFailed = $false
  # Caught, not propagated: a throwing ensure used to escape this wrapper and Codex never
  # launched at all, which is a lockout produced by the autostart helper itself (#5261).
  try { & 'C:\Users\makhmeto\AppData\Roaming\npm\node_modules\@bitkyc08\opencodex\node_modules\bun\bin\bun.exe' 'C:\Users\makhmeto\AppData\Roaming\npm\node_modules\@bitkyc08\opencodex\src\cli\index.ts' ensure *> $null; if ($LASTEXITCODE -ne 0) { $ocxEnsureFailed = $true } }
  catch { $ocxEnsureFailed = $true }
  finally {
    if ($null -eq $priorRuntimeSource) { Remove-Item Env:\OCX_BUN_RUNTIME_SOURCE -ErrorAction SilentlyContinue }
    else { $env:OCX_BUN_RUNTIME_SOURCE = $priorRuntimeSource }
    if ($null -eq $priorRuntimePath) { Remove-Item Env:\OCX_BUN_RUNTIME_PATH -ErrorAction SilentlyContinue }
    else { $env:OCX_BUN_RUNTIME_PATH = $priorRuntimePath }
  }
  if ($ocxEnsureFailed) { [Console]::Error.WriteLine('opencodex: proxy autostart failed; launching Codex anyway. Run ''ocx doctor'' for details, or ''ocx restore'' to hand Codex back to its own account.') }
}
$progressLauncher = 'D:\CC\Tools\codex-progress-tui\codex-launch.exe'
if (Test-Path -LiteralPath $progressLauncher) {
  & $progressLauncher @args
} else {
  & 'C:\Users\makhmeto\AppData\Roaming\npm\codex.opencodex-real.ps1' @args
}
$codexExitCode = $LASTEXITCODE
} finally {
  if ($hadApiAuthToken) { $env:OPENCODEX_API_AUTH_TOKEN = $priorApiAuthToken }
  else { Remove-Item Env:\OPENCODEX_API_AUTH_TOKEN -ErrorAction SilentlyContinue }
}
exit $codexExitCode
