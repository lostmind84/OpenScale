import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import Inventory from '../src/admin/components/Inventory.svelte'
import { inventoryOf } from '../src/admin/lib/inventory'
import { FLV_1_IMPORT, FLV_IMPORT } from './fixtures/health'

/**
 * L'inventaire du catalogue est-il rendu AU MOT PRÈS, à partir des chiffres de l'API ?
 *
 * §14.4 écrit la phrase en entier, et elle décide si un bénévole s'inquiète ou non :
 *
 * ```
 * Catalogue du 24/07/2026 — 355 produits reçus
 *   331 pesables            (181 avec photo, 174 sans)
 *     8 non pesables        préemballés (7), code interne 0490 (1)
 *    16 anomalies           à corriger dans Odoo         [voir les 16 lignes]
 *   + 1 unité divergente    pesable, unité à corriger    [voir la ligne]
 * ```
 *
 * Ce test monte le VRAI composant sur une charge d'API et lit le DOM. Il ne teste pas la
 * fonction pure toute seule : ce qu'un bénévole lit est le rendu, et un gabarit qui
 * écrirait « 331 » en dur passerait un test de fonction pure sans broncher.
 */

let host: HTMLElement
let component: unknown

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
})

/** Monte l'inventaire sur un import et rend le texte des lignes. */
function render(record = FLV_IMPORT, motives = MOTIVES): string[] {
  // Le lien « voir les 16 lignes » n'est offert que si quelqu'un sait où il mène : le
  // tableau de bord le branche sur la page Catalogue, qui porte le rapport de correction.
  component = mount(Inventory, {
    target: host,
    props: { record, motives, onshowrows: () => undefined },
  })
  flushSync()
  return [...host.querySelectorAll('.row')].map((row) => collapse(row.textContent ?? ''))
}

/** La répartition des non-pesables telle que la sert `GET /admin/api/health`. */
const MOTIVES = [
  { code: 'PREPACKAGED_PRODUCT', value: '', count: 7 },
  { code: 'INTERNAL_CODE_NOT_WEIGHABLE', value: '0490', count: 1 },
]

/** Réduit les blancs du HTML à une espace : l'alignement est du CSS, pas du texte. */
function collapse(text: string): string {
  return text.replace(/\s+/gu, ' ').trim()
}

describe('l’inventaire de §14.4, mot pour mot', () => {
  it('titre la phrase avec la date et les produits REÇUS', () => {
    render()
    const headline = host.querySelector('.headline')?.textContent
    expect(headline).toBe('Catalogue du 24/07/2026 — 355 produits reçus')
  })

  it('écrit les quatre lignes exactement comme le document les donne', () => {
    expect(render()).toEqual([
      '331 pesables (181 avec photo, 174 sans)',
      '8 non pesables préemballés (7), code interne 0490 (1)',
      '16 anomalies à corriger dans Odoo voir les 16 lignes',
      '+ 1 unité divergente pesable, unité à corriger voir la ligne',
    ])
  })

  it('accroche « 181 avec photo, 174 sans » aux 355 REÇUS, et 181 + 174 le prouve', () => {
    // §14.2 le dit dans les mêmes termes : « 181 produits sur 355 ». Accrocher la
    // parenthèse aux 331 pesables afficherait un nombre qui n'existe pas.
    expect(FLV_IMPORT.images_decoded_count + 174).toBe(FLV_IMPORT.rows_read_count)
    expect(render()[0]).toContain('(181 avec photo, 174 sans)')
  })

  it('ne dit JAMAIS « 46 produits en erreur »', () => {
    // Trois raisons, dans l'ordre : c'est faux — un boulgour préemballé n'est pas en
    // erreur —, c'est alarmant sans action possible, et cela noie le seul chiffre qui
    // doive attirer l'œil. Le total n'existe donc nulle part.
    const whole = collapse(host.textContent ?? '') + render().join(' ')
    expect(whole).not.toMatch(/46/u)
    expect(whole).not.toMatch(/en erreur/u)
  })

  it('tient sur UNE ligne pour les deux catalogues réels', () => {
    // « Les deux doivent tenir sur une ligne et rester lisibles : c'est la seule
    // contrainte que cet écran impose au chiffre » (§14.4).
    expect(inventoryOf(FLV_IMPORT, MOTIVES).oneLine).toBe(
      '355 reçus · 331 pesables · 8 non pesables · 16 anomalies · 1 unité divergente',
    )
    expect(inventoryOf(FLV_1_IMPORT, []).oneLine).toBe(
      '153 reçus · 107 pesables · 39 non pesables · 7 anomalies · 5 unités divergentes',
    )
  })

  it('met les unités divergentes au pluriel quand il y en a cinq', () => {
    const lines = render(FLV_1_IMPORT, [])
    expect(lines.at(-1)).toBe('+ 5 unités divergentes pesable, unité à corriger voir les 5 lignes')
  })

  it('n’écrit AUCUNE répartition quand la base n’en porte pas', () => {
    // Inventer « préemballés » pour meubler la ligne serait inventer un chiffre.
    const lines = render(FLV_IMPORT, [])
    expect(lines[1]).toBe('8 non pesables')
  })

  it('tire chaque chiffre de la charge et d’aucun gabarit', () => {
    // La preuve par la déformation : d'autres chiffres d'API, d'autres chiffres à l'écran.
    const other = { ...FLV_IMPORT, rows_read_count: 12, weighable_count: 9, images_decoded_count: 4 }
    const lines = render(other, [])
    expect(host.querySelector('.headline')?.textContent).toContain('12 produits reçus')
    expect(lines[0]).toBe('9 pesables (4 avec photo, 8 sans)')
  })
})
