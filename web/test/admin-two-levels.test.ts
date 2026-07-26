import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../src/admin/App.svelte'
import { nominalHealth } from './fixtures/health'

/**
 * Les DEUX NIVEAUX de §14.4, vérifiés là où ils comptent : dans les requêtes.
 *
 * 99 % des utilisateurs ne sont pas à l'aise avec l'informatique, et mettre neuf pages
 * d'expert devant eux est une faute. Ce test tient les deux moitiés de la décision :
 *
 *  - les DEUX pages bénévoles s'ouvrent sans mot de passe et n'appellent que des routes non
 *    authentifiées — le tableau de bord, les neuf boutons, le fichier de diagnostic
 *    (ADR-018, important-10) ;
 *  - les SIX pages expertes en demandent un, et n'appellent RIEN avant de l'avoir : un écran
 *    qui aurait lu `GET /admin/api/config` pour préparer le terrain aurait déjà pris un 401.
 *
 * Le mot de passe voyage dans un cookie `HttpOnly` : le front ne peut pas le lire, donc la
 * frontière ne peut pas être vérifiée « côté état ». Elle se vérifie sur les ROUTES appelées,
 * qui est ce que le service, lui, protège vraiment.
 */

/** Les routes que le service protège (§14.5). Aucune ne doit être appelée sans session. */
const GUARDED = [
  '/admin/api/config',
  '/admin/api/config/versions',
  '/admin/api/ports',
  '/admin/api/printers',
  '/admin/api/journal',
  '/admin/api/technical',
  '/admin/api/imports',
  '/admin/api/replay',
]

/** Le bon mot de passe de ce banc. Le service, lui, ne connaît que son empreinte argon2id. */
const PASSWORD = 'le bon mot de passe'

let host: HTMLElement
let component: unknown
/** Toutes les requêtes passées, dans l'ordre : c'est la preuve que ce test cherche. */
let calls: { route: string; method: string }[] = []
/** Vrai quand le service a délivré une session : le cookie est simulé par ce drapeau. */
let session = false

beforeEach(() => {
  calls = []
  session = false
  host = document.createElement('div')
  document.body.appendChild(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/**
 * Le service, réduit à ce que ce test doit trancher : qui répond 401 et qui ne le fait pas.
 */
function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  const method = init?.method ?? 'GET'
  calls.push({ route, method })

  if (route === '/admin/api/health') return json(nominalHealth())
  if (route === '/admin/api/session' && method === 'POST') {
    const body = JSON.parse(String(init?.body ?? '{}')) as { password?: string }
    if (body.password !== PASSWORD) return refusal(401, 'Mot de passe incorrect.')
    session = true
    return json({ expires_at: '2026-07-24T12:30:00.000Z', session_minutes: 30 })
  }
  if (route.startsWith('/admin/api/troubleshooting/')) {
    return json({ done: true, message: 'C’est fait.' })
  }
  if (GUARDED.some((guarded) => route.startsWith(guarded))) {
    if (!session) return refusal(401, 'Cette adresse demande une session ouverte.')
    if (route.startsWith('/admin/api/config')) {
      return json({
        config: { station: { number: 2 } },
        config_fingerprint: 'a1b2c3d4',
        retired_keys: [],
        pending_confirmation: null,
      })
    }
    return json({ ports: [], printers: [], weighings: [], entries: [], versions: [] })
  }
  return json({})
}

/** Une réponse 200 portant un corps JSON. */
function json(body: unknown): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

/** Un refus, dans la forme exacte de `problem` (`internal/web/server.go`). */
function refusal(status: number, message: string): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify({ code: '', message }), { status }))
}

/** Monte l'écran et attend que le tableau de bord soit vraiment dessiné. */
async function open(): Promise<void> {
  component = mount(App, { target: host, props: {} })
  flushSync()
  await vi.waitUntil(() => {
    flushSync()
    return host.querySelectorAll('[data-light]').length > 0
  })
}

/**
 * Laisse ce qui est en vol se terminer, puis met le DOM à jour.
 *
 * Trois cycles de macrotâches : un appel traverse `fetch`, la lecture du corps, l'analyse
 * JSON et parfois une deuxième lecture derrière. Aucune horloge métier n'est en jeu ici —
 * la règle « aucun test ne dort » porte sur l'horloge injectée du poste, pas sur le tour de
 * boucle d'événements d'un navigateur simulé.
 */
async function settle(): Promise<void> {
  for (let round = 0; round < 3; round += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
    flushSync()
  }
}

/** Le bouton dont le libellé contient ce fragment. */
function button(fragment: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  return found as HTMLButtonElement
}

/** Touche un bouton et laisse ce qu'il déclenche se terminer. */
async function press(fragment: string): Promise<void> {
  button(fragment).click()
  await settle()
}

/** Les routes appelées jusqu'ici. */
function routes(): string[] {
  return calls.map((call) => call.route)
}

/** Tape un mot de passe dans le champ nommé et soumet le formulaire. */
async function submitPassword(id: string, value: string): Promise<void> {
  const field = host.querySelector<HTMLInputElement>(`#${id}`)
  expect(field, `le champ ${id} manque`).not.toBeNull()
  if (field === null) return
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
  field.form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await settle()
}

describe('mode bénévole : deux pages, ouvertes par défaut, SANS mot de passe', () => {
  it('ouvre le tableau de bord et n’appelle que la route non authentifiée', async () => {
    await open()

    expect(host.querySelectorAll('[data-light]')).toHaveLength(6)
    expect(routes()).toEqual(['/admin/api/health'])
    expect(host.querySelector('input[type="password"]')).toBeNull()
  })

  it('ouvre le dépannage sans mot de passe, et ses boutons ne passent que par /troubleshooting/', async () => {
    await open()
    await press('Dépannage')

    expect(host.querySelector('input[type="password"]')).toBeNull()
    await press('J’ai changé le rouleau')

    expect(routes()).toContain('/admin/api/troubleshooting/roll-changed')
    expect(routes()).not.toContain('/admin/api/session')
    expect(host.querySelector('[data-notice]')?.textContent).toBe('C’est fait.')
  })

  it('offre les neuf boutons de §14.4, plus le fichier de diagnostic', async () => {
    await open()
    await press('Dépannage')

    for (const label of [
      'Tester la balance',
      'Tester l’imprimante',
      'Imprimer une étiquette de test',
      'Réimprimer la dernière',
      'Recharger le catalogue',
      'Basculer en saisie manuelle',
      'J’ai changé le rouleau',
      'Choisir un fichier',
    ]) {
      expect((host.textContent ?? '').includes(label), label).toBe(true)
    }
    const archive = host.querySelector<HTMLAnchorElement>('a[download]')
    expect(archive?.getAttribute('href')).toBe('/admin/api/diagnostic.zip')
  })

  it('n’offre « Imprimer sur l’imprimante du poste voisin » que si un secours est configuré', async () => {
    await open()
    await press('Dépannage')
    expect(host.textContent).not.toContain('imprimante du poste voisin')

    // Le même écran, sur un poste dont la configuration déclare une imprimante de secours.
    vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/admin/api/health') {
        return json(
          nominalHealth({
            printing: {
              fallback_available: true,
              on_fallback: false,
              name: 'Étiqueteuse 2',
              banner: '',
            },
          }),
        )
      }
      return fakeFetch(input, init)
    })
    unmount(component as Parameters<typeof unmount>[0])
    component = undefined
    await open()
    await press('Dépannage')
    expect(host.textContent).toContain('imprimante du poste voisin')
  })
})

describe('mode expert : six pages, derrière le mot de passe', () => {
  it('demande le mot de passe et n’appelle AUCUNE route protégée avant de l’avoir', async () => {
    await open()
    await press('Réglages avancés')

    expect(host.querySelector('input[type="password"]')).not.toBeNull()
    for (const guarded of GUARDED) {
      expect(routes().some((route) => route.startsWith(guarded)), guarded).toBe(false)
    }
  })

  it('refuse un mauvais mot de passe avec la phrase du service, et reste fermé', async () => {
    await open()
    await press('Réglages avancés')
    await submitPassword('admin-password', 'au hasard')

    expect(host.querySelector('[data-failure]')?.textContent).toBe('Mot de passe incorrect.')
    expect(host.querySelector('input[type="password"]')).not.toBeNull()
    expect(routes()).not.toContain('/admin/api/config')
  })

  it('ouvre les six pages une fois la session obtenue, et lit alors la configuration', async () => {
    await open()
    await press('Réglages avancés')
    await submitPassword('admin-password', PASSWORD)

    expect(host.querySelector('input[type="password"]')).toBeNull()
    expect(routes()).toContain('/admin/api/config')
    for (const tab of ['Matériel', 'Étiquette', 'Règles', 'Catalogue', 'Journal', 'Poste']) {
      expect((host.textContent ?? '').includes(tab), tab).toBe(true)
    }
  })

  it('redemande le mot de passe quand la session expire sous les doigts', async () => {
    await open()
    await press('Réglages avancés')
    await submitPassword('admin-password', PASSWORD)

    // Trente minutes passent : le service refuse, et un tableau vide sans explication se
    // lirait comme « il n'y a rien », qui est faux.
    session = false
    await press('Journal')

    expect(host.querySelector('input[type="password"]')).not.toBeNull()
    expect(host.querySelector('[data-failure]')?.textContent).toBe(
      'Cette adresse demande une session ouverte.',
    )
  })

  it('propose le code de secours, parce qu’un poste en kiosque n’a pas d’invite de commande', async () => {
    await open()
    await press('Réglages avancés')
    await press('Mot de passe oublié')

    expect(host.querySelector('#recovery-code')).not.toBeNull()
    expect(host.querySelector('#recovery-password')).not.toBeNull()
  })
})
