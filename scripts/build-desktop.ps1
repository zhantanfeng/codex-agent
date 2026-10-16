$ErrorActionPreference = 'Stop'
$repoDir = Split-Path $PSScriptRoot -Parent
Push-Location $repoDir
try {
    $env:GOCACHE = Join-Path $repoDir '.cache/go-build'
    $env:GOPATH = Join-Path $repoDir '.cache/gopath'
    New-Item -ItemType Directory -Force -Path (Join-Path $repoDir 'bin') | Out-Null
    go build -tags desktop,production -trimpath -ldflags '-H windowsgui -s -w' -o bin/dcode-desktop.exe ./cmd/desktop
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed.' }
    Write-Host "Built: $repoDir\bin\dcode-desktop.exe"
} finally {
    Pop-Location
}
