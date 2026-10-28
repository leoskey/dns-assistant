[CmdletBinding()]
param([string]$ReleaseDir = 'artifacts/releases')

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$version = (Get-Content "$projectRoot/internal/app/VERSION" -Raw).Trim()
$release = [IO.Path]::GetFullPath((Join-Path $projectRoot $ReleaseDir))
$name = "dns-assistant-$version-windows-amd64"
$testDir = Join-Path $release ("check-" + [guid]::NewGuid().ToString('N'))
$installDir = Join-Path $testDir 'installed'
$groupDir = Join-Path ([Environment]::GetFolderPath('Programs')) 'DNS Assistant'
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{9AF6AF86-48D6-4738-853B-56A76998A1E7}_is1'
if (Test-Path $uninstallKey) { throw 'DNS Assistant is already installed; use a clean user account for installer tests.' }
if (Test-Path -LiteralPath $groupDir) { throw 'DNS Assistant Start Menu folder already exists; use a clean user account for installer tests.' }
foreach ($line in Get-Content "$release/SHA256SUMS") {
    $expected, $file = $line -split '  ', 2
    if ((Get-FileHash -LiteralPath (Join-Path $release $file) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
        throw "Checksum mismatch: $file"
    }
}
Expand-Archive -LiteralPath "$release/$name.zip" -DestinationPath "$testDir/portable"
foreach ($file in @('dns-assistant.exe', 'README.md', 'THIRD_PARTY_NOTICES.md', 'licenses')) {
    if (-not (Test-Path -LiteralPath "$testDir/portable/$file")) { throw "Missing package file: $file" }
}
$portableVersion = & "$testDir/portable/dns-assistant.exe" version
if ($LASTEXITCODE -ne 0 -or $portableVersion -notlike "DNS Assistant v$version (*)") { throw 'Portable executable failed.' }
$defaultConfig = & "$testDir/portable/dns-assistant.exe" config | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $defaultConfig.rules.Count -eq 0) { throw 'Default rules unavailable.' }
$installArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=`"$installDir`"")
$installer = Start-Process -FilePath "$release/$name-setup.exe" -ArgumentList $installArgs -WindowStyle Hidden -Wait -PassThru
if ($installer.ExitCode -ne 0) { throw "Installer failed: $($installer.ExitCode)" }
try {
    $installedVersion = & "$installDir/dns-assistant.exe" version
    if ($LASTEXITCODE -ne 0 -or $installedVersion -ne $portableVersion) { throw 'Installed executable differs.' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$groupDir/DNS Assistant.lnk")
    if ($shortcut.TargetPath -ne "$installDir\dns-assistant.exe" -or $shortcut.Arguments -ne "standalone -data `"$env:LOCALAPPDATA\DNS Assistant\data`"") {
        throw "Start Menu shortcut mismatch: target=$($shortcut.TargetPath); arguments=$($shortcut.Arguments); group=$groupDir"
    }
    New-Item -ItemType Directory -Path "$installDir/data/controller" -Force | Out-Null
    'preserve-user-data' | Set-Content -LiteralPath "$installDir/data/controller/keep.txt"
    $upgrade = Start-Process -FilePath "$release/$name-setup.exe" -ArgumentList $installArgs -WindowStyle Hidden -Wait -PassThru
    if ($upgrade.ExitCode -ne 0 -or (Get-Content "$installDir/data/controller/keep.txt" -Raw).Trim() -ne 'preserve-user-data') {
        throw 'Reinstall failed or removed data.'
    }
}
finally {
    $uninstaller = Start-Process -FilePath "$installDir/unins000.exe" -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -WindowStyle Hidden -Wait -PassThru
    if ($uninstaller.ExitCode -ne 0) { throw "Uninstaller failed: $($uninstaller.ExitCode)" }
}
if (Test-Path -LiteralPath "$installDir/dns-assistant.exe") { throw 'Uninstall left the executable behind.' }
if ((Get-Content "$installDir/data/controller/keep.txt" -Raw).Trim() -ne 'preserve-user-data') { throw 'Uninstall removed user data.' }
if (Test-Path $uninstallKey) { throw 'Uninstall left its registration behind.' }
if (Test-Path -LiteralPath $groupDir) { throw 'Uninstall left its Start Menu group behind.' }
Write-Output 'PASS: checksums, portable executable, default rules, install, shortcut, reinstall, uninstall, preserved data.'
