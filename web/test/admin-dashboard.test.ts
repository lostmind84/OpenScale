import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import Dashboard from '../src/admin/pages/Dashboard.svelte'
import type { HealthDTO } from '../src/admin/lib/dto'
import { preferences } from '../src/admin/lib/preferences.svelte'
import { nominalHealth } from './fixtures/health'

/**
 * Le tableau de bord dit-il la vérité ?
 *
 * C'est la page qu'un bénévole regarde en premier quand quelque chose ne va pas, et les
 * trois défauts corrigés ici avaient le même vice : l'écran AFFIRMAIT quelque chose que
 * le poste ne savait pas. Une cadence de « 0 ms » sur une balance muette, un « NON
 * CONFIGURÉ » sur un système qui n'avait pas répondu, et une liste sans plafond qui
 * repoussait hors de l'écran la ligne que bloquant-7 veut visible.
 */

let host: HTMLElement
let component: Record<string, unknown> | null = null

beforeEach(() => {
  // La préférence est un singleton de module : sans remise à zéro, le test qui la coche
  // la laisserait cochée pour tous ceux d'après.
  preferences.showTechnicalNames = false
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  if (component !== null) unmount(component)
  host.remove()
  component = null
})

/** Monte le tableau de bord sur un état de poste. */
function show(health: HealthDTO): void {
  component = mount(Dashboard, { target: host, props: { health } })
  flushSync()
}

/** L'état d'un poste nominal, que chaque test abîme sur un point précis. */
function nominal(): HealthDTO {
  return nominalHealth()
}

describe('la cadence n’affirme que ce qui a été mesuré', () => {
  it('ne dit pas « 0 ms » quand la balance ne répond pas', () => {
    const health = nominal()
    health.scale_present = true
    health.state.scale = {
      ...health.state.scale,
      connected: false,
      median_ms: 0,
      observations_count: 0,
    }

    show(health)

    const said = host.querySelector('[data-cadence]')?.textContent ?? ''
    expect(said).toContain('ne répond pas')
    // Le défaut : « Une mesure toutes les 0 ms », suivie deux lignes plus bas de
    // « moins de huit intervalles ont été observés ». Les deux ne peuvent pas être vraies.
    expect(said).not.toContain('0 ms')
  })

  it('ne dit pas de cadence tant qu’aucun intervalle n’a été mesuré', () => {
    const health = nominal()
    health.scale_present = true
    health.state.scale = {
      ...health.state.scale,
      connected: true,
      median_ms: 0,
      observations_count: 0,
    }

    show(health)

    expect(host.querySelector('[data-cadence]')?.textContent).toContain('Aucun intervalle')
  })
})

describe('le redémarrage sans intervention (bloquant-7)', () => {
  it('distingue « je ne sais pas » de « non configuré »', () => {
    const health = nominal()
    health.unattended_restart = {
      configured: false,
      known: false,
      detail: 'le gestionnaire de services n’a pas répondu',
      remedy: '',
    }

    show(health)

    const line = host.querySelector('[data-restart]')
    // `known` existait dans le DTO et n'était jamais lu : un poste qui ne SAIT PAS était
    // accusé de n'être pas configuré.
    expect(line?.textContent).toContain('INCONNU')
    expect(line?.getAttribute('data-verdict')).toBe('unknown')
  })

  it('distingue visuellement NON CONFIGURÉ, comme §14.4 l’exige mot pour mot', () => {
    const health = nominal()
    health.unattended_restart = {
      configured: false,
      known: true,
      detail: 'AutoAdminLogon vaut 0',
      remedy: 'Ouvrez netplwiz et cochez la case.',
    }

    show(health)

    const line = host.querySelector('[data-restart]')
    expect(line?.textContent).toContain('NON CONFIGURÉ')
    // `data-configured` était posé sans qu'aucune règle CSS ne le lise : le seul état qui
    // doive attirer l'œil ressemblait à tous les autres.
    expect(line?.getAttribute('data-verdict')).toBe('missing')
    expect(host.textContent).toContain('netplwiz')
  })
})

/**
 * Le code d'un événement suit la même règle ici qu'au Journal, et pour la même raison.
 *
 * Les « dix derniers événements » sont le journal technique montré court : cacher
 * `ERR-CAT-05` sur une page pour le montrer sur l'autre ne cacherait rien du tout. Le
 * message français, lui, dit ce qui s'est passé sans avoir besoin du code.
 */
describe('le code technique des dix derniers événements', () => {
  /** Un événement du journal technique, tel que `internal/web/health.go` le sert. */
  function withEvent(): HealthDTO {
    const health = nominal()
    health.events = [
      {
        id: 1,
        occurred_at: '2026-07-28T09:02:12.000Z',
        level: 'error',
        source: 'catalog',
        code: 'ERR-CAT-05',
        message: 'Le fichier du catalogue est illisible.',
        detail: '',
      },
    ]
    return health
  }

  it('reste caché tant que personne ne demande les noms techniques', () => {
    show(withEvent())

    expect(host.textContent).toContain('Le fichier du catalogue est illisible.')
    expect(host.textContent).not.toContain('ERR-CAT-05')
  })

  it('revient là où il était dès que l’interrupteur est coché', () => {
    preferences.showTechnicalNames = true

    show(withEvent())

    expect(host.querySelector('.events .code')?.textContent).toBe('ERR-CAT-05')
  })
})

describe('les décisions locales', () => {
  it('sont plafonnées, et annoncent leur total', () => {
    const health = nominal()
    health.decisions = Array.from({ length: 12 }, (_, i) => ({
      product_id: `p${String(i)}`,
      offered: false,
      min_weight_g: null,
      reason: 'retiré par le magasin',
      decided_at: '2026-07-24T10:00:00Z',
      decided_by: 'un responsable',
    }))

    show(health)

    // Sans plafond, trente décisions repoussaient la ligne du redémarrage trois écrans
    // plus bas — celle-là même que bloquant-7 veut visible.
    expect(host.querySelectorAll('.decisions li')).toHaveLength(5)
    expect(host.textContent).toContain('12 décisions en vigueur')
  })
})
