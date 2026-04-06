$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072

$version = "1.7.3"
$url = "https://github.com/Intina47/jot/releases/download/v$version/jot_v$version_windows_amd64.zip"
$checksum = "a7f129d1074be84462ea31a31bde810150cb9680a4a460e4cf258a3ecc180376"

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
