import type { PrinterDeviceDTO, TransportDTO } from './dto'
import { frenchInteger } from './format'

/**
 * Le transport d'octets choisi, et la clé de `printer.options` dans laquelle il fait
 * écrire (§8.4).
 *
 * **Laquelle est la bonne n'est jamais décidé ici** : c'est le poste qui le dit, transport
 * par transport, dans `health.printer_transports`. Ce module ne fait que lire ce registre
 * — mais il le lit à trois endroits qui doivent s'accorder : la liste déroulante, le champ
 * d'appareil en dessous, et la liste des destinations qu'un clic écrirait dedans.
 *
 * Le défaut est ce qui arrive quand on ne les accorde pas : le champ d'appareil était
 * câblé sur `queue` quoi qu'on choisisse au-dessus, et un poste réglé sur `tcp`
 * enregistrait l'adresse de son imprimante dans la clé de la file Windows. Rien ne le
 * refusait — aucun contrôle ne lie une clé à un transport — et le poste n'imprimait pas.
 */

/**
 * Les trois clés de `printer.options` qui DÉSIGNENT UN APPAREIL (§8.4).
 *
 * Elles sont énumérées pour deux choses seulement : ouvrir le volet sur celle qu'un
 * contrôle a refusée, et savoir laquelle le champ d'appareil doit lâcher quand le
 * transport change.
 */
export const DEVICE_KEYS = ['queue', 'path', 'address']

/**
 * La clé sur laquelle l'écran se rabat quand il ne peut pas savoir.
 *
 * Deux cas, tous deux rares et tous deux honnêtes : un binaire sans registre de
 * transports, et un fichier nommant un transport que ce poste ne connaît pas. La liste
 * déroulante dit déjà le second en toutes lettres ; ce que ce repli achète, c'est qu'il
 * reste un champ à corriger au lieu d'un volet vide.
 */
export const DEFAULT_DEVICE_KEY = 'queue'

/** Ce que chaque clé d'appareil décide, en une phrase de bénévole. */
export const DEVICE_HINTS: Record<string, string> = {
  queue: 'Choisissez-la dans la liste ci-dessus : une file mal orthographiée ne s’imprime pas.',
  path: 'Le nœud d’impression de ce poste, /dev/usb/lp0 ou le lien que la règle udev lui donne.',
  address:
    'L’adresse de l’imprimante sur le réseau, 192.168.0.43 — le port 9100 est ajouté s’il manque.',
}

/**
 * Vrai quand la configuration nomme un transport que ce poste ne déclare pas.
 *
 * @param transports - les transports que CE POSTE porte.
 * @param chosen - celui que le fichier nomme.
 */
export function transportUnknown(transports: TransportDTO[], chosen: string): boolean {
  return chosen !== '' && !transports.some((candidate) => candidate.id === chosen)
}

/**
 * Ce que la liste « Transport » propose, la valeur en cours COMPRISE.
 *
 * Un `<select>` dont la valeur ne figure dans aucune option se rabat en silence sur la
 * première : l'écran afficherait « File d'impression Windows » sur un poste réglé sur
 * autre chose, et le premier geste de qui vient corriger ce réglage serait de le
 * réenregistrer tel qu'il croit le lire. La valeur inconnue est donc gardée, et nommée.
 *
 * @param transports - les transports que CE POSTE porte.
 * @param chosen - celui que le fichier nomme.
 */
export function transportChoices(
  transports: TransportDTO[],
  chosen: string,
): { value: string; label: string }[] {
  if (transports.length === 0) return []
  return [
    ...(transportUnknown(transports, chosen)
      ? [{ value: chosen, label: `${chosen} — inconnu de ce poste` }]
      : []),
    ...transports.map((candidate) => ({ value: candidate.id, label: candidate.label })),
  ]
}

/**
 * La clé de `printer.options` que le transport CHOISI lit, et elle seule.
 *
 * @param transports - les transports que CE POSTE porte.
 * @param chosen - celui que le fichier nomme.
 */
export function deviceKeyOf(transports: TransportDTO[], chosen: string): string {
  return transports.find((candidate) => candidate.id === chosen)?.key ?? DEFAULT_DEVICE_KEY
}

/**
 * Ce que dit la ligne des destinations écartées.
 *
 * Un compte tout seul — « 4 destinations ne sont pas proposées » — laisse chercher ; ce
 * qui fait gagner du temps est le nom du réglage à changer, dans les mots mêmes de la
 * liste déroulante qui est juste en dessous.
 *
 * @param printers - toutes les destinations que la plateforme connaît.
 * @param transports - les transports que CE POSTE porte.
 * @param key - la clé que le transport choisi lit.
 */
export function reachElsewhere(
  printers: PrinterDeviceDTO[],
  transports: TransportDTO[],
  key: string,
): string {
  const elsewhere = [
    ...new Set(
      printers
        .filter((device) => device.key !== key)
        .map((device) => labelOfTransportReading(transports, device.key)),
    ),
  ].filter((label) => label !== '')
  const unreachable = printers.filter((device) => device.key !== key).length
  const count = `${frenchInteger(unreachable)} ${
    unreachable > 1 ? 'destinations ne sont pas proposées' : 'destination n’est pas proposée'
  }`
  if (elsewhere.length === 0) return `${count} : aucun transport de ce poste ne les lit.`
  return `${count} : choisissez « ${elsewhere.join(' » ou « ')} » pour les voir.`
}

/**
 * Le libellé du premier transport qui lit cette clé, ou rien.
 *
 * Le PREMIER, parce que deux transports peuvent lire la même clé — `devfile` et `file`
 * lisent tous deux `path` — et que l'ordre du registre est celui de §8.4 : le défaut
 * d'abord.
 *
 * @param transports - les transports que CE POSTE porte.
 * @param key - la clé d'appareil.
 */
function labelOfTransportReading(transports: TransportDTO[], key: string): string {
  return transports.find((candidate) => candidate.key === key)?.label ?? ''
}
