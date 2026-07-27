import { afterEach, describe, expect, it, vi } from 'vitest'
import { Admin } from '../src/admin/lib/session.svelte'

/**
 * Le refus atteint-il l'écran, et y reste-t-il ?
 *
 * Il n'y arrivait pas. `refresh()` remettait le champ d'erreur à vide à chaque succès, et
 * il tourne toutes les trois secondes : « Mot de passe incorrect. » s'affichait puis
 * disparaissait avant qu'on ait fini de le lire. Le même champ servait au SONDAGE et à
 * l'ACTE, deux choses qui n'ont rien à voir — neuf boutons de dépannage, la connexion et
 * deux exports échouaient ainsi en silence.
 */

afterEach(() => vi.unstubAllGlobals())

/** Un service qui répond au tableau de bord et refuse tout le reste. */
function serviceRefusing(status: number, message: string, headers: HeadersInit = {}): void {
  vi.stubGlobal('fetch', async (route: string) => {
    if (String(route).includes('/health')) {
      return new Response(JSON.stringify({ station: 1 }), { status: 200 })
    }
    return new Response(JSON.stringify({ message }), { status, headers })
  })
}

describe('l’erreur d’un acte et celle du lien sont deux choses', () => {
  it('garde le refus quand le sondage réussit derrière', async () => {
    serviceRefusing(401, 'Mot de passe incorrect.')
    const admin = new Admin(60_000)

    await admin.login('faux')
    expect(admin.actionError).toBe('Mot de passe incorrect.')

    // Le sondage tourne toutes les trois secondes et lit /health SANS ENCOMBRE.
    await admin.refresh()

    expect(admin.actionError).toBe('Mot de passe incorrect.')
    expect(admin.linkError).toBe('')
  })

  it('n’écrit l’échec du sondage que dans le champ du lien', async () => {
    vi.stubGlobal('fetch', async () => new Response('', { status: 503 }))
    const admin = new Admin(60_000)

    await admin.refresh()

    expect(admin.linkError).not.toBe('')
    expect(admin.actionError).toBe('')
  })

  it('efface la phrase de succès dès qu’un acte commence', async () => {
    serviceRefusing(401, 'Refusé.')
    const admin = new Admin(60_000)
    admin.notice = 'La configuration est enregistrée et appliquée.'

    await admin.login('faux')

    // Sans quoi deux réponses cohabitent à l'écran, la périmée au-dessus.
    expect(admin.notice).toBe('')
  })
})

describe('les refus que le front ne savait pas lire', () => {
  it('dit combien de temps dure le verrouillage', async () => {
    serviceRefusing(429, 'Trop d’essais.', { 'Retry-After': '240' })
    const admin = new Admin(60_000)

    await admin.login('faux')

    expect(admin.actionError).toContain('4 minutes')
  })

  it('renvoie vers le code de secours quand aucun mot de passe n’est posé', async () => {
    serviceRefusing(409, 'Ce poste n’a pas encore de mot de passe.')
    const admin = new Admin(60_000)

    await admin.login('openscale')

    expect(admin.needsFirstPassword).toBe(true)
  })
})
