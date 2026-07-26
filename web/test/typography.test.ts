import { describe, expect, it } from 'vitest'
import {
  NAME_MAX_LINES,
  NAME_SIZE_MAX_PX,
  NAME_SIZE_MIN_PX,
  REFERENCE_SIZE_PX,
  fitNameSize,
  type Measurer,
} from '../src/lib/typography'
import { catalogFromExport } from './fixtures/odoo'

/**
 * Le nom de produit descend-il de corps au lieu d'être tronqué ?
 *
 * §14.2 fait du nom l'élément principal de la tuile — 49 % du catalogue réel n'a
 * pas de photo — et exige qu'il tienne « sans points de suspension ».
 *
 * Ce fichier porte aussi la démonstration de l'écart arithmétique de §14.2 :
 * « 34 px / 700 doit tenir [le nom de 69 caractères] dans une tuile de 230 px …
 * la tuile en autorise trois [lignes] » n'est vrai pour AUCUNE chasse moyenne
 * plausible. Le test le prouve par encadrement plutôt que par une valeur inventée.
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

describe('ÉCART DOCUMENTAIRE — 69 caractères ne tiennent pas en 3 lignes à 34 px', () => {
  it('déborde pour toute chasse moyenne entre 0,35 et 0,75 em', () => {
    for (let advanceEm = 0.35; advanceEm <= 0.75; advanceEm += 0.05) {
      const inkAt34 = LONGEST_REAL_NAME.length * advanceEm * NAME_SIZE_MAX_PX
      const roomOn3Lines = NAME_MAX_LINES * TILE_CONTENT_PX
      expect(inkAt34).toBeGreaterThan(roomOn3Lines)
    }
  })

  it('exigerait un corps de l’ordre de 15 px, soit sous le plancher de la §14.2', () => {
    // Corps maximal tenant 69 caractères sur 3 lignes de TILE_CONTENT_PX, pour une
    // chasse de 0,55 em : 3 × 239,4 / (69 × 0,55) ≈ 18,9 px — et 15,5 px à 0,67 em.
    const fitting = (advanceEm: number) =>
      (NAME_MAX_LINES * TILE_CONTENT_PX) / (LONGEST_REAL_NAME.length * advanceEm)
    expect(fitting(0.55)).toBeLessThan(NAME_SIZE_MAX_PX / 1.5)
    expect(fitting(0.67)).toBeLessThan(NAME_SIZE_MIN_PX)
  })

  it('choisit ALORS le plancher et laisse la tuile grandir, plutôt que de tronquer', () => {
    // « sans troncature » est l'exigence, « trois lignes » la préférence.
    const size = fitNameSize(LONGEST_REAL_NAME, TILE_CONTENT_PX, measurerAt(0.67))
    expect(size).toBe(NAME_SIZE_MIN_PX)
  })
})
