import type { Draft } from './draft.svelte'

/**
 * The tier grid as the configuration document carries it.
 *
 * It is read from the DOCUMENT rather than from a type: the configuration travels exactly
 * as the file writes it (§11.4), and a screen demanding a fixed shape would refuse a file
 * that a station accepts. Everything here is arithmetic on what a person typed, which is
 * why it is testable with no field and no browser.
 */

/** One tier of the grid, as the document carries it. */
export interface Tier {
  code: string
  label: string
  abbrev: string
  /**
   * The raw value of `discount_percent` as the document carries it, or null when the tier
   * declares none.
   */
  written: string | null
  /** The discount in percent when this field can show it exactly, null otherwise. */
  discount: number | null
  rank: number
}

/**
 * Reads the tier grid from the draft.
 *
 * @param source - the configuration being edited.
 */
export function tiersOf(source: Draft): Tier[] {
  const value = source.value('pricing.tiers')
  if (!Array.isArray(value)) return []
  return value.map((raw) => {
    const row = (raw ?? {}) as Record<string, unknown>
    const discountValue = row.discount_percent
    return {
      code: String(row.code ?? ''),
      label: String(row.label ?? ''),
      abbrev: String(row.abbrev ?? ''),
      written: discountValue === undefined ? null : String(discountValue),
      discount: showable(discountValue) ? (discountValue as number) : null,
      rank: Number(row.rank ?? 0),
    }
  })
}

/**
 * Whether a value read from the document is a discount a field can show.
 *
 * The draft holds whatever a file carries, including what a hand edit put there. Showing
 * 33.333 as « 33,3 » would display a figure nobody declared, and one arrow key would then
 * save it — so the line falls back to read-only instead.
 *
 * The tenth is tested with a tolerance and not with `Number.isInteger(value * 10)`,
 * because `10.2 * 10` is 101.99999999999999 in binary floating point. That is the very
 * reason the kernel stores tenths as an integer.
 *
 * @param value - what the document holds for that tier.
 */
function showable(value: unknown): boolean {
  if (typeof value !== 'number' || !Number.isFinite(value)) return false
  if (value < 0 || value > 100) return false
  return Math.abs(value * 10 - Math.round(value * 10)) < 1e-9
}

/**
 * A discount as a volunteer writes it: a French comma, no trailing zero.
 *
 * @param discount - the discount in percent, or null.
 */
export function discountText(discount: number | null): string {
  return discount === null ? '' : String(discount).replace('.', ',')
}

/**
 * What the discount does to a price, on a round ten euros.
 *
 * Ten euros is not decoration: `1000 c x (100 - d) / 100` falls exactly on a cent for
 * every discount at a tenth of a point, so this preview needs NO rounding and cannot
 * contradict the label coming out of the printer. It reads no product and calls no route.
 *
 * @param discount - the discount in percent.
 */
export function previewOf(discount: number): string {
  const cents = 1000 - Math.round(discount * 10)
  return `${String(Math.trunc(cents / 100))},${String(cents % 100).padStart(2, '0')}`
}
