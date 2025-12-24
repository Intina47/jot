$ErrorActionPreference = "Stop"

$unformatted = gofmt -l .
if ($unformatted) {
  Write-Error ("gofmt check failed; run gofmt -w .`n" + ($unformatted -join "`n"))
}

go test ./...
