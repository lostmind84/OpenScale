#!/bin/sh
# Installe ET met à jour un poste de pesée OpenScale en une seule commande — §15.3, §15.5.
#
# C'EST LE SEUL FICHIER LINUX DU PROJET QUI VIT HORS DE L'ARCHIVE. Il est téléchargé et
# exécuté d'une traite :
#
#     curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh | sudo sh
#
# La MÊME commande installe un poste neuf et met à jour un poste déjà installé : un bénévole
# n'a qu'une ligne à retrouver, et le script sait lequel des deux gestes s'applique.
#
# Ce qu'il fait, dans cet ordre :
#
#   1. CONTRÔLES PRÉALABLES — root, Linux, architecture, outils. Échouer sur les premières
#      lignes coûte moins cher qu'échouer après plusieurs dizaines de mégaoctets.
#   2. La dernière release, demandée à l'API. AUCUN NUMÉRO DE VERSION N'EST ÉCRIT ICI :
#      l'adresse ci-dessus pointe la branche main, ce fichier est donc téléchargé tel quel
#      pendant des années, et une version figée s'installerait indéfiniment.
#   3. LE POSTE EST-IL DÉJÀ À JOUR ? Si oui, on s'arrête sans toucher au service.
#   4. L'archive ET SHA256SUMS-archives.txt, puis la comparaison des empreintes. ★ AVANT
#      la décompression : extraire une archive non vérifiée écrit sur le disque des fichiers
#      dont on ne sait pas d'où ils viennent, et la ligne suivante en exécute un EN ROOT.
#   5. Décompression, et droit d'exécution rendu — unzip ne restaure pas les modes Unix de
#      toutes les archives.
#   6. install.sh sur un poste neuf, update.sh sur un poste installé. ★ JAMAIS install.sh
#      sur un poste installé : c'est update.sh qui porte l'arrêt contrôlé, la sauvegarde
#      horodatée du binaire et le RETOUR ARRIÈRE AUTOMATIQUE.
#   7. Le dossier extrait est conservé sous /var/lib/openscale/installer. install.sh ne
#      copie aucun script : sans cette étape, le poste n'aurait ni désinstalleur ni script
#      de mise à jour, et TROUBLESHOOTING.md enverrait un bénévole chercher un fichier
#      disparu.
#
# CE SCRIPT NE POSE AUCUNE QUESTION LUI-MÊME, et la raison est technique : sous
# « curl … | sh », l'entrée standard EST le script, et un « read » y avalerait la suite du
# fichier au lieu d'attendre un humain.
#
# Les six questions de la version Windows se partagent alors en deux, et le partage n'est
# plus celui qui était écrit ici :
#
#   CE QUI N'A TOUJOURS RIEN À DEMANDER — le mot de passe du compte, qui n'existe pas (le
#   compte openscale n'a ni mot de passe ni interpréteur de commandes), le mode pilote, qui
#   n'a pas d'objet (l'application Access ne tourne pas sous Linux), et l'ouverture de
#   session automatique, que le kiosque n'attend pas (c'est une unité systemd qu'install.sh
#   active toujours).
#
#   CE QUI EN A, ET QUE CE SCRIPT NE FAIT QUE RELAYER — le numéro de ce poste, son nom et
#   son mot de passe d'administration. install.sh les demande LUI-MÊME quand son entrée
#   standard est un terminal, ce qui est le cas d'une installation lancée depuis une clé
#   USB et jamais celui d'un « curl … | sh ». Ce script ne lui coupe donc pas la parole : il
#   lui passe ses options et le laisse décider.
#
# COMMENT ON DONNE CES TROIS VALEURS À UN BOOTSTRAP :
#
#   curl -fsSL <url> | sudo sh -s -- --station-number 2 --station-name 'Poste 2 - fruits'
#
#   Et pour le mot de passe d'administration, par l'ENVIRONNEMENT plutôt que par une
#   option : /proc/<pid>/cmdline est lisible par tous les comptes de la machine,
#   /proc/<pid>/environ par le seul propriétaire du processus.
#
#   curl -fsSL <url> | sudo OPENSCALE_ADMIN_PASSWORD='...' sh -s -- --yes --station-number 2
#
#   ★ ET C'EST PAR L'ENVIRONNEMENT QUE LE SECRET DESCEND VERS install.sh, même quand il est
#   arrivé ici par --admin-password : le relayer en argument le publierait une seconde fois,
#   dans la ligne de commande d'un processus lancé en root. C'est la règle de bootstrap.ps1,
#   qui appelle install.ps1 DANS LE MÊME PROCESSUS pour la même raison.
#
# POUR UN POSTE SANS INTERNET, rien de tout cela ne sert : l'archive se copie sur une clé
# USB et install.sh se lance seul, comme avant. Voir INSTALLATION.md.
#
# Options de ce script :
#   --version vX      installe ce tag au lieu de la dernière release (aligner un poste sur
#                     les autres, ou revenir en arrière)
#   --force           met à jour même si le poste porte déjà cette version
#   --force-install   relance install.sh sur un poste installé, au lieu d'update.sh — le
#                     geste de réparation de TROUBLESHOOTING.md
#
# Options relayées telles quelles à install.sh, et à lui seul (une mise à jour ne
# reconfigure rien) : --station-number, --station-name, --admin-password, --yes,
# --install-dir, --data-dir.
#
# /bin/sh et non bash : Debian minimale a dash, et un script d'installation n'est pas
# l'endroit où découvrir qu'un shell manque.

set -eu

PRODUCT='OpenScale'
REPOSITORY='lostmind84/OpenScale'
API_HOST='api.github.com'
RAW_URL="https://raw.githubusercontent.com/$REPOSITORY/main/deploy/linux/bootstrap.sh"
CHECKSUM_ASSET='SHA256SUMS-archives.txt'
USER_AGENT='OpenScale-bootstrap'
BINARY='/usr/local/bin/openscale'
UNIT='/etc/systemd/system/openscale.service'
INSTALLER_DIR='/var/lib/openscale/installer'

FORCE=0
FORCE_INSTALL=0
WANTED_VERSION=''

# CE QUI DESCEND VERS install.sh, une variable par option plutôt qu'une liste accumulée à
# la volée. sh POSIX n'a qu'un seul tableau — les paramètres positionnels —, et le remplir
# pendant qu'on le parcourt fait relire à la boucle ce qu'elle vient d'y ajouter. Les
# reconstruire au moment de l'appel, comme install.sh reconstruit les siens, coûte six noms
# recopiés et se relit sans réfléchir.
#
# ADMIN_PASSWORD accepte aussi l'environnement, et la variable est RETIRÉE de
# l'environnement tout de suite : sans cela, curl, unzip et sha256sum en hériteraient tous
# dans leur propre /proc/<pid>/environ.
ADMIN_PASSWORD=${OPENSCALE_ADMIN_PASSWORD:-}
unset OPENSCALE_ADMIN_PASSWORD
STATION_NUMBER=''
STATION_NAME=''
INSTALL_DIR=''
DATA_DIR=''
ASSUME_YES=0

log() { printf '  %s\n' "$1"; }
fail() { printf 'bootstrap.sh : %s\n' "$1" >&2; exit 1; }

# usage est écrit en toutes lettres et non extrait de l'en-tête : sous « curl … | sh », ce
# fichier n'existe nulle part sur le disque, et il n'y a donc rien à relire.
usage() {
  printf 'bootstrap.sh — installe et met à jour un poste de pesée %s.\n\n' "$PRODUCT"
  printf '  curl -fsSL %s | sudo sh\n\n' "$RAW_URL"
  printf '  --version vX      installe ce tag au lieu de la dernière release\n'
  printf '  --force           met à jour même si le poste porte déjà cette version\n'
  printf '  --force-install   relance install.sh au lieu de update.sh, pour réparer un poste\n'
  printf '\n  Relayées à install.sh, sur une installation seulement :\n'
  printf '  --station-number <n>       le numéro de ce poste dans la coopérative\n'
  printf '  --station-name <texte>     le nom que lisent les bénévoles\n'
  printf '  --admin-password <secret>  le mot de passe d'"'"'administration. Préférez la variable\n'
  printf '                             OPENSCALE_ADMIN_PASSWORD : une ligne de commande est\n'
  printf '                             lisible par tout utilisateur de la machine\n'
  printf '  --yes                      ne pose aucune question et n'"'"'attend personne\n'
  printf '  --install-dir <chemin>     le répertoire du binaire\n'
  printf '  --data-dir <chemin>        le répertoire des données du poste\n'
}

# fetch rend le corps de la réponse sur la sortie standard ; download l'écrit dans un
# fichier. curl OU wget : une Debian minimale a rarement les deux, et jamais aucun des deux
# n'est un cas qu'on peut réparer ici.
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -H "User-Agent: $USER_AGENT" "$1"
  else
    wget -qO- --header="User-Agent: $USER_AGENT" "$1"
  fi
}
download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -H "User-Agent: $USER_AGENT" -o "$2" "$1"
  else
    wget -qO "$2" --header="User-Agent: $USER_AGENT" "$1"
  fi
}

# Le « v » de tête est le seul écart de forme entre un tag et ce que le binaire annonce :
# v0.1 et 0.1 nomment la même release, et internal/update/version.go le dit aussi.
without_v() { printf '%s' "${1#v}"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --force) FORCE=1 ;;
    --force-install) FORCE_INSTALL=1 ;;
    --version)
      shift
      [ $# -gt 0 ] || fail 'option --version : le tag à installer manque'
      WANTED_VERSION="$1"
      ;;
    --version=*) WANTED_VERSION="${1#--version=}" ;;
    --station-number)
      shift
      [ $# -gt 0 ] || fail 'option --station-number : le numéro manque'
      STATION_NUMBER="$1"
      ;;
    --station-number=*) STATION_NUMBER="${1#--station-number=}" ;;
    --station-name)
      shift
      [ $# -gt 0 ] || fail 'option --station-name : le nom manque'
      STATION_NAME="$1"
      ;;
    --station-name=*) STATION_NAME="${1#--station-name=}" ;;
    --admin-password)
      shift
      [ $# -gt 0 ] || fail 'option --admin-password : le mot de passe manque'
      ADMIN_PASSWORD="$1"
      ;;
    --admin-password=*) ADMIN_PASSWORD="${1#--admin-password=}" ;;
    --install-dir)
      shift
      [ $# -gt 0 ] || fail 'option --install-dir : le répertoire manque'
      INSTALL_DIR="$1"
      ;;
    --install-dir=*) INSTALL_DIR="${1#--install-dir=}" ;;
    --data-dir)
      shift
      [ $# -gt 0 ] || fail 'option --data-dir : le répertoire manque'
      DATA_DIR="$1"
      ;;
    --data-dir=*) DATA_DIR="${1#--data-dir=}" ;;
    --yes) ASSUME_YES=1 ;;
    -h|--help)
      usage
      exit 0
      ;;
    *) fail "option inconnue : $1" ;;
  esac
  shift
done

# --- 1. Contrôles préalables ---------------------------------------------------------
# Lancé sans droits, ce script s'arrête sur la commande à retaper et non sur un
# « Permission denied » au premier install, une fois l'archive téléchargée.
[ "$(id -u)" -eq 0 ] || fail "à lancer en root : curl -fsSL $RAW_URL | sudo sh"

SYSTEM=$(uname -s)
[ "$SYSTEM" = 'Linux' ] || fail "ce script installe un poste Linux, et cette machine est un $SYSTEM"

# L'ARCHITECTURE EST DÉCIDÉE AVANT TOUT TÉLÉCHARGEMENT. Le parc a des mini-PC amd64 et des
# Raspberry Pi arm64, release.yml publie une archive pour chacun, et se tromper produit
# « cannot execute binary file: Exec format error » — un message qui accuse le binaire alors
# que l'erreur a été faite trois étapes plus tôt.
MACHINE=$(uname -m)
case "$MACHINE" in
  x86_64|amd64) ARCH='amd64' ;;
  aarch64|arm64) ARCH='arm64' ;;
  *) fail "architecture « $MACHINE » : $PRODUCT est publié pour x86_64 et aarch64" ;;
esac

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  fail 'ni curl ni wget sur cette machine : apt-get install -y curl'
fi

# unzip n'est PAS sur une Debian 12 minimale, et « unzip: command not found » arriverait
# après le téléchargement, sur un poste où l'on croyait avoir fini.
if ! command -v unzip >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    log "installation d'unzip"
    apt-get install --no-install-recommends -y unzip
  else
    fail 'unzip est absent : installez-le avec le gestionnaire de paquets de cette distribution'
  fi
fi

printf '\n'
printf '=========================================================================\n'
printf " Installation d'un poste de pesée %s\n" "$PRODUCT"
printf '=========================================================================\n'

# --- 2. La release --------------------------------------------------------------------
# /releases/latest et non /releases : ce point de l'API exclut les brouillons et les
# pré-versions PAR CONTRAT, ce qui évite d'avoir à les filtrer ici.
if [ -n "$WANTED_VERSION" ]; then
  RELEASE_URL="https://$API_HOST/repos/$REPOSITORY/releases/tags/$WANTED_VERSION"
else
  RELEASE_URL="https://$API_HOST/repos/$REPOSITORY/releases/latest"
fi

printf '\n'
log 'recherche de la version à installer...'
RELEASE=$(fetch "$RELEASE_URL") || fail "impossible de joindre $API_HOST. Ce poste a-t-il accès à Internet ? Sinon, installez depuis une clé USB : voir INSTALLATION.md."

# Le JSON de l'API tient sur une seule ligne : « tr , \n » en fait des lignes, sur
# lesquelles un sed non glouton dit ce qu'on lui demande. Aucune valeur utile ici ne
# contient de virgule.
json_strings() { printf '%s' "$RELEASE" | tr ',' '\n' | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p"; }

TAG=$(json_strings 'tag_name' | head -n 1)
[ -n "$TAG" ] || fail "la réponse de $API_HOST ne porte aucun tag : la release demandée existe-t-elle ?"

ARCHIVE_URL=$(json_strings 'browser_download_url' | grep -- "-linux-$ARCH\.zip$" | head -n 1)
CHECKSUM_URL=$(json_strings 'browser_download_url' | grep -- "/$CHECKSUM_ASSET$" | head -n 1)
[ -n "$ARCHIVE_URL" ] || fail "la release $TAG ne publie aucune archive pour linux-$ARCH"
[ -n "$CHECKSUM_URL" ] || fail "la release $TAG ne publie pas $CHECKSUM_ASSET : il n'y a rien à quoi comparer ce qui va être téléchargé, et rien ne sera installé"
ARCHIVE_NAME=${ARCHIVE_URL##*/}
log "version $TAG — $ARCHIVE_NAME"

# --- 3. Le poste est-il déjà à jour ? ------------------------------------------------
# ★ CE GARDE N'EST PAS UNE COMMODITÉ. Ce one-liner sera relancé par réflexe sur des postes
# déjà à jour ; sans lui, chacune de ces exécutions arrêterait le service, réécrirait le
# binaire avec les mêmes octets et le redémarrerait — EN PLEINE JOURNÉE DE VENTE.
#
# Un binaire de développement s'annonce « dev » et n'est égal à aucun tag : il ne déclenche
# donc jamais ce garde, et c'est voulu.
INSTALLED_VERSION=''
if [ -x "$BINARY" ]; then
  INSTALLED_VERSION=$("$BINARY" --version 2>/dev/null | awk '{print $2}') || INSTALLED_VERSION=''
fi
if [ "$FORCE" -eq 0 ] && [ -n "$INSTALLED_VERSION" ] &&
  [ "$(without_v "$INSTALLED_VERSION")" = "$(without_v "$TAG")" ]; then
  printf '\n'
  log "ce poste est déjà en $INSTALLED_VERSION : rien à faire, le service n'a pas été touché."
  log 'Pour réinstaller cette version quand même : ajoutez --force.'
  exit 0
fi

# --- 4. Téléchargement, puis vérification AVANT toute décompression -------------------
WORKSPACE=$(mktemp -d)
trap 'rm -rf "$WORKSPACE"' EXIT INT TERM

log 'téléchargement...'
download "$ARCHIVE_URL" "$WORKSPACE/$ARCHIVE_NAME"
download "$CHECKSUM_URL" "$WORKSPACE/$CHECKSUM_ASSET"

# Le fichier est celui que produit « sha256sum *.zip » : « <empreinte>  <nom> ». On y
# cherche le NOM, pas la position d'une ligne.
EXPECTED=$(awk -v name="$ARCHIVE_NAME" '{ sub(/^\*/, "", $2); if ($2 == name) { print $1; exit } }' \
  "$WORKSPACE/$CHECKSUM_ASSET")
[ -n "$EXPECTED" ] || fail "$CHECKSUM_ASSET ne porte aucune empreinte pour $ARCHIVE_NAME"
ACTUAL=$(sha256sum "$WORKSPACE/$ARCHIVE_NAME" | cut -d ' ' -f 1)
if [ "$ACTUAL" != "$EXPECTED" ]; then
  rm -f "$WORKSPACE/$ARCHIVE_NAME"
  fail "l'archive téléchargée ne correspond pas à son empreinte publiée. Attendu $EXPECTED, obtenu $ACTUAL. Rien n'a été installé, et le fichier a été supprimé."
fi
log 'empreinte vérifiée'

# --- 5. Décompression -----------------------------------------------------------------
unzip -q "$WORKSPACE/$ARCHIVE_NAME" -d "$WORKSPACE"
EXTRACTED="$WORKSPACE/${ARCHIVE_NAME%.zip}"
[ -d "$EXTRACTED" ] || fail "l'archive $ARCHIVE_NAME ne contient pas le dossier attendu"
# Linux ne marque pas ce qui vient d'Internet — il n'y a pas d'Unblock-File à faire ici.
# Le droit d'exécution, en revanche, ne survit pas à toutes les archives.
chmod +x "$EXTRACTED/openscale" "$EXTRACTED"/*.sh
log "décompressé dans $EXTRACTED"

# --- 6. Installation ou mise à jour ----------------------------------------------------
# ★ SUR UN POSTE INSTALLÉ, C'EST update.sh ET JAMAIS install.sh. install.sh est idempotent
# et « marcherait » ; il perdrait exactement ce qui distingue une mise à jour d'une
# installation — l'arrêt contrôlé sur le budget de §13.4, la sauvegarde horodatée du
# binaire, la vérification de /healthz et la restauration automatique de la version
# précédente. Un binaire fautif laisserait le poste à l'arrêt, sans rien à remettre.
#
# C'est l'update.sh de l'archive qui vient d'arriver : le script de la version qui arrive
# pilote sa propre mise à jour, comme le fait internal/update/stager.go sous Windows.
printf '\n'
if [ -x "$BINARY" ] && [ -f "$UNIT" ] && [ "$FORCE_INSTALL" -eq 0 ]; then
  log "poste déjà installé en ${INSTALLED_VERSION:-version inconnue} : mise à jour vers $TAG"
  # Les options d'identité ne descendent PAS ici, et ce n'est pas un oubli : une mise à jour
  # ne reconfigure rien. Un poste qui reçoit une version nouvelle garde son numéro, son nom
  # et son mot de passe — les redemander à chaque mise à jour serait la meilleure façon
  # d'en changer un par accident, un mardi, sur un poste qui marchait.
  sh "$EXTRACTED/update.sh"
else
  # ★ CE SCRIPT VIT SUR main, LES ARCHIVES SONT FIGÉES À LEUR TAG. --version installe une
  # release antérieure, dont l'install.sh ne connaît pas forcément les options
  # d'aujourd'hui, et une option inconnue y est un refus net — APRÈS le téléchargement,
  # l'empreinte et la décompression, c'est-à-dire au pire moment pour perdre un bénévole.
  # On la retire, et on dit ce que sa perte coûte : « elle est ignorée » laisserait
  # quelqu'un devant un poste qui ne se comporte pas comme la notice.
  set --
  if [ -n "$STATION_NUMBER" ]; then
    set -- "$@" --station-number "$STATION_NUMBER"
  fi
  if [ -n "$STATION_NAME" ]; then
    set -- "$@" --station-name "$STATION_NAME"
  fi
  if [ -n "$INSTALL_DIR" ]; then
    set -- "$@" --install-dir "$INSTALL_DIR"
  fi
  if [ -n "$DATA_DIR" ]; then
    set -- "$@" --data-dir "$DATA_DIR"
  fi
  if [ "$ASSUME_YES" -eq 1 ]; then
    set -- "$@" --yes
  fi
  # La liste tourne sur elle-même : chaque option est retirée en tête et remise en queue si
  # elle survit. Le compteur est ce qui borne le tour — sans lui, la boucle relirait sans
  # fin ce qu'elle vient de remettre.
  INSTALLER="$EXTRACTED/install.sh"
  ROUNDS=$#
  while [ "$ROUNDS" -gt 0 ]; do
    OPTION="$1"
    shift
    ROUNDS=$((ROUNDS - 1))
    VALUE=''
    HAS_VALUE=0
    case "$OPTION" in
      --yes) ;;
      *)
        VALUE="$1"
        shift
        ROUNDS=$((ROUNDS - 1))
        HAS_VALUE=1
        ;;
    esac
    # L'étiquette du « case » de install.sh, et non le nom seul : « --station-number » se
    # lit aussi dans sa prose, qui survit à une version qui n'aurait plus l'option.
    if grep -q -F -- "$OPTION)" "$INSTALLER"; then
      if [ "$HAS_VALUE" -eq 1 ]; then
        set -- "$@" "$OPTION" "$VALUE"
      else
        set -- "$@" "$OPTION"
      fi
      continue
    fi
    log "la version $TAG ne connaît pas l'option $OPTION : elle est ignorée."
    case "$OPTION" in
      --station-number) log "le numéro du poste se posera sur l'écran d'administration." ;;
      --station-name) log "le nom du poste se posera sur l'écran d'administration." ;;
      --install-dir|--data-dir) log "les emplacements par défaut seront employés." ;;
      --yes) log "l'installeur posera ses questions s'il a quelqu'un devant lui." ;;
    esac
  done

  # ★ LE SECRET NE PASSE PAS PAR LA LIGNE DE COMMANDE. Il descend par l'ENVIRONNEMENT du
  # seul processus qui en a besoin : /proc/<pid>/cmdline est lisible par tous les comptes de
  # la machine, /proc/<pid>/environ par le seul propriétaire du processus. C'est l'équivalent
  # de l'appel « dans le même processus » de bootstrap.ps1, qui refuse pour cette raison
  # exacte de s'élever quand on lui a donné un mot de passe.
  if [ -n "$ADMIN_PASSWORD" ] && ! grep -q -F 'OPENSCALE_ADMIN_PASSWORD' "$INSTALLER"; then
    ADMIN_PASSWORD=''
    log "la version $TAG ne lit pas OPENSCALE_ADMIN_PASSWORD : le mot de passe d'administration se posera à l'écran, avec le code de secours de la fiche."
  fi
  if [ -n "$ADMIN_PASSWORD" ]; then
    OPENSCALE_ADMIN_PASSWORD="$ADMIN_PASSWORD" sh "$EXTRACTED/install.sh" "$@"
  else
    sh "$EXTRACTED/install.sh" "$@"
  fi
fi

# --- 7. Les scripts survivent à l'installation ----------------------------------------
# install.sh copie le binaire, les unités, la configuration livrée et les notices. Il ne
# copie AUCUN script : update.sh et uninstall.sh ne survivaient jusqu'ici que parce que
# l'archive restait dans le répertoire de qui l'avait décompressée. Un poste installé depuis
# un répertoire temporaire n'aurait pas de désinstalleur.
KEPT="$INSTALLER_DIR/$TAG"
rm -rf "$KEPT"
mkdir -p "$INSTALLER_DIR"
mv "$EXTRACTED" "$INSTALLER_DIR/$TAG"

printf '\n'
printf ' Les scripts de ce poste (mise à jour, désinstallation) sont dans :\n'
printf '      %s\n' "$KEPT"
printf '\n'
printf ' Prochaine mise à jour : relancez la même commande, ou\n'
printf '      sudo %s/update.sh --latest\n' "$KEPT"
