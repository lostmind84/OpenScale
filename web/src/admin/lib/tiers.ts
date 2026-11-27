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

type TierRecord = Record<string, unknown>

/** Reads a shallow-cloned copy of each tier object from the draft. */
function rawTiers(draft: Draft): TierRecord[] {
  const value = draft.value('pricing.tiers')
  if (!Array.isArray(value)) return []
  return value.map((raw) => ({ ...((raw ?? {}) as TierRecord) }))
}

/** Picks an unused internal code for a newly added tier. */
function nextUniqueCode(existing: TierRecord[]): string {
  if (existing.length === 0) return 'STANDARD'
  const used = new Set(existing.map((row) => String(row.code ?? '')))
  let index = existing.length + 1
  while (used.has(`TARIF_${String(index)}`)) {
    index += 1
  }
  return `TARIF_${String(index)}`
}

/** Computes the next display rank above all existing tiers. */
function nextRankOf(existing: TierRecord[]): number {
  const highest = existing.reduce((max, row) => {
    const rank = Number(row.rank ?? 0)
    return Number.isFinite(rank) && rank > max ? rank : max
  }, 0)
  return highest + 1
}

/** Collects every non-empty tier code present in the rows. */
function codesOf(rows: TierRecord[]): string[] {
  return rows.map((row) => String(row.code ?? '')).filter((code) => code !== '')
}

/** Keeps secondary_codes aligned with all non-primary tiers in the grid. */
function syncSecondaryCodes(draft: Draft, codes: string[], primaryCode: string): void {
  draft.set(
    'pricing.secondary_codes',
    codes.filter((code) => code !== primaryCode),
  )
}

/** Picks the best surviving row to act as the catalog-price reference tier. */
function pickReferenceRow(rows: TierRecord[]): TierRecord | undefined {
  return (
    rows.find((row) => row.discount_percent === undefined || row.discount_percent === 0) ?? rows[0]
  )
}

/**
 * Appends a new tier to the grid and fills missing role references so saving stays valid.
 *
 * @param draft - the configuration being edited.
 */
export function addTier(draft: Draft): void {
  const existing = rawTiers(draft)
  const code = nextUniqueCode(existing)
  const rank = existing.length === 0 ? 1 : nextRankOf(existing)
  const added: TierRecord =
    existing.length === 0
      ? { code, label: 'Prix', abbrev: '', rank: 1 }
      : { code, label: `Tarif ${String(rank)}`, abbrev: '', discount_percent: 0, rank }

  const nextRows = [...existing, added]
  const codes = codesOf(nextRows)

  const currentPrimary = String(draft.value('pricing.primary_code') ?? '')
  const nextPrimary = codes.includes(currentPrimary) ? currentPrimary : code
  if (currentPrimary !== nextPrimary) {
    draft.set('pricing.primary_code', nextPrimary)
  }

  const currentReference = String(draft.value('pricing.reference_code') ?? '')
  if (!codes.includes(currentReference)) {
    delete added.discount_percent
    draft.set('pricing.reference_code', code)
  }

  draft.set('pricing.tiers', nextRows)
  syncSecondaryCodes(draft, codes, nextPrimary)
}

/**
 * Removes one tier (when more than one remains), renumbers ranks, and realigns
 * primary, reference, and secondary tier codes.
 *
 * Removing a tier is not irreversible until saved, and must never leave the new
 * reference tier carrying a forbidden discount_percent key.
 *
 * @param draft - the configuration being edited.
 * @param index - zero-based index of the tier to remove.
 */
export function removeTier(draft: Draft, index: number): void {
  const existing = rawTiers(draft)
  if (existing.length <= 1 || index < 0 || index >= existing.length) return

  const nextRows = existing
    .filter((_row, position) => position !== index)
    .map((row, position) => ({ ...row, rank: position + 1 }))
  const codes = codesOf(nextRows)

  const currentPrimary = String(draft.value('pricing.primary_code') ?? '')
  const nextPrimary = codes.includes(currentPrimary) ? currentPrimary : (codes[0] ?? '')
  if (currentPrimary !== nextPrimary) {
    draft.set('pricing.primary_code', nextPrimary)
  }

  const currentReference = String(draft.value('pricing.reference_code') ?? '')
  if (!codes.includes(currentReference)) {
    const refRow = pickReferenceRow(nextRows)
    if (refRow !== undefined) {
      delete refRow.discount_percent
      draft.set('pricing.reference_code', String(refRow.code ?? ''))
    }
  }

  draft.set('pricing.tiers', nextRows)
  syncSecondaryCodes(draft, codes, nextPrimary)
}

/**
 * Updates `pricing.primary_code` and aligns `pricing.secondary_codes` with every
 * remaining tier code in the grid.
 *
 * @param draft - the configuration being edited.
 * @param code - chosen primary tier code.
 */
export function setPrimaryCode(draft: Draft, code: string): void {
  draft.set('pricing.primary_code', code)
  const tiers = tiersOf(draft)
  if (tiers.some((tier) => tier.code === code && tier.code !== '')) {
    const codes = tiers.map((tier) => tier.code).filter((candidate) => candidate !== '')
    syncSecondaryCodes(draft, codes, code)
  }
}

/**
 * Builds `<select>` options for tier role selectors without masking an unknown value
 * carried by the file on disk.
 *
 * @param tiers - tiers currently in the grid.
 * @param current - currently configured tier code.
 */
export function tierChoices(tiers: Tier[], current: string): { value: string; label: string }[] {
  const seen = new Set<string>()
  const choices: { value: string; label: string }[] = []
  for (const tier of tiers) {
    if (tier.code === '' || seen.has(tier.code)) continue
    seen.add(tier.code)
    choices.push({
      value: tier.code,
      label: tier.label.trim() !== '' ? tier.label : tier.code,
    })
  }
  if (choices.length > 0 && !seen.has(current)) {
    choices.push({ value: current, label: current === '' ? '—' : current })
  }
  return choices
}
