$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOCACHE = Join-Path $projectRoot '.cache\go-build'
$env:GOPATH = Join-Path $projectRoot '.cache\gopath'

Push-Location $projectRoot
try {
    go test ./...
    New-Item -ItemType Directory -Force 'bin' | Out-Null
    go build -trimpath -o 'bin\codex-relay.exe' ./cmd/relay
    go build -trimpath -o 'bin\codex-remote.exe' ./cmd/agent

    if (-not $env:JAVA_HOME) {
        $env:JAVA_HOME = 'C:\Program Files\Android\Android Studio\jbr'
    }
    $env:GRADLE_USER_HOME = Join-Path $projectRoot '.gradle'
    .\gradlew.bat :android-app:testDebugUnitTest :android-app:assembleDebug
}
finally {
    Pop-Location
}

