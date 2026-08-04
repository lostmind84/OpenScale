import { describe, expect, it } from 'vitest'
import {
  ALL_CATEGORIES,
  MIN_PRODUCTS_FOR_CHIP,
  chips,
  filterProducts,
  visibleProducts,
  type Catalog,
  type Presentation,
} from '../src/lib/catalog'
import { catalogFromExport, qualifyExport, readExport } from './fixtures/odoo'

/**
 * Les catégories sont-elles des FILTRES, et une catégorie creuse disparaît-elle
 * de la barre sans que ses produits disparaissent avec elle ?
 *
 * L'ancienne application posait quatre boutons en dur parce que quatre
 * formulaires étaient préconstruits au démarrage. La répartition réelle interdit
 * cette parité et s'inverse d'un export à l'autre : en 2022 « Autres » menait à UN
 * SEUL produit — un quart de barre pour une tuile —, en 2026 c'est la catégorie la
 * plus peuplée (§14.3-2, ADR-024).
 */

describe('flv.csv, 2026 — la barre suit la donnée', () => {
  const catalog = catalogFromExport('flv.csv')
  const bar = chips(catalog)

  it('ouvre sur « Tout », qui porte les 331 pesables', () => {
    expect(bar[0]?.code).toBe(ALL_CATEGORIES)
    expect(bar[0]?.label).toBe('Tout')
    expect(bar[0]?.count).toBe(331)
  })

  it('écrit l’effectif PESABLE sur la puce, jamais le nombre de lignes reçues', () => {
    // « Autres » reçoit 140 lignes et n'a que 126 tuiles : afficher 140 promettrait
    // au client quatorze produits qu'il ne trouvera pas.
    const rows = qualifyExport(readExport('flv.csv')).filter((r) => r.letter === 'A')
    expect(rows).toHaveLength(140)
    expect(bar.find((c) => c.code === 'other')?.count).toBe(126)
  })

  it('classe les puces par le rang de la CONFIGURATION, pas par leur effectif', () => {
    // Trié par effectif, « Autres » (126) passerait devant « Fruits » (28). Le rang
    // configuré gagne : c'est ce que l'inversion 2022 → 2026 rend nécessaire.
    expect(bar.map((c) => c.code)).toEqual([ALL_CATEGORIES, 'fruits', 'vegetables', 'bulk', 'other'])
    expect(bar.map((c) => c.count)).toEqual([331, 28, 67, 110, 126])
  })

  it('somme exactement : les quatre puces rendent les 331 de « Tout »', () => {
    const sum = bar.slice(1).reduce((total, c) => total + c.count, 0)
    expect(sum).toBe(bar[0]?.count)
  })

  it('s’accorde avec le `product_count` que le serveur a compté de son côté', () => {
    // La puce affiche un effectif recompté sur les tuiles servies ; le serveur en
    // envoie un, calculé indépendamment. S'ils divergent, quelqu'un ment.
    for (const chip of bar.slice(1)) {
      const served = catalog.categories.find((c) => c.code === chip.code)
      expect(chip.count).toBe(served?.product_count)
    }
  })
})

describe('flv_1.csv, 2022 — « Autres » ne compte qu’un seul produit', () => {
  const catalog = catalogFromExport('flv_1.csv')
  const products = visibleProducts(catalog)
  const bar = chips(catalog)

  it('mesure bien une catégorie sous le seuil que sert ce poste', () => {
    const others = filterProducts(products, 'other', '')
    expect(others).toHaveLength(1)
    expect(others.length).toBeLessThan(MIN_PRODUCTS_FOR_CHIP)
  })

  it('ne lui donne AUCUNE puce', () => {
    expect(bar.map((c) => c.code)).not.toContain('other')
    expect(bar.map((c) => c.code)).toEqual([ALL_CATEGORIES, 'fruits', 'vegetables', 'bulk'])
  })

  it('garde pourtant son produit dans « Tout »', () => {
    const orphan = filterProducts(products, 'other', '')[0]
    expect(orphan).toBeDefined()
    expect(filterProducts(products, ALL_CATEGORIES, '')).toContainEqual(orphan)
    expect(bar[0]?.count).toBe(107)
  })

  it('le laisse trouvable à la recherche, ce qui est la seule chose qui compte', () => {
    const orphan = filterProducts(products, 'other', '')[0]
    expect(orphan).toBeDefined()
    const typed = (orphan?.search.split(' ')[0] ?? '') as string
    expect(typed).not.toBe('')
    expect(filterProducts(products, ALL_CATEGORIES, typed)).toContainEqual(orphan)
  })

  it('renverse la barre de 2026 : ici « Légumes » mène, « Autres » a disparu', () => {
    expect(bar.map((c) => `${c.code}:${c.count}`)).toEqual([
      ':107',
      'fruits:10',
      'vegetables:84',
      'bulk:12',
    ])
  })
})

describe('le seuil et le masquage sont deux mécanismes distincts', () => {
  const catalog = catalogFromExport('flv.csv')

  it('un produit sous le seuil servi pas de puce, un produit au seuil une puce', () => {
    const bulk = filterProducts(visibleProducts(catalog), 'bulk', '')
    const justUnder: Catalog = { ...catalog, products: bulk.slice(0, MIN_PRODUCTS_FOR_CHIP - 1) }
    const justAt: Catalog = { ...catalog, products: bulk.slice(0, MIN_PRODUCTS_FOR_CHIP) }
    expect(chips(justUnder).map((c) => c.code)).toEqual([ALL_CATEGORIES])
    expect(chips(justAt).map((c) => c.code)).toEqual([ALL_CATEGORIES, 'bulk'])
  })

  it('une catégorie MASQUÉE sur ce poste retire aussi ses produits de « Tout »', () => {
    // Le poste « fruits » n'a pas à montrer le vrac : c'est une vraie décision de
    // magasin, et elle ne se confond pas avec « trop peu de produits pour une puce ».
    const hidden: Catalog = {
      ...catalog,
      categories: catalog.categories.map((c) => (c.code === 'bulk' ? { ...c, visible: false } : c)),
    }
    const shown = visibleProducts(hidden)
    expect(shown.some((p) => p.category_code === 'bulk')).toBe(false)
    expect(shown).toHaveLength(331 - 110)
    expect(chips(hidden).map((c) => c.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'other',
    ])
    expect(chips(hidden)[0]?.count).toBe(221)
  })
})

describe('le seuil vient de la configuration du poste', () => {
  const catalog = catalogFromExport('flv.csv')

  it('à 5 — le défaut livré — les quatre rayons ont leur puce', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 5 },
    }
    expect(chips(served).map((c) => c.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'bulk',
      'other',
    ])
  })

  it('à 70, Fruits (28) et Légumes (67) perdent la leur, Vrac (110) et Autres (126) la gardent', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 70 },
    }
    expect(chips(served).map((c) => c.code)).toEqual([ALL_CATEGORIES, 'bulk', 'other'])
  })

  it('ne retire aucun produit de « Tout » en retirant une puce', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 70 },
    }
    // 331 pesables, et le poste de référence ne masque rien : le compte de « Tout » ne
    // bouge pas d'un produit quand deux puces disparaissent.
    expect(chips(served)[0]?.count).toBe(331)
    expect(visibleProducts(served)).toHaveLength(331)
  })

  it('retombe sur le défaut quand le poste ne sert pas la clé', () => {
    // Un binaire plus ancien que ce réglage : la barre reste celle d'aujourd'hui.
    const older: Partial<Presentation> = { ...catalog.presentation }
    delete older.min_products_for_chip
    const served = { ...catalog, presentation: older } as Catalog
    expect(chips(served).map((c) => c.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'bulk',
      'other',
    ])
  })
})
