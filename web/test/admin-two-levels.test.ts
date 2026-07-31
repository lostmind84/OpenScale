import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../src/admin/App.svelte'
import { CODE_NO_PASSWORD } from '../src/admin/lib/api'
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

/**
 * Ce que le service OUVRE en lecture depuis ADR-033. Aucune de ces routes ne demande
 * de session : la charge utile est expurgée de ses deux empreintes avant de partir.
 */
const OPEN = [
  '/admin/api/config',
  '/admin/api/config/versions',
  '/admin/api/ports',
  '/admin/api/printers',
  '/admin/api/journal',
  '/admin/api/technical',
  '/admin/api/imports',
]

/** Ce qui reste protégé : l'ACTE, jamais le regard. */
const GUARDED = ['/admin/api/replay', '/admin/api/catalog/import']

/** Le bon mot de passe de ce banc. Le service, lui, ne connaît que son empreinte argon2id. */
const PASSWORD = 'le bon mot de passe'

let host: HTMLElement
let component: unknown
/** Toutes les requêtes passées, dans l'ordre : c'est la preuve que ce test cherche. */
let calls: { route: string; method: string }[] = []
/** Vrai quand le service a délivré une session : le cookie est simulé par ce drapeau. */
let session = false
/** Vrai quand ce poste n'a JAMAIS eu de mot de passe : le service répond alors 409. */
let noPassword = false

beforeEach(() => {
  calls = []
  session = false
  noPassword = false
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
  // L'écriture est protégée, la lecture ne l'est pas (ADR-033).
  const writes = init?.method !== undefined && init.method !== 'GET'
  if (writes || GUARDED.some((guarded) => route.startsWith(guarded))) {
    if (noPassword) {
      // Avec son CODE : c'est lui, et non le statut, qui distingue « ce poste n'a jamais
      // eu de mot de passe » des autres 409 du service (compte à rebours armé, poste
      // occupé). Un banc qui l'omettrait ferait passer la lecture fautive.
      return refusal(409, 'Ce poste n’a pas encore de mot de passe. Saisissez le code de ' +
        'secours de la fiche d’installation pour en poser un.', CODE_NO_PASSWORD)
    }
    if (!session) return refusal(401, 'Cette adresse demande une session ouverte.')
  }
  if (OPEN.some((open) => route.startsWith(open)) || writes) {
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
function refusal(status: number, message: string, code = ''): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify({ code, message }), { status }))
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

/**
 * Modifie un champ de la page ouverte, pour armer le bouton d'enregistrement.
 *
 * Il est désarmé tant que rien n'a bougé — « Aucune modification à enregistrer » — et un
 * test qui le presserait sans avoir rien touché ne mesurerait que ce désarmement.
 */
async function changeAField(): Promise<void> {
  const field = host.querySelector<HTMLInputElement>('main input[type="text"], main input:not([type])')
  expect(field, 'aucun champ à modifier sur cette page').not.toBeNull()
  if (field === null) return
  field.value = 'COM9'
  field.dispatchEvent(new Event('input', { bubbles: true }))
  field.dispatchEvent(new Event('change', { bubbles: true }))
  await settle()
}

/** Les routes appelées jusqu'ici. */
function routes(): string[] {
  return calls.map((call) => call.route)
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

describe('les six pages de réglages : ouvertes en lecture, protégées à l’écriture (ADR-033)', () => {
  it('ouvre une page de réglages SANS rien demander, et lit la configuration', async () => {
    await open()
    await press('Matériel')

    // Il n'y a plus de porte : la configuration est lue, et elle est expurgée de ses
    // deux empreintes avant de partir — un mot de passe n'y gardait rien.
    expect(host.querySelector('input[type="password"]')).toBeNull()
    expect(routes()).toContain('/admin/api/config')
  })

  it('montre les neuf pages d’emblée, en deux groupes et sans « Réglages avancés »', async () => {
    await open()

    const rail = host.querySelector('.rail')?.textContent ?? ''
    for (const page of ['Tableau de bord', 'Dépannage', 'Matériel', 'Étiquette', 'Règles',
      'Catalogue', 'Journal', 'Poste']) {
      expect(rail.includes(page), page).toBe(true)
    }
    expect(rail).toContain('Au quotidien')
    expect(rail).toContain('Réglages')
    // La porte a disparu : plus rien à trouver à l'aveugle.
    expect(rail).not.toContain('Réglages avancés')
  })

  it('demande le mot de passe au moment d’ENREGISTRER, et pas avant', async () => {
    await open()
    await press('Matériel')
    expect(host.querySelector('input[type="password"]')).toBeNull()

    await changeAField()
    session = false
    await press('Enregistrer la configuration')

    // Le panneau s'ouvre parce qu'on a agi, pas parce qu'on est entré.
    expect(host.querySelector('input[type="password"]')).not.toBeNull()
  })

  it('propose le code de secours, parce qu’un poste en kiosque n’a pas d’invite de commande', async () => {
    await open()
    await press('Matériel')

    // Le poste répond 409 : il n'a jamais eu de mot de passe. Il n'y a rien à taper
    // qu'un code de secours, celui de la fiche d'installation.
    await changeAField()
    session = false
    noPassword = true
    await press('Enregistrer la configuration')

    expect((host.textContent ?? '')).toContain('code de secours')
    expect(host.querySelectorAll('input').length).toBeGreaterThanOrEqual(2)
  })
})
