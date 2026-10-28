[CmdletBinding()]
param(
    [string]$BinaryPath,
    [string]$IsccPath,
    [string]$OutputDir = 'artifacts/releases'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Push-Location $projectRoot
try {
    $version = (Get-Content 'internal/app/VERSION' -Raw).Trim()
    if ($version -notmatch '^\d+\.\d+\.\d+$') { throw 'Invalid application version.' }
    if (-not $IsccPath) {
        $compiler = Get-Command ISCC.exe -ErrorAction SilentlyContinue
        if ($compiler) { $IsccPath = $compiler.Source }
        else {
            $IsccPath = @(
                "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
                "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
            ) | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
        }
    }
    if (-not $IsccPath -or -not (Test-Path -LiteralPath $IsccPath)) {
        throw 'Inno Setup 6 is required to build the installer. Set -IsccPath to ISCC.exe.'
    }
    $releaseDir = [IO.Path]::GetFullPath((Join-Path $projectRoot $OutputDir))
    New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null
    $packageDir = Join-Path $releaseDir "dns-assistant-$version-windows-amd64"
    New-Item -ItemType Directory -Path $packageDir -Force | Out-Null
    if (-not $BinaryPath) {
        & npm.cmd ci --prefix web --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'Frontend dependencies failed.' }
        & npm.cmd run build --prefix web
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
        $revision = & git rev-parse HEAD
        if ($LASTEXITCODE -ne 0) { throw 'Cannot read source revision.' }
        $oldGOOS, $oldGOARCH, $oldCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
        try {
            $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
            & go build -trimpath "-ldflags=-s -w -X dns-assistant/internal/app.Revision=$revision" -o "$packageDir/dns-assistant.exe" ./cmd/dns-assistant
            if ($LASTEXITCODE -ne 0) { throw 'Windows binary build failed.' }
        }
        finally { $env:GOOS = $oldGOOS; $env:GOARCH = $oldGOARCH; $env:CGO_ENABLED = $oldCGO }
    }
    else { Copy-Item -LiteralPath $BinaryPath -Destination "$packageDir/dns-assistant.exe" -Force }
    $binaryVersion = & "$packageDir/dns-assistant.exe" version
    if ($LASTEXITCODE -ne 0 -or $binaryVersion -notlike "DNS Assistant v$version (*)") {
        throw 'Packaged executable version does not match source.'
    }
    Copy-Item -LiteralPath 'docs/windows.md' -Destination "$packageDir/README.md" -Force
    Copy-Item -LiteralPath 'THIRD_PARTY_NOTICES.md' -Destination $packageDir -Force
    Copy-Item -LiteralPath 'licenses' -Destination $packageDir -Recurse -Force
    $archive = "$packageDir.zip"
    Compress-Archive -Path "$packageDir/dns-assistant.exe", "$packageDir/README.md", "$packageDir/THIRD_PARTY_NOTICES.md", "$packageDir/licenses" -DestinationPath $archive -Force
    & $IsccPath "/DAppVersion=$version" "/DPackageDir=$packageDir" "/DReleaseDir=$releaseDir" 'deploy/windows/setup.iss'
    if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed.' }
    $installer = "$packageDir-setup.exe"
    @($archive, $installer) | ForEach-Object {
        $hash = Get-FileHash -LiteralPath $_ -Algorithm SHA256
        "$($hash.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($_))"
    } | Set-Content -LiteralPath "$releaseDir/SHA256SUMS" -Encoding ascii
    Write-Output "Windows packages: $releaseDir"
}
finally { Pop-Location }
