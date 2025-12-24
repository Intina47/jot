$ErrorActionPreference = "Stop"

$version = "1.0.0"
$url = "https://github.com/Intina47/jot/releases/download/v$version/jot_v$version_windows_amd64.zip"
$checksum = "7cb50d41dedba35ce91ef29805d727773263fcf8d287dcf8e99855e168f89817"

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName "jot" -Url $url -UnzipLocation $toolsDir -Checksum $checksum -ChecksumType "sha256"
