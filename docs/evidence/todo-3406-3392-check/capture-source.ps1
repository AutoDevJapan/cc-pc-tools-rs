$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$workspace = [IO.Path]::GetFullPath((Join-Path $evidence '../..')).TrimEnd('\','/')
$green = '490ffd47e78aa0c5f7a4c5d2f054bddd05746314'
$head = (git -C $workspace rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $head -ne $green) { throw 'Expected fixed Green HEAD' }
$dirty = @(git -C $workspace status --porcelain=v1 --untracked-files=no)
if ($LASTEXITCODE -ne 0 -or $dirty.Count -ne 0) { throw 'Tracked source changed' }
$metadata = Get-Content -LiteralPath (Join-Path $evidence 'metadata.stdout.log') -Raw | ConvertFrom-Json
$metaResult = Get-Content -LiteralPath (Join-Path $evidence 'metadata.result.json') -Raw | ConvertFrom-Json
if ($metaResult.status -ne 'exited' -or $metaResult.exit_code -ne 0) { throw 'Metadata command did not succeed' }
$pending = [Collections.Generic.Queue[object]]::new()
$seen = [Collections.Generic.Dictionary[string,object]]::new([StringComparer]::OrdinalIgnoreCase)
$rootPackages = @($metadata.packages | Where-Object name -eq 'cc-ask')
if ($rootPackages.Count -ne 1) { throw 'cc-ask package was not uniquely resolved' }
$pending.Enqueue($rootPackages[0])
while ($pending.Count -gt 0) {
    $package = $pending.Dequeue()
    $manifest = [IO.Path]::GetFullPath($package.manifest_path)
    if ($seen.ContainsKey($manifest)) { continue }
    if (-not $manifest.StartsWith($workspace + '\',[StringComparison]::OrdinalIgnoreCase) -and
        -not $manifest.StartsWith($workspace + '/',[StringComparison]::OrdinalIgnoreCase)) { throw 'Local dependency is outside this worktree' }
    $seen.Add($manifest,$package)
    foreach ($dependency in $package.dependencies) {
        if (-not $dependency.path) { continue }
        $dependencyManifest = [IO.Path]::GetFullPath((Join-Path $dependency.path 'Cargo.toml'))
        $matches = @($metadata.packages | Where-Object { [IO.Path]::GetFullPath($_.manifest_path) -eq $dependencyManifest })
        if ($matches.Count -ne 1) { throw 'Local dependency manifest was not uniquely resolved' }
        $pending.Enqueue($matches[0])
    }
}
$packages = @($seen.Values | Sort-Object name | ForEach-Object {
    [pscustomobject]@{ name=$_.name; version=$_.version; manifest=[IO.Path]::GetRelativePath($workspace,$_.manifest_path).Replace('\','/') }
})
$roots = @($packages | ForEach-Object { [IO.Path]::GetDirectoryName($_.manifest).Replace('\','/') })
$commonInputs = @('Cargo.toml','Cargo.lock','.cargo/config','.cargo/config.toml','.config/nextest.toml','rust-toolchain','rust-toolchain.toml')
$pathspec = @($commonInputs) + $roots
$treeLines = @(git -C $workspace -c core.quotepath=false ls-tree -r --full-tree $head -- @pathspec)
if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate source inputs' }
$files = @($treeLines | ForEach-Object {
    if ($_ -notmatch '^([0-9]+) blob ([0-9a-f]{40})\t(.+)$') { throw 'Unexpected tree entry' }
    $mode = $Matches[1]
    $blob = $Matches[2]
    $relative = $Matches[3]
    if ($mode -eq '120000') { throw 'Source symlink needs explicit inspection' }
    $absolute = Join-Path $workspace $relative
    [pscustomobject]@{ path=$relative; git_mode=$mode; git_blob_sha1=$blob; bytes=(Get-Item -LiteralPath $absolute).Length; materialized_sha256=(Get-FileHash -LiteralPath $absolute -Algorithm SHA256).Hash.ToLowerInvariant() }
} | Sort-Object path)
if ($files.Count -eq 0) { throw 'Empty source input closure' }
$ownership = Get-Content -LiteralPath (Join-Path $evidence 'cargo-ownership.json') -Raw | ConvertFrom-Json
$depInfoPath = Join-Path $ownership.path 'release/discord_ask.d'
$depInfo = Get-Content -LiteralPath $depInfoPath -Raw
$depInfo | Set-Content -LiteralPath (Join-Path $evidence 'release.dep-info.txt') -Encoding utf8NoBOM
$inputLine = @($depInfo -split '\r?\n' | Where-Object { $_ -match '^.+discord_ask\.exe: ' })
if ($inputLine.Count -ne 1) { throw 'Cannot identify release dep-info input line' }
$inputPart = $inputLine[0] -replace '^.+discord_ask\.exe: ',''
$depPaths = @([regex]::Matches($inputPart,'(?:\\ |[^ ])+') | ForEach-Object { $_.Value.Replace('\ ',' ') })
$pathsFromDepInfo = @($depPaths | ForEach-Object {
    $absolute = [IO.Path]::GetFullPath($_)
    $relative = [IO.Path]::GetRelativePath($workspace,$absolute).Replace('\','/')
    if ($relative -eq '..' -or $relative.StartsWith('../') -or [IO.Path]::IsPathRooted($relative)) { throw 'Release dep-info has an external source input' }
    if ($relative -notin $files.path) { throw "Unrecorded release source input: $relative" }
    $relative
} | Sort-Object -Unique)
$toolPaths = @('D:/CC/Tools/codex-jobs/cargo_gate.exe','D:/CC/Tools/sccache/sccache.exe')
$toolHashes = @($toolPaths | ForEach-Object { [pscustomobject]@{path=$_;sha256=(Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant()} })
$closure = [ordered]@{
    schema = 'todo3406-fixed-source-inputs-v1'
    todo_id = 3406
    job = $env:CC_JOB_ID
    source_head = $head
    source_tree = (git -C $workspace rev-parse 'HEAD^{tree}').Trim()
    local_packages = $packages
    files = $files
    file_count = $files.Count
    dep_info_inputs = $pathsFromDepInfo
    dep_info_input_count = $pathsFromDepInfo.Count
    dependency_identity = 'Registry dependencies and checksums are pinned by the recorded Cargo.lock and --locked commands; raw registry caches are not copied.'
    materialized_source = 'SHA256 records the bytes in this Windows checkout; Git blob IDs record canonical source bytes.'
    tools = $toolHashes
    compiler = 'toolchain.json'
    environment = @{ RUSTC_WRAPPER='D:/CC/Tools/sccache/sccache.exe';SCCACHE_DIR='D:/CC/BuildCache/sccache';SCCACHE_CACHE_SIZE='30G';SCCACHE_SERVER_PORT='42259';CARGO_TARGET_DIR=$ownership.path }
}
$closurePath = Join-Path $evidence 'input-closure.json'
$closure | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $closurePath -Encoding utf8NoBOM
[pscustomobject]@{source_head=$head;local_packages=@($packages.name);file_count=$files.Count;dep_info_input_count=$pathsFromDepInfo.Count;closure_sha256=(Get-FileHash -LiteralPath $closurePath -Algorithm SHA256).Hash.ToLowerInvariant()} | ConvertTo-Json -Depth 5
