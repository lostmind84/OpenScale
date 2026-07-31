import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Maintenance from '../src/admin/components/Maintenance.svelte'
import { Admin, type Admin as AdminType } from '../src/admin/lib/session.svelte'
import * as api from '../src/admin/lib/api'
import { AdminError } from '../src/admin/lib/api'

/**
 * La rubrique Maintenance, et les cinq choses qu'elle doit tenir.
 *
 *  1. les trois gestes sont rangés par brutalité croissante, et seul le dernier est
 *     ROUGE : rien ne défait un ordinateur qui redémarre ;
 *  2. ce dernier demande une confirmation, puis affiche un décompte ANNULABLE — c'est ce
 *     qui rend l'acte offrable ;
 *  3. un poste que personne ne relancerait répond 501, et l'écran montre sa phrase au
 *     lieu d'un bouton mort ;
 *  4. un 422 sur la relecture affiche TOUTES les fautes, pas la première ;
 *  5. aucun renvoi §X.Y ni ADR-0XX n'est visible.
 */

let host: HTMLElement

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  host.remove()
  vi.restoreAllMocks()
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
