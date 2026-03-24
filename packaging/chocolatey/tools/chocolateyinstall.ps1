$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072

$version = "1.6.1"
$url = "https://github.com/Intina47/jot/releases/download/v$version/jot_v$version_windows_amd64.zip"
$checksum = "eb7acfe8f9ac34b2429316637119dcd97ef2d4a13c431374a9a09a300a8c235e"

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
