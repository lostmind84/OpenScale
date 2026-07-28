import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Rules from '../src/admin/pages/Rules.svelte'
import Station from '../src/admin/pages/Station.svelte'
import { Draft } from '../src/admin/lib/draft.svelte'
import type { FaultDTO } from '../src/admin/lib/dto'
import { FIELD_LABELS } from '../src/admin/lib/fields'
import { preferences } from '../src/admin/lib/preferences.svelte'
import { Admin } from '../src/admin/lib/session.svelte'
import { nominalHealth } from './fixtures/health'

/**
 * Ce que les pages RENDENT, interrupteur décoché puis coché.
 *
 * `admin-wording.test.ts` lit la SOURCE des pages, et sa fonction `markupText` efface les
 * accolades de Svelte avant d'assertionner : `<code>{path}</code>` disparaît du texte
 * examiné, et trois clés de configuration s'affichaient donc à l'écran sous un banc vert —
 * l'interrupteur de la page Règles, la liste des refus d'un fichier importé et la colonne
 * « Champ » du diff. Un banc qui lit du texte source ne peut pas voir ce qu'une expression
 * rend à l'exécution ; celui-ci MONTE les pages et lit `host.textContent`.
 *
 * Le tri se fait sur l'index des libellés et non sur un motif « mot.mot » : « 10.5 » et
 * « flv_2.csv » ne sont pas des réglages.
 */

/** Les clés de configuration qu'un texte laisse voir, index en main. */
function keysIn(text: string): string[] {
  return Object.keys(FIELD_LABELS).filter((key) => text.includes(key))
}

let host: HTMLElement
let component: unknown
/** Ce que les contrôles du poste disent du fichier importé. */
let importFaults: FaultDTO[] = []

beforeEach(() => {
  globalThis.localStorage.clear()
  preferences.showTechnicalNames = false
  importFaults = []
  host = document.createElement('div')
  document.body.append(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/** Réduit les blancs et ramène toute apostrophe à une seule forme. */
function collapse(text: string): string {
  return text.replace(/\s+/gu, ' ').replace(/[’´`]/gu, "'").trim()
}

/** Le texte de la page entière, tel que l'œil le lit. */
function pageText(): string {
  return collapse(host.textContent ?? '')
}

/** Coche l'interrupteur, comme le rail le fait, et redessine. */
function showTechnicalNames(): void {
  preferences.showTechnicalNames = true
  flushSync()
}

/** Laisse les lectures se terminer, puis met le DOM à jour. */
async function settle(): Promise<void> {
  for (let round = 0; round < 8; round += 1) {
    await new Promise((done) => setTimeout(done, 0))
    flushSync()
  }
}

/**
 * La configuration de ce banc, écrite avec les VRAIES clés du poste.
 *
 * Un fixture aux clés inventées ne dirait rien : l'index ne les connaîtrait pas, et
 * l'assertion « interrupteur coché, la clé revient » passerait sans que rien ne s'affiche.
 */
function configWithPort(port: string): Record<string, unknown> {
  return {
    modified_at: '2026-07-24T09:12:00.000Z',
    station: { number: 2, name: 'Poste 2 — fruits', coop: 'La Cagette' },
    network: { listen: '127.0.0.1:8080', admin_on_lan: false },
    scale: { type: 'gram_xfoc', present: true, options: { port, baud: 9600 } },
    printer: { type: 'raster', options: { transport: 'local', queue: 'Étiqueteuse' } },
    catalog: { type: 'local_drop', options: { directory: 'C:\\depot' } },
    pricing: {
      tiers: [{ code: 'MEMBER', label: 'Adhérent', abbrev: 'A', discount_percent: 10, rank: 1 }],
      primary_code: 'MEMBER',
      reference_code: 'SOLIDARITY',
      amount_rounding: 'half_up',
      unit_price_rounding: 'half_up',
    },
    barcode: { verify_reference_check_digit: true },
    limits: {
      empty_max_g: 5,
      basket_check_enabled: true,
      basket_min_g: -282,
      basket_max_g: -270,
      min_weight_g: 10,
      max_weight_g: 15_000,
      max_tare_g: 9999,
      min_units: 1,
      max_units: 99,
      max_amount_cents: 99_999,
    },
  }
}

/** Les routes que ces deux pages touchent, et rien d'autre. */
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  const url = String(input)
  if (url === '/api/v1/catalog') return json({ products: [] })
  if (url === '/admin/api/config/import') {
    return json({ ...payload(fileConfig()), faults: importFaults, changed_blocks: ['scale'] })
  }
  if (url === '/admin/api/config/versions') return json({ versions: [] })
  if (url === '/admin/api/config') return json(payload(configWithPort('COM8')))
  if (url === '/admin/api/health') return json(nominalHealth())
  return json({ expires_at: '', session_minutes: 30 })
}

/**
 * Le fichier apporté d'un autre poste.
 *
 * Il diffère sur DEUX chemins : l'un que l'index nomme en français — `scale.options.port` —
 * et l'autre non — `pricing.tiers`. Le diff d'un clonage porte toujours les deux espèces,
 * et le repli de l'index est ce qui garde la seconde ligne identifiable.
 */
function fileConfig(): Record<string, unknown> {
  const config = configWithPort('COM3')
  const pricing = config.pricing as Record<string, unknown>
  pricing.tiers = [{ code: 'MEMBER', label: 'Adhérent', abbrev: 'A', discount_percent: 5, rank: 1 }]
  return config
}

/** La charge de `GET /admin/api/config`, telle que `internal/web/config.go` l'écrit. */
function payload(config: Record<string, unknown>): Record<string, unknown> {
  return { config, config_fingerprint: 'a1b2c3d4', retired_keys: [], pending_confirmation: null }
}

/** Une réponse JSON, comme le service en écrit. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/** Monte la page Règles sur la configuration de ce banc. */
function openRules(): void {
  const draft = new Draft(new Admin())
  draft.config = configWithPort('COM8')
  component = mount(Rules, { target: host, props: { draft, health: nominalHealth() } })
  flushSync()
}

/** Monte la page Poste, un fichier déjà importé : c'est là que sont les deux écarts. */
async function openStationWithAFile(): Promise<void> {
  const admin = new Admin(60_000)
  const draft = new Draft(admin)
  await draft.load()
  component = mount(Station, { target: host, props: { admin, draft, health: nominalHealth() } })
  flushSync()
  await settle()

  const input = host.querySelector<HTMLInputElement>('input[type="file"]')
  if (input === null) throw new Error('aucun sélecteur de fichier sur cette page')
  const file = new File([JSON.stringify(fileConfig())], 'config-poste1.json', {
    type: 'application/json',
  })
  Object.defineProperty(input, 'files', {
    value: { item: () => file, length: 1 },
    configurable: true,
  })
  input.dispatchEvent(new Event('change', { bubbles: true }))
  await settle()
}

/** Ce qu'une ligne du diff affiche, retrouvée par le chemin qui l'identifie. */
function diffRowText(path: string): string {
  const row = host.querySelector(`[data-diff] tr[data-path="${path}"]`)
  if (row === null) throw new Error(`aucune ligne « ${path} » dans le diff`)
  return collapse(row.textContent ?? '')
}

/** Ce que la colonne « Champ » d'une ligne du diff affiche, elle seule. */
function diffFieldCell(path: string): string {
  const cell = host.querySelector(`[data-diff] tr[data-path="${path}"] td`)
  if (cell === null) throw new Error(`aucune ligne « ${path} » dans le diff`)
  return collapse(cell.textContent ?? '')
}

/** Ce que la liste des refus du fichier importé affiche. */
function refusalsText(): string {
  const refusals = host.querySelector('[data-faults]')
  if (refusals === null) throw new Error('aucune liste de refus sur cette page')
  return collapse(refusals.textContent ?? '')
}

describe('la page Règles', () => {
  it('ne montre aucune clé de configuration, interrupteur décoché', () => {
    openRules()

    // Les deux interrupteurs de la page écrivaient leur chemin sans garde : « Ce poste
    // travaille avec un panier taré limits.basket_check_enabled » se lisait tel quel.
    expect(keysIn(pageText())).toEqual([])
  })

  it('rend la clé de ses interrupteurs quand on coche', () => {
    openRules()
    showTechnicalNames()

    const shown = keysIn(pageText())
    expect(shown).toContain('limits.basket_check_enabled')
    expect(shown).toContain('barcode.verify_reference_check_digit')
    // Et les champs de seuils, que `Field` gardait déjà derrière l'interrupteur.
    expect(shown).toContain('limits.max_weight_g')
  })
})

describe('la page Poste, un fichier importé sous les yeux', () => {
  it('ne montre aucune clé de configuration, interrupteur décoché', async () => {
    importFaults = [{ field: 'limits.max_weight_g', message: '99999 hors bornes [1, 50000].' }]
    await openStationWithAFile()

    // Les deux endroits fautifs sont bien à l'écran : sans eux, un banc vert ne dirait
    // rien de plus que « cette page n'affiche pas de refus et ne compare rien ».
    expect(host.querySelector('[data-faults]'), 'aucune liste de refus').not.toBeNull()
    expect(host.querySelector('[data-diff]'), 'aucun tableau de diff').not.toBeNull()
    expect(keysIn(pageText())).toEqual([])
  })

  it('nomme en français le champ que le fichier ferait refuser', async () => {
    importFaults = [{ field: 'limits.max_weight_g', message: '99999 hors bornes [1, 50000].' }]
    await openStationWithAFile()

    expect(refusalsText()).toContain('Poids maximum accepté')
    expect(refusalsText()).toContain('99999 hors bornes [1, 50000].')
  })

  it('nomme en français la colonne « Champ » du diff', async () => {
    await openStationWithAFile()

    const row = diffRowText('scale.options.port')
    expect(row).toContain('Port série')
    // Ce que la ligne COMPARE ne bouge pas d'un iota : elle change seulement de nom.
    expect(row).toContain('COM8')
    expect(row).toContain('COM3')
  })

  it('garde lisible la ligne d’un chemin que l’index ne nomme pas', async () => {
    await openStationWithAFile()

    // `pricing.tiers` n'a pas de libellé français : le repli affiche le chemin, et c'est
    // ce que la spécification prévoit — une ligne venue d'un bloc qu'aucune page n'édite
    // reste lisible plutôt que de disparaître.
    expect(diffFieldCell('pricing.tiers')).toBe('pricing.tiers')

    // Coché, le chemin ne s'écrit pas DEUX fois dans la même cellule.
    showTechnicalNames()
    expect(diffFieldCell('pricing.tiers')).toBe('pricing.tiers')
  })

  it('rend les clés du refus et du diff quand on coche', async () => {
    importFaults = [{ field: 'limits.max_weight_g', message: '99999 hors bornes [1, 50000].' }]
    await openStationWithAFile()
    showTechnicalNames()

    expect(refusalsText()).toContain('limits.max_weight_g')
    expect(diffRowText('scale.options.port')).toContain('scale.options.port')
  })
})
