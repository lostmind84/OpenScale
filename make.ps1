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
  [ValidateSet('all', 'test', 'vet', 'boundary', 'deps', 'build', 'dist', 'release', 'cover', 'front', 'front-check', 'clean', 'help')]
  [string]$Target = 'all',

  # -Version impose le numéro au lieu de le dériver de l'histoire, comme
  # `make release VERSION=v0.1`. La publication s'en sert : dans une release, la version
  # EST le tag, et `git describe --dirty` suffixerait « -dirty » dès que l'arbre est
  # modifié — ce qu'il est juste après la reconstruction de l'écran client.
  [string]$Version = ''
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

# La chaîne Go peut avoir été installée dans la session courante sans que le PATH
# du shell parent le sache.
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$env:Path = "$machinePath;$userPath"

# --- Ce dont ce script a besoin, vérifié AVANT de faire quoi que ce soit ----------------
#
# Les deux contrôles ci-dessous existent parce que leurs pannes ne ressemblent pas à leur
# cause. Un script qui échoue sur « Get-FileHash n'est pas reconnu » se lit comme une
# chaîne Go cassée, et on cherche du côté de Go pendant vingt minutes.
$missing = @()
foreach ($needed in 'Get-FileHash', 'Compress-Archive', 'Expand-Archive') {
  if (-not (Get-Command $needed -ErrorAction SilentlyContinue)) { $missing += $needed }
}
if ($missing.Count -gt 0) {
  Write-Host ''
  Write-Host "Ce shell ne fournit pas : $($missing -join ', ')" -ForegroundColor Red
  Write-Host "Version de PowerShell : $($PSVersionTable.PSVersion)"
  Write-Host ''
  Write-Host 'Lancez la construction avec PowerShell 7 :'
  Write-Host '    pwsh -File ./make.ps1 <cible>'
  Write-Host ''
  Write-Host "PowerShell 7 s'installe par  winget install Microsoft.PowerShell"
  Write-Host 'Sous Linux ou macOS, `make <cible>` fait la meme chose sans PowerShell.'
  exit 1
}

# Or-Else remplace l'opérateur `??`, qui n'existe qu'à partir de PowerShell 7.
#
# CE SCRIPT DOIT TOURNER SOUS WINDOWS POWERSHELL 5.1, qui est le seul shell garanti sur
# une machine Windows : pwsh 7 s'installe, 5.1 est là. Avec `??`, le fichier ne PARSE pas
# sous 5.1 — l'erreur porte sur un jeton inattendu et ne dit rien du script, si bien que
# le premier réflexe est de croire la chaîne Go cassée.
function Or-Else($value, $fallback) {
  if ($null -eq $value -or $value -eq '') { return $fallback }
  return $value
}

$version = Or-Else $Version (Or-Else (git describe --tags --always --dirty 2>$null) 'dev')
$commit = Or-Else (git rev-parse --short HEAD 2>$null) 'unknown'
$date = Or-Else (git log -1 --format=%cI 2>$null) 'unknown'
$ldflags = "-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date"

# Write-Utf8NoBom écrit un fichier texte SANS marque d'ordre des octets.
#
# `Set-Content -Encoding utf8NoBOM` ferait la même chose, et n'existe qu'à partir de
# PowerShell 6. Le BOM compte ici : `sha256sum -c SHA256SUMS` sous Linux lit les trois
# octets de la marque comme le début du premier nom de fichier et déclare l'archive
# corrompue, ce qui est le contraire de ce que ce fichier sert à prouver.
function Write-Utf8NoBom([string]$path, [string[]]$lines) {
  $encoding = New-Object System.Text.UTF8Encoding($false)
  [System.IO.File]::WriteAllText($path, (($lines -join "`n") + "`n"), $encoding)
}

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

function Invoke-Deps {
  go run ./tools/deps
  Assert-Success 'make deps'
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
  Invoke-Deps
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
    Write-Host 'front : web/package.json absent — rien a construire'
  }
}

function Invoke-FrontCheck {
  Invoke-Front
  npm --prefix web run check; Assert-Success 'svelte-check'
  npm --prefix web test; Assert-Success 'vitest'
  npm --prefix web run budget; Assert-Success 'budget'
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
  $sums = Get-FileHash dist/openscale-* -Algorithm SHA256 |
    ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" }
  Write-Utf8NoBom 'dist/SHA256SUMS' $sums
  Get-Content dist/SHA256SUMS
}

function Invoke-Release {
  <#
  .SYNOPSIS
    Fabrique les archives de §17.2 : ce qu'un bénévole copie sur une clé USB.
  .DESCRIPTION
    Une archive par cible, contenant le binaire, les scripts et unités de deploy/, les
    deux documents écrits pour un bénévole, la licence, et la configuration livrée.

    La configuration livrée est l'EXPORT SANS MATÉRIEL de testdata/config-lacagette.json,
    produit par le binaire lui-même : §17.2 la décrit « SANS le bloc matériel », et une
    simple copie emporterait le COM8 et la file SATO WS408_2 du poste de développement,
    que la comparaison d'empreinte de §15.5 rejetterait.
  #>
  Invoke-Dist
  Invoke-Build

  $staging = 'dist/staging'
  Remove-Item -Recurse -Force $staging -ErrorAction Ignore
  foreach ($target in @('windows/amd64', 'linux/amd64', 'linux/arm64')) {
    $os, $arch = $target -split '/'
    $ext = if ($os -eq 'windows') { '.exe' } else { '' }
    $deployDir = if ($os -eq 'windows') { 'deploy/windows' } else { 'deploy/linux' }
    $name = "openscale-$version-$os-$arch"
    $stage = Join-Path $staging $name
    New-Item -ItemType Directory -Force $stage | Out-Null

    Copy-Item "dist/openscale-$os-$arch$ext" (Join-Path $stage "openscale$ext")
    Copy-Item "$deployDir/*" $stage
    Copy-Item 'INSTALLATION.md', 'TROUBLESHOOTING.md', 'LICENSE', 'THIRD-PARTY.md' $stage

    & ./bin/openscale.exe config export testdata/config-lacagette.json `
      --output (Join-Path $stage 'config-lacagette.json') | Out-Null
    Assert-Success 'openscale config export'

    $sums = Get-ChildItem -File $stage | Get-FileHash -Algorithm SHA256 |
      ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" }
    Write-Utf8NoBom (Join-Path $stage 'SHA256SUMS') $sums

    $archive = "dist/$name.zip"
    Remove-Item $archive -ErrorAction Ignore
    Compress-Archive -Path $stage -DestinationPath $archive
    Write-Host "release : $archive"
  }
  Remove-Item -Recurse -Force $staging -ErrorAction Ignore
  Get-ChildItem dist/*.zip | Select-Object Name, Length
}

switch ($Target) {
  'help' { 'Cibles : test - vet - boundary - deps - build - dist - release - cover - front - front-check - clean' }
  'vet' { Invoke-Vet }
  'boundary' { Invoke-Boundary }
  'deps' { Invoke-Deps }
  'test' { Invoke-Test }
  'cover' { Invoke-Cover }
  'build' { Invoke-Build }
  'front' { Invoke-Front }
  'front-check' { Invoke-FrontCheck }
  'dist' { Invoke-Dist }
  'release' { Invoke-Release }
  'clean' { Remove-Item -Recurse -Force bin, dist, coverage.out -ErrorAction Ignore }
  'all' { Invoke-Test; Invoke-Build }
}
