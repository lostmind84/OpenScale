#!/bin/sh
# Ce qui s'installe une fois l'image construite.
#
# Ce fichier est commité en LF, et .gitattributes l'impose (`*.sh text eol=lf`) — la règle
# reste vraie ici, mais pas par le mécanisme qui l'a fait poser sur install.sh : ce shebang
# n'est jamais consulté, devcontainer.json lance ce fichier par `bash .devcontainer/post-
# create.sh`, pas par exécution directe. Un CRLF ici sortirait donc en `$'\r': command not
# found` de bash, pas en « Syntax error: word unexpected » de dash.
set -eu

# Le poste Windows monte le dépôt tel quel, et tout y appartient à root alors que ce
# script tourne sous vscode : git refuse alors le dépôt pour « dubious ownership », et
# la panne ne se présente jamais sous ce nom. Le premier symptôme vu est « boundary: 1
# violation(s) — voir docs/02-architecture.md §5.2 », parce que `go list` perd son
# horodatage VCS sur ce même refus de git — rien n'y mentionne git ni les droits. Le
# --replace-all évite d'empiler des doublons si ce script est rejoué.
git config --global --replace-all safe.directory "$PWD"

# Docker crée sous root les PARENTS manquants d'une cible de montage, pas seulement la
# cible elle-même. $HOME/.cache est un tel parent — go-build en est la seule cible montée
# — et golangci-lint écrit dans $HOME/.cache/golangci-lint : sans ce chown récursif sur
# .cache entier, `go build` échoue sur « permission denied » au fond d'un cache, ce qui
# ne ressemble pas à un problème de montage.
sudo chown -R vscode:vscode "$HOME/go" "$HOME/.cache" web/node_modules

# golangci-lint s'installe HORS MODULE, et ADR-039 l'impose : `make deps` compare go.mod
# aux deux tables de §17.1 dans les deux sens, et une dépendance de développement inscrite
# là ouvrirait un écart permanent.
#
# La VERSION N'EST PAS ÉCRITE ICI : elle est lue dans le Makefile, qui en est la source
# unique — exactement ce que fait l'étape `make lint` de ci.yml.
golangci_version=$(make -s golangci-version)
install_dir=$(mktemp -d)
# Sous `set -e`, un `go install` qui échoue fait sortir le script AVANT le `rm -rf` de la
# dernière ligne du bloc : ce trap couvre ce chemin-là en plus du chemin heureux.
trap 'rm -rf "$install_dir"' EXIT
(
  cd "$install_dir"
  go mod init lintinstall >/dev/null 2>&1
  go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$golangci_version"
)
rm -rf "$install_dir"

# mkdocs, pour rejouer `mkdocs build --strict` avant de pousser. Le feature python installe
# son propre interpréteur : pip y fonctionne, là où le python système d'Ubuntu refuserait
# (PEP 668, « externally-managed-environment »).
pip install --no-cache-dir -r handbook/requirements.txt

# `npm ci` et non `npm install` : les versions sont gelées dans package-lock.json (§14.1),
# et `ci` est DÉTERMINISTE — il ÉCHOUE sur un lock désynchronisé au lieu de le réparer
# en silence, là où `install` l'aurait accepté et modifié sans le dire.
#
# La seconde tentative ne couvre PAS « npm a échoué », elle couvre UNE panne nommée, mesurée
# sur le premier lancement de ce conteneur : « spawnSync .../esbuild/bin/esbuild ETXTBSY »
# dans le postinstall d'esbuild. npm pose un lien dur vers le binaire de plateforme qu'il
# vient d'extraire puis l'exécute, pendant que sa propre phase reify peut encore tenir cet
# inode ouvert en écriture — et Linux refuse d'exécuter un fichier ouvert en écriture. C'est
# une course entre deux morceaux de npm, elle ne dit rien du lock, et elle frappe un volume
# web/node_modules NEUF : le premier lancement d'un contributeur, précisément.
#
# Ce que coûte de ne pas la rattraper dépasse la minute perdue : la CLI n'exécute
# postCreateCommand qu'à la CRÉATION du conteneur. Le conteneur à moitié préparé reste, le
# `devcontainer up` suivant répond « success » sans rien préparer, et dev.sh annonce « Poste
# prêt » sur un web/node_modules vide (voir la sortie de secours qu'il nomme désormais).
#
# Le filtre sur ETXTBSY est ce qui garde `ci` déterministe : tout autre échec — un lock
# désynchronisé au premier chef — sort ici, à la première tentative, sans être répété.
if ! npm_failure=$(npm --prefix web ci 2>&1); then
  printf '%s\n' "$npm_failure"
  case "$npm_failure" in
    *ETXTBSY*)
      echo ''
      echo 'ETXTBSY : course connue entre les écritures de npm et son propre postinstall.'
      echo 'Seconde et dernière tentative, sur un cache déjà chaud.'
      npm --prefix web ci
      ;;
    *)
      exit 1
      ;;
  esac
fi

echo ''
echo 'Poste prêt. Ce que vous pouvez rejouer ici :'
echo '  make test          les deux passes, -race comprise'
echo '  make front-check   types, tests et budget de l ecran client'
echo '  mkdocs build --strict'
echo ''
echo 'Ce que ce conteneur NE juge PAS : les scripts d installation sous Windows'
echo 'PowerShell 5.1. Le job « scripts » de la CI le fait a chaque pull request.'
