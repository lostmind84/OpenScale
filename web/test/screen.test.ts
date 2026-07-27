import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../src/App.svelte'
import type { LabelDTO, StateDTO } from '../src/lib/dto'
import { catalogFromExport } from './fixtures/odoo'

/**
 * L'écran client complet, monté sur le catalogue réel.
 *
 * Ce test tient les quatre promesses que §14.3 formule en client :
 * la grille montre les 331 pesables d'un seul tenant · la recherche filtre EN
 * PLACE, la grille restant visible · la barre de réimpression est PERMANENTE ·
 * un toucher fait un seul POST, et il porte la clé d'idempotence.
 */

const catalog = catalogFromExport('flv.csv')

/** Les corps envoyés à `POST /api/v1/weigh`, dans l'ordre. */
let posted: { route: string; body: Record<string, unknown> }[] = []

/** Le dernier flux SSE ouvert, pour lui pousser des états à la main. */
let stream: FakeEventSource | null = null

/** Un `EventSource` de test : il n'ouvre rien et se laisse alimenter. */
class FakeEventSource {
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  #listeners: ((e: MessageEvent<string>) => void)[] = []

  constructor(readonly url: string) {
    stream = this
  }

  /** Le serveur émet `event: state` : c'est un événement NOMMÉ, pas `message`. */
  addEventListener(name: string, listener: (e: MessageEvent<string>) => void): void {
    if (name === 'state') this.#listeners.push(listener)
  }

  close(): void {
    this.closed = true
  }

  /** Pousse un état comme le ferait le Hub. */
  push(state: StateDTO): void {
    this.onopen?.()
    const event = new MessageEvent('state', { data: JSON.stringify(state) })
    for (const listener of this.#listeners) listener(event)
  }
}

/** Un état au repos, sac posé, matériel nominal — la forme exacte de `stateDTO`. */
function restingState(overrides: Partial<StateDTO> = {}): StateDTO {
  return {
    revision: 1,
    at: '2026-07-24T10:00:00.000Z',
    state: 'weight_stable',
    station: 2,
    weight: {
      available: true,
      expired: false,
      gross_g: 1236,
      tare_g: 0,
      net_g: 1236,
      quantity: 1,
      net_text: '1,236',
      stability: 'stable',
      latched: true,
      seq: 42,
      age_ms: 120,
      expiry_ms: 1200,
    },
    product: null,
    label: null,
    last_label: null,
    reprint: { available: false, job_id: '', printed_at: '' },
    message: null,
    sound: '',
    diagnostics: [],
    fault_code: '',
    arming_expires_at: '',
    scale: {
      connected: true,
      median_ms: 400,
      observations_count: 64,
      provisional: false,
      too_slow: false,
    },
    printer: { health: 'ready', detail: '', pending_jobs_count: 0, observed_at: '' },
    degraded: null,
    catalog_count: catalog.product_count,
    unlogged_weighings_count: 0,
    ...overrides,
  }
}

/** L'étiquette d'ail de §4, telle que `labelDTO` la porte. */
function garlicLabel(): LabelDTO {
  return {
    job_id: '01J9F2ABC',
    barcode: '0493021012365',
    product_id: '4412',
    product_name: 'ail',
    mode: 'by_weight',
    gross_g: 1236,
    tare_g: 0,
    net_g: 1236,
    net_text: '1,236',
    quantity: 1,
    prices: [],
    primary_code: 'A',
    reference_code: 'S',
  }
}

/** La tuile sélectionnée, telle que le flux la renvoie. */
function selected(product: { id: string; name: string }): StateDTO['product'] {
  return {
    id: product.id,
    name: product.name,
    category_code: 'bulk',
    mode: 'by_weight',
    unit_price_cents: 532,
    unit_price_text: '5,32',
    price_suffix: ' €/kg',
    image_url: '',
  }
}

let host: HTMLElement
let component: Record<string, unknown> | null = null

beforeEach(() => {
  posted = []
  stream = null
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    if (init?.method === 'POST') {
      posted.push({ route: input, body: JSON.parse(String(init.body)) as Record<string, unknown> })
      return new Response('{}', { status: 202 })
    }
    // L'administration a son propre contrat : lui servir le catalogue la ferait rendre un
    // tableau de bord sur des champs absents. Ce fichier teste l'écran CLIENT ; il vérifie
    // que l'administration s'ouvre et se ferme, pas ce qu'elle affiche.
    if (String(input).startsWith('/admin/api/')) {
      return new Response('{"message":"Poste indisponible dans ce test."}', { status: 503 })
    }
    return new Response(JSON.stringify(catalog), { status: 200 })
  })
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  if (component !== null) unmount(component)
  host.remove()
  component = null
  vi.unstubAllGlobals()
})

/** Monte l'écran, laisse le catalogue arriver, puis pousse un premier état. */
async function open(state = restingState()): Promise<void> {
  component = mount(App, { target: host })
  flushSync()
  await vi.waitUntil(() => host.querySelectorAll('button[data-product-id]').length > 0)
  stream?.push(state)
  flushSync()
}

/** Les tuiles actuellement rendues. */
function tiles(): HTMLElement[] {
  return [...host.querySelectorAll<HTMLElement>('button[data-product-id]')]
}

/** Clique une touche du clavier réduit par son libellé. */
function pressKey(label: string): void {
  const key = [...host.querySelectorAll<HTMLElement>('.panel button')].find(
    (b) => b.textContent?.trim() === label,
  )
  key?.click()
  flushSync()
}

describe('la grille et la barre basse', () => {
  it('montre les 331 pesables dès l’ouverture', async () => {
    await open()
    expect(tiles()).toHaveLength(331)
  })

  it('montre une puce par catégorie peuplée, « Tout » en tête', async () => {
    await open()
    const labels = [...host.querySelectorAll('.chip-label')].map((c) => c.textContent?.trim())
    expect(labels.slice(0, 5)).toEqual(['Tout', 'Fruits', 'Légumes', 'Vrac', 'Autres'])
  })

  it('filtre en place quand on touche une puce, sans changer d’écran', async () => {
    await open()
    const others = [...host.querySelectorAll<HTMLElement>('.chip')].find((c) =>
      c.textContent?.includes('Autres'),
    )
    others?.click()
    flushSync()
    expect(tiles()).toHaveLength(126)
    // La barre de réimpression et le bandeau sont toujours là : rien n'a été remplacé.
    expect(host.querySelector('.bar')).not.toBeNull()
    expect(host.querySelector('.banner')).not.toBeNull()
  })

  it('garde la barre de réimpression PERMANENTE, même sans étiquette', async () => {
    await open()
    expect(host.querySelector('.bar')?.textContent).toContain('Dernière étiquette')
  })

  it('active « Réimprimer » quand la fenêtre est ouverte, et pas sinon', async () => {
    await open(
      restingState({
        state: 'succeeded',
        last_label: garlicLabel(),
        reprint: { available: true, job_id: '01J9F2ABC', printed_at: '2026-07-24T10:00:01.000Z' },
      }),
    )
    const button = host.querySelector<HTMLButtonElement>('.reprint')
    expect(host.querySelector('.summary')?.textContent).toContain('ail 1,236 kg')
    expect(button?.disabled).toBe(false)

    stream?.push(restingState({ revision: 2 }))
    flushSync()
    expect(host.querySelector<HTMLButtonElement>('.reprint')).toBeNull()
  })
})

describe('ce que l’écran dit en permanence', () => {
  it('affiche la date et l’heure du catalogue en service', async () => {
    await open()
    // `updated_at` de la fixture, rendu dans le fuseau du poste.
    const bar = host.querySelector('.bar')?.textContent ?? ''
    expect(bar).toContain('Catalogue du')
    expect(bar).toMatch(/\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}:\d{2}/u)
  })

  it('ouvre l’administration d’un seul appui sur une touche nommée', async () => {
    await open()
    const key = [...host.querySelectorAll<HTMLElement>('.filters button')].find((b) =>
      b.textContent?.includes('Réglages'),
    )
    expect(key).toBeDefined()
    // Le coin muet de trois secondes n'existe plus : rien à trouver à l'aveugle.
    expect(host.querySelector('.admin-corner')).toBeNull()
  })

  it('rouvre l’administration après un retour à l’écran client', async () => {
    // Le garde d'ADR-032 ne se relâchait jamais : après un aller-retour, la touche
    // Réglages ne répondait plus JAMAIS. `mountAdmin` se garde déjà d'un doublon.
    await open()
    const key = [...host.querySelectorAll<HTMLElement>('.filters button')].find((b) =>
      b.textContent?.includes('Réglages'),
    ) as HTMLElement

    key.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') !== null)

    const back = [...document.querySelectorAll<HTMLElement>('[data-admin] button')].find((b) =>
      b.textContent?.includes('Revenir à l’écran client'),
    ) as HTMLElement
    back.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') === null)

    key.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') !== null)
  })

  it('applique la densité de tuile que le poste configure', async () => {
    await open()
    expect(host.querySelector('.screen')?.getAttribute('data-tile-size')).toBe('medium')
  })
})

describe('la recherche : un filtre en place, jamais une vue', () => {
  it('garde la grille visible et la réduit lettre après lettre', async () => {
    await open()
    host.querySelector<HTMLElement>('.search-key')?.click()
    flushSync()
    expect(tiles()).toHaveLength(331)

    pressKey('C')
    const afterC = tiles().length
    expect(afterC).toBeLessThan(331)
    expect(afterC).toBeGreaterThan(0)

    pressKey('A')
    expect(tiles().length).toBeLessThanOrEqual(afterC)
    // La grille n'a jamais disparu : c'est toute la différence avec l'existant,
    // qui remplaçait l'écran par FormulaireClavier puis FormulaireProduitsClavier.
    expect(host.querySelector('.grid')).not.toBeNull()
  })

  it('n’offre que les 26 lettres, l’espace et le retour arrière', async () => {
    await open()
    host.querySelector<HTMLElement>('.search-key')?.click()
    flushSync()
    const keys = [...host.querySelectorAll('.keys button')].map((b) => b.textContent?.trim() ?? '')
    const letters = keys.filter((k) => /^[A-Z]$/u.test(k))
    expect(new Set(letters).size).toBe(26)
    expect(keys).toContain('Espace')
    expect(keys.filter((k) => /[ÀÂÄÉÈÊËÎÏÔÖÙÛÜÇ]/u.test(k))).toEqual([])
    expect(keys.filter((k) => /[;'"°*]/u.test(k))).toEqual([])
  })

  it('efface en refermant le panneau : fermer EST l’effacement', async () => {
    await open()
    host.querySelector<HTMLElement>('.search-key')?.click()
    flushSync()
    pressKey('C')
    pressKey('A')
    expect(tiles().length).toBeLessThan(331)

    const close = [...host.querySelectorAll<HTMLElement>('.panel button')].find(
      (b) => b.textContent?.trim() === 'Fermer',
    )
    close?.click()
    flushSync()
    expect(host.querySelector('.panel')).toBeNull()
    expect(tiles()).toHaveLength(331)
  })
})

describe('un toucher, une étiquette', () => {
  it('envoie un seul POST, avec la clé, le poids vu et le numéro de mesure', async () => {
    await open()
    tiles()[0]?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await vi.waitUntil(() => posted.length > 0)

    expect(posted).toHaveLength(1)
    expect(posted[0]?.route).toBe('/api/v1/weigh')
    expect(posted[0]?.body).toMatchObject({
      product_id: catalog.products[0]?.id,
      seen_weight_g: 1236,
      measurement_seq: 42,
      tare_g: 0,
      units: 1,
    })
    expect(String(posted[0]?.body.key)).toHaveLength(26)
  })

  it('désactive la tuile jusqu’à la réponse du flux : pas de double étiquette', async () => {
    await open()
    const tile = tiles()[0] as HTMLElement
    tile.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await vi.waitUntil(() => posted.length > 0)
    flushSync()

    tile.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await Promise.resolve()
    expect(posted).toHaveLength(1)

    // Le flux répond : la tuile redevient touchable.
    stream?.push(restingState({ revision: 2 }))
    flushSync()
    expect(tiles()[0]?.hasAttribute('disabled')).toBe(false)
  })
})

describe('ce qui occupe l’écran, et rien d’autre', () => {
  it('laisse la grille visible sur un refus, avec le liseré sur la tuile touchée', async () => {
    const id = catalog.products[3]?.id as string
    await open(
      restingState({
        state: 'rejected',
        product: selected({ id, name: 'AIL' }),
        message: {
          level: 'warn',
          code: 'WEIGHT_TOO_LIGHT',
          text: 'Poids trop faible.',
          expires_at: '',
        },
      }),
    )
    expect(tiles()).toHaveLength(331)
    expect(host.querySelector('[role="alertdialog"]')).toBeNull()
    expect(host.querySelector(`[data-product-id="${id}"]`)?.classList.contains('rejected')).toBe(
      true,
    )
    expect(host.querySelector('.instruction')?.textContent?.trim()).toBe('Poids trop faible.')
  })

  it('RELÂCHE la tuile quand l’étiquette est sortie', async () => {
    // L'anneau dit « ce produit est en cours », pas « ce produit a été vendu ».
    // Sans cette règle, la tuile restait verte jusqu'au retrait du sac — donc pour
    // toujours sur un poste sans balance, et sur tout poste dont le client s'en va.
    const id = catalog.products[3]?.id as string
    await open(
      restingState({
        state: 'printing',
        product: selected({ id, name: 'AIL' }),
      }),
    )
    expect(host.querySelector(`[data-product-id="${id}"]`)?.classList.contains('selected')).toBe(
      true,
    )

    stream?.push(
      restingState({
        revision: 2,
        state: 'succeeded',
        product: selected({ id, name: 'AIL' }),
        last_label: garlicLabel(),
        reprint: { available: true, job_id: '01J9F2ABC', printed_at: '2026-07-24T10:00:01.000Z' },
      }),
    )
    flushSync()
    expect(host.querySelector(`[data-product-id="${id}"]`)?.classList.contains('selected')).toBe(
      false,
    )
    // Ce qui accuse le succès, c'est le bandeau et la barre — et le papier.
    expect(host.querySelector('.summary')?.textContent).toContain('ail 1,236 kg')
  })

  it('prend tout l’écran sur Faulted, avec le code lisible au téléphone', async () => {
    await open(
      restingState({
        state: 'faulted',
        fault_code: 'ERR-PRN-01',
        message: {
          level: 'error',
          code: 'ERR-PRN-01',
          text: 'L’imprimante ne répond pas.',
          expires_at: '',
        },
      }),
    )
    const screen = host.querySelector('[role="alertdialog"]')
    expect(screen).not.toBeNull()
    expect(screen?.textContent).toContain('ERR-PRN-01')
  })

  it('masque le poids quand la mesure est périmée', async () => {
    await open(restingState({ weight: { ...restingState().weight, expired: true } }))
    expect(host.querySelector('.weight')?.textContent).toContain('—')
    expect(host.querySelector('.tare')?.textContent).toBe('Poids indisponible')
  })
})
