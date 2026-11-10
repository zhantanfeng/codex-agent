param([string]$OutputPath = 'bin/dcode-desktop.exe')
$ErrorActionPreference = 'Stop'
$repoDir = Split-Path $PSScriptRoot -Parent
Push-Location $repoDir
try {
    $env:GOCACHE = Join-Path $repoDir '.cache/go-build'
    $env:GOPATH = Join-Path $repoDir '.cache/gopath'
    New-Item -ItemType Directory -Force -Path (Join-Path $repoDir 'bin') | Out-Null
    # Wails loads window/taskbar icon group 3 from the executable resources.
    go run github.com/tc-hib/go-winres@v0.3.1 make --in cmd/desktop/windows/winres.json --out cmd/desktop/rsrc --arch amd64
    if ($LASTEXITCODE -ne 0) { throw 'Desktop icon resource generation failed.' }
    go build -tags desktop,production -trimpath -ldflags '-H windowsgui -s -w' -o $OutputPath ./cmd/desktop
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed.' }
    Write-Host "Built: $OutputPath"
} finally {
    Pop-Location
}
