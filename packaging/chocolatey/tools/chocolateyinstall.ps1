$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072

$version = "1.5.3"
$url = "https://github.com/Intina47/jot/releases/download/v$version/jot_v$version_windows_amd64.zip"
$checksum = "7f83e66c049fc1951aef26a753a5f8be24dae35936ddc39ca740efec011470cf"

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
