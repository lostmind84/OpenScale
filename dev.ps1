<#
.SYNOPSIS
  Vérifie qu'un poste peut lancer le conteneur de développement, et dit quoi faire sinon.

.DESCRIPTION
  Une seule commande entre un clone et un conteneur de développement lancé, pour qui n'a
  que Docker : le chemin conteneur du guide de démarrage (handbook/getting-started.md)
  rejoue déjà, à la main, tout ce que la CI vérifie sauf les scripts d'installation sous
  PowerShell 5.1, qu'aucun conteneur Linux ne peut exécuter. Mais ses pannes de premier
  lancement ne se racontent pas d'elles-mêmes -- Docker installé mais pas démarré, ou la
  CLI devcontainer trouvable sans être exécutable. Ce script fait trois contrôles DANS
  CET ORDRE et s'arrête sur le premier qui échoue, en disant quoi faire.

  CE SCRIPT N'INSTALLE RIEN -- ni Docker, ni Node. Les installer demande des droits
  administrateur, un installeur graphique, et pour Docker Desktop un redémarrage ; un
  script du dépôt qui tenterait ça sur la machine de quelqu'un d'autre échouerait en
  silence, ce qu'une commande « qui vérifie et guide » existe justement pour éviter. Il
  contrôle, il nomme, il lance.

  Pas d'options, et c'est délibéré : ça garde la surface de parité avec dev.sh (voir
  deploy/parity_test.go) réduite à trois contrôles, plutôt qu'à une table de réglages à
  tenir en phase des deux côtés.

  CE SCRIPT DOIT TOURNER SOUS WINDOWS POWERSHELL 5.1, comme make.ps1 : c'est le seul
  PowerShell garanti sur un poste Windows neuf. Aucune syntaxe propre à 7 (`??`, `?.`,
  l'opérateur ternaire) n'y est employée.

.EXAMPLE
  .\dev.ps1
#>

$ErrorActionPreference = 'Stop'

# Test-CommandRuns renvoie si l'appel réussit (code de sortie 0), sans jamais laisser une
# ÉCRITURE SUR STDERR transformer un succès en échec.
#
# Sous Windows PowerShell 5.1 -- pas sous pwsh 7 -- $ErrorActionPreference = 'Stop' rend
# TERMINANTE toute écriture sur le flux d'erreur d'une commande NATIVE dont la sortie est
# redirigée (« *> $null » ci-dessous), même quand cette commande réussit et n'écrit là
# qu'un avertissement. Mesuré sur ce poste, sous les deux PowerShell : un
# « cmd /c "echo err 1>&2 && exit 0" » réussi devient une exception attrapée par le
# catch sous 5.1, jamais sous 7. « docker info » sur un moteur WSL2 émet un « WARNING: »
# sur stderr, et « devcontainer --version » hérite de tout avertissement expérimental que
# Node écrit là -- les deux auraient donc été déclarés en panne sous 5.1 alors qu'ils
# répondent.
#
# La préférence est desserrée le temps de CET appel seulement, et remise aussitôt après :
# une commande ABSENTE lève toujours une exception, quelle que soit la préférence, donc la
# branche « non installé » des deux contrôles continue de fonctionner sans elle.
#
# NE PAS « SIMPLIFIER » CE DÉTOUR : il tient la seule différence mesurée entre 5.1 et 7 sur
# ce script, et aucun banc du dépôt ne peut l'exécuter pour le revérifier -- le parseur de
# deploy/powershell_test.go ANALYSE ces scripts, il ne les fait pas tourner.
function Test-CommandRuns([scriptblock]$Command) {
  $previous = $ErrorActionPreference
  try {
    $ErrorActionPreference = 'Continue'
    & $Command *> $null
    return ($LASTEXITCODE -eq 0)
  }
  catch {
    return $false
  }
  finally {
    $ErrorActionPreference = $previous
  }
}

Write-Host '1. Docker'

# « docker info » et non « Get-Command docker » : un Docker installé mais pas démarré est
# le cas ordinaire, et seul « docker info » distingue les deux. Get-Command ne sert
# ci-dessous qu'à CHOISIR le bon message une fois ce contrôle-là en échec.
$dockerReady = Test-CommandRuns { docker info }

if (-not $dockerReady) {
  if (Get-Command docker -ErrorAction SilentlyContinue) {
    Write-Host "   La commande docker existe mais ne répond pas. Cause la plus probable :"
    Write-Host "     - Docker Desktop n'est pas démarré : lancez-le depuis le menu Démarrer."
    Write-Host "     - (WSL2) la distribution Linux qui héberge le moteur n'est pas lancée."
    Write-Host ''
    Write-Host "   Il n'y a pas de groupe docker sous Windows : Docker Desktop expose son"
    Write-Host "   démon par un named pipe que gère son propre service, pas par les"
    Write-Host "   permissions d'un groupe Unix comme le fait dev.sh sous Linux."
  }
  else {
    Write-Host "   Docker n'est pas installé. Sur cette machine :"
    Write-Host '     Installez Docker Desktop :'
    Write-Host '     https://docs.docker.com/desktop/setup/install/windows-install/'
  }
  exit 1
}
Write-Host '   Docker répond.'

Write-Host ''
Write-Host '2. La CLI devcontainer'

# « devcontainer --version » et non « Get-Command devcontainer », pour la même raison que
# le contrôle 1 : une commande présente dans le PATH n'est pas forcément exécutable telle
# quelle -- voir le commentaire équivalent de dev.sh, où c'est mesuré sous WSL avec un
# devcontainer installé côté Windows mais injoignable depuis Linux.
$devcontainerReady = Test-CommandRuns { devcontainer --version }

if (-not $devcontainerReady) {
  Write-Host "   La commande devcontainer est introuvable ou ne fonctionne pas. Installez-la :"
  Write-Host '     npm i -g @devcontainers/cli'
  if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
    Write-Host ''
    Write-Host "   npm est absent : installez Node d'abord."
    Write-Host '     winget install OpenJS.NodeJS.LTS'
    Write-Host '     (ou : https://nodejs.org/en/download)'
  }
  Write-Host ''
  Write-Host "   Un éditeur qui sait ouvrir un devcontainer (VS Code, Cursor, Windsurf) n'a"
  Write-Host "   besoin d'aucun Node pour ça : son extension parle au démon Docker directement."
  exit 1
}
Write-Host '   devcontainer est disponible.'

Write-Host ''
Write-Host '3. Tout est présent -- lancement du conteneur de développement'

# Le message d'échec dit un piège qu'on ne devine pas, et c'est le même des deux côtés (voir
# le commentaire équivalent de dev.sh) : la CLI n'exécute postCreateCommand qu'à la CRÉATION
# du conteneur. Une préparation qui meurt en route laisse le conteneur EN PLACE, et le
# « devcontainer up » suivant répond {"outcome":"success"} sans rien préparer -- ce script
# annoncerait « Poste prêt » sur un web/node_modules vide.
devcontainer up --workspace-folder .
if ($LASTEXITCODE -ne 0) {
  Write-Host ''
  Write-Host "   Le lancement a échoué. Si la construction de l'image est passée et que"
  Write-Host "   c'est la PRÉPARATION qui a lâché, ne relancez pas cette commande telle"
  Write-Host '   quelle : la CLI ne rejoue la préparation que sur un conteneur NEUF.'
  Write-Host '   Repartez de :'
  Write-Host '     devcontainer up --workspace-folder . --remove-existing-container'
  exit $LASTEXITCODE
}

Write-Host ''
Write-Host 'Poste prêt. Ce que vous pouvez rejouer depuis ce conteneur :'
Write-Host '  devcontainer exec --workspace-folder . make test'
Write-Host '  devcontainer exec --workspace-folder . make front-check'
Write-Host '  devcontainer exec --workspace-folder . mkdocs build --strict'
