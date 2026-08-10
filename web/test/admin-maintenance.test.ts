import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Maintenance from '../src/admin/components/Maintenance.svelte'
import { Admin, type Admin as AdminType } from '../src/admin/lib/session.svelte'
import * as api from '../src/admin/lib/api'
import { AdminError } from '../src/admin/lib/api'

/**
 * La rubrique Maintenance, et ce qu'elle doit tenir.
 *
 *  1. les trois gestes sont rangés par brutalité croissante, et seul le dernier est
 *     ROUGE : rien ne défait un ordinateur qui redémarre ;
 *  2. ce dernier demande une confirmation, puis affiche un décompte ANNULABLE — c'est ce
 *     qui rend l'acte offrable ;
 *  3. un poste que personne ne relancerait répond 501, et l'écran montre sa phrase au
 *     lieu d'un bouton mort ;
 *  4. le redémarrage du poste RACONTE son attente — ce qui se passe, depuis quand, et
 *     quoi faire quand ça dure — au lieu de laisser « En cours… » cinq minutes ;
 *  5. un 422 sur la relecture affiche TOUTES les fautes, pas la première ;
 *  6. aucun renvoi §X.Y ni ADR-0XX n'est visible.
 */

let host: HTMLElement

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  host.remove()
  vi.restoreAllMocks()
  // restoreAllMocks ne défait PAS un stubGlobal : sans cette ligne, le `fetch` d'un banc
  // resterait posé sur les suivants.
  vi.unstubAllGlobals()
})

/** Monte la rubrique et rend de quoi la démonter. */
function show(): { admin: AdminType; close: () => void } {
  const admin = new Admin()
  const panel = mount(Maintenance, { target: host, props: { admin } })
  flushSync()
  return { admin, close: () => unmount(panel) }
}

/** Le bouton d'un acte, par son nom stable. */
function act(name: string): HTMLButtonElement | null {
  return host.querySelector<HTMLButtonElement>(`[data-act="${name}"]`)
}

describe('la rubrique Maintenance', () => {
  it('offre les trois gestes', () => {
    const { close } = show()
    for (const name of ['reload-config', 'restart', 'reboot']) {
      expect(act(name), name).not.toBeNull()
    }
    close()
  })

  it('ne peint en rouge que le geste que rien ne défait', () => {
    const { close } = show()
    expect(act('reload-config')?.dataset.kind).toBe('write')
    expect(act('restart')?.dataset.kind).toBe('write')
    // L'ordinateur qui redémarre ne se rattrape pas : c'est le seul destructif des trois.
    expect(act('reboot')?.dataset.kind).toBe('destructive')
    close()
  })

  it('demande une confirmation avant d’armer le redémarrage de l’ordinateur', async () => {
    const armed = vi.spyOn(api, 'armReboot')
    const { close } = show()

    act('reboot')?.click()
    flushSync()
    expect(armed).not.toHaveBeenCalled()
    expect(host.querySelector('[data-confirm]')).not.toBeNull()

    close()
  })

  it('affiche un décompte annulable une fois le redémarrage armé', async () => {
    vi.spyOn(api, 'armReboot').mockResolvedValue({
      at: new Date(Date.now() + 30_000).toISOString(),
      seconds_left: 30,
    })
    const cancelled = vi
      .spyOn(api, 'cancelReboot')
      .mockResolvedValue({ done: true, message: 'Le redémarrage est annulé.' })
    const { close } = show()

    act('reboot')?.click()
    flushSync()
    act('reboot')?.click()
    await vi.waitFor(() => expect(host.querySelector('[data-countdown]')).not.toBeNull())
    flushSync()
    expect(host.textContent).toContain('30')

    act('cancel-reboot')?.click()
    await vi.waitFor(() => expect(cancelled).toHaveBeenCalled())
    await vi.waitFor(() => expect(host.querySelector('[data-countdown]')).toBeNull())
    close()
  })

  it('remonte la phrase du service quand le poste n’est relancé par personne', async () => {
    vi.spyOn(api, 'restartStation').mockRejectedValue(
      new AdminError(
        501,
        'Ce poste n’est pas lancé par un service : personne ne le redémarrerait.',
        'ERR-SYS-10',
      ),
    )
    const { admin, close } = show()

    act('restart')?.click()
    // La phrase remonte à l'Admin, qui en fait UNE bannière dans App.svelte. La rubrique
    // ne la recopie pas : deux exemplaires du même refus se lisent comme deux pannes.
    await vi.waitFor(() => expect(admin.actionError).toContain('personne ne le redémarrerait'))
    expect(host.textContent).not.toContain('personne ne le redémarrerait')
    close()
  })

  it('laisse le bouton de redémarrage armé tant que l’état du poste est inconnu', () => {
    // `admin.health` vaut null tant que GET /admin/api/health n'a pas répondu, et c'est
    // l'état d'un écran qui vient de s'ouvrir. Désarmer là-dessus rendrait le bouton mort
    // exactement sur le poste qui en a besoin : celui qui ne répond plus.
    const { admin, close } = show()
    expect(admin.health).toBeNull()
    expect(act('restart')?.disabled).toBe(false)
    close()
  })

  it('raconte l’attente pendant que le poste redémarre, puis dit qu’il est revenu', async () => {
    vi.spyOn(api, 'restartStation').mockResolvedValue({
      done: true,
      message: 'Le poste redémarre. L’écran revient tout seul.',
    })
    vi.stubGlobal('fetch', async () => new Response('', { status: 200 }))
    const { close } = show()

    act('restart')?.click()
    // Tout de suite, et pas au bout de la première sonde : entre le clic et la réponse,
    // l'écran client est noir et c'est là que le bénévole se demande quoi faire.
    await vi.waitFor(() => expect(host.querySelector('[data-restarting]')).not.toBeNull())
    expect(host.textContent).toContain('Le poste redémarre depuis')

    await vi.waitFor(() => expect(host.textContent).toContain('Le poste est revenu.'), {
      timeout: 5000,
      interval: 50,
    })
    expect(host.querySelector('[data-restarting]')).toBeNull()
    close()
  })

  it('affiche TOUTES les fautes d’un fichier refusé', async () => {
    vi.spyOn(api, 'reloadConfigFromDisk').mockRejectedValue(
      new AdminError(422, 'Cette configuration ne peut pas être appliquée.', 'ERR-CFG-01', [
        { field: 'journal.max_rows', message: '3 est sous le plancher de 100' },
        { field: 'catalog.type', message: 'type de source inconnu' },
      ]),
    )
    const { close } = show()

    act('reload-config')?.click()
    await vi.waitFor(() => expect(host.textContent).toContain('journal.max_rows'))
    // La seconde faute compte autant que la première : un écran qui en corrige une, la
    // réenregistre et découvre la suivante est un écran qu'on abandonne.
    expect(host.textContent).toContain('catalog.type')
    close()
  })

  it('dit ce que la relecture a mis en service', async () => {
    vi.spyOn(api, 'reloadConfigFromDisk').mockResolvedValue({
      config: {},
      config_fingerprint: 'a1b2c3d4',
      retired_keys: [],
      pending_confirmation: null,
    })
    const { close } = show()

    act('reload-config')?.click()
    await vi.waitFor(() => expect(host.textContent).toContain('a1b2c3d4'))
    close()
  })

  it('ne montre aucun renvoi de dossier', () => {
    const { close } = show()
    expect(host.textContent).not.toMatch(/§\s*\d/)
    expect(host.textContent).not.toMatch(/ADR-\d/)
    close()
  })
})

describe('l’attente d’un poste qui ne revient pas', () => {
  /**
   * Rend l'horloge du navigateur pilotable, et renvoie de quoi l'avancer.
   *
   * C'est la seule façon d'atteindre en une seconde de banc ce qu'un bénévole vit en cinq
   * minutes : le composant lit `Date.now()` pour dire depuis quand le poste redémarre, et
   * la boucle de sondage y lit son budget. Les intervalles, eux, restent RÉELS — c'est le
   * rendu qu'on regarde, et il doit se refaire tout seul.
   */
  function drivableClock(): { advance: (ms: number) => void } {
    let now = Date.now()
    vi.spyOn(Date, 'now').mockImplementation(() => now)
    return { advance: (ms: number) => (now += ms) }
  }

  it('invite à aller voir la machine, puis nomme le geste de réparation', async () => {
    const clock = drivableClock()
    vi.spyOn(api, 'restartStation').mockResolvedValue({
      done: true,
      message: 'Le poste redémarre. L’écran revient tout seul.',
    })
    // Le poste ne répond plus, et il ne répondra plus : c'est exactement la panne du
    // 10/08/2026 — le service s'arrête et son gestionnaire ne relance rien.
    vi.stubGlobal('fetch', async () => {
      throw new Error('le poste ne répond pas')
    })
    const { close } = show()

    act('restart')?.click()
    await vi.waitFor(() => expect(host.querySelector('[data-restarting]')).not.toBeNull())
    expect(host.textContent).not.toContain('Allez voir')

    clock.advance(90_000)
    await vi.waitFor(() => expect(host.textContent).toContain('Allez voir la machine'), {
      timeout: 5000,
      interval: 50,
    })
    expect(host.textContent).toContain('90 secondes')

    // Le budget d'attente est épuisé. « Allez le voir » ne disait pas quoi y faire : un
    // poste que rien ne relance se relance à la main, et son enregistrement doit être
    // reposé, sans quoi le bouton se conduira pareil la fois suivante.
    clock.advance(10 * 60 * 1000)
    await vi.waitFor(() => expect(host.textContent).toContain('service start'), {
      timeout: 5000,
      interval: 50,
    })
    expect(host.textContent).toContain('install.ps1')
    expect(host.querySelector('[data-restarting]')).toBeNull()
    close()
    // Le budget de CE banc, et non celui par défaut : il traverse deux attentes réelles —
    // le battement d'une seconde qui redessine la durée, puis la période de sondage — et
    // sous les cinq secondes par défaut, un échec sortirait « test timed out » à la place
    // de la phrase qui manque.
  }, 20_000)
})
