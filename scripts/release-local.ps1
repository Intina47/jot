param(
    [string]$Version,
    [string]$HomebrewTapPath,
    [switch]$SkipRelease,
    [switch]$PublishChocolatey,
    [switch]$PushHomebrewTap
)

$ErrorActionPreference = "Stop"

function Require-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "required command not found: $Name"
    }
}

function Get-RepoRoot {
    $root = Split-Path -Parent $PSScriptRoot
    return (Resolve-Path $root).Path
}

function Get-VersionFromMain {
    param([string]$MainPath)
    $match = Select-String -Path $MainPath -Pattern 'const version = "([^"]+)"'
    if (-not $match) {
        throw "could not read version from $MainPath"
    }
    return $match.Matches[0].Groups[1].Value
}

function Get-ReleaseNotesPath {
    param(
        [string]$Root,
        [string]$Version
    )

    $path = Join-Path $Root "release-notes\v$Version.md"
    if (-not (Test-Path $path)) {
        throw "missing release notes file: $path"
    }
    return $path
}

function Write-Formula {
    param(
        [string]$Path,
        [string]$Version,
        [string]$DarwinArm64Sha,
        [string]$DarwinAmd64Sha,
        [string]$LinuxAmd64Sha
    )

    $formula = @"
class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "$Version"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v$Version/jot_v$Version`_darwin_arm64.tar.gz"
      sha256 "$DarwinArm64Sha"
    else
      url "https://github.com/Intina47/jot/releases/download/v$Version/jot_v$Version`_darwin_amd64.tar.gz"
      sha256 "$DarwinAmd64Sha"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v$Version/jot_v$Version`_linux_amd64.tar.gz"
    sha256 "$LinuxAmd64Sha"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
"@
    Set-Content -Path $Path -Value $formula
}

function Update-ChocolateyFiles {
    param(
        [string]$Root,
        [string]$Version,
        [string]$Checksum
    )

    $nuspecPath = Join-Path $Root "packaging\chocolatey\jot.nuspec"
    $installPath = Join-Path $Root "packaging\chocolatey\tools\chocolateyinstall.ps1"
    $verificationPath = Join-Path $Root "packaging\chocolatey\tools\VERIFICATION.txt"

    (Get-Content $nuspecPath) `
        -replace '<version>.*</version>', "<version>$Version</version>" `
        -replace '<releaseNotes>.*</releaseNotes>', "<releaseNotes>https://github.com/Intina47/jot/releases/tag/v$Version</releaseNotes>" | Set-Content $nuspecPath
    (Get-Content $installPath) `
        -replace '^\$version = \".*\"$', "`$version = `"$Version`"" `
        -replace '^\$checksum = \".*\"$', "`$checksum = `"$Checksum`"" | Set-Content $installPath

    @"
Verification is intended to assist the Chocolatey moderators and community
in verifying that this package's contents are trustworthy.

Download URL:
https://github.com/Intina47/jot/releases/download/v$Version/jot_v$Version`_windows_amd64.zip

SHA256:
$Checksum
"@ | Set-Content $verificationPath
}

function Build-ReleaseArtifacts {
    param(
        [string]$Root,
        [string]$Version
    )

    Require-Command go
    $tarExe = Join-Path $env:WINDIR "System32\tar.exe"
    if (-not (Test-Path $tarExe)) {
        throw "expected tar at $tarExe"
    }

    $dist = Join-Path $Root "dist"
    $cache = Join-Path $Root ".gocache-release"
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
    Remove-Item -Recurse -Force (Join-Path $dist "*") -ErrorAction SilentlyContinue

    $env:GOCACHE = $cache
    $tag = "v$Version"
    $targets = @(
        @{ GOOS = "linux"; GOARCH = "amd64"; Ext = ""; Archive = "jot_${tag}_linux_amd64.tar.gz" },
        @{ GOOS = "darwin"; GOARCH = "amd64"; Ext = ""; Archive = "jot_${tag}_darwin_amd64.tar.gz" },
        @{ GOOS = "darwin"; GOARCH = "arm64"; Ext = ""; Archive = "jot_${tag}_darwin_arm64.tar.gz" },
        @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe"; Archive = "jot_${tag}_windows_amd64.zip" }
    )

    foreach ($target in $targets) {
        $tmp = Join-Path $dist ($target.GOOS + "_" + $target.GOARCH)
        New-Item -ItemType Directory -Force -Path $tmp | Out-Null

        $binaryName = "jot" + $target.Ext
        $binaryPath = Join-Path $tmp $binaryName

        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        go build -o $binaryPath .
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $($target.GOOS)/$($target.GOARCH)"
        }

        $archivePath = Join-Path $dist $target.Archive
        if ($target.GOOS -eq "windows") {
            if (Test-Path $archivePath) {
                Remove-Item $archivePath -Force
            }
            Compress-Archive -Path $binaryPath -DestinationPath $archivePath -Force
        } else {
            & $tarExe -czf $archivePath -C $tmp $binaryName
            if ($LASTEXITCODE -ne 0) {
                throw "archive failed for $($target.GOOS)/$($target.GOARCH)"
            }
        }
    }

    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    return $dist
}

function Ensure-GitHubRelease {
    param(
        [string]$Root,
        [string]$Version,
        [string]$ReleaseNotesPath,
        [string[]]$Artifacts
    )

    Require-Command gh
    $tag = "v$Version"
    Push-Location $Root
    try {
        cmd /c "gh release view $tag 1>nul 2>nul"
        if ($LASTEXITCODE -eq 0) {
            gh release edit $tag --title $tag --notes-file $ReleaseNotesPath
            if ($LASTEXITCODE -ne 0) {
                throw "failed to update release notes for $tag"
            }
            gh release upload $tag @Artifacts --clobber
            if ($LASTEXITCODE -ne 0) {
                throw "failed to upload assets to existing release $tag"
            }
            return
        }
        gh release create $tag @Artifacts --title $tag --notes-file $ReleaseNotesPath
        if ($LASTEXITCODE -ne 0) {
            throw "failed to create release $tag"
        }
    } finally {
        Pop-Location
    }
}

function Maybe-UpdateHomebrewTap {
    param(
        [string]$TapPath,
        [string]$Version,
        [string]$DarwinArm64Sha,
        [string]$DarwinAmd64Sha,
        [string]$LinuxAmd64Sha,
        [bool]$Push
    )

    if ([string]::IsNullOrWhiteSpace($TapPath)) {
        return
    }

    $formulaDir = Join-Path $TapPath "Formula"
    $formulaPath = Join-Path $formulaDir "jot-cli.rb"
    $legacyFormulaPath = Join-Path $formulaDir "jot.rb"
    New-Item -ItemType Directory -Force -Path $formulaDir | Out-Null
    if (Test-Path $legacyFormulaPath) {
        Remove-Item $legacyFormulaPath -Force
    }
    Write-Formula -Path $formulaPath -Version $Version -DarwinArm64Sha $DarwinArm64Sha -DarwinAmd64Sha $DarwinAmd64Sha -LinuxAmd64Sha $LinuxAmd64Sha

    if (-not $Push) {
        return
    }

    Require-Command git
    Push-Location $TapPath
    try {
        if (git diff --quiet) {
            return
        }
        git add -A Formula
        git commit -m "Update jot-cli to v$Version"
        git push
    } finally {
        Pop-Location
    }
}

function Maybe-PublishChocolatey {
    param(
        [string]$Root,
        [string]$Version
    )

    if (-not $PublishChocolatey) {
        return
    }
    if (-not $env:CHOCO_API_KEY) {
        throw "CHOCO_API_KEY is required to publish Chocolatey"
    }
    Require-Command choco
    Push-Location $Root
    try {
        choco pack .\packaging\chocolatey\jot.nuspec
        if ($LASTEXITCODE -ne 0) {
            throw "choco pack failed"
        }
        choco push ".\jot.$Version.nupkg" --source https://push.chocolatey.org/ --api-key $env:CHOCO_API_KEY
        if ($LASTEXITCODE -ne 0) {
            throw "choco push failed"
        }
    } finally {
        Pop-Location
    }
}

$root = Get-RepoRoot
if (-not $Version) {
    $Version = Get-VersionFromMain -MainPath (Join-Path $root "main.go")
}
$releaseNotesPath = Get-ReleaseNotesPath -Root $root -Version $Version

$dist = Build-ReleaseArtifacts -Root $root -Version $Version
$tag = "v$Version"

$darwinAmd64 = Join-Path $dist "jot_${tag}_darwin_amd64.tar.gz"
$darwinArm64 = Join-Path $dist "jot_${tag}_darwin_arm64.tar.gz"
$linuxAmd64 = Join-Path $dist "jot_${tag}_linux_amd64.tar.gz"
$windowsAmd64 = Join-Path $dist "jot_${tag}_windows_amd64.zip"
$installer = Join-Path $root "install.sh"
$logo = Join-Path $root "assets\jot-logo.png"

$darwinAmd64Sha = (Get-FileHash -Algorithm SHA256 $darwinAmd64).Hash.ToLower()
$darwinArm64Sha = (Get-FileHash -Algorithm SHA256 $darwinArm64).Hash.ToLower()
$linuxAmd64Sha = (Get-FileHash -Algorithm SHA256 $linuxAmd64).Hash.ToLower()
$windowsAmd64Sha = (Get-FileHash -Algorithm SHA256 $windowsAmd64).Hash.ToLower()

Write-Formula -Path (Join-Path $root "packaging\homebrew\jot-cli.rb") -Version $Version -DarwinArm64Sha $darwinArm64Sha -DarwinAmd64Sha $darwinAmd64Sha -LinuxAmd64Sha $linuxAmd64Sha
Update-ChocolateyFiles -Root $root -Version $Version -Checksum $windowsAmd64Sha
Maybe-UpdateHomebrewTap -TapPath $HomebrewTapPath -Version $Version -DarwinArm64Sha $darwinArm64Sha -DarwinAmd64Sha $darwinAmd64Sha -LinuxAmd64Sha $linuxAmd64Sha -Push $PushHomebrewTap.IsPresent

if (-not $SkipRelease) {
    Ensure-GitHubRelease -Root $root -Version $Version -ReleaseNotesPath $releaseNotesPath -Artifacts @(
        $installer,
        $logo,
        $darwinAmd64,
        $darwinArm64,
        $linuxAmd64,
        $windowsAmd64
    )
}

Maybe-PublishChocolatey -Root $root -Version $Version

Write-Host "local release complete for $tag"
Write-Host "darwin amd64 sha256: $darwinAmd64Sha"
Write-Host "darwin arm64 sha256: $darwinArm64Sha"
Write-Host "linux amd64 sha256:  $linuxAmd64Sha"
Write-Host "windows amd64 sha256: $windowsAmd64Sha"
