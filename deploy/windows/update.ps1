[CmdletBinding()]
param(
    [string]$Package,
    [string]$Checksums,
    [string]$InstallDir,
    [int]$WaitForProcessId,
    [string]$RestartArguments,
    [string]$DataDir,
    [switch]$Notify
)

$ErrorActionPreference = 'Stop'
if (-not $InstallDir) { $InstallDir = $PSScriptRoot }

function Get-UpdateFileHash([string]$Path) {
    # Avoid dependence on module auto-loading in the hidden updater process.
    $stream = [IO.File]::OpenRead($Path)
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try { return ([BitConverter]::ToString($sha256.ComputeHash($stream))).Replace('-', '') }
    finally { $sha256.Dispose(); $stream.Dispose() }
}

function Read-AppVersion([string]$Executable) {
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = $Executable
    $start.Arguments = 'version'
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $process = [Diagnostics.Process]::Start($start)
    try {
        if (-not $process.WaitForExit(10000)) { $process.Kill(); throw 'Application version check timed out.' }
        $output = $process.StandardOutput.ReadToEnd().Trim()
        $exitCode = $process.ExitCode
    }
    finally { $process.Dispose() }
    if ($exitCode -ne 0 -or $output -notmatch '^DNS Assistant v(\d+\.\d+\.\d+) \(.+\)$') {
        throw "Invalid application executable: $Executable"
    }
    return [version]$Matches[1]
}

function Assert-PlainPath([string]$Path) {
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    if ($item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw "Update does not follow symbolic links or junctions: $Path"
    }
}

function Expand-Update([string]$Archive, [string]$Manifest, [string]$Destination) {
    $name = [IO.Path]::GetFileName($Archive)
    $matchesFound = @(Get-Content -LiteralPath $Manifest | Where-Object { $_ -match ('^[a-fA-F0-9]{64}  ' + [regex]::Escape($name) + '$') })
    if ($matchesFound.Count -ne 1) { throw 'SHA256SUMS must contain exactly one matching package checksum.' }
    $expected = $matchesFound[0].Substring(0,64)
    if ((Get-UpdateFileHash $Archive) -ne $expected) { throw 'Package checksum mismatch; no files changed.' }
    if ((Get-Item -LiteralPath $Archive).Length -gt 256MB) { throw 'Update archive is too large.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($Archive)
    try {
        $names = @{}
        $total = 0L
        foreach ($entry in $zip.Entries) {
            $relative = $entry.FullName.Replace('\', '/')
            # The updater can replace only distributed program files, never data,
            # installation markers, backups or arbitrary paths from an archive.
            if ($relative -notmatch '^(dns-assistant\.exe|README\.md|THIRD_PARTY_NOTICES\.md|Update\.cmd|update\.ps1|licenses/|licenses/(?:[a-zA-Z0-9_@.+-]+/)*[a-zA-Z0-9_@.+-]+/?)$' -or
                $relative.Split('/') -contains '..' -or $relative.Split('/') -contains '.' -or $names.ContainsKey($relative)) {
                throw "Unexpected or duplicate package entry: $relative"
            }
            $names[$relative] = $true
            $total += $entry.Length
            if ($entry.Length -gt 128MB -or $total -gt 512MB -or $names.Count -gt 5000) { throw 'Expanded package is too large.' }
        }
        foreach ($required in @('dns-assistant.exe', 'README.md', 'THIRD_PARTY_NOTICES.md', 'Update.cmd', 'update.ps1')) {
            if (-not $names.ContainsKey($required)) { throw "Package is missing $required" }
        }
        foreach ($entry in $zip.Entries) {
            $relative = $entry.FullName.Replace('\', '/')
            $target = Join-Path $Destination $relative
            if ($relative.EndsWith('/')) { New-Item -ItemType Directory -Path $target -Force | Out-Null; continue }
            New-Item -ItemType Directory -Path (Split-Path $target -Parent) -Force | Out-Null
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $target, $false)
        }
    }
    finally { $zip.Dispose() }
}

function Install-Update([string]$Source, [string]$Target, [string]$Backup) {
    $changed = [Collections.Generic.List[object]]::new()
    $files = @(Get-ChildItem -LiteralPath $Source -Recurse -File)
    # Check every destination before making changes, including parent directories.
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($Source.Length + 1)
        $destination = Join-Path $Target $relative
        $ancestor = $destination
        while ($ancestor.Length -ge $Target.Length) {
            Assert-PlainPath $ancestor
            $ancestor = Split-Path $ancestor -Parent
        }
    }
    New-Item -ItemType Directory -Path (Split-Path $Backup -Parent) -Force | Out-Null
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    # A ZIP avoids extending the installed path with another deep directory tree.
    # Finish the complete program backup before replacing even the first file.
    $savedZip = [IO.Compression.ZipFile]::Open($Backup, 'Create')
    $savedHashes = @{}
    try {
        foreach ($file in $files) {
            $relative = $file.FullName.Substring($Source.Length + 1)
            $destination = Join-Path $Target $relative
            if (Test-Path -LiteralPath $destination) {
                $savedHashes[$relative] = Get-UpdateFileHash $destination
                [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($savedZip, $destination, $relative.Replace('\', '/')) | Out-Null
            }
        }
    }
    finally { $savedZip.Dispose() }
    $script:UpdateSafeToRestart = $false
    try {
        foreach ($file in $files) {
            $relative = $file.FullName.Substring($Source.Length + 1)
            $destination = Join-Path $Target $relative
            $existed = $savedHashes.ContainsKey($relative)
            New-Item -ItemType Directory -Path (Split-Path $destination -Parent) -Force | Out-Null
            # Register before copying so even a partial write is restored.
            $changed.Add(@{ Path = $destination; Relative = $relative; Existed = $existed })
            Copy-Item -LiteralPath $file.FullName -Destination $destination -Force
        }
        $script:UpdateSafeToRestart = $true
    }
    catch {
        $failure = $_
        $rollbackErrors = @()
        $savedZip = [IO.Compression.ZipFile]::OpenRead($Backup)
        try {
            for ($i = $changed.Count - 1; $i -ge 0; $i--) {
                $change = $changed[$i]
                try {
                    if ($change.Existed) {
                        if (-not (Test-Path -LiteralPath $change.Path) -or
                            (Get-UpdateFileHash $change.Path) -ne $savedHashes[$change.Relative]) {
                            $entry = $savedZip.GetEntry($change.Relative.Replace('\', '/'))
                            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $change.Path, $true)
                        }
                    }
                    elseif (Test-Path -LiteralPath $change.Path) { Remove-Item -LiteralPath $change.Path -Force }
                }
                catch { $rollbackErrors += $_.Exception.Message }
            }
        }
        finally { $savedZip.Dispose() }
        if ($rollbackErrors.Count) { throw "Update failed: $failure. Restore program files from $Backup. Rollback errors: $rollbackErrors" }
        $script:UpdateSafeToRestart = $true
        throw "Update failed; previous program files restored: $failure"
    }
}

function Wait-ForApplicationExit {
    if ($WaitForProcessId -le 0) { return }
    $process = Get-Process -Id $WaitForProcessId -ErrorAction SilentlyContinue
    if (-not $process) { return }
    try {
        $expected = Join-Path (Resolve-Path -LiteralPath $InstallDir).Path 'dns-assistant.exe'
        if (-not $process.HasExited -and $process.Path -ne $expected) { throw 'Update parent is not this application.' }
        if (-not $process.WaitForExit(60000)) { throw 'Application did not stop within 60 seconds; update cancelled.' }
    }
    finally { $process.Dispose() }
}

function Backup-UpdateData([string]$Directory, [string]$Backup, [string]$Work) {
    $dataRoot = (Resolve-Path -LiteralPath $Directory).Path.TrimEnd('\', '/')
    Assert-PlainPath $dataRoot
    $excluded = (Split-Path $Backup -Parent).TrimEnd('\', '/') + '\'
    $temporary = Join-Path $Work 'data-backup.zip'
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::Open($temporary, 'Create')
    try {
        foreach ($file in Get-ChildItem -LiteralPath $dataRoot -Recurse -Force -File) {
            if ($file.FullName.StartsWith($excluded, [StringComparison]::OrdinalIgnoreCase)) { continue }
            $ancestor = $file.FullName
            while ($ancestor.Length -ge $dataRoot.Length) { Assert-PlainPath $ancestor; $ancestor = Split-Path $ancestor -Parent }
            $relative = $file.FullName.Substring($dataRoot.Length + 1).Replace('\', '/')
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $file.FullName, $relative) | Out-Null
        }
    }
    finally { $zip.Dispose() }
    New-Item -ItemType Directory -Path (Split-Path $Backup -Parent) -Force | Out-Null
    Move-Item -LiteralPath $temporary -Destination $Backup
}

function Restart-Application {
    if (-not $RestartArguments) { return }
    $arguments = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($RestartArguments))
    if (-not $arguments.StartsWith('standalone ')) { throw 'Invalid restart arguments.' }
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = Join-Path (Resolve-Path -LiteralPath $InstallDir).Path 'dns-assistant.exe'
    $start.WorkingDirectory = (Resolve-Path -LiteralPath $InstallDir).Path
    $start.Arguments = $arguments
    # Launch independently of the updater's redirected console handles. Otherwise
    # callers capturing updater output wait until the restarted service exits.
    $start.UseShellExecute = $true
    $start.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    $process = [Diagnostics.Process]::Start($start)
    Write-Output "Restarted PID: $($process.Id)"
    $process.Dispose()
}

function Update-ManagementShortcut([string]$Root, [string]$Group = (Join-Path ([Environment]::GetFolderPath('Programs')) 'DNS Assistant')) {
    if (-not (Test-Path -LiteralPath (Join-Path $Root 'installed.flag'))) { return }
    $mainPath = Join-Path $Group 'DNS Assistant.lnk'
    if (-not (Test-Path -LiteralPath $mainPath)) { return }
    Assert-PlainPath $Group
    $shell = New-Object -ComObject WScript.Shell
    $main = $shell.CreateShortcut($mainPath)
    $executable = Join-Path $Root 'dns-assistant.exe'
    # Only migrate shortcuts whose primary entry belongs to this installation.
    if ($main.TargetPath -ine $executable) { return }
    $managementPath = Join-Path $Group 'Management.lnk'
    Assert-PlainPath $managementPath
    $management = $shell.CreateShortcut($managementPath)
    $management.TargetPath = $executable
    $management.Arguments = $main.Arguments
    $management.WorkingDirectory = $Root
    $management.Save()
    $oldPath = Join-Path $Group 'Management.url'
    if (Test-Path -LiteralPath $oldPath) {
        Assert-PlainPath $oldPath
        if ((Get-Content -LiteralPath $oldPath) -match '^URL=http://127\.0\.0\.1:8080/?$') {
            Remove-Item -LiteralPath $oldPath -Force
        }
    }
}

function Invoke-Update {
    $script:UpdateSafeToRestart = $true
    $script:UpdateLockAcquired = $false
    $root = (Resolve-Path -LiteralPath $InstallDir).Path.TrimEnd('\', '/')
    Assert-PlainPath $root
    $executable = Join-Path $root 'dns-assistant.exe'
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) { throw "Application not found: $executable" }
    $lock = $null
    $work = $null
    try {
        Assert-PlainPath (Join-Path $root '.dns-assistant.lock')
        try { $lock = [IO.File]::Open((Join-Path $root '.dns-assistant.lock'), 'OpenOrCreate', 'ReadWrite', 'None') }
        catch { throw 'Stop DNS Assistant with Ctrl+C before updating; another update may also be running.' }
        $script:UpdateLockAcquired = $true
        # Old releases do not hold the lock. Detect those before touching files.
        foreach ($process in @(Get-Process -Name 'dns-assistant' -ErrorAction SilentlyContinue)) {
            if (-not $process.Path -or $process.Path -eq $executable) { throw 'Stop DNS Assistant with Ctrl+C before updating.' }
        }
        $current = Read-AppVersion $executable
        # Keep extraction paths short for Windows PowerShell 5.1/.NET Framework.
        $tempRoot = [IO.Path]::GetTempPath().TrimEnd('\', '/')
        $work = Join-Path $tempRoot ('dau-' + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $work | Out-Null
        $source = Join-Path $work 'package'
        $releaseVersion = $null
        if (-not $Package) {
            if ($Checksums) { throw '-Checksums requires -Package.' }
            [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
            $headers = @{ 'User-Agent' = 'DNS-Assistant-Updater'; Accept = 'application/vnd.github+json' }
            $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/leoskey/dns-assistant/releases/latest' -Headers $headers -TimeoutSec 30
            if ($release.draft -or $release.prerelease -or $release.tag_name -notmatch '^v(\d+\.\d+\.\d+)$') { throw 'Invalid stable release metadata.' }
            $releaseVersion = [version]$Matches[1]
            if ($releaseVersion -le $current) { Write-Output "Already up to date: $current"; return }
            $assetName = "dns-assistant-$releaseVersion-windows-amd64.zip"
            foreach ($asset in @($assetName, 'SHA256SUMS')) {
                $entries = @($release.assets | Where-Object name -EQ $asset)
                $expectedURL = "https://github.com/leoskey/dns-assistant/releases/download/v$releaseVersion/$asset"
                if ($entries.Count -ne 1 -or $entries[0].browser_download_url -ne $expectedURL) { throw "Missing or unexpected release asset: $asset" }
                Invoke-WebRequest -UseBasicParsing -Uri $expectedURL -Headers $headers -OutFile (Join-Path $work $asset) -TimeoutSec 180
            }
            $archive = Join-Path $work $assetName
            $manifest = Join-Path $work 'SHA256SUMS'
        }
        else {
            $archive = (Resolve-Path -LiteralPath $Package).Path
            if ($Checksums) { $manifest = (Resolve-Path -LiteralPath $Checksums).Path }
            else { $manifest = Join-Path (Split-Path $archive -Parent) 'SHA256SUMS' }
        }
        Expand-Update $archive $manifest $source
        $next = Read-AppVersion (Join-Path $source 'dns-assistant.exe')
        if ($next -lt $current) { throw 'Downgrades require manual restoration of a matching data backup.' }
        if ($releaseVersion -and $next -ne $releaseVersion) { throw 'Executable version does not match the release.' }
        $backups = Join-Path $root '.update-backups'
        Assert-PlainPath $backups
        $backup = Join-Path $backups ([guid]::NewGuid().ToString('N') + '.zip')
        if ($DataDir) {
            Backup-UpdateData $DataDir ($backup -replace '\.zip$', '-data.zip') $work
            Write-Output "Data backup: $($backup -replace '\.zip$', '-data.zip')"
        }
        Install-Update $source $root $backup
        try { Update-ManagementShortcut $root }
        catch { Write-Warning "Program updated; Management shortcut could not be migrated. Use DNS Assistant.exe. $($_.Exception.Message)" }
        Write-Output "Updated $current -> $next. Data was preserved. Start DNS Assistant again."
        Write-Output "Previous program files: $backup"
    }
    finally {
        try {
            if ($work -and (Test-Path -LiteralPath $work)) {
                # Only this invocation's generated staging directory may be removed.
                $resolved = (Resolve-Path -LiteralPath $work).Path
                if ((Split-Path $resolved -Parent) -eq $tempRoot -and (Split-Path $resolved -Leaf) -match '^dau-[a-f0-9]{32}$') {
                    Remove-Item -LiteralPath $resolved -Recurse -Force
                }
            }
        }
        finally { if ($lock) { $lock.Dispose() } }
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    $failure = $null
    $stopped = $false
    try {
        Wait-ForApplicationExit
        $stopped = $true
        Invoke-Update
    }
    catch { $failure = $_.Exception.Message }
    if ($stopped -and $RestartArguments -and $script:UpdateSafeToRestart -and $script:UpdateLockAcquired) {
        try { Restart-Application }
        catch { $failure = "$failure Restart failed: $($_.Exception.Message)" }
    }
    if ($Notify) {
        Add-Type -AssemblyName System.Windows.Forms
        $message = if ($failure) { "Update failed. Check the tray or start DNS Assistant manually.`n`n$failure" } else { 'Update check completed. DNS Assistant has been started again.' }
        [Windows.Forms.MessageBox]::Show($message, 'DNS Assistant') | Out-Null
    }
    if ($failure) { Write-Error $failure -ErrorAction Continue; exit 1 }
}
