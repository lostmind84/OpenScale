import { describe, expect, it } from 'vitest'
import { hardwareIsHealthy } from '../src/lib/health'
import type { PrinterHealth, StateDTO, StationState } from '../src/lib/dto'

/**
 * La pastille de la barre du bas, et la règle que le banc L0 a imposée le 29/07/2026 :
 * **l'absence d'information n'est pas la santé.**
 *
 * Elle annonçait « Balance et imprimante disponibles » sur un poste dont ni la balance
 * ni l'imprimante n'étaient branchées, parce qu'elle s'allumait tant que rien ne
 * l'avait contredite. C'est le contraire de ce qu'un bénévole vient y chercher.
 */

/** Un instantané minimal : seuls les trois champs que la pastille lit sont posés. */
function snapshot(
  state: StationState,
  connected: boolean,
  printer: PrinterHealth,
): StateDTO {
  return {
    state,
    scale: { connected, median_ms: 0, observations_count: 0, provisional: true, too_slow: false },
    printer: { health: printer, detail: '', pending_jobs_count: 0, observed_at: '' },
  } as unknown as StateDTO
}

describe('la pastille ne verdit que sur une information', () => {
  it('reste éteinte tant qu’aucun instantané n’est arrivé', () => {
    expect(hardwareIsHealthy(null)).toBe(false)
  })

  it('reste éteinte pendant « initializing », qui est l’état d’un poste sans balance', () => {
    // Le cas rapporté : application lancée sans balance ni imprimante, pastille verte.
    // `scale.connected` vaut VRAI dans tous les états sauf `scale_lost` — c'est
    // `h.model.State != domain.ScaleLost` côté Hub —, donc lui seul ne suffit pas.
    expect(hardwareIsHealthy(snapshot('initializing', true, 'unknown'))).toBe(false)
  })

  it('s’allume quand le poste est au repos, balance connectée', () => {
    expect(hardwareIsHealthy(snapshot('idle', true, 'ready'))).toBe(true)
  })

  it('s’éteint quand la balance est perdue', () => {
    expect(hardwareIsHealthy(snapshot('scale_lost', false, 'ready'))).toBe(false)
  })

  it('s’éteint quand l’imprimante est en panne', () => {
    expect(hardwareIsHealthy(snapshot('idle', true, 'faulted'))).toBe(false)
  })

  it('reste allumée sur une imprimante « unknown », qui est l’état normal en RAW', () => {
    // N1 (§8.5) : les octets partent, rien ne revient. Peindre ce cas en orange
    // laisserait les quatre postes du parc orange pour toujours.
    expect(hardwareIsHealthy(snapshot('idle', true, 'unknown'))).toBe(true)
  })
})
