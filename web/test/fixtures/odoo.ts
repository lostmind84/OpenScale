import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Catalog, Category, Product } from '../../src/lib/catalog'
import { normalize } from '../../src/lib/normalize'

/**
 * Builds, from a REAL Odoo export, the catalog `GET /api/v1/catalog` will serve.
 *
 * This is a TEST HARNESS standing in for the Go importer of lot L7, which does
 * not exist yet. It is not a second implementation to be maintained: the moment
 * `catalog/csvodoo` exists, this file is replaced by a golden JSON it emits, and
 * the assertions of `catalog-fixture.test.ts` move with it.
 *
 * What makes it trustworthy in the meantime is that its output is checked against
 * the figures the architecture MEASURED on the same file — 355 rows, 331
 * weighable, 8 not weighable, 16 anomalies, 1 divergent unit, A/V/L/F = 140 / 118
 * / 68 / 29, 181 images (§10.2, §10.3). A harness that reproduces all of them
 * qualifies these 355 rows exactly as the service will.
 */

/** Answers « ce produit est-il pesable ? » — three answers, not two (§10.3). */
export type Qualification = 'weighable' | 'not_weighable' | 'anomaly'

/** One qualified row, with everything the import report needs to name it. */
export interface QualifiedRow {
  product: Product
  qualification: Qualification
  /** `NO_BARCODE`, `PREPACKAGED_PRODUCT`, `RESERVED_ZONE_NOT_EMPTY`, … */
  reason: string
  /** A finding that does not change the qualification: `UNIT_MISMATCH`. */
  finding: string
  /** Category letter as the CSV carries it: F, L, V or A. */
  letter: string
  csvLine: number
  hasImage: boolean
}

/** F/L/V/A → the configured codes. A CONSTANT of the Odoo adapter (§10.2 bis). */
const CATEGORY_OF_LETTER: Record<string, string> = {
  F: 'fruits',
  L: 'vegetables',
  V: 'bulk',
  A: 'other',
}

/** Where a letter outside F/L/V/A lands, so the grid is never empty. */
const FALLBACK_CATEGORY = 'other'

/**
 * The categories of the shipped configuration, `testdata/config-lacagette.json`.
 *
 * `product_count` is filled by {@link catalogOf}: the service counts the WEIGHABLE
 * products per shelf when it serves the catalog, and a chip that promised 140 tiles
 * and drew 126 would be a lie the volunteer cannot check (§14.4).
 */
export const LACAGETTE_CATEGORIES: Omit<Category, 'product_count'>[] = [
  { code: 'fruits', label: 'Fruits', rank: 1, color: '#C0392B', visible: true },
  { code: 'vegetables', label: 'Légumes', rank: 2, color: '#27AE60', visible: true },
  { code: 'bulk', label: 'Vrac', rank: 3, color: '#B7950B', visible: true },
  { code: 'other', label: 'Autres', rank: 4, color: '#5D6D7E', visible: true },
]

/** Highest unit price a row may declare, in cents: `MaxUnitPrice` of §6.1. */
const MAX_UNIT_PRICE_CENTS = 999_999

/** Prefixes of the weighing plan, with the width of their payload field (§6.2). */
const WEIGHING_PLAN: Record<string, { payload: number; mode: 'by_weight' | 'by_unit' }> = {
  '0493': { payload: 5, mode: 'by_weight' },
  '0494': { payload: 5, mode: 'by_weight' },
  '0495': { payload: 5, mode: 'by_weight' },
  '0496': { payload: 5, mode: 'by_weight' },
  '0497': { payload: 5, mode: 'by_weight' },
  '0498': { payload: 5, mode: 'by_weight' },
  '0499': { payload: 2, mode: 'by_unit' },
}

/** The three literal values of the `unite` column, with what each one displays. */
const SUFFIX_OF_UNIT: Record<string, string> = {
  kg: ' €/kg',
  'Litre(s)': ' € le litre',
  'Unité(s)': ' € l’unité',
}

/** Units naming a DISCRETE quantity, which contradicts a by-weight prefix. */
const DISCRETE_UNITS = new Set(['Unité(s)'])

/** Units naming a CONTINUOUS quantity, which contradicts a by-unit prefix. */
const CONTINUOUS_UNITS = new Set(['kg', 'Litre(s)'])

/**
 * The discount the primary tier ('A', Adhérent) takes off the reference price,
 * mirroring `discount_percent: 10` of `testdata/config-lacagette.json`.
 *
 * This fixture stands in for the Go importer, never for `domain.UnitPriceFor`
 * (§14.2): the arithmetic below is deliberately the same simple percentage, so
 * a fixture product's two tiles match what the real service would derive from
 * the same reference price.
 */
const MEMBER_DISCOUNT_PERCENT = 10

/** The primary tier's price, in cents, derived from a reference price. */
function memberCentsOf(referenceCents: number): number {
  return Math.round((referenceCents * (100 - MEMBER_DISCOUNT_PERCENT)) / 100)
}

/** Directory of the authentic exports, resolved from this file and not from the cwd. */
const CATALOG_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../../../testdata/catalog')

/** Reads one of the authentic exports of `testdata/catalog/`. */
export function readExport(name: string): string {
  return readFileSync(resolve(CATALOG_DIR, name), 'utf8')
}

/** Qualifies every row of an export, in file order. */
export function qualifyExport(csv: string): QualifiedRow[] {
  const records = parseCSV(csv)
  const rows: QualifiedRow[] = []
  const seen = new Set<string>()
  // Line 1 is the header, compared byte for byte by the service; here it is only
  // skipped, because a wrong header is a finding and never a qualification.
  for (let i = 1; i < records.length; i++) {
    const record = records[i] as string[]
    const qualified = qualifyRow(record, i + 1, seen)
    if (qualified !== null) rows.push(qualified)
  }
  return rows
}

/** Turns the qualified rows into the catalog `GET /api/v1/catalog` serves. */
export function catalogOf(rows: QualifiedRow[], revision = '"fixture"'): Catalog {
  const products = rows.filter((r) => r.qualification === 'weighable').map((r) => r.product)
  const counts = new Map<string, number>()
  for (const p of products) counts.set(p.category_code, (counts.get(p.category_code) ?? 0) + 1)
  return {
    revision,
    // L'instant de la bascule, tel que le Hub le date (§14.3). Figé ici : un
    // catalogue de test ne doit pas changer d'empreinte à chaque exécution.
    updated_at: '2026-07-27T08:06:48Z',
    // La version que la barre basse énonce en permanence (§14.3). Figée comme
    // la date, et pour la même raison.
    app_version: '2.4.0',
    product_count: products.length,
    products,
    categories: LACAGETTE_CATEGORIES.map((c) => ({
      ...c,
      product_count: counts.get(c.code) ?? 0,
    })),
    fallback_category: FALLBACK_CATEGORY,
    pricing: {
      primary_code: 'A',
      primary_label: 'Adhérent',
      tiers: [
        { code: 'A', label: 'Adhérent', abbrev: 'A', rank: 1 },
        { code: 'S', label: 'Solidaire', abbrev: 'S', rank: 2 },
      ],
    },
    presentation: {
      show_grid_prices: true,
      idle_timeout_s: 45,
      reprint_window_s: 60,
      sound: true,
      // À `true`, ÉCRIT, et différent du fichier livré qui masque ces produits : les
      // 331 tuiles sont le corpus du CATALOGUE et non celui d'un poste. Laissée à
      // `false` ici, une quinzaine d'assertions de grid, screen, chips et typography
      // basculeraient à 316 dans le même commit, et une vraie régression s'y cacherait.
      // `unit-products.test.ts` est le banc qui décrit ce qu'un poste en fait.
      show_by_unit_products: true,
      // Automatique, le défaut : les bancs qui portent sur le catalogue ne décrivent
      // pas un poste qui a réglé sa grille. `grid.test.ts` est celui qui le fait.
      grid_columns: 0,
    },
  }
}

/** Reads an export and serves it, which is what a station sees on first boot. */
export function catalogFromExport(name: string): Catalog {
  return catalogOf(qualifyExport(readExport(name)))
}

/** Applies the six questions of §10.3, in order, to one row. */
function qualifyRow(record: string[], csvLine: number, seen: Set<string>): QualifiedRow | null {
  const [id = '', name = '', barcode = '', price = '', letter = '', unit = ''] = record.map((f) =>
    f.trim(),
  )
  const image = (record[6] ?? '').trim()

  // 1. Is the row readable? Seven fields, a non-empty unique id, a non-empty name.
  if (record.length !== 7 || id === '' || name === '' || seen.has(id)) return null
  seen.add(id)

  const category = CATEGORY_OF_LETTER[letter] ?? FALLBACK_CATEGORY
  const prefix = barcode.slice(0, 4)
  const plan = WEIGHING_PLAN[prefix]
  const mode = plan?.mode ?? 'by_weight'
  const suffix = SUFFIX_OF_UNIT[unit] ?? (mode === 'by_unit' ? ' € l’unité' : ' €/kg')
  const cents = parsePriceCents(price)
  const imageName = image === '' ? '' : imageAsset(image)
  const referenceCents = cents ?? 0

  const product: Product = {
    id,
    name,
    search: normalize(name),
    category_code: category,
    mode,
    unit_price_cents: referenceCents,
    unit_price_text: euroText(referenceCents),
    price_suffix: suffix,
    image_url: imageName === '' ? '' : `/images/${imageName}`,
    // Two entries, primary first: 'A' (Adhérent) then 'S' (Solidaire, the
    // reference) — the fixture's `pricing.tiers` above declares exactly these
    // two, so every tile shows a double tarif, never just one.
    prices: [
      { code: 'A', text: euroText(memberCentsOf(referenceCents)) },
      { code: 'S', text: euroText(referenceCents) },
    ],
  }
  const base = { product, letter, csvLine, hasImage: image !== '', finding: '' }

  // 2. Is the price a usable number?
  if (cents === null || cents > MAX_UNIT_PRICE_CENTS) {
    return { ...base, qualification: 'anomaly', reason: 'PRICE_UNREADABLE' }
  }
  if (cents === 0) return { ...base, qualification: 'anomaly', reason: 'ZERO_PRICE' }

  // 3. Does the product have a barcode at all?
  if (barcode === '') return { ...base, qualification: 'not_weighable', reason: 'NO_BARCODE' }

  // 4. Is it a valid EAN-13?
  if (!isEAN13(barcode)) {
    return { ...base, qualification: 'anomaly', reason: 'INVALID_BARCODE' }
  }

  // 5. Is it a code of the weighing plan?
  if (plan === undefined) {
    const reason = prefix.startsWith('049')
      ? 'INTERNAL_CODE_NOT_WEIGHABLE'
      : 'PREPACKAGED_PRODUCT'
    return { ...base, qualification: 'not_weighable', reason }
  }

  // 6. Is the reserved zone empty? Without this a label would designate ANOTHER
  //    product: at 1,236 kg, 0493100100006 reads in the till as an 11,236 kg
  //    PATATE DOUCE SAF (§6.2, T32).
  const reserved = barcode.slice(12 - plan.payload, 12)
  if (reserved.replace(/0/gu, '') !== '') {
    return { ...base, qualification: 'anomaly', reason: 'RESERVED_ZONE_NOT_EMPTY' }
  }

  // Weighable. One extra finding that changes nothing: the prefix and the `unite`
  // column contradicting each other BY NATURE. The barcode is authoritative — it
  // is the only one of the two the till reads.
  const mismatch =
    (mode === 'by_weight' && DISCRETE_UNITS.has(unit)) ||
    (mode === 'by_unit' && CONTINUOUS_UNITS.has(unit))
  return {
    ...base,
    qualification: 'weighable',
    reason: '',
    finding: mismatch ? 'UNIT_MISMATCH' : '',
  }
}

/**
 * Parses a price to cents by INTEGER arithmetic, never by `parseFloat`.
 *
 * `"16.05"` is 1605, `"4.3"` is 430, `"3"` is 300. The comma is tolerated, though
 * neither real file contains one.
 */
function parsePriceCents(price: string): number | null {
  const match = /^(\d+)(?:[.,](\d{1,2}))?$/u.exec(price)
  if (match === null) return null
  const units = Number(match[1])
  const decimals = (match[2] ?? '').padEnd(2, '0')
  return units * 100 + Number(decimals)
}

/** Formats cents the way `domain.Cents.Euro` does: French comma, two decimals. */
function euroText(cents: number): string {
  return `${Math.trunc(cents / 100)},${String(cents % 100).padStart(2, '0')}`
}

/** Reports whether a string is 13 digits whose last one is the check digit. */
function isEAN13(code: string): boolean {
  if (!/^\d{13}$/u.test(code)) return false
  let even = 0
  let odd = 0
  for (let i = 0; i < 12; i++) {
    const digit = code.charCodeAt(i) - 48
    if ((i + 1) % 2 === 0) even += digit
    else odd += digit
  }
  return (10 - ((3 * even + odd) % 10)) % 10 === code.charCodeAt(12) - 48
}

/**
 * Names an image the way `GET /images/{sha}.{ext}` will.
 *
 * The format is recognised from the HEADER BYTES and never from an extension: the
 * legacy application named ten PNG files `.jpg` (§10.7).
 */
function imageAsset(base64: string): string {
  const bytes = Buffer.from(base64, 'base64')
  const sha = createHash('sha256').update(bytes).digest('hex')
  return `${sha}.${extensionOf(bytes)}`
}

/** Reads the format of an image from its first bytes. */
function extensionOf(bytes: Buffer): string {
  if (bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'jpg'
  if (bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47) return 'png'
  if (bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46) return 'gif'
  if (bytes[0] === 0x42 && bytes[1] === 0x4d) return 'bmp'
  return 'bin'
}

/**
 * Parses the exchange format: `;` separated, every value double quoted, CRLF.
 *
 * Written out rather than pulled from a package because the format is frozen by
 * Odoo and observed on two authentic files four and a half years apart — it has
 * not moved by one byte (§10.2).
 */
function parseCSV(text: string): string[][] {
  const withoutBOM = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text
  const records: string[][] = []
  let record: string[] = []
  let field = ''
  let quoted = false

  for (let i = 0; i < withoutBOM.length; i++) {
    const c = withoutBOM[i]
    if (quoted) {
      if (c === '"') {
        if (withoutBOM[i + 1] === '"') {
          field += '"'
          i++
        } else quoted = false
      } else field += c
      continue
    }
    if (c === '"') quoted = true
    else if (c === ';') {
      record.push(field)
      field = ''
    } else if (c === '\n') {
      record.push(field)
      records.push(record)
      record = []
      field = ''
    } else if (c !== '\r') field += c
  }
  if (field !== '' || record.length > 0) {
    record.push(field)
    records.push(record)
  }
  return records
}
