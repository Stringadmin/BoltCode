$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$previousToolchain = $env:GOTOOLCHAIN

Push-Location -LiteralPath $PSScriptRoot
try {
    Push-Location -LiteralPath 'frontend'
    try {
        npm ci
        npm run build
    } finally {
        Pop-Location
    }
    $env:GOTOOLCHAIN = 'go1.26.10+auto'
    go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --arch amd64 --out boltcode
    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
    go build -tags production -trimpath -ldflags '-H windowsgui -s -w' -o 'bin/BoltCode.exe' .
} finally {
    $env:GOTOOLCHAIN = $previousToolchain
    Pop-Location
}
