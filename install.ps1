# install.ps1 - Install Hazdo from GitHub releases (Windows PowerShell)
#
# Usage:
#   irm https://raw.githubusercontent.com/hari-incoming/hazdo/main/install.ps1 | iex
#   .\install.ps1
#   .\install.ps1 -Version v0.1.0
#   .\install.ps1 -InstallDir "C:\custom\path"

[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$InstallDir = "",
    [switch]$Help,
    [switch]$NativeWindows
)

# ─── Configuration ─────────────────────────────────────────────────────────────

$RepoOwner = "hari-incoming"
$RepoName = "hazdo"
$BinaryName = "azdo"
$ConfigDirName = "azdo-tui"
$GitHubApi = "https://api.github.com"
$GitHubDownload = "https://github.com"

$ErrorActionPreference = "Stop"

# ─── Output helpers ────────────────────────────────────────────────────────────

function Write-Step($msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Write-Info($msg) {
    Write-Host "[INFO]  $msg" -ForegroundColor Blue
}

function Write-Ok($msg) {
    Write-Host "[OK]    $msg" -ForegroundColor Green
}

function Write-Warn($msg) {
    Write-Host "[WARN]  $msg" -ForegroundColor Yellow
}

function Write-Err($msg) {
    Write-Host "[ERROR] $msg" -ForegroundColor Red
}

# ─── Help ──────────────────────────────────────────────────────────────────────

function Show-Usage {
    Write-Host @"

Usage: .\install.ps1 [OPTIONS]

Install Hazdo from GitHub releases (the executable command is azdo).

Options:
  -Version VERSION     Install a specific version (e.g., v0.1.0)
                       Default: latest release
  -InstallDir DIR      Custom installation directory
                       Default: %LOCALAPPDATA%\azdo-tui
  -NativeWindows       Install the native Windows build instead of using WSL
  -Help                Show this help message

Examples:
  # Install latest version
  irm https://raw.githubusercontent.com/hari-incoming/hazdo/main/install.ps1 | iex

  # Install specific version
  .\install.ps1 -Version v0.1.0

  # Install to custom directory
  .\install.ps1 -InstallDir "C:\tools\azdo-tui"
"@
}

if ($Help) {
    Show-Usage
    exit 0
}

# ─── WSL detection and installation ───────────────────────────────────────────

function Get-WslInstallTarget {
    $wsl = Get-Command wsl.exe -ErrorAction SilentlyContinue
    if (-not $wsl) {
        return $null
    }

    $distroOutput = & $wsl.Source --list --quiet 2>$null
    $distroExitCode = $LASTEXITCODE
    $distros = @(
        $distroOutput |
            ForEach-Object { "$_".Replace([char]0, "").Trim() } |
            Where-Object { $_ }
    )
    if ($distroExitCode -ne 0 -or $distros.Count -eq 0) {
        Write-Warn "WSL is available, but no usable Linux distribution is installed."
        return $null
    }

    $userDistros = @($distros | Where-Object { $_ -notmatch '^docker-desktop' })
    if ($userDistros.Count -eq 0) {
        Write-Warn "WSL has no user Linux distribution installed."
        return $null
    }

    $defaultDistro = $null
    $verboseOutput = & $wsl.Source --list --verbose 2>$null
    foreach ($line in $verboseOutput) {
        $lineText = "$line".Replace([char]0, "").Trim()
        if ($lineText -match '^\*\s+([^\s]+)') {
            $defaultDistro = $Matches[1]
            break
        }
    }
    if ($defaultDistro -notin $userDistros) {
        $defaultDistro = $userDistros[0]
    }

    $releaseOutput = & $wsl.Source --distribution $defaultDistro --exec sh -lc '. /etc/os-release && printf "%s|%s|%s" "${ID:-unknown}" "${VERSION_ID:-unknown}" "${PRETTY_NAME:-unknown Linux}"' 2>$null
    $releaseExitCode = $LASTEXITCODE
    $releaseInfo = (@($releaseOutput) -join "").Trim()
    if ($releaseExitCode -ne 0 -or -not $releaseInfo) {
        Write-Warn "Could not read the Linux distribution details from WSL."
        return $null
    }

    $releaseFields = $releaseInfo -split '\|', 3
    if ($releaseFields.Count -ne 3) {
        Write-Warn "WSL returned unexpected Linux distribution details."
        return $null
    }

    return [pscustomobject]@{
        Command    = $wsl.Source
        Name       = $defaultDistro
        Id         = $releaseFields[0]
        Version    = $releaseFields[1]
        PrettyName = $releaseFields[2]
    }
}

function Install-InWsl {
    param([pscustomobject]$Target)

    $installerPath = if ($PSScriptRoot) { Join-Path $PSScriptRoot "install.sh" } else { "" }
    $downloadedInstaller = $false
    if (-not $installerPath -or -not (Test-Path $installerPath)) {
        $installerPath = Join-Path ([System.IO.Path]::GetTempPath()) "Hazdo-install-$PID.sh"
        $installerUrl = "https://raw.githubusercontent.com/$RepoOwner/$RepoName/main/install.sh"
        Invoke-WebRequest -Uri $installerUrl -OutFile $installerPath -UseBasicParsing
        $downloadedInstaller = $true
    }

    try {
        $linuxPathOutput = & $Target.Command --distribution $Target.Name --exec wslpath -u $installerPath 2>$null
        $pathExitCode = $LASTEXITCODE
        $linuxInstallerPath = (@($linuxPathOutput) -join "").Trim()
        if ($pathExitCode -ne 0 -or -not $linuxInstallerPath) {
            throw "Could not access the installer from the selected WSL distribution."
        }

        $installArgs = @("--distribution", $Target.Name, "--exec", "sh", $linuxInstallerPath)
        if ($Version) {
            $installArgs += @("--version", $Version)
        }
        if ($InstallDir) {
            $installArgs += @("--install-dir", $InstallDir)
        }

        Write-Step "Installing the Linux build in WSL"
        & $Target.Command @installArgs
        if ($LASTEXITCODE -ne 0) {
            throw "The installer failed in WSL distribution '$($Target.Name)'."
        }
    }
    finally {
        if ($downloadedInstaller) {
            Remove-Item -Path $installerPath -Force -ErrorAction SilentlyContinue
        }
    }
}

# ─── Architecture detection ───────────────────────────────────────────────────

function Get-Architecture {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "x86_64" }
        "Arm64" { return "arm64" }
        default {
            # Fallback for older PowerShell
            $envArch = $env:PROCESSOR_ARCHITECTURE
            switch ($envArch) {
                "AMD64" { return "x86_64" }
                "ARM64" { return "arm64" }
                default {
                    Write-Err "Unsupported architecture: $envArch"
                    Write-Err "Hazdo supports x86_64 (amd64) and arm64."
                    exit 1
                }
            }
        }
    }
}

# ─── Version resolution ───────────────────────────────────────────────────────

function Get-LatestVersion {
    if ($Version) {
        if (-not $Version.StartsWith("v")) {
            $script:Version = "v$Version"
        }
        Write-Ok "Using specified version: $Version"
        return $Version
    }

    Write-Info "Fetching latest release version..."

    try {
        $releaseUrl = "$GitHubApi/repos/$RepoOwner/$RepoName/releases/latest"
        $response = Invoke-RestMethod -Uri $releaseUrl -Headers @{ "User-Agent" = "azdo-tui-installer" }
        $script:Version = $response.tag_name

        if (-not $Version) {
            Write-Err "Could not determine the latest version."
            Write-Err ""
            Write-Err "You can specify a version manually: .\install.ps1 -Version v0.1.0"
            Write-Err "Check available releases at: $GitHubDownload/$RepoOwner/$RepoName/releases"
            exit 1
        }

        Write-Ok "Latest version: $Version"
        return $Version
    }
    catch {
        Write-Err "Failed to fetch latest release from GitHub."
        Write-Err ""
        Write-Err "Possible causes:"
        Write-Err "  - No releases have been published yet"
        Write-Err "  - GitHub API rate limit exceeded (try again later)"
        Write-Err "  - Network connectivity issues"
        Write-Err ""
        Write-Err "You can specify a version manually: .\install.ps1 -Version v0.1.0"
        Write-Err "Check available releases at: $GitHubDownload/$RepoOwner/$RepoName/releases"
        exit 1
    }
}

# ─── Install directory resolution ─────────────────────────────────────────────

function Get-InstallDirectory {
    if ($InstallDir) {
        Write-Ok "Using custom install directory: $InstallDir"
        return $InstallDir
    }

    $defaultDir = Join-Path $env:LOCALAPPDATA $ConfigDirName
    Write-Ok "Install directory: $defaultDir"
    return $defaultDir
}

# ─── Download and install ─────────────────────────────────────────────────────

function Install-Binary {
    param(
        [string]$ArchName,
        [string]$TargetDir
    )

    $versionNum = $Version.TrimStart("v")
    $archiveName = "${BinaryName}_${versionNum}_Windows_${ArchName}.zip"
    $downloadUrl = "$GitHubDownload/$RepoOwner/$RepoName/releases/download/$Version/$archiveName"
    $checksumsUrl = "$GitHubDownload/$RepoOwner/$RepoName/releases/download/$Version/checksums.txt"

    Write-Info "Downloading $archiveName..."

    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "azdo-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

    try {
        $archivePath = Join-Path $tempDir $archiveName

        try {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath -UseBasicParsing
        }
        catch {
            Write-Err "Failed to download $archiveName"
            Write-Err ""
            Write-Err "Possible causes:"
            Write-Err "  - Version $Version may not exist"
            Write-Err "  - The release may not include a build for Windows/$ArchName"
            Write-Err "  - Network connectivity issues"
            Write-Err ""
            Write-Err "Check available releases at:"
            Write-Err "  $GitHubDownload/$RepoOwner/$RepoName/releases"
            exit 1
        }

        Write-Ok "Downloaded $archiveName"

        # Verify checksum
        Write-Info "Verifying checksum..."
        try {
            $checksumsPath = Join-Path $tempDir "checksums.txt"
            Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath -UseBasicParsing

            $checksums = Get-Content $checksumsPath
            $expectedLine = $checksums | Where-Object { $_ -match $archiveName }

            if ($expectedLine) {
                $expectedHash = ($expectedLine -split "\s+")[0]
                $actualHash = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()

                if ($expectedHash -eq $actualHash) {
                    Write-Ok "Checksum verified"
                }
                else {
                    Write-Err "Checksum verification failed!"
                    Write-Err "  Expected: $expectedHash"
                    Write-Err "  Got:      $actualHash"
                    Write-Err "The downloaded file may be corrupted. Please try again."
                    exit 1
                }
            }
            else {
                Write-Warn "Archive not found in checksums file. Skipping verification."
            }
        }
        catch {
            Write-Warn "Could not download checksums file. Skipping verification."
        }

        # Extract archive
        Write-Info "Extracting archive..."
        $extractDir = Join-Path $tempDir "extracted"
        Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
        Write-Ok "Extracted archive"

        # Find the binary
        $binaryFile = Get-ChildItem -Path $extractDir -Recurse -Filter "${BinaryName}.exe" | Select-Object -First 1

        if (-not $binaryFile) {
            Write-Err "Could not find '${BinaryName}.exe' in the extracted archive."
            Write-Err "Please report this issue at: https://github.com/$RepoOwner/$RepoName/issues"
            exit 1
        }

        # Create install directory
        if (-not (Test-Path $TargetDir)) {
            Write-Info "Creating install directory: $TargetDir"
            try {
                New-Item -ItemType Directory -Path $TargetDir -Force | Out-Null
            }
            catch {
                Write-Err "Failed to create directory: $TargetDir"
                Write-Err ""
                Write-Err "Possible fixes:"
                Write-Err "  - Run PowerShell as Administrator"
                Write-Err "  - Use a custom directory: .\install.ps1 -InstallDir `"C:\Users\$env:USERNAME\bin`""
                exit 1
            }
        }

        # Copy binary
        Write-Info "Installing ${BinaryName}.exe to $TargetDir..."
        Copy-Item -Path $binaryFile.FullName -Destination (Join-Path $TargetDir "${BinaryName}.exe") -Force
        Write-Ok "Installed ${BinaryName}.exe to $TargetDir"

        # Add to PATH if not already there
        $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        if ($currentPath -notlike "*$TargetDir*") {
            Write-Info "Adding $TargetDir to user PATH..."
            try {
                [Environment]::SetEnvironmentVariable(
                    "PATH",
                    "$currentPath;$TargetDir",
                    "User"
                )
                $env:PATH = "$env:PATH;$TargetDir"
                Write-Ok "Added to PATH (restart your terminal for changes to take effect)"
            }
            catch {
                Write-Warn "Could not update PATH automatically."
                Write-Warn "Add this directory to your PATH manually: $TargetDir"
            }
        }
        else {
            Write-Ok "$TargetDir is already in PATH"
        }
    }
    finally {
        # Cleanup
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# ─── Summary ───────────────────────────────────────────────────────────────────

function Show-Summary {
    Write-Host ""
    Write-Host "────────────────────────────────────────────────" -ForegroundColor Green
    Write-Host "  Hazdo $Version installed successfully!" -ForegroundColor Green
    Write-Host "────────────────────────────────────────────────" -ForegroundColor Green
    Write-Host ""

    Write-Host "  Next step: run azdo and follow the first-run setup wizard."
    Write-Host ""
    Write-Host "  Documentation: https://github.com/$RepoOwner/$RepoName#readme"
    Write-Host ""
}

# ─── Main ──────────────────────────────────────────────────────────────────────

function Main {
    Write-Host ""
    Write-Host "Hazdo installer" -ForegroundColor White
    Write-Host "Azure DevOps TUI for your terminal"
    Write-Host ""

    Write-Step "Detecting system"
    Write-Ok "Detected OS: Windows"
    $archName = Get-Architecture
    Write-Ok "Detected architecture: $archName"

    if (-not $NativeWindows) {
        $wslTarget = Get-WslInstallTarget
        if ($wslTarget) {
            Write-Ok "Detected WSL distribution: $($wslTarget.PrettyName) ($($wslTarget.Name))"
            Write-Info "Distro: $($wslTarget.Id) $($wslTarget.Version)"
            Install-InWsl -Target $wslTarget
            return
        }
        Write-Info "No usable WSL distribution found; installing the native Windows build."
    }
    else {
        Write-Info "Native Windows installation requested."
    }

    Write-Step "Resolving version"
    Get-LatestVersion | Out-Null
    $installDir = Get-InstallDirectory

    Write-Step "Downloading and installing"
    Install-Binary -ArchName $archName -TargetDir $installDir

    Show-Summary
}

Main
