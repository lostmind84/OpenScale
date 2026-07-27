import { describe, expect, it } from 'vitest'
import { catalogStamp } from '../src/lib/format'

/**
 * La date du catalogue est-elle écrite en français, quoi qu'en dise le système ?
 *
 * Cette ligne est affichée EN PERMANENCE (§14.3). `Intl.DateTimeFormat` la rendrait
 * selon la locale du système, qui n'est pas un réglage du poste : un poste installé
 * sur un Windows anglais afficherait `7/27/2026` au milieu d'un écran par ailleurs
 * entièrement français. La mise en forme est donc écrite à la main, et testée.
 */

describe('catalogStamp', () => {
  it('écrit jour/mois/année et heure:minute:seconde', () => {
    // Construit dans le fuseau LOCAL, puisque c'est l'heure du magasin qui est
    // affichée : le test dit la même chose à Paris et sur la CI en UTC.
    const at = new Date(2026, 6, 27, 8, 6, 48)
    expect(catalogStamp(at.toISOString())).toBe('27/07/2026 08:06:48')
  })

  it('complète les unités à deux chiffres', () => {
    const at = new Date(2026, 0, 3, 4, 5, 6)
    expect(catalogStamp(at.toISOString())).toBe('03/01/2026 04:05:06')
  })

  it('ne rend rien quand aucun catalogue n’est daté', () => {
    // Un poste dont le premier fichier n'est pas arrivé n'a pas de date à montrer,
    // et « 01/01/1970 » enverrait un bénévole chercher un import qui n'existe pas.
    expect(catalogStamp('')).toBe('')
  })

  it('ne rend rien d’une date que le serveur n’aurait pas su écrire', () => {
    expect(catalogStamp('pas une date')).toBe('')
  })
})
