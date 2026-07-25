<#
.SYNOPSIS
  Expose les cibles du Makefile sur un poste Windows sans GNU make.

.DESCRIPTION
  Le Makefile reste la référence — c'est lui que la CI exécute. Ce script est un
  simple relais pour le poste de développement Windows, où `make` n'est pas
  installé et n'a aucune raison de l'être : la chaîne Go est autosuffisante.

  Les deux passes de `test` sont identiques à celles du Makefile, et pour la même
  raison (important-3) : `-race` EXIGE cgo, donc un CGO_ENABLED=0 global casserait
  la seule vérification automatique des invariants de concurrence du Hub. Sous
  Windows, la passe `-race` demande gcc (mingw-w64) ; sans lui, elle est signalée
  comme sautée au lieu d'échouer en silence.

.EXAMPLE
  .\make.ps1 test
  .\make.ps1 build
  .\make.ps1 dist
#>
[CmdletBinding()]
param(
  [Parameter(Position = 0)]
  [ValidateSet('all', 'test', 'vet', 'boundary', 'build', 'dist', 'cover', 'front', 'clean', 'help')]
  [string]$Target = 'all'
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

# La chaîne Go peut avoir été installée dans la session courante sans que le PATH
# du shell parent le sache.
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$env:Path = "$machinePath;$userPath"

$version = (git describe --tags --always --dirty 2>$null) ?? 'dev'
$commit = (git rev-parse --short HEAD 2>$null) ?? 'unknown'
$date = (git log -1 --format=%cI 2>$null) ?? 'unknown'
$ldflags = "-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date"

function Assert-Success([string]$what) {
  if ($LASTEXITCODE -ne 0) { throw "$what a échoué (code $LASTEXITCODE)" }
}

function Invoke-Vet {
  go vet ./...
  Assert-Success 'go vet'
}

function Invoke-Boundary {
  go run ./tools/boundary
  Assert-Success 'make boundary'
}

function Invoke-Test {
  Invoke-Vet

  if (Get-Command gcc -ErrorAction Ignore) {
    $env:CGO_ENABLED = '1'
    go test ./... -race -count=1
    $raceCode = $LASTEXITCODE
    $env:CGO_ENABLED = '0'
    if ($raceCode -ne 0) { throw "go test -race a échoué (code $raceCode)" }
  }
  else {
    Write-Host 'test : passe -race SAUTEE — gcc absent. Installez mingw-w64 (prerequis de developpement documente au README) ou laissez la CI Linux la couvrir.' -ForegroundColor Yellow
  }

  # La passe qui compte pour la livraison : elle prouve que la configuration
  # reellement livree (zero cgo) compile et passe.
  $env:CGO_ENABLED = '0'
  go test ./... -count=1
  Assert-Success 'go test (CGO_ENABLED=0)'

  Invoke-Boundary
}

function Invoke-Cover {
  $env:CGO_ENABLED = '0'
  go test ./... -coverprofile=coverage.out -count=1
  Assert-Success 'go test -coverprofile'
  go tool cover -func=coverage.out | Select-Object -Last 1
}

function Invoke-Build {
  $env:CGO_ENABLED = '0'
  go build -trimpath -ldflags $ldflags -o bin/openscale.exe ./cmd/openscale
  Assert-Success 'go build'
  Write-Host "build : bin/openscale.exe ($version)"
}

function Invoke-Front {
  if (Test-Path 'web/package.json') {
    npm --prefix web ci; Assert-Success 'npm ci'
    npm --prefix web run build; Assert-Success 'npm run build'
  }
  else {
    Write-Host 'front : web/package.json absent (lot L6) — rien a construire'
  }
}

function Invoke-Dist {
  Invoke-Test
  New-Item -ItemType Directory -Force dist | Out-Null
  $env:CGO_ENABLED = '0'
  foreach ($target in @('windows/amd64', 'linux/amd64', 'linux/arm64')) {
    $os, $arch = $target -split '/'
    $ext = if ($os -eq 'windows') { '.exe' } else { '' }
    Write-Host "dist : $os/$arch"
    $env:GOOS = $os; $env:GOARCH = $arch
    go build -trimpath -ldflags $ldflags -o "dist/openscale-$os-$arch$ext" ./cmd/openscale
    Assert-Success "build $target"
  }
  Remove-Item Env:GOOS, Env:GOARCH -ErrorAction Ignore
  Get-FileHash dist/openscale-* -Algorithm SHA256 |
    ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } |
    Set-Content dist/SHA256SUMS -Encoding utf8NoBOM
  Get-Content dist/SHA256SUMS
}

switch ($Target) {
  'help' { 'Cibles : test - vet - boundary - build - dist - cover - front - clean' }
  'vet' { Invoke-Vet }
  'boundary' { Invoke-Boundary }
  'test' { Invoke-Test }
  'cover' { Invoke-Cover }
  'build' { Invoke-Build }
  'front' { Invoke-Front }
  'dist' { Invoke-Dist }
  'clean' { Remove-Item -Recurse -Force bin, dist, coverage.out -ErrorAction Ignore }
  'all' { Invoke-Test; Invoke-Build }
}
