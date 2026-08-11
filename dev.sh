#!/bin/sh
# Une seule commande entre un clone et un conteneur de développement lancé, pour qui n'a
# que Docker : le chemin conteneur du guide de démarrage (handbook/getting-started.md)
# rejoue déjà, à la main, tout ce que la CI vérifie sauf les scripts d'installation sous
# PowerShell 5.1, qu'aucun conteneur Linux ne peut exécuter. Mais ses pannes de premier
# lancement ne se racontent pas d'elles-mêmes -- Docker installé mais pas démarré, un
# utilisateur Linux pas encore dans le groupe docker (« permission denied ...
# /var/run/docker.sock », qui exige une déconnexion/reconnexion), ou un devcontainer
# trouvable dans le PATH sans être exécutable. Ce script fait trois contrôles DANS CET
# ORDRE et s'arrête sur le premier qui échoue, en disant quoi faire.
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
      # docker-buildx est un paquet séparé sur Arch : sans lui, « docker info » répond,
      # mais le contrôle 3 (« devcontainer up », qui construit l'image) tombe sur
      # « BuildKit is enabled but the buildx component is missing or broken ».
      echo '     sudo pacman -S docker docker-buildx'
      echo '     sudo systemctl enable --now docker'
      ;;
    fedora)
      # Syntaxe DNF5 : Fedora en est équipée depuis la version 41, et toutes les versions
      # encore maintenues répondent « argument inconnu » à la syntaxe DNF4
      # (« --add-repo »). docker-buildx-plugin, même raison que docker-buildx sur Arch.
      echo '     sudo dnf install -y dnf-plugins-core'
      echo '     sudo dnf config-manager addrepo --from-repofile https://download.docker.com/linux/fedora/docker-ce.repo'
      echo '     sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin'
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
    echo "   La commande docker existe mais ne répond pas. Causes possibles :"
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
# L'échec n'est pas laissé à « set -e », parce que le message qui manque ici est celui d'un
# piège qu'on ne devine pas : la CLI n'exécute postCreateCommand qu'à la CRÉATION du
# conteneur. Une préparation qui meurt en route -- npm, pip, une coupure réseau -- laisse le
# conteneur EN PLACE, et le « devcontainer up » suivant répond {"outcome":"success"} sans
# rien préparer ; ce script annoncerait alors « Poste prêt » sur un web/node_modules vide, et
# le premier « make front-check » du contributeur échouerait sur un message qui ne parle ni
# du conteneur ni de sa préparation. Repartir d'un conteneur neuf est la seule sortie.
if ! devcontainer up --workspace-folder .; then
  echo ''
  echo "   Le lancement a échoué. Si la construction de l'image est passée et que c'est la"
  echo '   PRÉPARATION qui a lâché, ne relancez pas cette commande telle quelle : la CLI ne'
  echo '   rejoue la préparation que sur un conteneur NEUF. Repartez de :'
  echo '     devcontainer up --workspace-folder . --remove-existing-container'
  exit 1
fi

echo ''
echo 'Poste prêt. Ce que vous pouvez rejouer depuis ce conteneur :'
echo '  devcontainer exec --workspace-folder . make test'
echo '  devcontainer exec --workspace-folder . make front-check'
echo '  devcontainer exec --workspace-folder . mkdocs build --strict'
