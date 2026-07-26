#!/bin/sh
# Installe un poste de pesée OpenScale sur une Debian 12 minimale — §15.3.
#
#     sudo ./install.sh
#
# Ce que le script fait :
#   1. installe cage, chromium et seatd — le kiosque Wayland mono-application ;
#   2. crée le compte openscale, dans les groupes dialout (SÉRIE) et lp (IMPRIMANTE) ;
#   3. pose le binaire, les répertoires et les deux unités systemd ;
#   4. pose les règles udev qui donnent au port série un nom STABLE ;
#   5. active et démarre le service, puis vérifie /healthz ;
#   6. écrit la fiche d'installation.
#
# Il est IDEMPOTENT : le relancer sur un poste déjà installé remet tout en place sans rien
# casser.
#
# /bin/sh et non bash : Debian minimale a dash, et un script d'installation n'est pas
# l'endroit où découvrir qu'un shell manque.

set -eu

PRODUCT='OpenScale'
SERVICE='openscale'
ACCOUNT='openscale'
BINARY='/usr/local/bin/openscale'
CONFIG_DIR='/etc/openscale'
DATA_DIR='/var/lib/openscale'
LOG_DIR='/var/log/openscale'
DOC_DIR='/usr/share/doc/openscale'
UNIT_DIR='/etc/systemd/system'
HERE=$(cd "$(dirname "$0")" && pwd)

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1"; }
fail() { printf 'install.sh : %s\n' "$1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "à lancer en root : sudo ./install.sh"
[ -f "$HERE/openscale" ] || fail "le binaire openscale est introuvable à côté de install.sh"

log "installation d'$PRODUCT sur $(hostname)"

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
if id "$ACCOUNT" >/dev/null 2>&1; then
  log "compte $ACCOUNT : déjà présent, groupes vérifiés"
  usermod -aG dialout,lp,video,input,render "$ACCOUNT" || true
else
  useradd --create-home --shell /usr/sbin/nologin \
    --groups dialout,lp,video,input,render "$ACCOUNT"
  log "compte $ACCOUNT créé"
fi

# --- 3. Binaire, répertoires, documentation -----------------------------------------
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
log "binaire installé : $("$BINARY" --version)"

# La configuration livrée : les valeurs du site, SANS le bloc matériel (§11.5). Elle est
# donc incomplète exprès — le numéro de poste, le port série et la file d'impression se
# règlent sur l'écran — et le poste démarre en attendant sur le profil neutre en servant
# la liste de ses fautes (§11.3).
if [ -f "$HERE/config-lacagette.json" ] && [ ! -f "$CONFIG_DIR/config.json" ]; then
  install -m 0640 -o "$ACCOUNT" -g "$ACCOUNT" "$HERE/config-lacagette.json" "$CONFIG_DIR/config.json"
  log "configuration livrée copiée dans $CONFIG_DIR/config.json"
elif [ -f "$CONFIG_DIR/config.json" ]; then
  log "configuration existante conservée : $CONFIG_DIR/config.json"
fi

# --- 4. Les règles udev -------------------------------------------------------------
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

# --- 5. Les unités systemd ----------------------------------------------------------
install -m 0644 "$HERE/openscale.service" "$UNIT_DIR/openscale.service"
install -m 0644 "$HERE/openscale-kiosk.service" "$UNIT_DIR/openscale-kiosk.service"
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

# --- 6. Vérification ----------------------------------------------------------------
# /healthz, et JAMAIS /readyz : une imprimante sans papier répond 503 sur /readyz, et une
# installation qui se croirait ratée pour un rouleau vide n'aurait rien compris à §15.3.
LISTEN=$(sed -n 's/.*"listen"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
  "$CONFIG_DIR/config.json" 2>/dev/null | head -n 1)
[ -n "$LISTEN" ] || LISTEN='127.0.0.1:8085'
case "$LISTEN" in
  0.0.0.0:*) LISTEN="127.0.0.1:${LISTEN##*:}" ;;
  :*)        LISTEN="127.0.0.1${LISTEN}" ;;
esac
ADDRESS="http://$LISTEN"

# DEUX adresses, et la seconde n'est pas une précaution vague : un poste dont la
# configuration est fautive démarre sur le PROFIL NEUTRE (§11.3) et sert donc sur
# l'adresse de ce profil — c'est exactement l'état d'un poste fraîchement installé, dont il
# reste à régler le numéro, la balance et l'imprimante.
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
  "$BINARY" doctor --config "$CONFIG_DIR/config.json" --data "$DATA_DIR" || true
fi

# --- 7. La fiche d'installation ------------------------------------------------------
FINGERPRINT=$("$BINARY" config fingerprint "$CONFIG_DIR/config.json" 2>/dev/null || echo "(à relever sur l'écran d'administration)")
SHEET="$DATA_DIR/install-sheet.txt"
cat > "$SHEET" <<SHEET_END
FICHE D'INSTALLATION — POSTE DE PESÉE OPENSCALE
===============================================
À IMPRIMER et à ranger dans le classeur du magasin.

Date d'installation ........ $(date '+%d/%m/%Y %H:%M')
Machine .................... $(hostname)
Version installée .......... $("$BINARY" --version)
Adresse de l'écran ......... $ADDRESS
Compte système ............. $ACCOUNT (sans mot de passe, sans shell)

CONFIGURATION
  Numéro de poste .......... (à choisir dans l'assistant de premier démarrage)
  Empreinte du fichier ..... $FINGERPRINT
  Les quatre postes doivent afficher la MÊME empreinte de 8 caractères :
      $BINARY config fingerprint

  ATTENTION, c'est normal : tant que le numéro de poste, la balance et
  l'imprimante ne sont pas réglés, la configuration est incomplète, le poste
  tourne en CONFIGURATION D'USINE et l'écran affiche une AUTRE empreinte que
  celle ci-dessus. Les deux se rejoignent dès que l'assistant est terminé.

CODE DE SECOURS D'ADMINISTRATION
  ........................................................
  À RECOPIER ICI À LA MAIN depuis l'assistant de premier démarrage.

EN CAS DE PROBLÈME
  systemctl status openscale
  journalctl -u openscale -n 50
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
 2. REDÉMARREZ LA MACHINE et vérifiez que le poste revient SEUL sur
    l'écran client. Cette recette est OBLIGATOIRE : c'est la seule preuve
    que le poste se relèvera d'une coupure de courant.
 3. Appui long de 3 secondes dans le coin bas-droit de l'écran client :
    l'assistant impose un mot de passe d'administration, puis réglez la
    balance, l'imprimante et le catalogue. Voir INSTALLATION.md.

 Journal du service :  journalctl -u openscale -f
FINAL
