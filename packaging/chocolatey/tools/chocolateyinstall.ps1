$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072

$version = "1.7.0"
$url = "https://github.com/Intina47/jot/releases/download/v$version/jot_v$version_windows_amd64.zip"
$checksum = "fb282425c64891734a2415cce1db3e07bc447da09eed4d33ab930dd7bea83c97"

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
