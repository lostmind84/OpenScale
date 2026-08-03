/**
 * The tokens the journal writes, and the French word each one reads as.
 *
 * A value this file has never heard of must not reach a volunteer as it came: it would
 * appear as an English word in a French column, and nobody would know it was new. That is
 * what {@link french} is for, and why no table is ever read directly.
 *
 * `sent` is the SUCCESS, and it is spelled out rather than shortened to « imprimée »
 * because the station does not know whether the label came out: it knows it handed the
 * bytes over (important-7). There is no `printed` and there never was — a filter asking
 * for one selected NOTHING on a station that had printed all day.
 */

/** How a weighing ended. The four values of `internal/domain/journal.go`. */
export const RESULTS: Record<string, string> = {
  sent: 'envoyée à l’imprimante',
  rejected: 'refusée',
  failed: 'en échec',
  reprint: 'réimpression',
}

/** The same four values as a filter, plus « toutes ». The plural reads as a heading. */
export const RESULT_FILTERS: { value: string; label: string }[] = [
  { value: '', label: 'toutes' },
  { value: 'sent', label: 'envoyées à l’imprimante' },
  { value: 'rejected', label: 'refusées' },
  { value: 'failed', label: 'en échec' },
  { value: 'reprint', label: 'réimpressions' },
]

/** Where a weight came from, in French. The three values of §12.3. */
export const SOURCES: Record<string, string> = {
  scale: 'balance',
  manual: 'saisie manuelle',
  replay: 'trame rejouée',
}

/** What the frame said about stability, in French. The four values of §9.2. */
export const STABILITIES: Record<string, string> = {
  stable: 'stable',
  unstable: 'instable',
  unknown: 'non déclarée par la balance',
  not_applicable: 'sans objet — saisie manuelle',
}

/** How the product is sold, in French. The two modes of ADR-021. */
export const MODES: Record<string, string> = {
  by_weight: 'au poids',
  by_unit: 'à l’unité',
}

/** The severity of a technical line, in French. The five levels of `internal/store`. */
export const LEVELS: Record<string, string> = {
  debug: 'mise au point',
  info: 'information',
  warn: 'avertissement',
  error: 'erreur',
  critical: 'critique',
}

/**
 * Reads one token of the service in French, and never lets an unknown one through.
 *
 * @param table - the translations of that field.
 * @param token - what the service wrote.
 * @param unknown - the French sentence for a token nobody has declared.
 */
export function french(table: Record<string, string>, token: string, unknown: string): string {
  return table[token] ?? unknown
}
