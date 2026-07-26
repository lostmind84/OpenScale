#!/bin/sh
# Désinstalle OpenScale d'un poste Debian — §15.5, important-15.
#
#     sudo ./uninstall.sh            garde les données
#     sudo ./uninstall.sh --purge    supprime aussi la configuration et la base
#
# Il LAISSE /var/lib/openscale et /etc/openscale INTACTS sauf --purge explicite : sans
# cela, la bascule est irréversible et le retour à l'application précédente impossible.
#
# Il n'y a rien à « restaurer » ici, contrairement à Windows : l'installation Linux
# n'écrase aucun réglage du système. Elle ajoute des fichiers — deux unités, une règle
# udev, un compte — et les retirer suffit. C'est une différence réelle entre les deux
# plateformes, pas un oubli : sous Windows, l'ouverture de session automatique, le plan
# d'alimentation et les stratégies Windows Update sont des réglages PARTAGÉS que
# l'installeur modifie, donc qu'il doit savoir remettre.

set -eu

ACCOUNT='openscale'
BINARY='/usr/local/bin/openscale'
CONFIG_DIR='/etc/openscale'
DATA_DIR='/var/lib/openscale'
LOG_DIR='/var/log/openscale'
DOC_DIR='/usr/share/doc/openscale'
UNIT_DIR='/etc/systemd/system'
PURGE=0
REMOVE_ACCOUNT=0

for argument in "$@"; do
  case "$argument" in
    --purge) PURGE=1 ;;
    --remove-account) REMOVE_ACCOUNT=1 ;;
    *) printf 'uninstall.sh : option inconnue %s (--purge, --remove-account)\n' "$argument" >&2; exit 2 ;;
  esac
done

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1"; }
[ "$(id -u)" -eq 0 ] || { printf 'uninstall.sh : à lancer en root\n' >&2; exit 1; }

for unit in openscale-kiosk.service openscale.service; do
  systemctl disable --now "$unit" 2>/dev/null || true
  rm -f "$UNIT_DIR/$unit"
  log "$unit arrêtée, désactivée et supprimée"
done
systemctl daemon-reload
systemctl reset-failed openscale.service openscale-kiosk.service 2>/dev/null || true

rm -f /etc/udev/rules.d/99-openscale.rules
udevadm control --reload 2>/dev/null || true
log 'règle udev supprimée'

rm -f "$BINARY"
rm -rf "$DOC_DIR"
log "binaire et documentation supprimés"

if [ "$REMOVE_ACCOUNT" -eq 1 ]; then
  userdel --remove "$ACCOUNT" 2>/dev/null || userdel "$ACCOUNT" 2>/dev/null || true
  log "compte $ACCOUNT supprimé"
else
  log "compte $ACCOUNT conservé (--remove-account pour le supprimer)"
fi

if [ "$PURGE" -eq 1 ]; then
  rm -rf "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
  cat <<END

Données supprimées : $CONFIG_DIR, $DATA_DIR, $LOG_DIR
Le journal des pesées n'existe plus. Le rapprochement de caisse des jours
précédents n'est plus possible depuis ce poste.
END
else
  cat <<END

Données CONSERVÉES :
  $CONFIG_DIR/config.json et ses cinq versions de secours
  $DATA_DIR/openscale.db : le journal des pesées, le catalogue, les compteurs
  $DATA_DIR/install-sheet.txt
Réinstaller par-dessus retrouvera tout. Pour tout supprimer : --purge.
END
fi
printf '\nDésinstallation terminée.\n'
