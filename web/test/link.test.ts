import { describe, expect, it } from 'vitest'
import {
  RECONNECT_AFTER_MS,
  UNAVAILABLE_AFTER_MS,
  WEIGHT_HIDDEN_AFTER_MS,
  linkHealth,
} from '../src/lib/link'

/**
 * Le chien de garde du navigateur tient-il les trois seuils de §14.3 ?
 *
 * « Perte du flux SSE : bandeau Reconnexion…, dernier état grisé, poids masqué au
 * bout de 1,5 s. Chien de garde client : > 2 s sans message → Poids indisponible
 * VISIBLE ; > 5 s → reconnexion forcée. »
 *
 * Aucun test ne dort : la fonction prend la durée de silence en paramètre, et
 * c'est précisément pour cela qu'elle est écrite ainsi.
 */

describe('le flux est ouvert et la station se tait', () => {
  it('montre le poids tant qu’on n’a pas atteint 1,5 s', () => {
    expect(linkHealth(WEIGHT_HIDDEN_AFTER_MS - 1, true).showWeight).toBe(true)
    expect(linkHealth(WEIGHT_HIDDEN_AFTER_MS - 1, true).banner).toBe('')
  })

  it('masque le poids À 1,5 s, sans encore rien dire', () => {
    const health = linkHealth(WEIGHT_HIDDEN_AFTER_MS, true)
    expect(health.showWeight).toBe(false)
    expect(health.banner).toBe('')
    expect(health.mustReconnect).toBe(false)
  })

  it('dit « Poids indisponible » à 2 s, et le dit VISIBLEMENT', () => {
    expect(linkHealth(UNAVAILABLE_AFTER_MS - 1, true).banner).toBe('')
    expect(linkHealth(UNAVAILABLE_AFTER_MS, true).banner).toBe('Poids indisponible')
  })

  it('force la reconnexion à 5 s, et pas avant', () => {
    expect(linkHealth(RECONNECT_AFTER_MS - 1, true).mustReconnect).toBe(false)
    expect(linkHealth(RECONNECT_AFTER_MS, true).mustReconnect).toBe(true)
  })
})

describe('le flux est tombé', () => {
  it('affiche « Reconnexion… » immédiatement', () => {
    expect(linkHealth(0, false).banner).toBe('Reconnexion…')
  })

  it('laisse le dernier poids 1,5 s, puis le masque', () => {
    expect(linkHealth(WEIGHT_HIDDEN_AFTER_MS - 1, false).showWeight).toBe(true)
    expect(linkHealth(WEIGHT_HIDDEN_AFTER_MS, false).showWeight).toBe(false)
  })

  it('rouvre le flux au bout de 5 s de silence', () => {
    expect(linkHealth(RECONNECT_AFTER_MS, false).mustReconnect).toBe(true)
  })
})

describe('les seuils restent ordonnés', () => {
  it('masque avant d’alerter, et alerte avant de reconnecter', () => {
    expect(WEIGHT_HIDDEN_AFTER_MS).toBeLessThan(UNAVAILABLE_AFTER_MS)
    expect(UNAVAILABLE_AFTER_MS).toBeLessThan(RECONNECT_AFTER_MS)
  })

  it('ne montre jamais un poids en même temps qu’il le déclare indisponible', () => {
    for (let ms = 0; ms <= RECONNECT_AFTER_MS + 500; ms += 50) {
      for (const open of [true, false]) {
        const health = linkHealth(ms, open)
        if (health.banner === 'Poids indisponible') expect(health.showWeight).toBe(false)
      }
    }
  })
})
