#!/bin/sh
# Met à jour le binaire d'un poste OpenScale, et revient en arrière si ça casse — §15.5.
#
#     sudo ./update.sh              depuis le répertoire de la nouvelle version
#     sudo ./update.sh --latest     va chercher la dernière release sur Internet
#
# Même procédure que update.ps1 sous Windows, et pour les mêmes raisons :
#   1. arrêt du service AVEC contrôle d'erreur, sur le budget de §13.4 ;
#   2. sauvegarde du binaire sous un nom HORODATÉ ;
#   3. copie, redémarrage, vérification de /healthz — JAMAIS /readyz ;
#   4. restauration automatique de la version précédente en cas d'échec.
#
# La configuration et la base ne sont pas touchées : elles vivent dans /etc/openscale et
# /var/lib/openscale, pas à côté du binaire.

set -eu

SERVICE='openscale'
BINARY='/usr/local/bin/openscale'
CONFIG='/etc/openscale/config.json'
DATA_DIR='/var/lib/openscale'
BACKUP_DIR="$DATA_DIR/backups"
BOOTSTRAP_URL='https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh'
HERE=$(cd "$(dirname "$0")" && pwd)

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1"; }
fail() { printf 'update.sh : %s\n' "$1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "à lancer en root : sudo ./update.sh"

# --- 0. --latest : le téléchargement n'est pas réécrit ici -----------------------------
# Résoudre la release, comparer l'empreinte et décompresser existe UNE FOIS dans ce dépôt,
# dans bootstrap.sh — le seul fichier qui doit savoir le faire sans rien pouvoir sourcer,
# puisqu'il vit HORS de l'archive. Ce mode le rappelle donc au lieu d'en écrire une seconde
# version, qui finirait par diverger de la première.
#
# IL N'Y A PAS DE BOUCLE : bootstrap.sh relance ce script SANS --latest, sur le binaire
# qu'il vient d'extraire à côté de lui. Un test le vérifie sur le texte des deux fichiers.
if [ "${1:-}" = '--latest' ]; then
  shift
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    fail "ni curl ni wget : --latest ne peut rien télécharger. Copiez l'archive sur une clé USB et lancez « sudo ./update.sh »."
  fi
  ENTRY=$(mktemp)
  trap 'rm -f "$ENTRY"' EXIT INT TERM
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$BOOTSTRAP_URL" -o "$ENTRY"
  else
    wget -qO "$ENTRY" "$BOOTSTRAP_URL"
  fi
  sh "$ENTRY" "$@"
  exit $?
fi

SOURCE="${1:-$HERE/openscale}"
[ -f "$SOURCE" ] || fail "le nouveau binaire est introuvable ($SOURCE)"
[ -f "$BINARY" ] || fail "aucun poste installé : c'est install.sh qu'il faut lancer"

log "mise à jour : $("$BINARY" --version)  ->  $("$SOURCE" --version)"

listen() {
  value=$(sed -n 's/.*"listen"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG" 2>/dev/null | head -n 1)
  [ -n "$value" ] || value='127.0.0.1:8085'
  case "$value" in
    0.0.0.0:*) value="127.0.0.1:${value##*:}" ;;
    :*)        value="127.0.0.1${value}" ;;
  esac
  printf 'http://%s' "$value"
}
ADDRESS=$(listen)

healthy() {
  attempt=0
  while [ "$attempt" -lt 60 ]; do
    # DEUX adresses, et la seconde n'est pas une précaution vague. Un poste sert sur
    # l'adresse que son fichier déclare même quand cette configuration est fautive par
    # ailleurs : c'est celle qu'il faut interroger d'abord. Il ne retombe sur l'adresse du
    # PROFIL NEUTRE (§11.3) que dans un seul cas, celui où network.listen est lui-même la
    # faute — champ vide, ou adresse que le poste ne peut pas lier. N'interroger alors que
    # l'adresse du fichier ferait conclure « le poste ne répond pas », et le retour arrière
    # remettrait la version précédente d'un poste que la mise à jour n'a pas cassé, sans
    # rien réparer de ce qui est en cause : le fichier.
    for candidate in "$ADDRESS" 'http://127.0.0.1:8085'; do
      if command -v curl >/dev/null 2>&1; then
        curl -fsS -m 2 "$candidate/healthz" >/dev/null 2>&1 && return 0
      elif command -v wget >/dev/null 2>&1; then
        wget -q -T 2 -O /dev/null "$candidate/healthz" && return 0
      fi
    done
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
      # Sans client HTTP, on se rabat sur systemd : Type=notify signifie que « active »
      # veut déjà dire « le poste a dit READY=1 », donc qu'il sert.
      systemctl is-active --quiet "$SERVICE" && return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  return 1
}

# --- 1. Arrêt -------------------------------------------------------------------------
# systemctl stop attend l'arrêt effectif, borné par TimeoutStopSec (45 s), qui suit la
# somme des budgets internes de §13.4. Un arrêt qu'on n'attend pas, c'est un binaire
# remplacé sous un processus qui le tient encore.
systemctl stop "$SERVICE" || fail "le service ne s'est pas arrêté : systemctl status $SERVICE"
log 'service arrêté'

# --- 2. Sauvegarde horodatée ---------------------------------------------------------
mkdir -p "$BACKUP_DIR"
STAMP=$(date '+%Y-%m-%dT%H-%M-%S')
BACKUP="$BACKUP_DIR/openscale-$STAMP"
cp -p "$BINARY" "$BACKUP"
log "version précédente sauvegardée dans $BACKUP"

DB_BEFORE=$(ls "$DATA_DIR" 2>/dev/null | grep '^openscale.db.before-' || true)

# --- 3. Copie, redémarrage, vérification ---------------------------------------------
install -m 0755 "$SOURCE" "$BINARY"
log 'nouveau binaire installé'

failure=''
systemctl start "$SERVICE" || failure="le service n'a pas démarré"
if [ -z "$failure" ] && ! healthy; then
  failure="le poste ne répond pas sur $ADDRESS/healthz"
fi

# --- 4. Retour arrière automatique ---------------------------------------------------
if [ -n "$failure" ]; then
  log "ÉCHEC : $failure"
  log 'restauration de la version précédente'
  systemctl stop "$SERVICE" || true
  install -m 0755 "$BACKUP" "$BINARY"
  systemctl start "$SERVICE" || true

  DB_AFTER=$(ls "$DATA_DIR" 2>/dev/null | grep '^openscale.db.before-' || true)
  NEW_DB=$(printf '%s\n' "$DB_AFTER" | grep -vxF "$(printf '%s\n' "$DB_BEFORE")" || true)

  cat >&2 <<END

=========================================================================
 LA MISE À JOUR A ÉCHOUÉ. LA VERSION PRÉCÉDENTE A ÉTÉ RESTAURÉE.
=========================================================================
 Raison : $failure
 Journal : journalctl -u $SERVICE -n 50
 Diagnostic : $BINARY doctor
END
  if [ -n "$NEW_DB" ]; then
    cat >&2 <<END
 LE SCHÉMA DE LA BASE A BOUGÉ pendant cette mise à jour. Le retour arrière
 est en TROIS gestes, et le troisième vous appartient :
   1. binaire restauré (fait)
   2. service redémarré (fait)
   3. si le poste refuse la base (ERR-DB-02), arrêtez le service et remettez
      une de ces copies à la place de $DATA_DIR/openscale.db :
$(printf '        %s/%s\n' "$DATA_DIR" $NEW_DB)
      Les pesées enregistrées depuis la mise à jour seront perdues :
      exportez le journal avant, depuis l'écran d'administration.
END
  fi
  exit 1
fi

# --- 5. Migration de la configuration --------------------------------------------------
# ICI et pas avant : la bascule vient d'être jugée réussie, et le retour arrière automatique
# de l'étape 4 est déjà écarté. Migrer plus tôt exposerait un retour arrière à restaurer un
# binaire PRÉCÉDENT qui relirait un config.json déjà migré, et perdrait ce que la migration a
# porté. Migrer ici n'ôte rien : le poste a démarré sans dépendre du fichier, la migration en
# mémoire suffit à le faire tourner.
#
# Un code de retour non nul n'est PAS un échec de la mise à jour, qui a déjà réussi : il
# signale qu'une clé reste à trancher à la main, sur un poste qui tourne.
"$BINARY" config migrate "$CONFIG" || \
  log 'configuration : une décision reste à prendre à la main (voir ci-dessus)'

log "mise à jour réussie : le poste répond sur $ADDRESS"
printf 'Version précédente conservée dans %s\n' "$BACKUP"
printf "Vérifiez l'empreinte de configuration : %s config fingerprint\n" "$BINARY"
