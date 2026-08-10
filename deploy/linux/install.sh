#!/bin/sh
# Installe un poste de pesée OpenScale sur une Debian 12 minimale — §15.3.
#
#     sudo ./install.sh
#     sudo ./install.sh --station-number 2 --station-name 'Poste 2 - fruits'
#     sudo OPENSCALE_ADMIN_PASSWORD='...' ./install.sh --yes --station-number 2 \
#          --station-name 'Poste 2 - fruits'
#
# Ce que le script fait, dans cet ordre :
#
#   1. installe cage, chromium et seatd — le kiosque Wayland mono-application ;
#   2. crée le compte openscale, dans les groupes dialout (SÉRIE) et lp (IMPRIMANTE) ;
#   3. pose le binaire, les répertoires, la documentation et la configuration livrée ;
#   4. POSE CE QUE SEULE L'INSTALLATION PEUT DEMANDER — le numéro de ce poste, son nom et
#      son mot de passe d'administration — et déclare la balance ABSENTE sur un poste NEUF.
#      Sans cette étape, le poste sortait de l'installeur en configuration d'usine, avec
#      trois fautes, et sans autre porte vers ses réglages que le code de secours de la
#      fiche qu'on venait justement de ranger au classeur ;
#   5. tire le CODE DE SECOURS quand le fichier n'en porte pas déjà un, et l'imprime sur la
#      fiche : c'est lui, et lui seul, qui rouvre un poste qui n'a AUCUN mot de passe ;
#   6. pose les règles udev et polkit, puis les deux unités systemd ;
#   7. active et démarre le service, puis vérifie /healthz ;
#   8. écrit la fiche d'installation.
#
# Il est IDEMPOTENT : le relancer sur un poste déjà installé remet tout en place sans rien
# casser. C'est le geste que TROUBLESHOOTING.md recommande sur un poste qui marche mal, et
# c'est lui qui décide de deux règles d'ici : la balance n'est déclarée absente que sur un
# poste NEUF — l'éteindre sur un poste en service le ferait passer en saisie manuelle un
# samedi matin —, et le code de secours n'est JAMAIS renouvelé, parce que la fiche déjà
# rangée au classeur doit rester vraie.
#
# LES QUESTIONS, ET QUAND ELLES SE POSENT
#
#   Une valeur qui manque est DEMANDÉE quand il y a quelqu'un devant ce script ; sinon
#   l'installation continue sans bloquer et le journal dit ce qui n'a pas été posé.
#
#   ★ « QUELQU'UN DEVANT CE SCRIPT » SE TESTE PAR « [ -t 0 ] », ET C'EST CE TEST-LÀ QUI
#   COMPTE. Sous « curl … | sh », l'entrée standard EST le script : un « read » n'y attend
#   personne, il avale la suite du fichier et l'installation part en morceaux. L'entrée
#   attachée à un terminal est donc la question à poser, et --yes est le choix explicite de
#   n'en poser aucune. C'est l'équivalent exact de Test-Interactive côté Windows, dont les
#   deux moitiés utiles — UserInteractive et IsInputRedirected — se ramènent ici à ce seul
#   test : un shell n'a pas de station de fenêtres, et il n'a pas non plus de commutateur
#   -NonInteractive que la troisième moitié va chercher sur la ligne de commande.
#
#   Le mot de passe se tape SANS ÉCHO et se confirme (ask_secret, plus bas).
#
# CE QUI N'A DÉLIBÉRÉMENT PAS D'ÉQUIVALENT DE CE CÔTÉ-CI
#
#   Trois options de install.ps1 ne sont pas ici, et leur absence est un arbitrage plutôt
#   qu'un retard. Les nommer coûte trois lignes ; les taire coûte la question à chaque
#   relecture, et la règle du dépôt est qu'une modification validée sur l'un des deux
#   installeurs se reporte sur l'autre — donc que ce qui ne se reporte PAS s'écrive.
#
#   -AccountPassword : le compte openscale de ce poste n'a ni mot de passe ni interpréteur
#     de commandes (useradd --shell /usr/sbin/nologin, aucun --password), il n'ouvre aucune
#     session et rien ne se tape en son nom. Il n'y a pas de secret à choisir.
#   -SkipAutoLogon : Windows a besoin d'une ouverture de session automatique parce que son
#     kiosque est une tâche déclenchée par une session. Ici l'écran client est une unité
#     systemd (openscale-kiosk.service, WantedBy=multi-user.target, PAMName=login) : elle
#     démarre à l'allumage de la machine, sans que personne ouvre de session. Il n'y a donc
#     rien à activer, et rien à sauter. Un poste qui ne serait PAS en libre-service —
#     l'unique usage de -SkipAutoLogon — se règle après coup en une commande :
#     « systemctl disable --now openscale-kiosk.service ».
#   -Pilot : ce mode existe pour la période pilote de L9, qui laisse l'application Access
#     relançable en moins de deux minutes ; il installe pour cela le service en démarrage
#     manuel. Access est une application Windows et ne tourne pas ici : il n'y a aucun
#     ancien poste à laisser reprendre la main sur cette machine.
#
# /bin/sh et non bash : Debian minimale a dash, et un script d'installation n'est pas
# l'endroit où découvrir qu'un shell manque. Pas de « read -s », pas de tableaux, pas de
# doubles crochets — dash refuse les trois.

set -eu

PRODUCT='OpenScale'
SERVICE='openscale'
ACCOUNT='openscale'

# LES EMPLACEMENTS LIVRÉS, sous leur propre nom, parce que deux choses en ont besoin : les
# valeurs par défaut ci-dessous, et la réécriture des unités quand --install-dir ou
# --data-dir a déplacé quelque chose (place_unit). Ce sont ceux que internal/platform/
# paths.go compile et que les deux unités nomment en clair.
STOCK_BINARY='/usr/local/bin/openscale'
STOCK_DATA_DIR='/var/lib/openscale'

BINARY="$STOCK_BINARY"
DATA_DIR="$STOCK_DATA_DIR"
# /etc/openscale n'est PAS déplaçable, et ce n'est pas un oubli : §11.1 sépare exprès ce
# qu'un opérateur édite et sauvegarde (/etc) de ce que le poste écrit (/var/lib). Une
# option --config-dir déferait cette séparation sur le poste qui l'emploierait, et c'est
# précisément ce que le document a tranché. Sous Windows la question ne se pose pas : la
# configuration et les données y vivent sous une même racine, d'où -DataRoot là-bas.
CONFIG_DIR='/etc/openscale'
CONFIG="$CONFIG_DIR/config.json"
LOG_DIR='/var/log/openscale'
DOC_DIR='/usr/share/doc/openscale'
UNIT_DIR='/etc/systemd/system'
HERE=$(cd "$(dirname "$0")" && pwd)

# Le plancher du mot de passe d'ADMINISTRATION, en points de code.
#
# CE CHIFFRE N'EST PAS D'ICI : l'autorité est web.MinPasswordLength, dans
# internal/web/argon2id.go, que l'écran de secours et « openscale config password »
# appliquent tous les deux. Il est recopié parce qu'un script shell ne sait pas lire une
# constante Go — common.ps1 le recopie pour la même raison et le dit dans les mêmes termes.
# Il sert à refuser AVANT la confirmation plutôt qu'après : le binaire refuse la même chose,
# mais une fois le mot de passe tapé deux fois.
MIN_ADMIN_PASSWORD_LENGTH=4

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1"; }
say() { printf '%s\n' "$1"; }
note() { printf '%s\n' "$1" >&2; }
fail() { printf 'install.sh : %s\n' "$1" >&2; exit 1; }

usage() {
  cat <<USAGE
install.sh — installe un poste de pesée $PRODUCT sur cette machine.

    sudo ./install.sh [options]

CE QUE L'INSTALLEUR DEMANDE quand il a quelqu'un devant lui et que personne n'y a répondu
d'avance. Donné ici, il ne demande rien :

  --station-number <n>       le numéro de ce poste dans la coopérative. C'est de lui que
                             dérive le nom du fichier de catalogue surveillé, flv_<n>.csv
  --station-name <texte>     le nom que lisent les bénévoles, « Poste 2 - fruits »
  --admin-password <secret>  le mot de passe d'administration du poste, celui qui donne le
                             droit d'en changer les réglages. ★ UN ARGUMENT DE LIGNE DE
                             COMMANDE EST LISIBLE PAR TOUT LE MONDE : /proc/<pid>/cmdline
                             l'est pour tous les comptes de la machine, et « ps » l'affiche.
                             Préférez la variable d'environnement OPENSCALE_ADMIN_PASSWORD,
                             qui ne passe que par /proc/<pid>/environ — lisible du seul
                             propriétaire du processus. Mieux encore : ne donnez rien, et
                             répondez à la question, qui n'écrit nulle part

LE RESTE :

  --yes                      ne pose AUCUNE question et n'attend personne. Sans lui, une
                             entrée standard qui n'est pas un terminal est reconnue toute
                             seule et les questions sont sautées de la même façon
  --install-dir <chemin>     le répertoire du binaire (défaut $(dirname "$STOCK_BINARY"))
  --data-dir <chemin>        le répertoire des données du poste (défaut $STOCK_DATA_DIR)
  --help                     affiche ceci

Ce qui n'a pas été demandé est écrit sur la fiche d'installation comme restant à faire.
USAGE
}

# --- Les options ----------------------------------------------------------------------
# Une boucle « while … case » et non getopts : getopts ne connaît que les options d'UNE
# lettre, et les noms longs sont ce qui rend les deux installeurs relisibles l'un contre
# l'autre. Chaque nom est celui de son paramètre Windows, en minuscules-tirets :
# -StationNumber → --station-number, -AdminPassword → --admin-password, -Yes → --yes,
# -InstallDir → --install-dir. La correspondance est mécanique EXPRÈS : c'est ce qui permet
# de vérifier à l'œil qu'une modification validée d'un côté a bien été reportée de l'autre.
#
# Une seule ne l'est pas, et c'est délibéré : -DataRoot devient --data-dir. Sous Windows,
# DataRoot est la racine qui contient à la fois config.json et data\ ; ici il n'existe
# aucun répertoire qui contienne les deux (§11.1, /etc et /var/lib), donc « racine » ne
# nommerait rien.
ADMIN_PASSWORD=${OPENSCALE_ADMIN_PASSWORD:-}
ADMIN_PASSWORD_FROM_ARGV=0
# ★ RETIRÉE DE L'ENVIRONNEMENT TOUT DE SUITE. Sans cela, apt-get, useradd, systemctl et le
# binaire lui-même hériteraient tous du secret dans leur propre /proc/<pid>/environ. Une
# variable de shell, elle, ne quitte pas ce processus.
unset OPENSCALE_ADMIN_PASSWORD
STATION_NUMBER=''
STATION_NAME=''
ASSUME_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --admin-password)
      shift
      [ $# -gt 0 ] || fail "option --admin-password : le mot de passe manque"
      ADMIN_PASSWORD="$1"
      ADMIN_PASSWORD_FROM_ARGV=1
      ;;
    --admin-password=*)
      ADMIN_PASSWORD="${1#--admin-password=}"
      ADMIN_PASSWORD_FROM_ARGV=1
      ;;
    --station-number)
      shift
      [ $# -gt 0 ] || fail "option --station-number : le numéro manque"
      STATION_NUMBER="$1"
      ;;
    --station-number=*) STATION_NUMBER="${1#--station-number=}" ;;
    --station-name)
      shift
      [ $# -gt 0 ] || fail "option --station-name : le nom manque"
      STATION_NAME="$1"
      ;;
    --station-name=*) STATION_NAME="${1#--station-name=}" ;;
    --install-dir)
      shift
      [ $# -gt 0 ] || fail "option --install-dir : le répertoire manque"
      BINARY="$1/openscale"
      ;;
    --install-dir=*) BINARY="${1#--install-dir=}/openscale" ;;
    --data-dir)
      shift
      [ $# -gt 0 ] || fail "option --data-dir : le répertoire manque"
      DATA_DIR="$1"
      ;;
    --data-dir=*) DATA_DIR="${1#--data-dir=}" ;;
    --yes) ASSUME_YES=1 ;;
    -h|--help)
      usage
      exit 0
      ;;
    *) fail "option inconnue : $1 (« install.sh --help » les liste)" ;;
  esac
  shift
done

# QUI PEUT ENCORE RÉPONDRE, décidé ici parce que cela ne dépend que des options et du
# terminal. Les deux cas restent distincts parce qu'ils n'ont pas la même cause : --yes est
# un CHOIX, une entrée standard qui n'est pas un terminal est un FAIT — un tube, un fichier
# de réponses, ou le corps de ce script sous « curl … | sh ».
ASKABLE=0
if [ "$ASSUME_YES" -eq 0 ] && [ -t 0 ]; then ASKABLE=1; fi

# Les unités livrées sont-elles encore vraies telles quelles ? place_unit s'en sert.
MOVED=0
if [ "$BINARY" != "$STOCK_BINARY" ] || [ "$DATA_DIR" != "$STOCK_DATA_DIR" ]; then MOVED=1; fi

# --- Les fonctions que les questions et la fiche emploient ------------------------------

# count_code_points rend le nombre de POINTS DE CODE d'un texte, celui que le binaire
# compte.
#
# ★ « ${#chaine} » COMPTERAIT DES OCTETS. Mesuré sous dash : « éàçü » y vaut 8, et un mot de
# passe de quatre lettres accentuées passerait donc un plancher de quatre sans le valoir —
# alors que web.MinPasswordLength dit « counted in CODE POINTS and not in bytes », que
# l'écran d'administration et « openscale config password » comptent l'un et l'autre des
# points de code, et que common.ps1 a écrit Measure-CodePoint pour la même raison. Une
# cinquième porte qui compterait des octets accepterait à l'installation ce que le poste
# refuse ensuite, sans que rien nulle part ne dise pourquoi.
#
# En UTF-8, les octets de continuation sont 0x80–0xBF et JAMAIS le premier octet d'un
# caractère : les retirer laisse exactement un octet par point de code. LC_ALL=C est ce qui
# garantit à tr de raisonner en octets plutôt qu'en caractères de la locale du poste.
count_code_points() {
  printf '%s' "$1" | LC_ALL=C tr -d '\200-\277' | wc -c | tr -d ' '
}

# config_field rend une valeur d'un bloc de premier niveau de config.json, ou rien.
#
# Le fichier a DEUX formes et les deux doivent se lire : celle qui est LIVRÉE, écrite à la
# main, où le bloc « station » tient sur une seule ligne ; et celle que le binaire réécrit —
# json.MarshalIndent avec deux espaces —, où chaque clé a la sienne. Ce qu'elles ont en
# commun est l'indentation de DEUX espaces des clés de premier niveau, et c'est là-dessus
# que la sortie du bloc se décide : « name » existe aussi dans les catégories du catalogue,
# et un grep nu y répondrait « Fruits ».
#
# Les échappements rendus sont ceux que l'encodeur de Go produit : \" et \\, plus les trois
# \u qu'il pose systématiquement sur &, < et >. Tout autre \u devient « ? » — ce serait un
# fichier que personne d'ici n'a écrit, et un point d'interrogation sur la fiche se voit,
# alors qu'une séquence recopiée telle quelle passerait pour le nom du poste.
config_field() {
  if [ ! -f "$1" ]; then
    return 0
  fi
  awk -v block="\"$2\"" -v key="\"$3\"" '
    BEGIN { inside = 0 }
    {
      line = $0
      if (inside == 0) {
        at = index(line, block ":")
        if (at == 0) { next }
        inside = 1
        line = substr(line, at + length(block) + 1)
      } else if (substr(line, 1, 3) ~ /^  [^ ]/) {
        exit
      }
      at = index(line, key ":")
      if (at == 0) { next }
      rest = substr(line, at + length(key) + 1)
      sub(/^[ \t]*/, "", rest)
      if (substr(rest, 1, 1) != "\"") {
        if (match(rest, /-?[0-9]+/)) { print substr(rest, RSTART, RLENGTH) }
        exit
      }
      value = ""
      i = 2
      n = length(rest)
      while (i <= n) {
        c = substr(rest, i, 1)
        if (c == "\\") {
          e = substr(rest, i + 1, 1)
          if (e == "u") {
            hex = substr(rest, i + 2, 4)
            if (hex == "0026") { value = value "&" }
            else if (hex == "003c") { value = value "<" }
            else if (hex == "003e") { value = value ">" }
            else { value = value "?" }
            i = i + 6
            continue
          }
          value = value e
          i = i + 2
          continue
        }
        if (c == "\"") { break }
        value = value c
        i = i + 1
      }
      print value
      exit
    }
  ' "$1"
}

# ask_secret pose une question dont la réponse ne s'affiche pas, la fait retaper, et la rend
# sur la SORTIE STANDARD.
#
# Les invites partent sur la sortie d'ERREUR, et ce n'est pas un détail de présentation :
# l'appelant capture cette fonction par substitution de commande, et une invite mêlée à la
# réponse ferait partie du mot de passe haché.
#
# ★ « read -s » EST UN BASHISME. dash répond « Illegal option -s » et l'installation
# s'arrête sur sa propre question. Couper l'écho du terminal est la forme portable, et le
# trap est ce qui le REND : sans lui, un Ctrl-C au milieu de la saisie laisse un opérateur
# qui tape à l'aveugle jusqu'à ce que quelqu'un lui souffle « stty sane ».
#
# LA CONFIRMATION EST LA MOITIÉ QUI COMPTE, et c'est le raisonnement de Read-ConfirmedSecret
# mot pour mot : un mot de passe tapé de travers en saisie masquée est un mot de passe que
# personne ne pourra plus jamais deviner — ni celui qui l'a tapé, ni la fiche, qui ne le
# porte pas.
#
# Les variables portent un préfixe au lieu d'être déclarées « local », qui n'est pas du sh
# POSIX : dash l'accepte, un autre shell POSIX peut le refuser, et un installeur n'est pas
# l'endroit où découvrir lequel.
ask_secret() {
  ask_prompt="$1"
  ask_minimum="$2"
  ask_stty=$(stty -g)
  trap 'stty "$ask_stty" 2>/dev/null || true' EXIT INT TERM
  stty -echo
  while true; do
    printf '%s : ' "$ask_prompt" >&2
    if ! read -r ask_first; then
      printf '\n' >&2
      fail "l'entrée standard s'est fermée pendant la saisie : rien n'a été posé"
    fi
    printf '\n' >&2
    if [ -z "$ask_first" ]; then
      note "   il en faut un : cette question n'a pas de réponse par défaut."
      continue
    fi
    case "$ask_first" in
      [[:space:]]*|*[[:space:]])
        note "   il commence ou finit par une espace : personne ne le retapera juste."
        continue
        ;;
    esac
    if [ "$(count_code_points "$ask_first")" -lt "$ask_minimum" ]; then
      note "   trop court : $ask_minimum caractères au minimum."
      continue
    fi
    printf '   Confirmation : ' >&2
    if ! read -r ask_again; then
      printf '\n' >&2
      fail "l'entrée standard s'est fermée pendant la confirmation : rien n'a été posé"
    fi
    printf '\n' >&2
    if [ "$ask_first" != "$ask_again" ]; then
      note '   les deux saisies ne sont pas les mêmes.'
      continue
    fi
    break
  done
  # L'écho est rendu ICI et pas seulement par le trap : celui-ci n'existe que pour le chemin
  # interrompu, et cette fonction doit laisser le terminal comme elle l'a trouvé même si un
  # appelant la lance un jour ailleurs que dans une substitution de commande.
  stty "$ask_stty" 2>/dev/null || true
  trap - EXIT INT TERM
  printf '%s' "$ask_first"
}

# place_unit pose une unité systemd, en réécrivant les emplacements qu'un poste déplacé
# rendrait faux.
#
# Les deux unités livrées nomment /usr/local/bin/openscale et /var/lib/openscale EN CLAIR,
# et elles le doivent : c'est ce qui les rend lisibles telles quelles à 8 h du matin, et un
# test de deploy/ les compare aux constantes de internal/platform/paths.go. Sur un poste où
# --install-dir ou --data-dir a déplacé quelque chose, ces deux chaînes deviennent FAUSSES —
# le service lancerait un binaire absent, et ProtectSystem=strict lui refuserait d'écrire là
# où il écrit. C'est ce que install.ps1 fait du marqueur %OPENSCALE_BINARY% dans le XML de
# la tâche planifiée, pour la même raison exactement.
#
# ★ « --data » ARRIVE AVEC, et il le faut : les emplacements par défaut du binaire sont
# COMPILÉS. Une unité qui pointerait ailleurs sans le dire laisserait le service écrire dans
# /var/lib/openscale quand même, et le poste tournerait sur deux répertoires à la fois.
# L'unité du kiosque n'en a pas besoin : elle ne lit que la configuration, qui ne bouge pas.
place_unit() {
  if [ "$MOVED" -eq 0 ]; then
    install -m 0644 "$1" "$2"
    return 0
  fi
  sed -e "s#$STOCK_BINARY#$BINARY#g" \
    -e "s#$STOCK_DATA_DIR#$DATA_DIR#g" \
    -e "s#\\(ExecStart=.*openscale serve\\)#\\1 --data $DATA_DIR#" \
    "$1" > "$2.nouveau"
  chmod 0644 "$2.nouveau"
  mv "$2.nouveau" "$2"
}

# --- Les contrôles préalables -----------------------------------------------------------
# Tout ce qui peut refuser refuse ICI, avant la première écriture : une installation qui
# échoue doit échouer avant d'avoir commencé, et non dix étapes plus loin sur un poste dont
# le compte, les répertoires et le binaire sont déjà posés.
[ "$(id -u)" -eq 0 ] || fail "à lancer en root : sudo ./install.sh"
[ -f "$HERE/openscale" ] || fail "le binaire openscale est introuvable à côté de install.sh"

case "$BINARY" in
  /*) ;;
  *) fail "--install-dir demande un chemin absolu" ;;
esac
case "$DATA_DIR" in
  /*) ;;
  *) fail "--data-dir demande un chemin absolu" ;;
esac
# systemd coupe une ligne d'unité sur les espaces, et place_unit substitue avec sed : les
# deux comptent sur des chemins qui n'en portent pas. Le refuser ici vaut mieux qu'un
# ExecStart coupé en deux que personne ne relit.
case "$BINARY$DATA_DIR" in
  *[[:space:]]*|*'#'*) fail "les chemins d'installation ne doivent porter ni espace ni « # »" ;;
esac

if [ -n "$ADMIN_PASSWORD" ] &&
  [ "$(count_code_points "$ADMIN_PASSWORD")" -lt "$MIN_ADMIN_PASSWORD_LENGTH" ]; then
  fail "le mot de passe d'administration doit faire au moins $MIN_ADMIN_PASSWORD_LENGTH caractères : c'est le plancher qu'applique le poste lui-même, et rien n'a été installé"
fi
if [ "$ADMIN_PASSWORD_FROM_ARGV" -eq 1 ]; then
  # Dit UNE fois, à l'endroit où c'est encore réparable. La ligne de commande de ce
  # processus reste lisible dans /proc tant qu'il tourne, et dans l'historique du shell
  # après : changer le mot de passe est le seul remède, et le savoir maintenant coûte moins
  # cher que de l'apprendre.
  log "AVERTISSEMENT : --admin-password place le secret sur la ligne de commande, que tout utilisateur de la machine lit dans /proc et que l'historique du shell garde. OPENSCALE_ADMIN_PASSWORD évite le premier ; ne rien donner du tout évite les deux."
fi

log "installation d'$PRODUCT sur $(hostname)"
if [ "$MOVED" -eq 1 ]; then
  log "emplacements déplacés : binaire $BINARY, données $DATA_DIR — les unités systemd sont réécrites en conséquence, mais uninstall.sh ne connaît que les emplacements par défaut"
fi

# --- 1. Les paquets -------------------------------------------------------------------
# --no-install-recommends : un poste de pesée n'a pas besoin des 200 Mo de recommandations
# de chromium, et chaque paquet installé est un paquet à mettre à jour pendant dix ans.
if command -v apt-get >/dev/null 2>&1; then
  log 'installation de cage, chromium et seatd'
  apt-get install --no-install-recommends -y cage chromium seatd
  systemctl enable --now seatd
else
  log 'apt-get absent : installez cage, chromium et seatd avec le gestionnaire de paquets de cette distribution'
fi

# --- 2. Le compte -------------------------------------------------------------------
# dialout = le PORT SÉRIE, lp = l'IMPRIMANTE, video et input = le kiosque Wayland. Un
# compte sans dialout tombe en saisie manuelle avec « accès refusé » sur un port qui
# existe, et c'est une heure de recherche.
#
# Ni mot de passe ni interpréteur de commandes : c'est ce qui fait que -AccountPassword n'a
# rien à demander ici (en-tête de ce fichier).
if id "$ACCOUNT" >/dev/null 2>&1; then
  log "compte $ACCOUNT : déjà présent, groupes vérifiés"
  usermod -aG dialout,lp,video,input,render "$ACCOUNT" || true
else
  useradd --create-home --shell /usr/sbin/nologin \
    --groups dialout,lp,video,input,render "$ACCOUNT"
  log "compte $ACCOUNT créé"
fi

# --- 3. Binaire, répertoires, documentation -----------------------------------------
install -d -m 0755 "$(dirname "$BINARY")"
install -m 0755 "$HERE/openscale" "$BINARY"
install -d -o "$ACCOUNT" -g "$ACCOUNT" -m 0750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
install -d -o "$ACCOUNT" -g "$ACCOUNT" -m 0750 \
  "$DATA_DIR/images" "$DATA_DIR/labels" \
  "$DATA_DIR/catalog" "$DATA_DIR/catalog/incoming" \
  "$DATA_DIR/catalog/archives" "$DATA_DIR/catalog/rejected"
install -d -m 0755 "$DOC_DIR"
# `if` et non `[ … ] && …` : sous « set -eu », un ET dont le test échoue rend un code non
# nul, et le script SORT. Un fichier optionnel absent — flv_demo.csv, par exemple —
# interromprait l'installation en silence, à la moitié.
for doc in INSTALLATION.md TROUBLESHOOTING.md SHA256SUMS flv_demo.csv; do
  if [ -f "$HERE/$doc" ]; then install -m 0644 "$HERE/$doc" "$DOC_DIR/$doc"; fi
done
VERSION=$("$BINARY" --version)
log "binaire installé : $VERSION"

# La configuration livrée : les valeurs du site, SANS le bloc matériel (§11.5). Elle est
# donc incomplète exprès — le port série et la file d'impression se règlent sur l'écran —
# et le poste démarre en attendant sur le profil neutre en servant la liste de ses fautes
# (§11.3). Ce que l'étape 4 lui ajoute est ce que l'écran ne peut PAS deviner : qui est ce
# poste.
CONFIG_IS_NEW=0
if [ -f "$HERE/config-lacagette.json" ] && [ ! -f "$CONFIG" ]; then
  install -m 0640 -o "$ACCOUNT" -g "$ACCOUNT" "$HERE/config-lacagette.json" "$CONFIG"
  CONFIG_IS_NEW=1
  log "configuration livrée copiée dans $CONFIG"
elif [ -f "$CONFIG" ]; then
  log "configuration existante conservée : $CONFIG"
fi

# --- 4. Qui est ce poste, et sa balance (§11.2, §15.5) --------------------------------
# CE QUE SEULE L'INSTALLATION PEUT DEMANDER. La configuration livrée est l'export de §11.5 :
# numéro 0, aucun nom, et une balance qui nomme son protocole sans nommer de port.
#
# LA BALANCE N'EST DÉCLARÉE ABSENTE QUE SUR UN POSTE NEUF, et le critère est « ce fichier
# vient d'être copié », rien d'autre. « scale.present = false » est la déclaration explicite
# de §11.2 : c'est vrai d'un poste qui sort de l'archive, où rien n'a encore été branché ni
# détecté ; c'est faux d'un poste en service, dont relancer l'installeur ne doit surtout pas
# éteindre la balance.
#
# LES BORNES DU NUMÉRO NE SONT PAS ICI. C'est le contrôle 1 de §11.3, le binaire les
# applique, il refuse en français et il n'écrit rien : on redemande. Les réécrire dans ce
# script en ferait un second endroit à corriger, et le premier à mentir.
NUMBER_IS_MISSING=1
if [ -n "$(config_field "$CONFIG" station number)" ] &&
  [ "$(config_field "$CONFIG" station number)" != '0' ]; then
  NUMBER_IS_MISSING=0
fi
NAME_IS_MISSING=1
if [ -n "$(config_field "$CONFIG" station name)" ]; then NAME_IS_MISSING=0; fi

NUMBER_ANSWER="$STATION_NUMBER"
NAME_ANSWER="$STATION_NAME"
WILL_ASK_IDENTITY=0
if [ "$ASKABLE" -eq 1 ] && [ -f "$CONFIG" ]; then
  if [ "$NUMBER_IS_MISSING" -eq 1 ] && [ -z "$NUMBER_ANSWER" ]; then WILL_ASK_IDENTITY=1; fi
  if [ "$NAME_IS_MISSING" -eq 1 ] && [ -z "$NAME_ANSWER" ]; then WILL_ASK_IDENTITY=1; fi
fi
if [ "$WILL_ASK_IDENTITY" -eq 1 ]; then
  say ''
  say " CE POSTE — répondez, puis l'installation reprend seule."
fi

while [ -f "$CONFIG" ]; do
  if [ "$ASKABLE" -eq 1 ] && [ "$NUMBER_IS_MISSING" -eq 1 ] && [ -z "$NUMBER_ANSWER" ]; then
    say ''
    say ' Numéro de ce poste dans la coopérative'
    say "   C'est de lui que dérive le nom du fichier de catalogue surveillé, flv_<n>.csv."
    while true; do
      printf ' Numéro : '
      if ! read -r NUMBER_ANSWER; then fail "l'entrée standard s'est fermée : le numéro n'a pas été posé"; fi
      case "$NUMBER_ANSWER" in
        ''|*[!0-9]*) say "   un nombre, et rien d'autre." ;;
        *) break ;;
      esac
    done
  fi
  if [ "$ASKABLE" -eq 1 ] && [ "$NAME_IS_MISSING" -eq 1 ] && [ -z "$NAME_ANSWER" ]; then
    say ''
    say ' Nom de ce poste'
    say "   Ce que les bénévoles lisent sur l'écran d'administration : « Poste 2 - fruits »."
    while true; do
      printf ' Nom : '
      if ! read -r NAME_ANSWER; then fail "l'entrée standard s'est fermée : le nom n'a pas été posé"; fi
      if [ -n "$NAME_ANSWER" ]; then break; fi
      say "   il en faut un : c'est ce qui distingue ce poste de ses voisins."
    done
  fi

  # Les options se construisent dans les PARAMÈTRES POSITIONNELS, et pas dans une chaîne :
  # c'est la seule façon, en sh POSIX, de transporter « Poste 2 - fruits » sans que le
  # découpage en mots n'en fasse trois arguments. Ce « set -- » écrase les paramètres du
  # script, et il en a le droit : la boucle d'analyse des options les a tous consommés.
  set -- config station "$CONFIG"
  if [ -n "$NUMBER_ANSWER" ]; then set -- "$@" --number "$NUMBER_ANSWER"; fi
  if [ -n "$NAME_ANSWER" ]; then set -- "$@" --name "$NAME_ANSWER"; fi
  if [ "$CONFIG_IS_NEW" -eq 1 ]; then set -- "$@" --no-scale; fi

  # « config station » et le fichier font TROIS paramètres ; au-delà, il y a quelque chose à
  # poser. Sans aucune option la commande refuserait — « config station ne change rien sans
  # --number, --name ou --no-scale » —, et ce refus-là n'en est pas un : c'est le cas d'une
  # réinstallation dont tout est déjà posé.
  STATION_STATUS=0
  if [ $# -gt 3 ]; then
    "$BINARY" "$@" || STATION_STATUS=$?
  fi
  if [ "$STATION_STATUS" -ne 0 ]; then
    # Sans personne pour retaper, un refus est un échec d'installation comme un autre : le
    # poste ne doit pas s'annoncer installé avec un numéro que ses propres contrôles
    # rejettent.
    if [ "$ASKABLE" -eq 0 ]; then
      fail "« openscale config station » a refusé ce qui lui a été transmis (code $STATION_STATUS) — rien n'a été écrit"
    fi
    log "la réponse a été refusée — rien n'a été écrit, on recommence"
    NUMBER_ANSWER=''
    NAME_ANSWER=''
    continue
  fi

  # ★ CE QUE LE JOURNAL DIT SE COMPOSE DE CE QUI A ÉTÉ TRANSMIS, et jamais du nombre
  # d'arguments : sur un poste NEUF il porte toujours « --no-scale », si bien qu'une liste
  # vide n'existe pas et que l'avertissement écrit pour le seul cas qui en a besoin — une
  # installation scriptée où personne n'a répondu — ne s'afficherait jamais. Le journal
  # annoncerait alors une identité posée sur un poste qui n'a ni numéro ni nom.
  POSED=''
  UNANSWERED=''
  if [ -n "$NUMBER_ANSWER" ]; then
    POSED="numéro $NUMBER_ANSWER"
  elif [ "$NUMBER_IS_MISSING" -eq 1 ]; then
    UNANSWERED='le numéro'
  fi
  if [ -n "$NAME_ANSWER" ]; then
    if [ -n "$POSED" ]; then POSED="$POSED, "; fi
    POSED="${POSED}nom « $NAME_ANSWER »"
  elif [ "$NAME_IS_MISSING" -eq 1 ]; then
    if [ -n "$UNANSWERED" ]; then UNANSWERED="$UNANSWERED et "; fi
    UNANSWERED="${UNANSWERED}le nom"
  fi

  if [ -n "$POSED" ]; then
    SAID="identité du poste posée dans $CONFIG : $POSED"
  elif [ -n "$UNANSWERED" ]; then
    SAID="identité du poste NON posée : rien n'a été demandé ni transmis"
  else
    SAID="identité du poste inchangée : numéro et nom déjà posés dans $CONFIG"
  fi
  if [ -n "$UNANSWERED" ]; then
    SAID="$SAID — il reste à régler sur l'écran d'administration : $UNANSWERED"
  fi
  log "$SAID"
  break
done
SCALE_DISABLED="$CONFIG_IS_NEW"

# --- 5. Le code de secours d'administration (§14.4, important-10) ---------------------
# « Un code de secours de 8 caractères est généré à l'installation, imprimé sur la fiche
# d'installation et consigné dans le classeur du magasin. » C'est ici et pas sur l'écran :
# le code est ce qui rouvre un poste QUI N'A AUCUN MOT DE PASSE, et c'est l'état d'un poste
# dont l'étape suivante n'a pas pu poser le sien — une installation scriptée, une console
# sans clavier. Un script shell ne sait pas produire une empreinte argon2id ; le binaire le
# fait, et il est le seul à afficher le code en clair, une fois.
#
# Une réinstallation ne le fait PAS tourner : la fiche déjà rangée au classeur doit rester
# vraie, et personne ne peut relire un code qui n'existe plus qu'en empreinte.
#
# L'appel n'est pas toléré s'il échoue, et ce n'est pas un oubli : sous « set -e », une
# substitution de commande qui sort non nulle arrête le script. Un poste sans code de
# secours ET sans mot de passe n'a plus aucune porte.
RECOVERY_CODE=''
if [ -f "$CONFIG" ] && [ -z "$(config_field "$CONFIG" admin recovery_code_hash)" ]; then
  RECOVERY_PRINTED=$("$BINARY" config recovery-code "$CONFIG")
  RECOVERY_CODE=$(printf '%s\n' "$RECOVERY_PRINTED" |
    sed -n 's/^Code de secours de ce poste : \(.*\)$/\1/p' | head -n 1)
  if [ -n "$RECOVERY_CODE" ]; then
    log "code de secours d'administration tiré (il n'est écrit que sur la fiche)"
  else
    log "code de secours d'administration NON relu dans la sortie du binaire : la fiche portera une ligne à remplir à la main"
  fi
elif [ -f "$CONFIG" ]; then
  log "code de secours existant conservé : la fiche déjà classée reste valable"
fi

# --- 6. Le mot de passe d'administration (§14.4) --------------------------------------
# LE POSER ICI EST TOUT L'OBJET DE CETTE ÉTAPE. Jusqu'ici le poste sortait de l'installeur
# sans aucun mot de passe : le premier réglage ouvrait le panneau « Ce poste n'a pas encore
# de mot de passe », qui réclame le code de secours — donc la fiche, donc le classeur, donc
# quelqu'un qui sait où il est. Le code de secours ne disparaît pas pour autant : il reste
# le recours d'un poste dont le mot de passe est perdu, et la seule porte d'une installation
# scriptée où personne n'était là pour répondre.
#
# ★ SUR L'ENTRÉE STANDARD, JAMAIS SUR LA LIGNE DE COMMANDE. « openscale config » ne déclare
# aucune option qui prendrait un secret, et le dit dans son propre usage : un argument se
# lit dans /proc/<pid>/cmdline, que tout utilisateur de la machine peut ouvrir.
#
# Rien à régler sur l'encodage, contrairement à Set-NativeOutputEncoding sous Windows : un
# tube Unix transporte les octets tels quels, et ils sont déjà en UTF-8 des deux côtés.
ADMIN_POSED=0
if [ -n "$(config_field "$CONFIG" admin password_hash)" ]; then ADMIN_POSED=1; fi
if [ "$ASKABLE" -eq 1 ] && [ -z "$ADMIN_PASSWORD" ] && [ "$ADMIN_POSED" -eq 0 ] &&
  [ -f "$CONFIG" ]; then
  say ''
  say " Mot de passe d'administration de ce poste"
  say "   Il protège le droit de CHANGER le poste : les prix, l'étiquette, le catalogue."
  say "   $MIN_ADMIN_PASSWORD_LENGTH caractères au minimum, tapé deux fois, et il ne s'affiche pas."
  say "   Il n'est imprimé NULLE PART, pas même sur la fiche : prenez-en un que l'équipe"
  say "   connaît. Oublié, il se repose avec le code de secours de la fiche."
  ADMIN_PASSWORD=$(ask_secret ' Mot de passe' "$MIN_ADMIN_PASSWORD_LENGTH")
fi
if [ -n "$ADMIN_PASSWORD" ] && [ -f "$CONFIG" ]; then
  printf '%s\n' "$ADMIN_PASSWORD" | "$BINARY" config password "$CONFIG"
  ADMIN_PASSWORD=''
  ADMIN_POSED=1
  log "mot de passe d'administration posé (il n'est écrit ni dans ce journal ni sur la fiche)"
elif [ "$ADMIN_POSED" -eq 0 ]; then
  log "mot de passe d'administration NON posé : le premier réglage le demandera, avec le code de secours de la fiche"
fi

# --- 7. Les règles udev ---------------------------------------------------------------
if [ -f "$HERE/99-openscale.rules" ]; then
  install -m 0644 "$HERE/99-openscale.rules" /etc/udev/rules.d/99-openscale.rules
  udevadm control --reload
  udevadm trigger
  log 'règles udev posées : le port série a un nom stable'
  if [ -e /dev/openscale-serial ]; then
    log "port série reconnu : /dev/openscale-serial -> $(readlink -f /dev/openscale-serial)"
  else
    log 'aucun /dev/openscale-serial : la balance est débranchée, ou son adaptateur porte'
    log "d'autres identifiants USB. Relevez-les avec « lsusb » et corrigez la règle :"
    if command -v lsusb >/dev/null 2>&1; then lsusb; fi
  fi
fi

# --- 7 bis. Le droit de redémarrer l'ordinateur --------------------------------------
# Le service tourne en « openscale » et non en root : sans cette règle, le bouton
# « Redémarrer l'ordinateur » de l'écran d'administration est refusé par polkit. Le poste
# marche quand même, et c'est pourquoi son absence n'arrête pas l'installation — mais
# « openscale doctor » la signale, parce que le défaut ne se verrait sinon qu'au moment
# où quelqu'un en a besoin.
if [ -f "$HERE/49-openscale-reboot.rules" ]; then
  if [ -d /etc/polkit-1/rules.d ]; then
    install -m 0644 "$HERE/49-openscale-reboot.rules" \
      /etc/polkit-1/rules.d/49-openscale-reboot.rules
    log "règle polkit posée : le poste peut redémarrer l'ordinateur depuis l'écran"
  else
    log '/etc/polkit-1/rules.d absent : le bouton « Redémarrer l'"'"'ordinateur » répondra'
    log "que ce poste ne sait pas le faire. Tout le reste fonctionne."
  fi
fi

# --- 8. Les unités systemd ----------------------------------------------------------
place_unit "$HERE/openscale.service" "$UNIT_DIR/openscale.service"
place_unit "$HERE/openscale-kiosk.service" "$UNIT_DIR/openscale-kiosk.service"
systemctl daemon-reload
# systemd-analyze verify dit tout de suite ce qu'une unité fautive coûterait à trouver au
# prochain démarrage. Son échec n'arrête PAS l'installation : il peut refuser une
# directive parfaitement valide sur une version plus récente que celle du poste.
if command -v systemd-analyze >/dev/null 2>&1; then
  systemd-analyze verify "$UNIT_DIR/openscale.service" "$UNIT_DIR/openscale-kiosk.service" \
    || log 'systemd-analyze a des remarques sur les unités : lisez-les ci-dessus'
fi

systemctl enable openscale.service
systemctl enable openscale-kiosk.service
systemctl restart openscale.service
log 'service activé et démarré'

# --- 9. Vérification ----------------------------------------------------------------
# /healthz, et JAMAIS /readyz : une imprimante sans papier répond 503 sur /readyz, et une
# installation qui se croirait ratée pour un rouleau vide n'aurait rien compris à §15.3.
LISTEN=$(sed -n 's/.*"listen"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
  "$CONFIG" 2>/dev/null | head -n 1)
[ -n "$LISTEN" ] || LISTEN='127.0.0.1:8085'
case "$LISTEN" in
  0.0.0.0:*) LISTEN="127.0.0.1:${LISTEN##*:}" ;;
  :*)        LISTEN="127.0.0.1${LISTEN}" ;;
esac
ADDRESS="http://$LISTEN"

# DEUX adresses, et la seconde n'est pas une précaution vague : un poste dont la
# configuration est fautive démarre sur le PROFIL NEUTRE (§11.3) et sert donc sur
# l'adresse de ce profil — c'est exactement l'état d'un poste fraîchement installé, dont il
# reste à régler la balance et l'imprimante.
FACTORY_ADDRESS='http://127.0.0.1:8085'
healthy=0
attempt=0
while [ "$attempt" -lt 30 ]; do
  for candidate in "$ADDRESS" "$FACTORY_ADDRESS"; do
    if command -v curl >/dev/null 2>&1; then
      curl -fsS -m 2 "$candidate/healthz" >/dev/null 2>&1 && { healthy=1; break; }
    elif command -v wget >/dev/null 2>&1; then
      wget -q -T 2 -O /dev/null "$candidate/healthz" && { healthy=1; break; }
    else
      log 'ni curl ni wget : vérification de /healthz sautée'
      healthy=2
      break
    fi
  done
  if [ "$healthy" -ne 0 ]; then break; fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$healthy" -eq 2 ]; then healthy=0; fi
if [ "$healthy" -eq 1 ]; then
  log "le poste répond sur $ADDRESS/healthz"
else
  log "le poste ne répond pas sur $ADDRESS — diagnostic :"
  "$BINARY" doctor --config "$CONFIG" --data "$DATA_DIR" || true
fi

# --- 10. La fiche d'installation ------------------------------------------------------
# CE QUE LE FICHIER DIT, et non ce que ce script croit y avoir écrit. La fiche part au
# classeur et elle y reste des années : elle doit porter l'identité que le poste applique, y
# compris quand une réinstallation n'a rien redemandé parce que tout était déjà posé.
FINGERPRINT=$("$BINARY" config fingerprint "$CONFIG" 2>/dev/null ||
  printf '%s' "(à relever sur l'écran d'administration)")
SHEET_NUMBER=$(config_field "$CONFIG" station number)
SHEET_NAME=$(config_field "$CONFIG" station name)
if [ -z "$SHEET_NUMBER" ] || [ "$SHEET_NUMBER" = '0' ]; then SHEET_NUMBER='(pas encore posé)'; fi
if [ -z "$SHEET_NAME" ]; then SHEET_NAME='(pas encore posé)'; fi

# La fiche N'IMPRIME PAS le mot de passe d'administration, qui est pourtant posé ici. Ce
# n'est pas un oubli : cette feuille part au classeur du magasin, et ce mot de passe donne
# le droit de CHANGER le poste. Ce qu'elle porte à sa place est ce dont on a besoin quand il
# est perdu : qui l'a posé, et le code de secours.
if [ "$ADMIN_POSED" -eq 1 ]; then
  SHEET_ADMIN="  POSÉ À L'INSTALLATION, par la personne qui a lancé l'installeur.
  Il n'est écrit NULLE PART : ni sur cette fiche, ni dans la configuration, qui
  n'en garde qu'une empreinte argon2id. Redemandez-le à cette personne. Perdu
  pour tout le monde, il se repose en ligne de commande (plus bas)."
else
  SHEET_ADMIN="  PAS ENCORE POSÉ : l'installation n'a pas eu de quoi poser la question
  (installation scriptée, ou entrée standard qui n'est pas un terminal). Le
  premier geste qui change le poste le demandera, et c'est le CODE DE SECOURS
  ci-dessous qui ouvre cette porte-là."
fi

# Le code n'existe en clair QU'ICI : le poste n'en garde que l'empreinte argon2id.
if [ -n "$RECOVERY_CODE" ]; then
  SHEET_RECOVERY="  $RECOVERY_CODE
  Tiré à l'installation. Il n'est affiché nulle part ailleurs et le poste ne
  sait pas le relire : cette feuille est la seule copie."
else
  SHEET_RECOVERY="  ........................................................
  À RECOPIER ICI À LA MAIN : ce poste portait déjà un code, et seule son
  empreinte est conservée. Reprenez-le sur la fiche précédente, ou tirez-en un
  nouveau avec « openscale config recovery-code »."
fi

# L'écart d'empreinte d'un poste dont la balance n'est pas encore réglée. §15.5 fait
# comparer les quatre postes À L'ŒIL : un écart que la fiche n'annonce pas est un écart
# qu'on prend pour une panne, et qu'on « répare » en touchant à la configuration.
SHEET_SCALE=''
if [ "$SCALE_DISABLED" -eq 1 ]; then
  SHEET_SCALE="
  ATTENTION, LA BALANCE DE CE POSTE EST DÉCLARÉE ABSENTE. L'installation la
  désactive sur un poste neuf, où elle n'est encore ni branchée ni détectée.
  Tant qu'elle n'est pas remise en service — page « Matériel », « Détecter
  automatiquement », puis « Utiliser cette balance » sur le port qui répond —,
  CE POSTE N'AFFICHE PAS LA MÊME EMPREINTE QUE SES VOISINS, et ce n'est pas
  une panne. Elle rejoint celle du parc dès que la balance est déclarée."
fi

SHEET="$DATA_DIR/install-sheet.txt"
cat > "$SHEET" <<SHEET_END
FICHE D'INSTALLATION — POSTE DE PESÉE OPENSCALE
===============================================
À IMPRIMER et à ranger dans le classeur du magasin.
Elle porte le code de secours de ce poste : ne la laissez pas sur la machine.

Date d'installation ........ $(date '+%d/%m/%Y %H:%M')
Machine .................... $(hostname)
Version installée .......... $VERSION
Adresse de l'écran ......... $ADDRESS
Compte système ............. $ACCOUNT (sans mot de passe, sans shell)

CONFIGURATION
  Numéro de poste .......... $SHEET_NUMBER
  Nom du poste ............. $SHEET_NAME
  Empreinte du fichier ..... $FINGERPRINT
  Les quatre postes doivent afficher la MÊME empreinte de 8 caractères :
      $BINARY config fingerprint

  ATTENTION, c'est normal : tant que la balance, l'imprimante et le catalogue
  ne sont pas réglés, la configuration est incomplète, le poste tourne en
  CONFIGURATION D'USINE et l'écran affiche une AUTRE empreinte que celle
  ci-dessus. Les deux se rejoignent dès que les réglages sont terminés.
  C'est à ce moment-là qu'on compare les quatre postes.
$SHEET_SCALE

MOT DE PASSE D'ADMINISTRATION
$SHEET_ADMIN
  Il protège le droit de CHANGER le poste — les prix, le gabarit d'étiquette,
  le dépôt suivi. Le poste ne demande rien pour être REGARDÉ : toutes les
  pages se lisent, et la question arrive au moment où l'on change quelque
  chose. $MIN_ADMIN_PASSWORD_LENGTH caractères au minimum.

CODE DE SECOURS D'ADMINISTRATION
$SHEET_RECOVERY
  IL SERT À UN POSTE QUI N'A AUCUN MOT DE PASSE, et à lui seul : c'est là que
  l'écran le demande, et nulle part ailleurs.
  1. Bouton « Réglages » sur l'écran client : l'engrenage, tout à droite de la
     barre du bas. L'administration s'ouvre sur le Tableau de bord.
  2. Colonne de gauche, page « Matériel », puis « Détecter automatiquement ».
     Ce premier geste qui change le poste est celui qui pose la question.
  3. Le panneau « Ce poste n'a pas encore de mot de passe » demande ce code,
     puis le mot de passe à poser. $MIN_ADMIN_PASSWORD_LENGTH caractères au minimum.
  Si le poste a DÉJÀ un mot de passe — c'est le cas dès que l'installation a
  posé le sien — ce code ne se saisit plus à l'écran. La reprise en main passe
  alors par la ligne de commande, sur le poste, en root :
      systemctl stop $SERVICE
      $BINARY config password $CONFIG
      systemctl start $SERVICE

EN CAS DE PROBLÈME
  systemctl status $SERVICE
  journalctl -u $SERVICE -n 50
  $BINARY doctor
SHEET_END
chown "$ACCOUNT:$ACCOUNT" "$SHEET"
log "fiche d'installation écrite dans $SHEET"

cat <<FINAL

=========================================================================
 $PRODUCT est installé. IL RESTE TROIS CHOSES À FAIRE, dans cet ordre :
=========================================================================
 1. IMPRIMEZ la fiche d'installation et rangez-la dans le classeur :
      $SHEET
    Elle contient le code de secours de ce poste. Supprimez-la de la machine ensuite.
 2. REDÉMARREZ LA MACHINE et vérifiez que le poste revient SEUL sur
    l'écran client. Cette recette est OBLIGATOIRE : c'est la seule preuve
    que le poste se relèvera d'une coupure de courant.
 3. Bouton « Réglages » sur l'écran client — l'engrenage, tout à droite de la
    barre du bas —, page « Matériel », « Détecter automatiquement ». Le port où
    la balance répond porte alors un bouton « Utiliser cette balance » : c'est
    LUI qui la remet en service, avec son protocole et son port. Réglez ensuite
    l'imprimante et le catalogue. Voir INSTALLATION.md.
FINAL

if [ "$ADMIN_POSED" -eq 1 ]; then
  cat <<FINAL
    Le mot de passe d'administration est POSÉ : le poste le demandera au premier
    réglage. Il n'est écrit nulle part, pas même sur la fiche.
FINAL
else
  cat <<FINAL
    Ce poste n'a AUCUN mot de passe d'administration : le premier réglage ouvrira
    le panneau qui réclame le CODE DE SECOURS de la fiche, puis le mot de passe.
FINAL
fi

if [ "$SCALE_DISABLED" -eq 1 ]; then
  # §15.5 fait comparer les empreintes des quatre postes À L'ŒIL. Un écart annoncé est une
  # étape restante ; un écart qu'on découvre est une panne qu'on croit réparer en touchant
  # à la configuration.
  cat <<FINAL
    La balance de ce poste NEUF est déclarée absente tant qu'elle n'est pas
    détectée : son empreinte de configuration diffère donc de celle de ses
    voisins, et les rejoint dès que la balance est remise en service.
FINAL
fi

printf '\n Journal du service :  journalctl -u %s -f\n' "$SERVICE"
