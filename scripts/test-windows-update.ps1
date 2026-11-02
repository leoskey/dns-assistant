[CmdletBinding()]
param([string]$ReleaseDir = 'artifacts/releases')

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$release = [IO.Path]::GetFullPath((Join-Path $projectRoot $ReleaseDir))
$version = (Get-Content "$projectRoot/internal/app/VERSION" -Raw).Trim()
$archive = Join-Path $release "dns-assistant-$version-windows-amd64.zip"
$testRoot = Join-Path $release ('update-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot | Out-Null
. "$projectRoot/deploy/windows/update.ps1"

function Expect-Failure([scriptblock]$Action, [string]$Message) {
    $failed = $false
    try { & $Action }
    catch { $failed = $true; if ($_.Exception.Message -notlike "*$Message*") { throw "Unexpected error: $_" } }
    if (-not $failed) { throw "Expected failure: $Message" }
}

function Write-Manifest([string]$ArchivePath, [string]$ManifestPath) {
    "$((Get-FileHash -LiteralPath $ArchivePath).Hash)  $([IO.Path]::GetFileName($ArchivePath))" | Set-Content -LiteralPath $ManifestPath -Encoding ascii
}

try {
    $initialStaging = @(Get-ChildItem -LiteralPath ([IO.Path]::GetTempPath()) -Directory -Filter 'dau-*' | ForEach-Object FullName)
    $InstallDir = Join-Path $testRoot 'portable with spaces'
    Expand-Archive -LiteralPath $archive -DestinationPath $InstallDir
    New-Item -ItemType Directory -Path "$InstallDir/data/controller" -Force | Out-Null
    'user-data' | Set-Content -LiteralPath "$InstallDir/data/controller/keep.txt"
    'installed-marker' | Set-Content -LiteralPath "$InstallDir/installed.flag"
    'previous-readme' | Set-Content -LiteralPath "$InstallDir/README.md"
    $Package = $archive
    $Checksums = Join-Path $release 'SHA256SUMS'
    # Exercise the actual entrypoint with a package, spaces in paths and no network.
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$InstallDir/update.ps1" -Package $Package -Checksums $Checksums
    if ($LASTEXITCODE -ne 0) { throw 'Offline updater entrypoint failed.' }
    if ((Get-Content "$InstallDir/data/controller/keep.txt" -Raw).Trim() -ne 'user-data') { throw 'Data changed.' }
    if ((Get-Content "$InstallDir/installed.flag" -Raw).Trim() -ne 'installed-marker') { throw 'Installation marker changed.' }
    if ((Get-FileHash "$InstallDir/dns-assistant.exe").Hash -ne (Get-FileHash "$release/dns-assistant-$version-windows-amd64/dns-assistant.exe").Hash) { throw 'Executable differs from package.' }
    $backup = @(Get-ChildItem -LiteralPath "$InstallDir/.update-backups" -File)
    if ($backup.Count -ne 1) { throw 'Previous program backup missing.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $savedZip = [IO.Compression.ZipFile]::OpenRead($backup[0].FullName)
    $reader = [IO.StreamReader]::new($savedZip.GetEntry('README.md').Open())
    try { if ($reader.ReadToEnd().Trim() -ne 'previous-readme') { throw 'Backup contents changed.' } }
    finally { $reader.Dispose(); $savedZip.Dispose() }
    if (@(Get-ChildItem -LiteralPath $InstallDir -Directory -Force | Where-Object Name -Match '^\.update-[a-f0-9]{32}$').Count) { throw 'Staging directory leaked.' }

    $lock = [IO.File]::Open("$InstallDir/.dns-assistant.lock", 'OpenOrCreate', 'ReadWrite', 'None')
    try { Expect-Failure { Invoke-Update } 'Stop DNS Assistant' }
    finally { $lock.Dispose() }
    $badManifest = Join-Path $testRoot 'bad-checksums'
    (('0' * 64) + '  ' + [IO.Path]::GetFileName($archive)) | Set-Content -LiteralPath $badManifest
    Expect-Failure { Expand-Update $archive $badManifest "$testRoot/rejected" } 'checksum mismatch'
    '' | Set-Content -LiteralPath $badManifest
    Expect-Failure { Expand-Update $archive $badManifest "$testRoot/rejected" } 'exactly one'
    $line = "$((Get-FileHash $archive).Hash)  $([IO.Path]::GetFileName($archive))"
    @($line, $line) | Set-Content -LiteralPath $badManifest
    Expect-Failure { Expand-Update $archive $badManifest "$testRoot/rejected" } 'exactly one'
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    foreach ($entryName in @('../escape.txt', 'data/keep.txt', 'installed.flag', 'C:/escape.txt')) {
        $badZip = Join-Path $testRoot ([guid]::NewGuid().ToString('N') + '.zip')
        $zip = [IO.Compression.ZipFile]::Open($badZip, 'Create')
        $entry = $zip.CreateEntry($entryName)
        $zip.Dispose()
        Write-Manifest $badZip $badManifest
        Expect-Failure { Expand-Update $badZip $badManifest "$testRoot/rejected" } 'Unexpected'
    }
    $missing = Join-Path $testRoot 'missing.zip'
    $zip = [IO.Compression.ZipFile]::Open($missing, 'Create')
    $entry = $zip.CreateEntry('README.md')
    $zip.Dispose()
    Write-Manifest $missing $badManifest
    Expect-Failure { Expand-Update $missing $badManifest "$testRoot/rejected" } 'missing dns-assistant.exe'

    # Force a real mid-copy failure after the executable has already changed.
    $source = Join-Path $testRoot 'rollback-source'
    $target = Join-Path $testRoot 'rollback-target'
    New-Item -ItemType Directory -Path $source, $target | Out-Null
    foreach ($file in @('dns-assistant.exe', 'README.md')) {
        'old' | Set-Content -LiteralPath "$target/$file"
        'new' | Set-Content -LiteralPath "$source/$file"
    }
    $held = [IO.File]::Open("$target/README.md", 'Open', 'Read', 'Read')
    try { Expect-Failure { Install-Update $source $target "$testRoot/rollback-backup.zip" } 'previous program files restored' }
    finally { $held.Dispose() }
    foreach ($file in @('dns-assistant.exe', 'README.md')) {
        if ((Get-Content "$target/$file" -Raw).Trim() -ne 'old') { throw "Rollback failed: $file" }
    }

    # Exercise release selection and downloads using controlled HTTP responses.
    $Package = ''
    $Checksums = ''
    $metadata = @{ tag_name = "v$version"; draft = $false; prerelease = $false; assets = @() }
    $downloadCount = 0
    function Invoke-RestMethod { param($Uri, $Headers, $TimeoutSec); return $metadata }
    function Invoke-WebRequest {
        param($Uri, $Headers, $OutFile, $TimeoutSec, [switch]$UseBasicParsing)
        $script:downloadCount++
        if ($Uri.EndsWith('/SHA256SUMS')) { Copy-Item -LiteralPath "$script:release/SHA256SUMS" -Destination $OutFile }
        else { Copy-Item -LiteralPath $script:archive -Destination $OutFile }
    }
    try {
        Invoke-Update
        if ($downloadCount -ne 0) { throw 'Current version unexpectedly downloaded a package.' }
        $metadata.prerelease = $true
        Expect-Failure { Invoke-Update } 'Invalid stable release'
        $metadata.prerelease = $false
        $metadata.tag_name = 'v999.0.0'
        Expect-Failure { Invoke-Update } 'Missing or unexpected'
        $metadata.tag_name = "v$version"
        $assetName = [IO.Path]::GetFileName($archive)
        $metadata.assets = @($assetName, 'SHA256SUMS') | ForEach-Object {
            @{ name = $_; browser_download_url = "https://github.com/leoskey/dns-assistant/releases/download/v$version/$_" }
        }
        $originalVersionReader = ${function:Read-AppVersion}
        function Read-AppVersion([string]$Executable) {
            if ($Executable -eq (Join-Path $InstallDir 'dns-assistant.exe')) { return [version]'0.0.0' }
            return & $originalVersionReader $Executable
        }
        try {
            Invoke-Update
            if ($downloadCount -ne 2) { throw 'Online update did not download the package and checksum.' }
        }
        finally { Set-Item Function:Read-AppVersion $originalVersionReader }
    }
    finally { Remove-Item Function:Invoke-RestMethod, Function:Invoke-WebRequest }
    $leakedStaging = @(Get-ChildItem -LiteralPath ([IO.Path]::GetTempPath()) -Directory -Filter 'dau-*' | Where-Object { $initialStaging -notcontains $_.FullName })
    if ($leakedStaging.Count) { throw 'Temporary update directory leaked.' }
    Write-Output 'PASS: offline/controlled-online updates, current release, malformed/missing release assets, preserved data/installation marker, backup, lock, checksum rejection, unsafe archives, missing executable, mid-copy rollback, staging cleanup.'
}
finally {
    $resolved = (Resolve-Path -LiteralPath $testRoot).Path
    if ((Split-Path $resolved -Parent) -eq $release -and (Split-Path $resolved -Leaf) -match '^update-test-[a-f0-9]{32}$') {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
