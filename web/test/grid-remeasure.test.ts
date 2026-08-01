import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Grid from '../src/components/Grid.svelte'
import { visibleProducts, type Product } from '../src/lib/catalog'
import { catalogFromExport } from './fixtures/odoo'

/**
 * Quand le bloc de nom est-il RELU ? — et rien d'autre.
 *
 * jsdom ne met rien en page. Ce banc n'en attend donc aucune largeur : il en
 * INJECTE, élément par élément, et n'affirme qu'une seule chose — que la mesure
 * du bloc de nom est refaite quand ce qui la change a changé. Aucun corps de
 * caractère, aucune largeur de colonne, aucun nombre de rangées n'est vérifié
 * ici ; ces nombres-là se mesurent au navigateur et nulle part ailleurs.
 *
 * Ce qu'il tient est un défaut mesuré le 01/08/2026 : l'effet de relecture ne
 * dépendait que de la largeur de la grille, or le facteur d'échelle change
 * `--tile-pad` et donc la largeur utile SANS que la colonne change de taille.
 * Les noms étaient ajustés contre une tuile non mise à l'échelle puis posés dans
 * une tuile qui l'était — « SUCRE RAPADURA COMPLET - » demandant 330,03 px d'un
 * bloc de 328,35, et à 3 colonnes sur 1920, 63 rangées sur 111 qui grandissent.
 *
 * Ce fichier est séparé de `grid.test.ts` À DESSEIN : Svelte ne construit son
 * `ResizeObserver` qu'une fois, au premier montage, et le garde pour la durée du
 * module. Le remplacer par un observateur pilotable exige donc d'être le premier
 * à monter une grille.
 */

const products = visibleProducts(catalogFromExport('flv.csv')).slice(0, 24) as Product[]

/** Les largeurs et hauteurs injectées, par élément. */
const widths = new WeakMap<Element, number>()
const heights = new WeakMap<Element, number>()

/** Combien de fois le bloc de nom a été relu. C'est la seule grandeur observée. */
let nameBoxReads = 0

/**
 * Les éléments que Svelte observe, et de quoi leur signaler une mise en page.
 *
 * `notify` vit pour tout le fichier et n'est JAMAIS remis à null entre deux cas :
 * Svelte construit son `ResizeObserver` une seule fois, au premier `observe()`
 * du module, et le garde ensuite. Le remettre à null rendait les cas suivants
 * muets — et vrais par accident, ce qui est pire que faux.
 */
const observed = new Set<Element>()
let notify: ((entries: { target: Element }[]) => void) | null = null

/** Un `ResizeObserver` qui n'observe rien et se déclenche sur commande. */
class DrivableResizeObserver {
  constructor(callback: (entries: { target: Element }[]) => void) {
    notify = callback
  }

  observe(element: Element): void {
    observed.add(element)
  }

  unobserve(element: Element): void {
    observed.delete(element)
  }

  disconnect(): void {
    observed.clear()
  }
}

const realWidth = Object.getOwnPropertyDescriptor(Element.prototype, 'clientWidth')
const realHeight = Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight')

let host: HTMLElement | null = null
let component: Record<string, unknown> | null = null

beforeEach(() => {
  nameBoxReads = 0
  observed.clear()
  Object.defineProperty(Element.prototype, 'clientWidth', {
    configurable: true,
    get(this: Element): number {
      if (this.classList.contains('name-box')) nameBoxReads += 1
      return widths.get(this) ?? 0
    },
  })
  Object.defineProperty(Element.prototype, 'clientHeight', {
    configurable: true,
    get(this: Element): number {
      return heights.get(this) ?? 0
    },
  })
  vi.stubGlobal('ResizeObserver', DrivableResizeObserver)
})

afterEach(() => {
  if (component !== null) unmount(component)
  host?.remove()
  component = null
  host = null
  if (realWidth !== undefined) Object.defineProperty(Element.prototype, 'clientWidth', realWidth)
  if (realHeight !== undefined) Object.defineProperty(Element.prototype, 'clientHeight', realHeight)
  vi.unstubAllGlobals()
})

/** Monte la grille pour un réglage de colonnes. */
function render(gridColumns: number): HTMLElement {
  host = document.createElement('div')
  document.body.appendChild(host)
  component = mount(Grid, { target: host, props: { products, gridColumns, onpick: () => {} } })
  flushSync()
  return host
}

/** Donne à un élément la largeur qu'un navigateur lui aurait donnée. */
function widen(selector: string, px: number): void {
  const element = host?.querySelector(selector)
  if (element !== null && element !== undefined) widths.set(element, px)
}

/** Rejoue un cycle de mise en page : chaque élément observé relit sa taille. */
function relayout(): void {
  notify?.([...observed].map((target) => ({ target })))
  flushSync()
}

describe('la relecture du bloc de nom, et ce qui la déclenche', () => {
  it('relit quand le FACTEUR change, la colonne gardant exactement sa largeur', () => {
    // Le cas du défaut, et il est propre : à nombre de colonnes fixé, la largeur
    // d'une colonne ne dépend pas de `--tile-min`. La sonde qui répond fait donc
    // passer le facteur de 1 à sa vraie valeur SANS toucher à la colonne — et
    // c'est précisément là que la largeur utile bouge, par le padding.
    render(7)
    widen('.grid', 1904)
    relayout()

    const before = nameBoxReads
    widen('.min-probe', 352)
    relayout()

    expect(nameBoxReads).toBeGreaterThan(before)
  })

  it('relit quand la largeur de la grille change, le facteur restant le même', () => {
    render(7)
    widen('.grid', 1904)
    relayout()

    const before = nameBoxReads
    widen('.grid', 1366)
    relayout()

    expect(nameBoxReads).toBeGreaterThan(before)
  })

  it('relit aussi en mode automatique, où le facteur ne bouge jamais de 1', () => {
    render(0)
    widen('.grid', 1904)
    widen('.min-probe', 352)
    relayout()

    const before = nameBoxReads
    widen('.grid', 3840)
    relayout()

    expect(nameBoxReads).toBeGreaterThan(before)
  })

  it('ne relit pas quand rien de ce dont elle dépend n’a bougé', () => {
    // Sans quoi le cas ci-dessus serait vrai par accident : un effet qui se
    // rejoue à chaque cycle passerait les trois autres sans rien garantir.
    render(7)
    widen('.grid', 1904)
    widen('.min-probe', 352)
    relayout()

    const before = nameBoxReads
    relayout()

    expect(nameBoxReads).toBe(before)
  })
})
