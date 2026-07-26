import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import Dashboard from '../src/admin/pages/Dashboard.svelte'
import type { HealthDTO } from '../src/admin/lib/dto'
import { lightsOf, type Light } from '../src/admin/lib/lights'
import { nominalHealth, nominalState } from './fixtures/health'

/**
 * Les six feux de §14.4, et la seule règle qui les rend utiles : **un feu qui n'est pas
 * vert dit QUOI FAIRE.**
 *
 * « Imprimante en panne » n'apprend rien à un bénévole debout devant un poste muet. C'est la
 * même règle que `diag.Report.Validate()` applique aux quinze contrôles de `openscale
 * doctor`, et pour la même raison : un échec sans remède apprend à ignorer l'écran.
 *
 * Le tableau ci-dessous est un scénario de panne par ligne. Le test ne se contente pas de
 * vérifier la couleur : il exige la consigne, et il exige qu'elle NOMME le geste.
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

/** Monte le tableau de bord et rend les feux tels que le DOM les porte. */
function render(health: HealthDTO): Map<string, { level: string; text: string }> {
  component = mount(Dashboard, { target: host, props: { health } })
  flushSync()
  const drawn = new Map<string, { level: string; text: string }>()
  for (const node of host.querySelectorAll('[data-light]')) {
    drawn.set(node.getAttribute('data-light') ?? '', {
      level: node.getAttribute('data-level') ?? '',
      text: (node.textContent ?? '').replace(/\s+/gu, ' ').trim(),
    })
  }
  return drawn
}

/** Le feu nommé, pris dans la liste que compose {@link lightsOf}. */
function light(health: HealthDTO, id: string): Light {
  const found = lightsOf(health).find((candidate) => candidate.id === id)
  if (found === undefined) throw new Error(`aucun feu « ${id} » : les six de §14.4 sont exigés`)
  return found
}

/** Les scénarios de panne, un par ligne, et ce que le feu doit nommer. */
const BREAKDOWNS: { what: string; id: string; level: string; says: RegExp; health: HealthDTO }[] = [
  {
    what: 'la balance ne répond plus',
    id: 'scale',
    level: 'fault',
    says: /câble.*Tester la balance|Tester la balance.*câble/su,
    health: nominalHealth({
      state: nominalState({
        scale: {
          connected: false,
          median_ms: 400,
          observations_count: 64,
          provisional: false,
          too_slow: false,
        },
      }),
    }),
  },
  {
    what: 'l’imprimante ne peut pas imprimer',
    id: 'printer',
    level: 'fault',
    says: /capot.*Tester l’imprimante/su,
    health: nominalHealth({
      state: nominalState({
        printer: {
          health: 'faulted',
          detail: 'Plus de papier.',
          pending_jobs_count: 1,
          observed_at: '2026-07-24T12:00:00.000Z',
        },
      }),
    }),
  },
  {
    what: 'le rouleau arrive en fin de vie',
    id: 'roll',
    level: 'warn',
    says: /J’ai changé le rouleau/u,
    health: nominalHealth({
      roll: {
        printed_count: 940,
        capacity_count: 1000,
        remaining_count: 60,
        level: 'warn',
        message: 'rouleau à changer : environ 60 étiquettes restantes.',
        known: true,
      },
    }),
  },
  {
    what: 'la grille est vide',
    id: 'catalog',
    level: 'fault',
    says: /Déposez le fichier/u,
    health: nominalHealth({ state: nominalState({ catalog_count: 0 }) }),
  },
  {
    what: 'le disque est plein',
    id: 'disk',
    level: 'fault',
    says: /Faites de la place/u,
    health: nominalHealth({
      disk: {
        path: 'C:\\ProgramData\\OpenScale',
        free_bytes: 0,
        total_bytes: 128_000_000_000,
        alert_mb: 500,
      },
    }),
  },
  {
    what: 'le disque passe sous le seuil déclaré',
    id: 'disk',
    level: 'warn',
    says: /Faites de la place/u,
    health: nominalHealth({
      disk: {
        path: 'C:\\ProgramData\\OpenScale',
        free_bytes: 300_000_000,
        total_bytes: 128_000_000_000,
        alert_mb: 500,
      },
    }),
  },
  {
    what: 'des pesées sont sorties sans être journalisées',
    id: 'journal',
    level: 'fault',
    says: /fichier de diagnostic.*ne redémarrez rien/su,
    health: nominalHealth({
      counters: { unlogged_weighings_count: 3, journal_rows_count: 1236 },
    }),
  },
  {
    what: 'le poste n’a pas de journal ouvert',
    id: 'journal',
    level: 'unknown',
    says: /fichier de diagnostic/u,
    health: nominalHealth({
      counters: { unlogged_weighings_count: 0, journal_rows_count: -1 },
    }),
  },
  {
    what: 'l’imprimante ne dit rien de son état',
    id: 'printer',
    level: 'unknown',
    says: /étiquette de test/u,
    health: nominalHealth({
      state: nominalState({
        printer: {
          health: 'unknown',
          detail: '',
          pending_jobs_count: 0,
          observed_at: '2026-07-24T12:00:00.000Z',
        },
      }),
    }),
  },
  {
    what: 'aucun compteur de rouleau n’est publié',
    id: 'roll',
    level: 'unknown',
    says: /Réglages avancés → Matériel/u,
    health: nominalHealth({ roll: null }),
  },
]

describe('les six feux du tableau de bord', () => {
  it('en dessine six, dans l’ordre où §14.4 les énumère', () => {
    expect(lightsOf(nominalHealth()).map((candidate) => candidate.id)).toEqual([
      'scale',
      'printer',
      'roll',
      'catalog',
      'disk',
      'journal',
    ])
    expect([...render(nominalHealth()).keys()]).toHaveLength(6)
  })

  it('les met tous au vert sur un poste nominal', () => {
    const drawn = render(nominalHealth())
    for (const [id, state] of drawn) expect(state.level, id).toBe('ok')
  })

  it.each(BREAKDOWNS)('$what : le feu $id passe en $level et DIT quoi faire', (scenario) => {
    const drawn = render(scenario.health)
    expect(drawn.get(scenario.id)?.level).toBe(scenario.level)

    const remedy = light(scenario.health, scenario.id).remedy
    expect(remedy).not.toBe('')
    expect(remedy).toMatch(scenario.says)
    // La consigne est LUE à l'écran, et pas seulement calculée : un bénévole ne clique pas
    // pour savoir quoi faire.
    expect(drawn.get(scenario.id)?.text).toContain(remedy)
  })

  it('n’accepte AUCUN verdict non vert sans consigne, sur tous les scénarios', () => {
    // La règle générale, et non l'énumération : c'est celle que `Report.Validate()` tient
    // côté Go, et elle doit tenir pour un feu que personne n'a pensé à inventorier.
    for (const scenario of BREAKDOWNS) {
      for (const candidate of lightsOf(scenario.health)) {
        if (candidate.level === 'ok' || candidate.level === 'off') continue
        expect(candidate.remedy, `${candidate.id} en ${candidate.level}`).not.toBe('')
      }
    }
  })

  it('ÉTEINT le feu de la balance sur un poste qui n’en a pas, au lieu de le laisser rouge', () => {
    // C'est tout l'intérêt de la déclaration `scale.present` (§11.2) : un poste sans balance
    // n'est pas un poste en panne, et un feu rouge permanent est un feu qu'on n'regarde plus.
    const health = nominalHealth({
      scale_present: false,
      state: nominalState({
        scale: {
          connected: false,
          median_ms: 0,
          observations_count: 0,
          provisional: true,
          too_slow: false,
        },
      }),
    })
    const scale = light(health, 'scale')
    expect(scale.level).toBe('off')
    expect(scale.remedy).toBe('')
    expect(render(health).get('scale')?.level).toBe('off')
  })

  it('affiche la cadence OBSERVÉE, celle que le poste mesure', () => {
    const health = nominalHealth({
      state: nominalState({
        scale: {
          connected: true,
          median_ms: 620,
          observations_count: 12,
          provisional: false,
          too_slow: false,
        },
      }),
    })
    render(health)
    const cadence = host.querySelector('[data-cadence]')?.textContent ?? ''
    expect(cadence.replace(/\s+/gu, ' ')).toContain('620 ms')
    expect(cadence.replace(/\s+/gu, ' ')).toContain('12 intervalles mesurés')
  })

  it('nomme la source du catalogue, son chemin surveillé et son dernier essai', () => {
    render(nominalHealth())
    const source = host.querySelector('[data-source]')?.textContent ?? ''
    expect(source).toContain('flv_2.csv')
    expect(source).toContain('C:\\ProgramData\\OpenScale\\catalog\\incoming')

    const attempt = host.querySelector('[data-attempt]')?.textContent ?? ''
    expect(attempt.replace(/\s+/gu, ' ')).toContain('Dernier essai : 24/07/2026')
    expect(attempt).toContain('appliqué')
  })

  it('dit « NON CONFIGURÉ » et la consigne quand le poste ne revient pas seul (bloquant-7)', () => {
    const health = nominalHealth({
      unattended_restart: {
        configured: false,
        known: true,
        detail:
          'NON : après une coupure de courant, ce poste restera sur l’écran de connexion.',
        remedy: 'Relancez install.ps1 en administrateur — c’est son étape 3 (§15.2).',
      },
    })
    render(health)
    const line = host.querySelector('[data-restart]')?.textContent ?? ''
    expect(line).toContain('NON CONFIGURÉ')
    expect(host.textContent).toContain('Relancez install.ps1')
    // Ce n'est PAS un septième feu : les six restent ceux des périphériques et des
    // ressources (§14.4).
    expect([...render(health).keys()]).toHaveLength(6)
  })

  it('montre la version et l’empreinte de configuration en huit caractères', () => {
    render(nominalHealth())
    expect(host.querySelector('[data-version]')?.textContent).toBe('1.0.0')
    const fingerprint = host.querySelector('[data-fingerprint]')?.textContent ?? ''
    expect(fingerprint).toHaveLength(8)
  })
})
