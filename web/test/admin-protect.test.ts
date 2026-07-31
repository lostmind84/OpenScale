import { afterEach, describe, expect, it, vi } from 'vitest'
import { AdminError, CODE_NO_PASSWORD } from '../src/admin/lib/api'
import { Admin } from '../src/admin/lib/session.svelte'

/**
 * Le mot de passe est-il demandé à l'ACTE, et l'acte se rejoue-t-il ?
 *
 * ADR-033 : on peut tout voir, on ne peut pas tout écrire. Le mot de passe cesse d'être
 * une porte qu'on franchit avant de regarder ; il devient une question posée au moment
 * d'agir. Ce qui rend la chose supportable est le REJEU : sans lui, l'exploitant perdrait
 * la saisie qu'il vient de faire et devrait la recommencer après s'être authentifié.
 */

afterEach(() => vi.unstubAllGlobals())

/** Un service qui accepte la session et rien d'autre. */
function serviceAcceptingSession(): void {
  vi.stubGlobal('fetch', async (route: string) => {
    if (String(route).includes('/session')) {
      return new Response('{"expires_at":"","session_minutes":30}', { status: 200 })
    }
    return new Response(JSON.stringify({ station: 1 }), { status: 200 })
  })
}

describe('un acte protégé demande le mot de passe, puis se rejoue', () => {
  it('ne fait pas ressaisir ce qu’on venait de faire', async () => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)
    let attempts = 0
    const action = async (): Promise<string> => {
      attempts += 1
      if (attempts === 1) throw new AdminError(401, 'Session expirée ou absente.')
      return 'enregistré'
    }

    const promise = admin.protect(action)
    await vi.waitUntil(() => admin.pending !== null)
    expect(admin.pending?.kind).toBe('password')

    await admin.answerPassword('openscale')

    expect(await promise).toBe('enregistré')
    expect(attempts).toBe(2)
    expect(admin.pending).toBeNull()
  })

  it('demande le CODE DE SECOURS quand aucun mot de passe n’est posé', async () => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)
    let attempts = 0
    const action = async (): Promise<string> => {
      attempts += 1
      if (attempts === 1) {
        throw new AdminError(409, 'Ce poste n’a pas encore de mot de passe.', CODE_NO_PASSWORD)
      }
      return 'enregistré'
    }

    const promise = admin.protect(action)
    await vi.waitUntil(() => admin.pending !== null)
    // Un poste neuf n'a pas de mot de passe à taper : il a une fiche d'installation.
    expect(admin.pending?.kind).toBe('first-password')

    await admin.answerRecovery('ABCDEFGH', 'un-nouveau-mot-de-passe')

    expect(await promise).toBe('enregistré')
    expect(attempts).toBe(2)
  })

  /**
   * Un 409 qui n'est PAS l'absence de mot de passe ne réclame pas la fiche d'installation.
   *
   * Le statut était le seul critère, et 409 est ce que répondent aussi un compte à rebours
   * déjà armé, une confirmation que personne n'attend et une mise à jour sur un poste
   * occupé. Un exploitant authentifié depuis dix minutes voyait donc « Ce poste n'a pas
   * encore de mot de passe » sur un double appui de « Confirmer », abandonnait, recliquait,
   * et l'acte suivant passait sans rien demander : les trois symptômes d'un même bug.
   */
  it.each([
    ['une confirmation que personne n’attend', 'Aucune confirmation n’est attendue.'],
    ['un compte à rebours déjà armé', 'Une confirmation est attendue.'],
    ['un poste occupé', 'Une pesée est en cours.'],
  ])('laisse le refus à l’écran quand le 409 est %s', async (_situation, message) => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)
    let attempts = 0

    const done = await admin.protect(async () => {
      attempts += 1
      throw new AdminError(409, message)
    })

    expect(done).toBeNull()
    expect(admin.pending).toBeNull()
    expect(admin.needsFirstPassword).toBe(false)
    expect(admin.actionError).toBe(message)
    expect(attempts).toBe(1)
  })

  it('ne demande rien quand la session est déjà ouverte', async () => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)

    const done = await admin.protect(async () => 'enregistré')

    expect(done).toBe('enregistré')
    expect(admin.pending).toBeNull()
  })

  it('abandonne sans rejouer quand on referme le panneau', async () => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)
    let attempts = 0
    const promise = admin.protect(async () => {
      attempts += 1
      throw new AdminError(401, 'Session expirée ou absente.')
    })

    await vi.waitUntil(() => admin.pending !== null)
    admin.cancelPassword()

    expect(await promise).toBeNull()
    expect(attempts).toBe(1)
    expect(admin.pending).toBeNull()
  })

  it('n’affiche pas « session ouverte » au-dessus de l’acte qu’elle vient d’autoriser', async () => {
    // S'authentifier n'est pas l'acte, c'en est l'antichambre. La phrase de la connexion
    // cohabitait avec le refus de l'enregistrement qu'elle venait d'autoriser : deux
    // messages vrais, illisibles ensemble.
    serviceAcceptingSession()
    const admin = new Admin(60_000)
    let attempts = 0

    const promise = admin.protect(async () => {
      attempts += 1
      if (attempts === 1) throw new AdminError(401, 'Session expirée ou absente.')
      return 'enregistré'
    })
    await vi.waitUntil(() => admin.pending !== null)
    await admin.answerPassword('openscale')
    await promise

    expect(admin.notice).toBe('')
  })

  it('laisse le refus à l’écran quand ce n’est pas une question de session', async () => {
    serviceAcceptingSession()
    const admin = new Admin(60_000)

    const done = await admin.protect(async () => {
      throw new AdminError(422, 'Cette configuration ne peut pas être appliquée.')
    })

    expect(done).toBeNull()
    expect(admin.pending).toBeNull()
    expect(admin.actionError).toBe('Cette configuration ne peut pas être appliquée.')
  })
})
