import { describe, expect, it } from 'vitest'
import {
  NAME_BOX_PX,
  NAME_LINE_HEIGHT,
  NAME_SIZE_MAX_PX,
  NAME_SIZE_MIN_PX,
  REFERENCE_SIZE_PX,
  fitNameSize,
  linesAvailable,
  type Measurer,
} from '../src/lib/typography'
import { catalogFromExport } from './fixtures/odoo'

/**
 * Le nom de produit descend-il de corps au lieu d'être tronqué, ET la tuile
 * garde-t-elle sa hauteur ?
 *
 * §14.2 fait du nom l'élément principal de la tuile — 49 % du catalogue réel n'a
 * pas de photo — et exige qu'il tienne « sans points de suspension ». ADR-030
 * ajoute la seconde moitié : ce qui est tenu constant est le BLOC, pas le nombre
 * de lignes, sans quoi 331 produits donnent 331 hauteurs de tuile.
 */

/**
 * Un mesureur paramétré par une chasse moyenne, en em.
 *
 * On ne prétend pas connaître la chasse d'Inter : on balaie un intervalle qui
 * l'encadre très largement — 0,35 em est plus étroit que n'importe quelle
 * humanistique, 0,75 em plus large.
 */
function measurerAt(advanceEm: number): Measurer {
  return (text: string) => text.length * advanceEm * REFERENCE_SIZE_PX
}

/**
 * Largeur utile d'une tuile sur l'écran de référence.
 *
 * 1920 px, 16 px de marge de chaque côté, `repeat(auto-fill, minmax(230px, 1fr))`
 * et 8 px de gouttière donnent 7 colonnes (§14.3-1), donc (1888 − 6 × 8) / 7 par
 * colonne, moins 24 px de padding intérieur.
 */
const TILE_CONTENT_PX = (1888 - 6 * 8) / 7 - 24

const LONGEST_REAL_NAME =
  '♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU PERIGORD-MV'

/**
 * Le retour à la ligne du navigateur, écrit ici comme SPÉCIFICATION.
 *
 * `overflow-wrap: break-word` : un fragment qui tiendrait sur une ligne à lui
 * seul descend entier, un fragment plus large que la tuile est coupé. Un mot se
 * coupe aussi APRÈS un trait d'union — « Arc-en-Ciel » — mais jamais après une
 * barre oblique : mesuré dans le navigateur, « abc-def » prend deux lignes là où
 * « abc/def » n'en prend qu'une et déborde. C'est cette règle-là que
 * `fitNameSize` doit reproduire, et la recopier ici est ce qui rend l'accord
 * entre les deux vérifiable au lieu d'être supposé.
 */
function linesUsed(name: string, sizePx: number, widthPx: number, measure: Measurer): number {
  const scale = sizePx / REFERENCE_SIZE_PX
  const space = (measure('a a') - measure('aa')) * scale
  let lines = 1
  let used = 0
  for (const word of name.split(/\s+/u).filter((w) => w.length > 0)) {
    const parts = word.split(/(?<=[-‐–—])(?!\d)/u).filter((p) => p.length > 0)
    for (const [index, part] of parts.entries()) {
      const width = measure(part) * scale
      const glue = index === 0 ? space : 0
      if (used > 0 && used + glue + width <= widthPx) {
        used += glue + width
        continue
      }
      if (used > 0) {
        lines++
        used = 0
      }
      if (width <= widthPx) {
        used = width
        continue
      }
      // Le navigateur coupe ENTRE DEUX GLYPHES : chaque ligne sauf la dernière
      // s'arrête donc jusqu'à un caractère avant le bord, et rien d'autre ne
      // rentre derrière le fragment coupé.
      const chars = [...part].length
      const perLine = Math.max(1, Math.floor((widthPx * chars) / width))
      lines += Math.ceil(chars / perLine) - 1
      used = widthPx
    }
  }
  return lines
}

describe('linesAvailable — un corps plus petit achète des lignes dans le même bloc', () => {
  it('donne 2 lignes au corps nominal et 4 au plancher, dans 88 px', () => {
    expect(linesAvailable(NAME_SIZE_MAX_PX, NAME_BOX_PX)).toBe(2)
    expect(linesAvailable(NAME_SIZE_MIN_PX, NAME_BOX_PX)).toBe(4)
  })

  it('rend toujours au moins une ligne, même sur un bloc absurde', () => {
    expect(linesAvailable(NAME_SIZE_MAX_PX, 0)).toBe(1)
  })
})

describe('fitNameSize', () => {
  const measure = measurerAt(0.55)

  it('laisse un nom court à son corps nominal de 34 px', () => {
    expect(fitNameSize('AIL', TILE_CONTENT_PX, measure)).toBe(NAME_SIZE_MAX_PX)
  })

  it('ne descend jamais sous le plancher de 18 px, le plus petit corps de §14.2', () => {
    expect(fitNameSize('X'.repeat(500), TILE_CONTENT_PX, measure)).toBe(NAME_SIZE_MIN_PX)
  })

  it('décroît quand le nom s’allonge, sans jamais remonter', () => {
    const sizes = [4, 12, 27, 40, 69].map((n) =>
      fitNameSize('A'.repeat(n), TILE_CONTENT_PX, measure),
    )
    expect(sizes).toEqual([...sizes].sort((a, b) => b - a))
  })

  it('reste borné entre le plancher et le nominal sur les 331 noms réels', () => {
    const products = catalogFromExport('flv.csv').products
    for (const p of products) {
      const size = fitNameSize(p.name, TILE_CONTENT_PX, measure)
      expect(size).toBeGreaterThanOrEqual(NAME_SIZE_MIN_PX)
      expect(size).toBeLessThanOrEqual(NAME_SIZE_MAX_PX)
    }
  })

  it('rend le nominal quand aucune mesure n’est disponible, plutôt qu’une estimation', () => {
    expect(fitNameSize(LONGEST_REAL_NAME, 0, measure)).toBe(NAME_SIZE_MAX_PX)
  })
})

describe('ADR-030 — les 331 tuiles ont la même hauteur', () => {
  const measure = measurerAt(0.55)

  it('fait tenir CHACUN des 331 noms réels dans le bloc de 88 px', () => {
    const products = catalogFromExport('flv.csv').products
    const overflowing = products.filter((p) => {
      const size = fitNameSize(p.name, TILE_CONTENT_PX, measure)
      return linesUsed(p.name, size, TILE_CONTENT_PX, measure) > linesAvailable(size, NAME_BOX_PX)
    })
    // Aucun : c'est ce qui fait que la grille dessine UN rythme et non 331.
    expect(overflowing.map((p) => p.name)).toEqual([])
  })

  it('n’occupe jamais plus que le bloc, corps et interligne compris', () => {
    const products = catalogFromExport('flv.csv').products
    for (const p of products) {
      const size = fitNameSize(p.name, TILE_CONTENT_PX, measure)
      const used = linesUsed(p.name, size, TILE_CONTENT_PX, measure) * size * NAME_LINE_HEIGHT
      expect(used).toBeLessThanOrEqual(NAME_BOX_PX)
    }
  })

  it('donne au nom de 69 caractères un corps intermédiaire, ni nominal ni plancher', () => {
    // C'est le gain de l'ADR : à « au plus trois lignes », ce nom tombait au
    // plancher de 18 px ; dans un bloc de hauteur fixe il achète une quatrième
    // ligne en ne cédant que quelques points de corps.
    const size = fitNameSize(LONGEST_REAL_NAME, TILE_CONTENT_PX, measure)
    expect(size).toBeGreaterThan(NAME_SIZE_MIN_PX)
    expect(size).toBeLessThan(NAME_SIZE_MAX_PX)
  })

  it('choisit le plancher plutôt que de tronquer, quand rien ne tient', () => {
    // « sans troncature » est l'exigence : la tuile — et avec elle toute sa
    // rangée — grandit, ce que `grid-auto-rows: minmax(hauteur, auto)` permet.
    expect(fitNameSize('X'.repeat(500), TILE_CONTENT_PX, measurerAt(0.67))).toBe(NAME_SIZE_MIN_PX)
  })
})
