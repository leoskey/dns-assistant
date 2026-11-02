[CmdletBinding()]
param([string]$ReleaseDir = 'artifacts/releases')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
if (-not (Get-Process explorer -ErrorAction SilentlyContinue)) { throw 'Tray integration tests require a logged-in Windows desktop with Explorer.' }
$release = [IO.Path]::GetFullPath((Join-Path $projectRoot $ReleaseDir))
$version = (Get-Content "$projectRoot/internal/app/VERSION" -Raw).Trim()
$name = "dns-assistant-$version-windows-amd64"
$oldBinary, $oldPackage = $env:DNS_ASSISTANT_DESKTOP_BINARY, $env:DNS_ASSISTANT_DESKTOP_PACKAGE
Push-Location $projectRoot
try {
    $env:DNS_ASSISTANT_DESKTOP_BINARY = Join-Path $release "$name/dns-assistant.exe"
    $env:DNS_ASSISTANT_DESKTOP_PACKAGE = Join-Path $release "$name.zip"
    & go test ./cmd/dns-assistant -run 'TestDesktopUpdateCheck|TestStartupToggleAndMenu|TestPackagedWindowsGUI|TestPackagedConsoleMode|TestPackagedTrayLifecycle|TestPackagedManagementWindow|TestTrayUpdateRestart' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'Tray integration failed.' }
}
finally {
    $env:DNS_ASSISTANT_DESKTOP_BINARY, $env:DNS_ASSISTANT_DESKTOP_PACKAGE = $oldBinary, $oldPackage
    Pop-Location
}
