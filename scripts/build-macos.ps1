param([string]$Version = '0.1.14')
$ErrorActionPreference = 'Stop'
$repoDir = Split-Path $PSScriptRoot -Parent
$previous = @{ GOOS=$env:GOOS; GOARCH=$env:GOARCH; CGO_ENABLED=$env:CGO_ENABLED; GOCACHE=$env:GOCACHE; GOPATH=$env:GOPATH }
Push-Location $repoDir
try {
    $env:GOCACHE = Join-Path $repoDir '.cache/go-build'
    $env:GOPATH = Join-Path $repoDir '.cache/gopath'
    $env:GOOS = 'darwin'
    $env:GOARCH = 'arm64'
    $env:CGO_ENABLED = '0'
    $packageDir = Join-Path $repoDir "bin/dcode-$Version-macos-arm64"
    New-Item -ItemType Directory -Force -Path $packageDir | Out-Null
    go build -trimpath -ldflags '-s -w' -o "$packageDir/codex-remote" ./cmd/agent
    if ($LASTEXITCODE -ne 0) { throw 'macOS ARM64 Agent build failed.' }
    Copy-Item -LiteralPath 'scripts/macos/start.command' -Destination "$packageDir/start.command"
    Copy-Item -LiteralPath 'docs/macos.md' -Destination "$packageDir/README.md"
    $archivePath = Join-Path $repoDir "bin/dcode-$Version-macos-arm64.tar.gz"
    $env:GOOS = $previous.GOOS
    $env:GOARCH = $previous.GOARCH
    $env:CGO_ENABLED = $previous.CGO_ENABLED
    go run ./scripts/package-macos.go $packageDir $archivePath
    if ($LASTEXITCODE -ne 0) { throw 'macOS package creation failed.' }
    Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath
    Write-Host "Built: $archivePath"
} finally {
    foreach ($name in $previous.Keys) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
    Pop-Location
}
