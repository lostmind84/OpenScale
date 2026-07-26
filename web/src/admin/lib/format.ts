/**
 * La mise en forme que le service ne fait pas à la place de l'écran.
 *
 * La règle du contrat reste entière : quand un jumeau `_text` existe, l'écran l'affiche
 * et ne recalcule rien (`internal/web/dto.go`). Les routes d'administration n'en portent
 * pas pour les dates, les octets et les durées, parce que ces trois-là sont des unités
 * de lecture et pas des grandeurs métier — aucune ne finit sur une étiquette.
 */

/** Milliseconds in one second, spelled out rather than repeated. */
const MS_PER_SECOND = 1000

/**
 * Une date au format que lit un bénévole : 24/07/2026.
 *
 * L'instant arrive en UTC et se lit en heure LOCALE : le poste est dans le magasin, et
 * « Catalogue du 23/07 » un matin du 24 juillet ferait douter de tout le reste.
 *
 * @param iso - un instant RFC 3339, ou une chaîne vide.
 * @returns la date, ou une chaîne vide si l'instant est vide ou illisible.
 */
export function frenchDate(iso: string): string {
  const instant = parse(iso)
  if (instant === null) return ''
  return [
    pad(instant.getDate()),
    pad(instant.getMonth() + 1),
    String(instant.getFullYear()),
  ].join('/')
}

/**
 * Une date et une heure : 24/07/2026 à 11:02.
 *
 * @param iso - un instant RFC 3339, ou une chaîne vide.
 */
export function frenchDateTime(iso: string): string {
  const instant = parse(iso)
  if (instant === null) return ''
  return `${frenchDate(iso)} à ${pad(instant.getHours())}:${pad(instant.getMinutes())}`
}

/**
 * Une heure seule : 11:02:37. C'est la forme utile dans une liste d'événements du jour.
 *
 * @param iso - un instant RFC 3339, ou une chaîne vide.
 */
export function frenchTime(iso: string): string {
  const instant = parse(iso)
  if (instant === null) return ''
  return [
    pad(instant.getHours()),
    pad(instant.getMinutes()),
    pad(instant.getSeconds()),
  ].join(':')
}

/**
 * Une taille en octets, dans l'unité où elle se lit : 700 Mo, 12,3 Go.
 *
 * Les unités décimales et non binaires : c'est celle que Windows et un fournisseur de
 * disque annoncent, et un écran qui dirait « 11,4 Gio » là où le système dit « 12,3 Go »
 * ferait douter de la mesure.
 *
 * @param bytes - une taille en octets.
 */
export function frenchBytes(bytes: number): string {
  const mega = bytes / 1_000_000
  if (mega < 1000) return `${Math.round(mega)} Mo`
  return `${decimal(mega / 1000)} Go`
}

/**
 * Un nombre entier, groupé par milliers avec l'espace insécable du français.
 *
 * @param value - un entier.
 */
export function frenchInteger(value: number): string {
  const digits = String(Math.abs(Math.trunc(value)))
  const groups: string[] = []
  for (let end = digits.length; end > 0; end -= 3) {
    groups.unshift(digits.slice(Math.max(0, end - 3), end))
  }
  // Une espace INSÉCABLE : « 1 236 pesées » ne doit jamais se couper entre le 1 et le 236.
  return (value < 0 ? '-' : '') + groups.join(' ')
}

/**
 * Une durée en millisecondes, telle qu'on en parle : « 400 ms », « 2,5 s ».
 *
 * @param ms - une durée en millisecondes.
 */
export function frenchDuration(ms: number): string {
  if (ms < MS_PER_SECOND) return `${String(Math.round(ms))} ms`
  return `${decimal(ms / MS_PER_SECOND)} s`
}

/** Un nombre à une décimale, virgule française. */
function decimal(value: number): string {
  return value.toFixed(1).replace('.', ',')
}

/** Deux chiffres, comme toute date et toute heure les écrit. */
function pad(value: number): string {
  return String(value).padStart(2, '0')
}

/** Lit un instant, et rend null pour tout ce qui n'en est pas un. */
function parse(iso: string): Date | null {
  if (iso === '') return null
  const instant = new Date(iso)
  return Number.isNaN(instant.getTime()) ? null : instant
}
