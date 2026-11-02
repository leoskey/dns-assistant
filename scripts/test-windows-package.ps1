[CmdletBinding()]
param([string]$ReleaseDir = 'artifacts/releases', [string]$IsccPath)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$version = (Get-Content "$projectRoot/internal/app/VERSION" -Raw).Trim()
$release = [IO.Path]::GetFullPath((Join-Path $projectRoot $ReleaseDir))
$name = "dns-assistant-$version-windows-amd64"
$testDir = Join-Path $release ("check-" + [guid]::NewGuid().ToString('N'))
$installDir = Join-Path $testDir 'installed'
$testId = 'DNS-Assistant-Test-' + [guid]::NewGuid().ToString('N')
$groupDir = Join-Path ([Environment]::GetFolderPath('Programs')) $testId
$uninstallKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$($testId)_is1"
if ((Test-Path $uninstallKey) -or (Test-Path -LiteralPath $groupDir)) { throw 'Test installation identifier already exists.' }
foreach ($line in Get-Content "$release/SHA256SUMS") {
    $expected, $file = $line -split '  ', 2
    if ((Get-FileHash -LiteralPath (Join-Path $release $file) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
        throw "Checksum mismatch: $file"
    }
}
Expand-Archive -LiteralPath "$release/$name.zip" -DestinationPath "$testDir/portable"
foreach ($file in @('dns-assistant.exe', 'README.md', 'THIRD_PARTY_NOTICES.md', 'licenses', 'Update.cmd', 'update.ps1')) {
    if (-not (Test-Path -LiteralPath "$testDir/portable/$file")) { throw "Missing package file: $file" }
}
$portableVersion = (& "$testDir/portable/dns-assistant.exe" version | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $portableVersion -notlike "DNS Assistant v$version (*)") { throw 'Portable executable failed.' }
$defaultConfig = & "$testDir/portable/dns-assistant.exe" config | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $defaultConfig.rules.Count -eq 0) { throw 'Default rules unavailable.' }
if (-not $IsccPath) {
    $compiler = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($compiler) { $IsccPath = $compiler.Source }
    else {
        $IsccPath = @("${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe") |
            Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    }
}
if (-not $IsccPath) { throw 'Inno Setup 6 is required for isolated installer validation.' }
# Compile the same installer with test-only identity/group to preserve an existing
# application, its uninstall registration, shortcuts and startup preference.
& $IsccPath "/DAppVersion=$version" "/DPackageDir=$release\$name" "/DReleaseDir=$testDir" "/DInstallerAppId=$testId" "/DInstallerGroupName=$testId" "$projectRoot/deploy/windows/setup.iss" > "$testDir/installer-build.log"
if ($LASTEXITCODE -ne 0) { throw 'Isolated installer compilation failed.' }
$testInstaller = Join-Path $testDir "$name-setup.exe"
$installArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=`"$installDir`"")
$installer = Start-Process -FilePath $testInstaller -ArgumentList $installArgs -WindowStyle Hidden -Wait -PassThru
if ($installer.ExitCode -ne 0) { throw "Installer failed: $($installer.ExitCode)" }
try {
    $installedVersion = (& "$installDir/dns-assistant.exe" version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $installedVersion -ne $portableVersion) { throw 'Installed executable differs.' }
    if (-not (Test-Path -LiteralPath "$installDir/installed.flag")) { throw 'Installed data path marker missing.' }
    $updateShortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$groupDir/Update.lnk")
    if ($updateShortcut.TargetPath -ne "$installDir\Update.cmd") { throw 'Update shortcut missing.' }
    $managementShortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$groupDir/Management.lnk")
    if ($managementShortcut.TargetPath -ne "$installDir\dns-assistant.exe") { throw 'Management shortcut must open the native application.' }
    # Exercise migration of the old browser shortcut without touching the real group.
    $legacyShortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$groupDir/Management.url")
    $legacyShortcut.TargetPath = 'http://127.0.0.1:8080'
    $legacyShortcut.Save()
    . "$installDir/update.ps1"
    Update-ManagementShortcut $installDir $groupDir
    if (Test-Path -LiteralPath "$groupDir/Management.url") { throw 'Updater left the old browser shortcut.' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$groupDir/DNS Assistant.lnk")
    if ($shortcut.TargetPath -ne "$installDir\dns-assistant.exe" -or $shortcut.Arguments -ne "standalone -data `"$env:LOCALAPPDATA\DNS Assistant\data`"") {
        throw "Start Menu shortcut mismatch: target=$($shortcut.TargetPath); arguments=$($shortcut.Arguments); group=$groupDir"
    }
    New-Item -ItemType Directory -Path "$installDir/data/controller" -Force | Out-Null
    'preserve-user-data' | Set-Content -LiteralPath "$installDir/data/controller/keep.txt"
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$installDir/update.ps1" -Package "$release/$name.zip" -Checksums "$release/SHA256SUMS"
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath "$installDir/installed.flag")) { throw 'Installed edition update failed.' }
    $legacyShortcut.Save()
    $upgrade = Start-Process -FilePath $testInstaller -ArgumentList $installArgs -WindowStyle Hidden -Wait -PassThru
    if (Test-Path -LiteralPath "$groupDir/Management.url") { throw 'Installer left the old browser shortcut.' }
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
$resolvedTest = (Resolve-Path -LiteralPath $testDir).Path
if ((Split-Path $resolvedTest -Parent) -eq $release -and (Split-Path $resolvedTest -Leaf) -match '^check-[a-f0-9]{32}$') {
    Remove-Item -LiteralPath $resolvedTest -Recurse -Force
}
