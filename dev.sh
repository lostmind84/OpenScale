#!/bin/sh
# Une seule commande entre un clone et un conteneur de développement lancé, pour qui n'a
# que Docker : quatre commandes documentées suffisent à rejouer six des sept contrôles de
# la CI (voir .devcontainer/), mais leurs pannes de premier lancement ne se racontent pas
# d'elles-mêmes -- Docker installé mais pas démarré, un utilisateur Linux pas encore dans
# le groupe docker (« permission denied ... /var/run/docker.sock », qui exige une
# déconnexion/reconnexion). Ce script fait trois contrôles DANS CET ORDRE et s'arrête sur
# le premier qui échoue, en disant quoi faire.
#
# CE SCRIPT N'INSTALLE RIEN -- ni Docker, ni Node. Les installer demande root, un
# installeur graphique sous Windows, et une déconnexion/reconnexion sous Linux ; un
# script du dépôt qui tenterait ça sur la machine de quelqu'un d'autre échouerait en
# silence, ce qu'une commande « qui vérifie et guide » existe justement pour éviter. Il
# contrôle, il nomme, il lance.
#
# Pas d'options, et c'est délibéré : ça garde la surface de parité avec dev.ps1 (voir
# deploy/parity_test.go) réduite à trois contrôles, plutôt qu'à une table de réglages à
# tenir en phase des deux côtés.
#
# /bin/sh et non bash : un contributeur Debian minimale ou Alpine n'a que dash, et ce
# script n'est pas l'endroit où découvrir qu'un shell manque.
set -eu

# --- Détection de la distribution, pour la ligne d'installation de Docker ou de Node -
#
# Le responsable du produit travaille sous Arch : une ligne « apt install » montrée à un
# utilisateur Arch est pire qu'aucune ligne, parce qu'elle se tape et échoue.
detect_distro() {
  if [ -r /etc/os-release ]; then
    info=$( . /etc/os-release && printf '%s %s' "${ID:-}" "${ID_LIKE:-}" )
    case "$info" in
      *ubuntu*|*debian*) echo debian; return ;;
      *arch*) echo arch; return ;;
      *fedora*) echo fedora; return ;;
    esac
  fi
  echo inconnue
}

docker_install_hint() {
  case "$(detect_distro)" in
    debian)
      echo '     sudo apt-get update && sudo apt-get install -y docker.io'
      echo '     sudo systemctl enable --now docker'
      ;;
    arch)
      echo '     sudo pacman -S docker'
      echo '     sudo systemctl enable --now docker'
      ;;
    fedora)
      echo '     sudo dnf install -y dnf-plugins-core'
      echo '     sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo'
      echo '     sudo dnf install -y docker-ce docker-ce-cli containerd.io'
      echo '     sudo systemctl enable --now docker'
      ;;
    *)
      if [ "$(uname -s)" = 'Darwin' ]; then
        echo '     Installez Docker Desktop pour Mac :'
        echo '     https://docs.docker.com/desktop/setup/install/mac-install/'
      else
        echo "     Distribution non reconnue par ce script -- suivez :"
        echo '     https://docs.docker.com/engine/install/'
      fi
      ;;
  esac
}

node_install_hint() {
  case "$(detect_distro)" in
    debian) echo '     sudo apt-get update && sudo apt-get install -y nodejs npm' ;;
    arch)   echo '     sudo pacman -S nodejs npm' ;;
    fedora) echo '     sudo dnf install -y nodejs npm' ;;
    *)
      if [ "$(uname -s)" = 'Darwin' ]; then
        echo '     brew install node'
      else
        echo "     Distribution non reconnue par ce script -- suivez :"
        echo '     https://nodejs.org/en/download'
      fi
      ;;
  esac
}

echo '1. Docker'
if ! docker info >/dev/null 2>&1; then
  if command -v docker >/dev/null 2>&1; then
    echo "   La commande docker existe mais ne répond pas. Deux causes possibles :"
    echo "     - Docker n'est pas démarré : démarrez Docker Desktop, ou"
    echo '       « sudo systemctl start docker »'
    if [ "$(uname -s)" = 'Linux' ]; then
      user=$(id -un 2>/dev/null || echo "${LOGNAME:-}")
      echo "     - votre utilisateur n'est pas dans le groupe docker (le message serait"
      echo '       « permission denied ... /var/run/docker.sock »). Corrigez avec :'
      echo "         sudo usermod -aG docker $user"
      echo "       PUIS DÉCONNECTEZ-VOUS ET RECONNECTEZ-VOUS : l'appartenance à un groupe"
      echo "       Unix ne se relit qu'à l'ouverture de session suivante -- un « docker"
      echo "       info » relancé dans le même terminal échouera encore."
    fi
  else
    echo "   Docker n'est pas installé. Sur cette machine :"
    docker_install_hint
  fi
  exit 1
fi
echo '   Docker répond.'

echo ''
echo '2. La CLI devcontainer'
# « devcontainer --version » et non « command -v devcontainer », pour la même raison que
# le contrôle 1 : sous WSL, le PATH de Windows s'invite dans celui de la distribution, et
# un « devcontainer » qui existe par ce chemin-là est souvent un binaire Node installé côté
# Windows (par exemple par nvm-windows) -- présent dans le PATH, injoignable au premier
# lancement réel, faute d'un Node accessible depuis Linux. Mesuré sur ce projet.
if ! devcontainer --version >/dev/null 2>&1; then
  echo '   La commande devcontainer est introuvable ou ne fonctionne pas. Installez-la :'
  echo '     npm i -g @devcontainers/cli'
  if ! command -v npm >/dev/null 2>&1; then
    echo ''
    echo "   npm est absent : installez Node d'abord."
    node_install_hint
  fi
  echo ''
  echo "   Un éditeur qui sait ouvrir un devcontainer (VS Code, Cursor, Windsurf) n'a"
  echo "   besoin d'aucun Node pour ça : son extension parle au démon Docker directement."
  exit 1
fi
echo '   devcontainer est disponible.'

echo ''
echo '3. Tout est présent -- lancement du conteneur de développement'
devcontainer up --workspace-folder .

echo ''
echo 'Poste prêt. Ce que vous pouvez rejouer depuis ce conteneur :'
echo '  devcontainer exec --workspace-folder . make test'
echo '  devcontainer exec --workspace-folder . make front-check'
echo '  devcontainer exec --workspace-folder . mkdocs build --strict'
