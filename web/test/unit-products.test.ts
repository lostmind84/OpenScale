import { describe, expect, it } from 'vitest'
import {
  ALL_CATEGORIES,
  chips,
  filterProducts,
  visibleProducts,
  type Catalog,
} from '../src/lib/catalog'
import { catalogFromExport } from './fixtures/odoo'

/**
 * Ce qu'un poste montre des produits vendus à l'unité, sur le VRAI fichier.
 *
 * Une tuile vendue à l'unité imprime une étiquette sans jamais lire la balance : c'est un
 * geste à part, que toute coopérative n'a pas à offrir au comptoir. Le réglage
 * `ui.show_by_unit_products` décide de sa présence dans la grille, et rien d'autre — le
 * poste sert toujours ses 331 tuiles, c'est l'écran qui en montre 316.
 *
 * Les quinze produits concernés sont ceux du fichier authentique `flv.csv`, préfixés
 * `0499`. Les chiffres de ce banc ne sont pas des vœux : ils sont mesurés sur ce fichier,
 * et `catalog-fixture.test.ts` verrouille déjà « 316 au poids, 15 à l'unité ».
 */

const catalog = catalogFromExport('flv.csv')

/** Les identifiants Odoo des quinze produits vendus à l'unité de `flv.csv`. */
const BY_UNIT_IDS = [
  '1541', '2310', '627', '645', '950', '1173', '1178', '1620',
  '1808', '1860', '1893', '2898', '2969', '3507', '4613',
]

/** Le même catalogue, vu par un poste qui montre ou masque les produits à l'unité. */
function seenWith(showByUnit: boolean): Catalog {
  return {
    ...catalog,
    presentation: { ...catalog.presentation, show_by_unit_products: showByUnit },
  }
}

describe('la grille d’un poste qui masque les produits vendus à l’unité', () => {
  it('descend de 331 à 316 tuiles, et n’en garde aucune vendue à l’unité', () => {
    const shown = visibleProducts(seenWith(false))

    expect(shown).toHaveLength(316)
    expect(shown.filter((p) => p.mode === 'by_unit')).toEqual([])
  })

  it('retire exactement les quinze produits vendus à l’unité du fichier réel', () => {
    const kept = new Set(visibleProducts(seenWith(false)).map((p) => p.id))
    const removed = catalog.products.filter((p) => !kept.has(p.id)).map((p) => p.id)

    expect(removed.slice().sort()).toEqual(BY_UNIT_IDS.slice().sort())
  })

  it('les rend toutes dès que le poste choisit de les montrer', () => {
    const shown = visibleProducts(seenWith(true))

    expect(shown).toHaveLength(331)
    expect(shown.filter((p) => p.mode === 'by_unit')).toHaveLength(15)
  })

  it('lit le PRÉFIXE du code-barres et jamais la colonne « unite » du CSV', () => {
    // « CAROTTE BOTTE SAF » est la seule unité divergente du fichier : son préfixe la
    // vend au poids, sa colonne `unite` la libelle « à l'unité ». Elle porte donc le
    // suffixe « € l'unité » sur sa tuile, et elle doit RESTER — sinon le filtre se
    // règlerait sur un libellé d'affichage, que la caisse ne lit jamais.
    const shown = visibleProducts(seenWith(false))
    const labelled = shown.filter((p) => p.price_suffix.includes('unit'))

    expect(labelled.map((p) => p.name)).toEqual(['CAROTTE BOTTE SAF'])
    expect(labelled[0]?.mode).toBe('by_weight')
  })
})

describe('la recherche ne rattrape pas un produit masqué', () => {
  // Chacun de ces cinq mots ne désigne, dans tout le fichier, qu'un produit vendu à
  // l'unité : le cas ne peut donc pas passer par accident sur un homonyme au poids.
  const words = ['melon', 'avocat', 'pasteque', 'mangue', 'menthe']

  it.each(words)('« %s » ne rend rien quand le poste les masque', (word) => {
    expect(filterProducts(visibleProducts(seenWith(false)), ALL_CATEGORIES, word)).toEqual([])
  })

  it.each(words)('« %s » rend sa tuile quand le poste les montre', (word) => {
    expect(filterProducts(visibleProducts(seenWith(true)), ALL_CATEGORIES, word)).toHaveLength(1)
  })
})

describe('les puces se recomptent sur ce que la grille montre', () => {
  it('garde ses quatre rayons, et leur somme fait le compte de « Tout »', () => {
    const bar = chips(seenWith(false))
    const counts = Object.fromEntries(bar.map((chip) => [chip.code, chip.count]))

    // Les quinze masqués se répartissent 8 légumes, 5 fruits, 2 vrac : aucun rayon ne
    // passe sous MIN_PRODUCTS_FOR_CHIP, donc aucune puce ne disparaît.
    expect(bar.map((chip) => chip.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'bulk',
      'other',
    ])
    expect(counts).toEqual({
      [ALL_CATEGORIES]: 316,
      fruits: 23,
      vegetables: 59,
      bulk: 108,
      other: 126,
    })
    const shelves = bar.filter((chip) => chip.code !== ALL_CATEGORIES)
    expect(shelves.reduce((total, chip) => total + chip.count, 0)).toBe(316)
  })
})
