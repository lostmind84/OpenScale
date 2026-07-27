import { describe, expect, it } from 'vitest'
import { readable, wash } from '../src/lib/color'
import { LACAGETTE_CATEGORIES } from './fixtures/odoo'

/**
 * Une couleur écrite à la main dans un fichier de configuration se comporte-t-elle
 * à l'écran ?
 *
 * `categories[].color` vient de la configuration du poste et de nulle part
 * ailleurs (§14.3-2) : quelqu'un choisit quatre valeurs hexadécimales dans un
 * JSON, sans aperçu, des mois avant que la tuile existe. L'ocre du fichier livré,
 * `#B7950B`, plafonne à 2,7:1 sur blanc — une initiale de catégorie dessinée
 * telle quelle est illisible à 80 cm, et aucun soin apporté au CSS ne peut la
 * rattraper puisque le CSS ne voit jamais la valeur.
 */

/** Composante linéarisée d'un canal sRGB, formule WCAG 2.1. */
function channel(value: number): number {
  const c = value / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

/** Contraste d'une couleur `rgb(r, g, b)` contre le blanc. */
function contrastOnWhite(rgb: string): number {
  const [r, g, b] = [...rgb.matchAll(/\d+/gu)].map((m) => Number(m[0])) as [number, number, number]
  return 1.05 / (0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b) + 0.05)
}

describe('readable — la couleur d’un rayon, assombrie jusqu’à être lisible', () => {
  it('rend les quatre couleurs du fichier livré lisibles, l’ocre compris', () => {
    for (const category of LACAGETTE_CATEGORIES) {
      expect(contrastOnWhite(readable(category.color))).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('CONSTAT : l’ocre livré est à 2,7:1 tel quel — c’est la raison de ce module', () => {
    const ochre = LACAGETTE_CATEGORIES.find((c) => c.code === 'bulk')?.color as string
    expect(ochre).toBe('#B7950B')
    expect(contrastOnWhite(`rgb(183, 149, 11)`)).toBeLessThan(3)
    expect(contrastOnWhite(readable(ochre))).toBeGreaterThanOrEqual(4.5)
  })

  it('ne touche pas à une couleur déjà lisible', () => {
    expect(readable('#1c1b19')).toBe('rgb(28, 27, 25)')
  })

  it('termine sur le noir plutôt que de boucler', () => {
    expect(contrastOnWhite(readable('#ffffff'))).toBeGreaterThanOrEqual(4.5)
  })
})

describe('wash — la même couleur, à 10 %, derrière une tuile', () => {
  it('reste très clair : c’est un fond, pas un aplat', () => {
    for (const category of LACAGETTE_CATEGORIES) {
      // Au moins 12:1 contre l'encre : le nom du produit se lit par-dessus.
      const washed = wash(category.color)
      const [r, g, b] = [...washed.matchAll(/\d+/gu)].map((m) => Number(m[0])) as [
        number,
        number,
        number,
      ]
      expect(Math.min(r, g, b)).toBeGreaterThanOrEqual(224)
    }
  })

  it('tombe sur le gris d’attente quand la configuration ne dit rien d’exploitable', () => {
    // Une couleur illisible coûte une tuile grise, jamais un écran qui ne rend pas.
    expect(wash('var(--ink-muted)')).toBe(wash('#8a867c'))
    expect(wash('')).toBe(wash('#8a867c'))
  })

  it('accepte la forme courte à trois chiffres', () => {
    expect(wash('#fff')).toBe('rgb(255, 255, 255)')
  })
})
