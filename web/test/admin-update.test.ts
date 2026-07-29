import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Update from '../src/admin/pages/Update.svelte'
import type { UpdateDTO } from '../src/admin/lib/dto'
import { Admin } from '../src/admin/lib/session.svelte'
import * as api from '../src/admin/lib/api'

/**
 * La page Mise à jour, et les cinq choses qu'elle doit tenir.
 *
 *  1. le bouton NOMME la version qu'il installera — c'est ce que le service exige dans le
 *     corps de la demande, et le refus 409 existe pour que le bénévole valide ce qu'il a
 *     lu et non ce qui est arrivé depuis ;
 *  2. l'acte est IRRÉVERSIBLE au sens d'ADR-037 : le retour arrière est automatique sur le
 *     binaire, il ne l'est pas sur la base ;
 *  3. les quatre issues d'update.ps1 se disent en quatre phrases différentes — « annulée,
 *     le poste marche » et « le poste ne répond pas » ne demandent pas la même chose ;
 *  4. un poste où la mise à jour n'existe pas le DIT, au lieu de montrer un bouton mort ;
 *  5. aucun renvoi §X.Y ni ADR-0XX n'est visible.
 */

/** Un poste sur 2.0.3 dont le dépôt publie 2.1.0. */
const NOMINAL: UpdateDTO = {
  running: '2.0.3',
  repository: 'lostmind84/OpenScale',
  supported: true,
  available: true,
  latest: '2.1.0',
  published_at: '2026-07-28T09:14:22Z',
  html_url: 'https://github.com/lostmind84/OpenScale/releases/tag/2.1.0',
  checked_at: '2026-07-29T08:12:00Z',
  outcome: null,
}

let host: HTMLElement

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  host.remove()
  vi.restoreAllMocks()
})

/** Monte la page et attend qu'elle ait lu son état. */
async function show(state: UpdateDTO): Promise<() => void> {
  vi.spyOn(api, 'fetchUpdate').mockResolvedValue(state)
  const admin = new Admin()
  const page = mount(Update, { target: host, props: { admin } })
  await vi.waitFor(() => expect(host.textContent).toContain(state.running))
  flushSync()
  return () => unmount(page)
}

describe('la page Mise à jour', () => {
  it('nomme la version que le bouton installera', async () => {
    const close = await show(NOMINAL)
    const button = host.querySelector<HTMLButtonElement>('[data-act="apply"]')
    expect(button).not.toBeNull()
    expect(button?.textContent).toContain('2.1.0')
    close()
  })

  it('traite l’installation comme un acte qui ne se défait pas', async () => {
    const close = await show(NOMINAL)
    const button = host.querySelector<HTMLButtonElement>('[data-act="apply"]')
    // Le retour arrière est automatique sur le binaire, jamais sur la base : les
    // migrations s’appliquent au démarrage et y revenir est un geste humain.
    expect(button?.dataset.kind).toBe('destructive')
    close()
  })

  it('affiche la version installée et le dépôt suivi', async () => {
    const close = await show(NOMINAL)
    expect(host.textContent).toContain('2.0.3')
    expect(host.textContent).toContain('lostmind84/OpenScale')
    close()
  })

  it('ne propose rien à un poste déjà à jour', async () => {
    const close = await show({ ...NOMINAL, available: false, latest: '2.0.3' })
    expect(host.querySelector('[data-act="apply"]')).toBeNull()
    expect(host.textContent).toContain('2.0.3')
    close()
  })

  it('dit qu’elle n’existe pas plutôt que de montrer un bouton mort', async () => {
    const close = await show({ ...NOMINAL, supported: false, available: false })
    expect(host.querySelector('[data-act="apply"]')).toBeNull()
    // La version installée reste lisible : c’est la première chose qu’on demande au
    // téléphone, et elle ne dépend pas de la mise à jour.
    expect(host.textContent).toContain('2.0.3')
    close()
  })

  it('dit les quatre issues en quatre phrases différentes', async () => {
    const said = new Set<string>()
    for (const status of [
      'succeeded',
      'rolled-back',
      'rolled-back-unhealthy',
      'not-started',
    ] as const) {
      const close = await show({
        ...NOMINAL,
        outcome: {
          status,
          from: '2.0.3',
          to: '2.1.0',
          reason: 'le poste ne répond pas',
          finished_at: '2026-07-29T10:16:04Z',
        },
      })
      const report = host.querySelector('[data-outcome]')?.textContent ?? ''
      expect(report).not.toBe('')
      said.add(report)
      close()
      host.replaceChildren()
    }
    expect(said.size).toBe(4)
  })

  it('n’annonce aucune tentative sur un poste qui n’a jamais basculé', async () => {
    const close = await show(NOMINAL)
    expect(host.querySelector('[data-outcome]')).toBeNull()
    close()
  })

  it('ne montre aucun renvoi de dossier', async () => {
    const close = await show({
      ...NOMINAL,
      outcome: {
        status: 'rolled-back',
        from: '2.0.3',
        to: '2.1.0',
        reason: 'le poste ne répond pas',
        finished_at: '2026-07-29T10:16:04Z',
      },
    })
    expect(host.textContent).not.toMatch(/§\s*\d/)
    expect(host.textContent).not.toMatch(/ADR-\d/)
    close()
  })

  it('survit à une lecture qui échoue, sans page blanche', async () => {
    vi.spyOn(api, 'fetchUpdate').mockRejectedValue(new Error('service injoignable'))
    const admin = new Admin()
    const page = mount(Update, { target: host, props: { admin } })
    // Attendre « du texte » ne prouve rien : « Lecture… » en est, et il est là avant
    // que la promesse rejetée ne soit arrivée. C'est la PHRASE qu'on attend.
    await vi.waitFor(() => expect(host.textContent).toContain('injoignable'))
    flushSync()
    unmount(page)
  })
})
