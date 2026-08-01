import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Catalog } from '../src/lib/catalog'
import type { StateDTO } from '../src/lib/dto'
import { Session } from '../src/lib/session.svelte'
import { catalogFromExport } from './fixtures/odoo'

/**
 * Ce qui décide qu'un écran client redemande son catalogue.
 *
 * Deux choses le décident, et une seule était branchée. Le nombre de tuiles bouge
 * quand un import passe ; les réglages d'écran, eux, bougent quand un exploitant
 * enregistre une page d'administration — et jusqu'ici l'écran d'à côté n'en savait
 * rien. « On change le réglage, on enregistre, et rien ne se passe » est exactement
 * la conclusion contre laquelle le contrôle 46 d'ADR-031 avait été écrit.
 *
 * L'empreinte est OPAQUE : ce banc ne lui donne jamais de sens, il ne fait que la
 * faire changer. C'est aussi tout ce que le navigateur en fait.
 */

const catalog = catalogFromExport('flv.csv')

/** Le catalogue que la route sert, remplaçable par un cas. */
let served: Catalog = catalog

/** Combien de fois `GET /api/v1/catalog` a été demandé depuis le début du cas. */
let catalogRequests = 0

/** Le dernier flux ouvert, pour lui pousser des états à la main. */
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

  addEventListener(name: string, listener: (e: MessageEvent<string>) => void): void {
    if (name === 'state') this.#listeners.push(listener)
  }

  close(): void {
    this.closed = true
  }

  push(state: StateDTO): void {
    this.onopen?.()
    const event = new MessageEvent('state', { data: JSON.stringify(state) })
    for (const listener of this.#listeners) listener(event)
  }
}

/**
 * Un état réduit aux deux champs dont dépend la relecture.
 *
 * `#receive` ne lit que ceux-là ; un DTO complet ferait croire que les autres
 * pèsent sur la décision, et c'est précisément ce qu'on vérifie ici.
 */
function stateWith(catalogCount: number, digest: string): StateDTO {
  return { catalog_count: catalogCount, presentation_digest: digest } as StateDTO
}

let session: Session | null = null

/** Ouvre une session et attend que son premier catalogue soit arrivé. */
async function opened(): Promise<Session> {
  const started = new Session()
  session = started
  started.start()
  await vi.waitUntil(() => started.catalog !== null)
  return started
}

/** Laisse partir tout ce que la boucle d'événements a en attente. */
function settle(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0))
}

/**
 * Pousse un état, compte ce qui a été demandé, puis laisse la relecture finir.
 *
 * L'assertion est IMMÉDIATE, et c'est ce qui lui donne sa valeur : `#receive`
 * appelle `fetch` de façon synchrone, donc « aucune requête » se constate sans
 * attendre — là où un délai de complaisance rendrait ce cas vrai par hasard.
 */
async function push(state: StateDTO, expected: number): Promise<void> {
  stream?.push(state)
  expect(catalogRequests).toBe(expected)
  await settle()
}

beforeEach(() => {
  served = catalog
  stream = null
  catalogRequests = 0
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', async (input: string) => {
    if (String(input).startsWith('/api/v1/catalog')) catalogRequests++
    return new Response(JSON.stringify(served), { status: 200 })
  })
})

afterEach(() => {
  session?.stop()
  session = null
  vi.unstubAllGlobals()
})

describe('la relecture du catalogue sur changement de présentation', () => {
  it('ne redemande rien quand ni le compte ni l’empreinte ne bougent', async () => {
    const open = await opened()
    await push(stateWith(open.catalog?.product_count ?? 0, 'p1'), 1)
    await push(stateWith(open.catalog?.product_count ?? 0, 'p1'), 1)
  })

  it('redemande le catalogue dès que l’empreinte de présentation change', async () => {
    const open = await opened()
    const count = open.catalog?.product_count ?? 0
    await push(stateWith(count, 'p1'), 1)
    await push(stateWith(count, 'p2'), 2)
  })

  it('le redemande sur un changement d’empreinte À COMPTE ÉGAL', async () => {
    // Le cas qui manquait : un réglage d'affichage ne touche pas au nombre de
    // produits pesables, donc l'ancienne condition ne s'en apercevait jamais.
    const open = await opened()
    const count = open.catalog?.product_count ?? 0
    await push(stateWith(count, 'p1'), 1)
    served = {
      ...catalog,
      presentation: { ...catalog.presentation, grid_columns: 7 },
    }
    await push(stateWith(count, 'p2'), 2)
    expect(open.presentation.grid_columns).toBe(7)
  })

  it('le redemande encore quand le nombre de tuiles bouge, empreinte inchangée', async () => {
    const open = await opened()
    await push(stateWith((open.catalog?.product_count ?? 0) - 1, 'p1'), 2)
  })

  it('ne redemande rien sur le PREMIER état : le catalogue vient d’être chargé', async () => {
    // Aucun état précédent à comparer, et aucune comparaison à faire : `start()`
    // a déjà chargé la présentation du poste une milliseconde plus tôt.
    const open = await opened()
    await push(stateWith(open.catalog?.product_count ?? 0, 'peu-importe'), 1)
  })
})

describe('les réglages d’écran par défaut, tant qu’aucun catalogue n’est arrivé', () => {
  it('laisse la grille en automatique plutôt que de figer un nombre de colonnes', () => {
    expect(new Session().presentation.grid_columns).toBe(0)
  })
})
