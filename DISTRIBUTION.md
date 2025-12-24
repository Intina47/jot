# Distribution plan

This project ships prebuilt binaries from GitHub Releases and uses small wrappers
for brew, choco, and npm.

## Versioning

- Keep `version` in `main.go` in sync with tags.
- Tag releases as `v1.0.0` and push the tag.
- The release workflow builds artifacts on tag push.

## Automation

The release workflow also publishes:

- npm package (requires `NPM_TOKEN`)
- Homebrew tap updates (requires `HOMEBREW_TAP_TOKEN` with write access to `Intina47/homebrew-jot`)
- Chocolatey package (requires `CHOCO_API_KEY`)

All secrets live in the `Intina47/jot` GitHub repo settings.

## Release artifacts

The GitHub Actions release workflow publishes:

- `jot_<tag>_darwin_amd64.tar.gz`
- `jot_<tag>_darwin_arm64.tar.gz`
- `jot_<tag>_linux_amd64.tar.gz`
- `jot_<tag>_windows_amd64.zip`

Each archive contains a single `jot` (or `jot.exe`) binary.

## Homebrew (tap)

- Create a tap: `yourname/homebrew-jot`
- Formula downloads the macOS/Linux tarballs from GitHub Releases.
- Use the release asset SHA256 for each platform.

Formula location in this repo:

- `packaging/homebrew/jot.rb`

Formula sketch:

```ruby
class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/yourname/jot"
  version "1.0.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/yourname/jot/releases/download/v1.0.0/jot_v1.0.0_darwin_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/yourname/jot/releases/download/v1.0.0/jot_v1.0.0_darwin_amd64.tar.gz"
      sha256 "..."
    end
  end

  on_linux do
    url "https://github.com/yourname/jot/releases/download/v1.0.0/jot_v1.0.0_linux_amd64.tar.gz"
    sha256 "..."
  end

  def install
    bin.install "jot"
  end
end
```

## Chocolatey

- Package installs `jot.exe` into `tools` and shims it.
- Point the package at the GitHub release `.zip`.
- Use checksums for integrity.

Key files in this repo:

- `packaging/chocolatey/jot.nuspec`
- `packaging/chocolatey/tools/chocolateyinstall.ps1`

Install script sketch:

```powershell
$url = "https://github.com/yourname/jot/releases/download/v1.0.0/jot_v1.0.0_windows_amd64.zip"
$checksum = "..."
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
```

## npm wrapper

Publish a tiny Node package (`@intina47/jot`) that installs the correct binary.

Behavior:

- Detect `process.platform` and `process.arch`.
- Download the matching GitHub release asset.
- Place it in `bin/` and mark it executable.
- Expose the CLI via `bin: { "jot": "bin/jot" }`.

Minimal layout in this repo:

```
packaging/npm/package.json
packaging/npm/install.js
packaging/npm/bin/jot
```

`package.json` sketch:

```json
{
  "name": "@intina47/jot",
  "version": "1.0.0",
  "bin": { "jot": "bin/jot" },
  "os": ["darwin", "linux", "win32"],
  "cpu": ["x64", "arm64"]
}
```

`install.js` sketch:

- Map platform/arch to asset name.
- Download from GitHub Releases.
- Extract and place the binary at `bin/jot` (or `bin/jot.exe` on Windows).

This keeps distribution consistent with brew and choco without rewriting the CLI.
