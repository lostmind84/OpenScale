import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, describe, expect, it } from 'vitest'
import Grid from '../src/components/Grid.svelte'
import { ALL_CATEGORIES, filterProducts, visibleProducts, type Product } from '../src/lib/catalog'
import { AUTOMATIC_COLUMNS, GRID_COLUMNS_AUTO, gridTemplateColumns } from '../src/lib/grid'
import { NAME_SIZE_MAX_PX } from '../src/lib/typography'
import { catalogFromExport } from './fixtures/odoo'

/**
 * La grille rend-elle TOUT le catalogue, sans plafond nulle part ?
 *
 * C'est le défaut le plus coûteux de l'ancienne application : `FormulaireSquelette`
 * porte 120 contrôles `Image0…Image119` et sa boucle de remplissage cherche
 * `"Image" & i` dans un `Select Case` SANS branche par défaut. Passé i = 119, rien
 * n'est écrit, rien n'est journalisé, et la boucle continue jusqu'à EOF. Sur
 * `flv.csv`, la catégorie « Autres » compte 126 pesables pour 120 emplacements :
 * six produits vendables ne s'affichent sur aucune balance aujourd'hui, sans un
 * message ni une ligne de journal (§14.3-1).
 *
 * Ce test est ce qui interdit qu'un plafond revienne un jour par la fenêtre.
 */

const catalog = catalogFromExport('flv.csv')
const products = visibleProducts(catalog)

let host: HTMLElement | null = null
let component: Record<string, unknown> | null = null

afterEach(() => {
  if (component !== null) unmount(component)
  host?.remove()
  component = null
  host = null
})

/**
 * Monte la grille sur une liste de produits et rend les tuiles obtenues.
 *
 * @param extra - props additionnelles (ex. `primaryCode`, `tierAbbrev`), fusionnées
 * après les props par défaut sans changer les appels existants.
 */
function render(list: Product[], extra: Record<string, unknown> = {}): HTMLElement[] {
  host = document.createElement('div')
  document.body.appendChild(host)
  component = mount(Grid, { target: host, props: { products: list, onpick: () => {}, ...extra } })
  flushSync()
  return [...host.querySelectorAll<HTMLElement>('button[data-product-id]')]
}

/** L'élément qui porte la déclaration de grille. */
function grid(): HTMLElement {
  return host?.querySelector('.grid') as HTMLElement
}

/** Le scroller, qui porte le facteur d'échelle de CETTE grille. */
function scroller(): HTMLElement {
  return host?.querySelector('.grid-scroll') as HTMLElement
}

describe('la grille, sur le catalogue réel de testdata/catalog/flv.csv', () => {
  it('reçoit les 331 pesables et n’en garde pas 330', () => {
    expect(products).toHaveLength(331)
  })

  it('rend UNE tuile par produit, les 331 d’un seul tenant', () => {
    const tiles = render(products)
    expect(tiles).toHaveLength(331)
  })

  it('rend le 331ᵉ produit comme le premier — aucune tuile n’est perdue en queue', () => {
    const tiles = render(products)
    const rendered = new Set(tiles.map((t) => t.dataset.productId))
    const missing = products.filter((p) => !rendered.has(p.id))
    expect(missing).toEqual([])
  })

  it('rend les 126 pesables d’« Autres » — six de plus que les 120 emplacements', () => {
    // C'est l'assertion qui date le défaut : « Autres » a franchi 120 produits
    // quelque part entre le catalogue de 2022 (A = 1) et celui de 2026 (A = 140).
    const others = filterProducts(products, 'other', '')
    expect(others).toHaveLength(126)
    expect(render(others)).toHaveLength(126)
  })

  it('affiche le nom de 69 caractères en entier, sans points de suspension', () => {
    const longest = products.reduce((a, b) => (b.name.length > a.name.length ? b : a))
    expect(longest.name).toBe(
      '♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU PERIGORD-MV',
    )
    expect(longest.name).toHaveLength(69)
    const tile = render([longest])[0] as HTMLElement
    expect(tile.querySelector('.name')?.textContent).toBe(longest.name)
    expect(tile.textContent).not.toContain('…')
  })

  it('donne une initiale, et non un cadre gris, aux 154 tuiles sans photo', () => {
    // 154 et non 174 : le « (181 avec photo, 174 sans) » de §14.4 porte sur les
    // 355 lignes REÇUES et non sur les 331 tuiles — 181 + 174 = 355. Mesuré sur
    // le fichier : 177 tuiles illustrées, 154 sans (voir catalog-fixture.test.ts).
    const withoutPhoto = products.filter((p) => p.image_url === '')
    expect(withoutPhoto).toHaveLength(154)
    const tile = render([withoutPhoto[0] as Product])[0] as HTMLElement
    expect(tile.querySelector('img')).toBeNull()
    expect(tile.querySelector('.initial')?.textContent?.trim()).not.toBe('')
  })

  it('annonce son effectif dans le DOM, pour qu’un plafond se voie', () => {
    render(products)
    expect(host?.querySelector('.grid')?.getAttribute('data-tile-count')).toBe('331')
  })
})

describe('la recherche, filtre en place et sans plafond de résultats', () => {
  it('réduit la grille lettre après lettre, insensible aux accents', () => {
    const steps = ['c', 'ca', 'car', 'carotte'].map(
      (q) => filterProducts(products, ALL_CATEGORIES, q).length,
    )
    // Chaque lettre réduit ou laisse égal ; jamais elle n'augmente.
    expect(steps).toEqual([...steps].sort((a, b) => b - a))
    expect(steps.at(-1)).toBeGreaterThan(0)
  })

  it('trouve un produit dont le nom porte un cœur, tapé sans le cœur', () => {
    const found = filterProducts(products, ALL_CATEGORIES, 'lentilles vertes')
    expect(found.some((p) => p.name.includes('♥'))).toBe(true)
  })

  it('trouve un nom accentué tapé sans accent, au clavier réduit', () => {
    const accented = products.filter((p) => /[éèêëàâïôöùûüç]/iu.test(p.name))
    expect(accented.length).toBeGreaterThan(0)
    const target = accented[0] as Product
    const typed = target.search.split(' ')[0] as string
    expect(filterProducts(products, ALL_CATEGORIES, typed)).toContainEqual(target)
  })

  it('ne plafonne pas les résultats : une lettre courante en rend plus de 50', () => {
    // L'existant refusait au-delà de 50 résultats. Une seule lettre suffit à
    // franchir ce seuil sur le catalogue réel : le plafond serait visible tous les jours.
    const many = filterProducts(products, ALL_CATEGORIES, 'a')
    expect(many.length).toBeGreaterThan(50)
    expect(render(many)).toHaveLength(many.length)
  })
})

describe('le nombre de colonnes, réglable et automatique par défaut', () => {
  const source = readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), '../src/components/Grid.svelte'),
    'utf8',
  )
  const few = products.slice(0, 12)

  it('à 0, la feuille de style porte MOT POUR MOT la déclaration d’aujourd’hui', () => {
    expect(source).toContain(
      'grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));',
    )
  })

  // La grille est dessinée par CETTE feuille de style, et mesurée par une sonde que
  // l'écran d'administration déclare de son côté (Catalog.svelte). Les deux doivent
  // dire la même chose : une sonde qui déclarerait autre chose compterait les colonnes
  // d'une grille que personne ne voit. `lib/grid.ts` est la seule source des deux, et
  // ce cas est ce qui rattache la feuille de style — que le CSS scopé de Svelte
  // interdit d'alimenter depuis un module — à ce qu'elle porte.
  it('la feuille de style dit ce que le module dit, sinon la sonde mesure autre chose', () => {
    expect(source).toContain(`grid-template-columns: ${AUTOMATIC_COLUMNS};`)
    expect(gridTemplateColumns(GRID_COLUMNS_AUTO)).toBe(AUTOMATIC_COLUMNS)
  })

  it('à 0, ne pose AUCUNE déclaration en ligne : la feuille de style reste seule à décider', () => {
    render(few, { gridColumns: 0 })
    expect(grid().getAttribute('style')).toBeNull()
  })

  it('sans le réglage du tout, se comporte comme à 0 — une configuration d’avant passe', () => {
    render(few)
    expect(grid().getAttribute('style')).toBeNull()
  })

  // Le point-virgule final est celui que Svelte ajoute en normalisant l'attribut :
  // c'est le DOM qui est vérifié ici, pas la chaîne que le composant a écrite.
  it.each([3, 7, 12])('à %i, déclare repeat(N, minmax(0, 1fr))', (columns) => {
    render(few, { gridColumns: columns })
    expect(grid().getAttribute('style')).toBe(
      `grid-template-columns: repeat(${columns}, minmax(0, 1fr));`,
    )
  })

  it('n’écrit JAMAIS 1fr seul, qui vaut minmax(auto, 1fr)', () => {
    // Une piste `auto` ne descend pas sous la largeur min-content de son contenu,
    // et le contenu contient « CRANBERRY/CANNEBERGES » : à 10 colonnes, `1fr`
    // donnerait une grille plus large que l'écran. Même piège que Tile.svelte:132.
    render(few, { gridColumns: 10 })
    expect(grid().getAttribute('style')).not.toMatch(/repeat\(\d+,\s*1fr\)/u)
  })

  it('donne au squelette de chargement la grille du poste, et non celle d’un autre', () => {
    render([], { gridColumns: 6, loading: true })
    expect(grid().getAttribute('style')).toBe('grid-template-columns: repeat(6, minmax(0, 1fr));')
  })
})

describe('le repli sans mise en page, qui est ce que jsdom permet de vérifier', () => {
  // jsdom ne fait AUCUNE mise en page : ni `clamp()`, ni `auto-fill`, ni la
  // largeur d'une sonde. Ce qui se teste ici est donc le repli — pas de sonde,
  // facteur 1 — exactement comme l'absence de `document.fonts`. Les nombres, eux,
  // se mesurent au navigateur.
  const few = products.slice(0, 12)

  it('reste au facteur 1 : une sonde à 0 ne donne pas un facteur', () => {
    render(few, { gridColumns: 7 })
    expect(scroller().getAttribute('style')).toContain('--tile-scale: 1')
  })

  it('n’écrit ni NaN ni Infinity, qui invalideraient les quatre jetons d’un coup', () => {
    render(few, { gridColumns: 7 })
    expect(scroller().getAttribute('style') ?? '').not.toMatch(/NaN|Infinity/u)
  })

  it('porte le facteur sur SON scroller, jamais sur la racine du document', () => {
    // L'administration s'ouvre dans la même fenêtre et pose le sien pour mesurer
    // un brouillon : deux échelles à la fois, chacune sur son sous-arbre.
    render(few, { gridColumns: 7 })
    expect(scroller().hasAttribute('data-tile-scale')).toBe(true)
    expect(document.documentElement.style.getPropertyValue('--tile-scale')).toBe('')
  })

  it('laisse les noms à leur corps nominal, comme sans canvas', () => {
    const tile = render(few, { gridColumns: 7 })[0] as HTMLElement
    expect(tile.querySelector<HTMLElement>('.name')?.style.fontSize).toBe(`${NAME_SIZE_MAX_PX}px`)
  })
})

describe('le double tarif de la tuile, empilé primaire d’abord (ADR-036)', () => {
  const primaryCode = 'A'
  const tierAbbrev: Record<string, string> = { A: 'A', S: 'S' }

  it('rend un tarif par entrée de product.prices, dans l’ordre reçu — jamais recalculé', () => {
    const product = products[0] as Product
    const tile = render([product], { primaryCode, tierAbbrev })[0] as HTMLElement
    const prices = [...tile.querySelectorAll<HTMLElement>('.price')]
    expect(prices).toHaveLength(product.prices.length)
    prices.forEach((price, i) => {
      expect(price.querySelector('.amount')?.textContent).toBe(product.prices[i]?.text)
    })
  })

  it('seul le tarif dont le code correspond à primaryCode porte le badge plein ; les autres, l’anneau creux', () => {
    const product = products[0] as Product
    const tile = render([product], { primaryCode, tierAbbrev })[0] as HTMLElement
    const prices = [...tile.querySelectorAll<HTMLElement>('.price')]
    prices.forEach((price, i) => {
      const isPrimary = product.prices[i]?.code === primaryCode
      expect(price.classList.contains('secondary')).toBe(!isPrimary)
      expect(price.querySelector('.abbrev')?.classList.contains('hollow')).toBe(!isPrimary)
    })
  })

  it('l’abréviation affichée vient de tierAbbrev, jamais du code brut du palier', () => {
    const product = products[0] as Product
    const tile = render([product], { primaryCode, tierAbbrev })[0] as HTMLElement
    const abbrevs = [...tile.querySelectorAll<HTMLElement>('.abbrev')].map((el) =>
      el.textContent?.trim(),
    )
    expect(abbrevs).toEqual(product.prices.map((p) => tierAbbrev[p.code]))
  })
})
