#!/bin/sh
# Ce qui s'installe une fois l'image construite.
#
# Ce fichier est commité en LF, et .gitattributes l'impose (`*.sh text eol=lf`) — la règle
# reste vraie ici, mais pas par le mécanisme qui l'a fait poser sur install.sh : ce shebang
# n'est jamais consulté, devcontainer.json lance ce fichier par `bash .devcontainer/post-
# create.sh`, pas par exécution directe. Un CRLF ici sortirait donc en `$'\r': command not
# found` de bash, pas en « Syntax error: word unexpected » de dash.
set -eu

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
npm ci --prefix web

echo ''
echo 'Poste prêt. Ce que vous pouvez rejouer ici :'
echo '  make test          les deux passes, -race comprise'
echo '  make front-check   types, tests et budget de l ecran client'
echo '  mkdocs build --strict'
echo ''
echo 'Ce que ce conteneur NE juge PAS : les scripts d installation sous Windows'
echo 'PowerShell 5.1. Le job « scripts » de la CI le fait a chaque pull request.'
