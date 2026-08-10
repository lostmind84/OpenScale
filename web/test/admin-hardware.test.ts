import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Hardware from '../src/admin/pages/Hardware.svelte'
import type { HealthDTO } from '../src/admin/lib/dto'
import { Draft } from '../src/admin/lib/draft.svelte'
import { Admin } from '../src/admin/lib/session.svelte'
import { nominalHealth, nominalState } from './fixtures/health'

/**
 * La page Matériel de §14.4, et ce qu'elle disait de faux ou pas du tout.
 *
 * Elle est la page de la MISE EN SERVICE : c'est devant elle qu'un bénévole est assis le
 * jour où le poste arrive, avec un câble dans une main et le téléphone dans l'autre. Ce
 * que ce fichier tient :
 *
 *  - les 20 dernières trames s'affichent SANS qu'on les demande, en hexa et en clair
 *    (« toujours actif : ce n'est plus un réglage ») — mais SEULEMENT sur un port que le
 *    poste a vraiment énuméré, et jamais sur une frappe partielle du champ « Port série » ;
 *  - l'écoute REND LE PORT avant qu'un balayage ou un auto-test ne parte : sur un poste
 *    Windows le port est exclusif, et se le disputer fait échouer la détection sur le seul
 *    port où il y avait quelque chose à trouver ;
 *  - « Détecter automatiquement » annonce où il en est et ne se laisse pas relancer ;
 *  - un port qui REFUSE laisse une ligne, au lieu d'être avalé en silence ;
 *  - un port RECONNU offre le geste qui met cette balance en service, et un port muet
 *    n'offre rien : la détection sait quel protocole a répondu, et le faire retaper de
 *    mémoire laissait en configuration d'usine un poste dont la balance venait d'être
 *    trouvée ;
 *  - les actes PROTÉGÉS — détection, recherche d'imprimante, auto-tests — demandent le mot
 *    de passe et sont rejoués, au lieu de laisser un bandeau sans porte ;
 *  - `printer.health` n'atteint jamais l'écran en anglais ;
 *  - tant que la configuration n'est pas arrivée, la page n'affirme rien — ni la case à
 *    cocher, ni la légende des trames ;
 *  - un volet ouvert à la main RESTE OUVERT sous le sondage d'état, qui repasse toutes
 *    les trois secondes et emportait avec lui les deux seuls champs de l'écran qui
 *    nomment un port série.
 */

/** Une trame GRAM telle qu'elle sort du câble, STX compris. */
const FRAME = '\u0002ST,GS,+  1.234kg\u0003'

/** Le souffle entre deux manches d'écoute, tel que la page le tient (§14.4). */
const PAUSE_MS = 250

/** La configuration d'un poste dont la balance est branchée sur COM8. */
const CONFIG = {
  scale: { type: 'gram_xfoc', present: true, options: { port: 'COM8', baud_rate: 9600 } },
  printer: { type: 'raster', options: { transport: 'winspool', queue: 'Étiqueteuse' } },
}

let host: HTMLElement
let component: unknown
let admin: Admin
let draft: Draft
/** Toutes les requêtes passées, dans l'ordre : « méthode route ». */
let calls: string[] = []
/** Les ports que le poste énumère. */
let ports: { name: string; description: string; vid: string; pid: string }[] = []
/**
 * Les files d'impression que la plateforme déclare.
 *
 * `key` est la clé de `printer.options` dans laquelle la destination va, et c'est
 * l'ÉNUMÉRATION qui le dit : une file Windows va dans `queue`, un nœud d'impression dans
 * `path`, un hôte trouvé sur le port 9100 dans `address`. Rien dans le nom ne les
 * distingue.
 */
let printers: { name: string; key: string; detail: string; default: boolean }[] = []
/** Ce que « Rechercher l'imprimante » rapporte du réseau : des ADRESSES. */
let discovered: { name: string; key: string; detail: string; default: boolean }[] = []
/**
 * Ce que la PREMIÈRE capture répond ; les manches suivantes ne rendent rien.
 *
 * Une balance émet en continu, mais un test qui compterait des trames arrivant quatre
 * fois par seconde ne mesurerait que sa propre lenteur.
 */
let heard: string[] = []
/** Les ports sur lesquels une capture a VRAIMENT été demandée, dans l'ordre. */
let capturedPorts: string[] = []
/** Combien de captures sont en vol, et combien l'étaient quand une détection est partie. */
let capturesInFlight = 0
let capturesDuringDetect = 0
/**
 * Ce qu'un port répond à la détection : le rapport du service, ou le refus qu'il oppose.
 *
 * `driver` et `frames` ne sont là QUE lorsque les parseurs ont reconnu quelque chose —
 * c'est ce que `cmd/openscale/detect.go` met dans le rapport, et un port qui a parlé sans
 * être compris n'en porte aucun des deux. Un banc qui nommerait un driver sur tous les
 * ports ne verrait plus la seule différence qui compte à l'écran.
 */
interface Answer {
  status: number
  message: string
  /** Le driver qui a reconnu ce qui sortait du câble, tel que `scale.type` l'attend. */
  driver?: string
  /** Combien de trames valides la fenêtre de détection a comptées. */
  frames?: number
}

/** Ce que chaque port répond à la détection, ou le refus qu'il oppose. */
let answers = new Map<string, Answer>()
/** Vrai quand le service exige une session : tout acte protégé prend un 401. */
let guarded = false
/** Les appels mis en attente, quand un test veut regarder pendant qu'ils sont en vol. */
let held: (() => void)[] = []
let heldCaptures: (() => void)[] = []
let heldTests: (() => void)[] = []
/** Vrai quand l'appel correspondant doit rester en vol jusqu'à ce que le test le libère. */
let holding = false
let holdingCaptures = false
let holdingTests = false
/** Le tableau de bord que le poste sert au sondage, tel que le test l'a voulu. */
let served: HealthDTO = nominalHealth()

beforeEach(() => {
  calls = []
  served = nominalHealth()
  ports = [{ name: 'COM8', description: 'USB-Serial CH340', vid: '1A86', pid: '7523' }]
  printers = []
  discovered = []
  heard = []
  capturedPorts = []
  capturesInFlight = 0
  capturesDuringDetect = 0
  answers = new Map()
  guarded = false
  held = []
  heldCaptures = []
  heldTests = []
  holding = false
  holdingCaptures = false
  holdingTests = false
  host = document.createElement('div')
  document.body.appendChild(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  // Rien ne doit rester en vol : une capture tenue après le démontage garde un port.
  holding = false
  holdingCaptures = false
  holdingTests = false
  for (const release of [...held, ...heldCaptures, ...heldTests]) release()
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/**
 * Le service, réduit aux routes que cette page touche.
 *
 * `guarded` vaut pour les CINQ routes protégées de `internal/web/server.go` — capture,
 * détection, recherche d'imprimante et auto-tests — et pour elles seules : c'est cette
 * frontière-là que la page doit respecter, pas une frontière de confort.
 */
async function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  const method = init?.method ?? 'GET'
  calls.push(`${method} ${route}`)
  const body = JSON.parse(String(init?.body ?? '{}')) as { port?: string }

  if (route === '/admin/api/session' && method === 'POST') {
    guarded = false
    return json({ expires_at: '', session_minutes: 30 })
  }
  if (route === '/admin/api/config') {
    return json({
      config: CONFIG,
      config_fingerprint: 'a1b2c3d4',
      retired_keys: [],
      pending_confirmation: null,
    })
  }
  // Le sondage d'état, celui que `Admin.start` rejoue toutes les trois secondes. Il
  // repasse par `json`, donc par une sérialisation : chaque tour rend un objet NEUF,
  // exactement comme le poste. Un objet mémorisé et resservi ne changerait pas d'identité,
  // n'invaliderait aucune dérivée, et ce banc ne verrait plus rien de ce qui suit.
  if (route === '/admin/api/health') return json(served)
  if (route === '/admin/api/ports') return json({ ports })
  if (route === '/admin/api/printers') return json({ printers })
  if (route === '/admin/api/printers/discover') {
    if (guarded) return refusal(401, 'Cette adresse demande une session ouverte.')
    return json({ printers: discovered })
  }
  if (route.startsWith('/admin/api/printer/test')) {
    if (guarded) return refusal(401, 'Cette adresse demande une session ouverte.')
    if (holdingTests) await new Promise<void>((resolve) => heldTests.push(resolve))
    return json({ message: 'L’auto-test est parti à l’imprimante.' })
  }
  if (route === '/admin/api/scale/capture') return capture(body.port ?? '')
  if (route === '/admin/api/scale/detect') {
    capturesDuringDetect = Math.max(capturesDuringDetect, capturesInFlight)
    if (guarded) return refusal(401, 'Cette adresse demande une session ouverte.')
    if (holding) await new Promise<void>((resolve) => held.push(resolve))
    const answer: Answer = answers.get(body.port ?? '') ?? { status: 200, message: 'aucune trame.' }
    if (answer.status !== 200) return refusal(answer.status, answer.message)
    const valid = answer.frames ?? 0
    return json({
      port: body.port ?? '',
      driver: answer.driver ?? '',
      valid_frames_count: valid,
      frames: valid > 0 ? [FRAME] : [],
      message: answer.message,
    })
  }
  return json({})
}

/** Une capture, qui TIENT le port tant qu'elle est en vol. */
async function capture(port: string): Promise<Response> {
  capturedPorts.push(port)
  if (guarded) return refusal(401, 'Cette adresse demande une session ouverte.')
  capturesInFlight += 1
  try {
    if (holdingCaptures) await new Promise<void>((resolve) => heldCaptures.push(resolve))
    const frames = heard
    heard = []
    return json({ frames })
  } finally {
    capturesInFlight -= 1
  }
}

/** Une réponse 200 portant un corps JSON. */
function json(body: unknown): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

/** Un refus, dans la forme exacte de `problem` (`internal/web/server.go`). */
function refusal(status: number, message: string): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify({ code: '', message }), { status }))
}

/**
 * Monte la page Matériel.
 *
 * Le prop `health` est BRANCHÉ sur le `$state` que le sondage remplace, par un accesseur,
 * exactement comme `App.svelte` écrit `health={admin.health}`. Un objet figé — ce que ce
 * banc passait — ne rejoue jamais l'effet de gabarit de la page : la page était montée
 * dans un poste sans horloge, et tout ce que le sondage abîme restait invisible.
 *
 * @param health - le tableau de bord servi par le poste.
 * @param readConfig - faux pour observer la page AVANT que la configuration n'arrive.
 */
async function open(health: HealthDTO = nominalHealth(), readConfig = true): Promise<void> {
  admin = new Admin(60_000)
  draft = new Draft(admin)
  served = health
  if (readConfig) await draft.load()
  await admin.refresh()
  component = mount(Hardware, {
    target: host,
    props: {
      admin,
      draft,
      get health(): HealthDTO {
        return admin.health ?? health
      },
    },
  })
  flushSync()
  await settle()
}

/** Un tour du sondage d'état, celui que la page subit toutes les trois secondes. */
async function poll(): Promise<void> {
  await admin.refresh()
  flushSync()
}

/** Laisse ce qui est en vol se terminer, puis met le DOM à jour. */
async function settle(rounds = 4): Promise<void> {
  for (let round = 0; round < rounds; round += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
    flushSync()
  }
}

/** Attend qu'une condition devienne vraie, en redessinant à chaque essai. */
async function waitFor(what: () => boolean): Promise<void> {
  await vi.waitUntil(
    () => {
      flushSync()
      return what()
    },
    { timeout: 2000, interval: 5 },
  )
}

/** Le texte de la page, espaces normalisés. */
function text(): string {
  return (host.textContent ?? '').replace(/\s+/gu, ' ')
}

/** La légende du visualiseur de trames : la première phrase de l'encadré. */
function caption(): string {
  return (host.querySelector('[data-frames] p')?.textContent ?? '').replace(/\s+/gu, ' ')
}

/** Le bouton dont le libellé contient ce fragment. */
function button(fragment: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  return found as HTMLButtonElement
}

/** Le champ de la configuration que ce chemin de clé désigne. */
function field(path: string): HTMLInputElement {
  const id = 'field-' + path.replace(/\./gu, '-')
  const found = host.querySelector<HTMLInputElement>('#' + id)
  if (found === null) throw new Error(`aucun champ « ${path} » à l'écran`)
  return found
}

/** La liste déroulante que ce chemin de clé désigne. */
function choice(path: string): HTMLSelectElement {
  const id = 'field-' + path.replace(/\./gu, '-')
  const found = host.querySelector<HTMLSelectElement>('select#' + id)
  if (found === null) throw new Error(`aucune liste « ${path} » à l'écran`)
  return found
}

/** Vrai quand ce chemin de clé porte une commande à l'écran, quelle qu'elle soit. */
function shown(path: string): boolean {
  return host.querySelector('#field-' + path.replace(/\./gu, '-')) !== null
}

/**
 * Choisit une valeur dans une liste déroulante, comme un doigt le fait.
 *
 * L'événement `change` et non une écriture directe : c'est celui que le navigateur émet, et
 * c'est la voie de retour que la page écoute.
 *
 * @param path - le chemin de la clé que la liste règle.
 * @param value - la valeur à choisir.
 */
async function pick(path: string, value: string): Promise<void> {
  const list = choice(path)
  list.value = value
  // `bubbles` n'est pas une précaution : Svelte 5 délègue `change` à la racine, et un
  // événement qui ne remonte pas n'atteint jamais le gestionnaire.
  list.dispatchEvent(new Event('change', { bubbles: true }))
  await settle()
}

/** Combien d'appels ont été passés sur cette route. */
function countOf(fragment: string): number {
  return calls.filter((line) => line.includes(fragment)).length
}

/** Le volet replié dont le titre porte ce fragment. */
function folded(fragment: string): HTMLDetailsElement {
  const found = [...host.querySelectorAll('details')].find((candidate) =>
    (candidate.querySelector('summary')?.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun volet « ${fragment} » à l'écran`)
  return found
}

/**
 * Touche le titre d'un volet, comme un doigt le fait.
 *
 * Le clic et non `details.open = true` : c'est le navigateur qui bascule l'attribut, puis
 * l'événement `toggle` qui repart vers la page — et cette voie de retour est précisément
 * celle que le correctif rétablit. Poser l'attribut à la main la court-circuiterait et
 * ferait passer le banc pour la mauvaise raison. `settle` laisse passer la macrotâche sur
 * laquelle jsdom émet `toggle`.
 *
 * @param volet - le volet à toucher.
 */
async function tap(volet: HTMLDetailsElement): Promise<void> {
  volet.querySelector('summary')?.click()
  await settle()
}

describe('le visualiseur des 20 dernières trames, TOUJOURS actif (§14.4)', () => {
  it('écoute le port sans qu’on le lui demande, et rend hexa ET décodé', async () => {
    heard = [FRAME]
    await open()
    await waitFor(() => host.querySelectorAll('[data-frame]').length > 0)

    // Personne n'a rien touché : la capture est partie toute seule.
    expect(calls).toContain('POST /admin/api/scale/capture')
    const row = host.querySelector('[data-frame]')
    // 02 = STX, 53 54 = « ST ». L'hexa est celui des octets, pas une paraphrase.
    expect(row?.querySelector('[data-hex]')?.textContent).toContain('02 53 54')
    // Le décodé montre les mêmes octets tels qu'un humain les lit, caractères de
    // commande NOMMÉS : c'est le « hexa + ASCII » de `openscale capture` (§15.1).
    const decoded = (row?.querySelector('[data-decoded]')?.textContent ?? '').replace(
      /\s+/gu,
      ' ',
    )
    expect(decoded).toContain('ST,GS,+ 1.234kg')
    expect(decoded).toContain('STX')
  })

  it('n’en garde JAMAIS plus de vingt, quoi qu’en serve le poste', async () => {
    heard = Array.from({ length: 26 }, (_, index) => `ST,GS,+  ${String(index)}.000kg`)
    await open()
    await waitFor(() => host.querySelectorAll('[data-frame]').length > 0)

    expect(host.querySelectorAll('[data-frame]').length).toBe(20)
    // Une liste bornée ANNONCE son plafond, sans quoi elle ment par omission.
    expect(text()).toContain('20')
  })

  it('accorde sa légende au singulier quand UNE seule trame est arrivée', async () => {
    // Le cas normal de la mise en service : une balance qui n'émet qu'au posé de sac
    // rend une trame par manche, et « Les 1 dernières trames » est du charabia.
    heard = [FRAME]
    await open()
    await waitFor(() => host.querySelectorAll('[data-frame]').length === 1)

    expect(caption()).not.toContain('Les 1 ')
    expect(caption()).toContain('Une seule trame')
  })

  it('n’écoute QUE des ports énumérés : une frappe partielle n’en ouvre aucun', async () => {
    heard = [FRAME]
    await open()
    await waitFor(() => host.querySelectorAll('[data-frame]').length === 1)

    // `Field` émet à chaque caractère : « C » est un état que la saisie traverse
    // toujours en route vers « COM3 ». Ouvrir un port sur ce nom-là est un refus
    // certain, et il figeait l'écoute « toujours active » pour de bon.
    const port = field('scale.options.port')
    port.value = 'C'
    port.dispatchEvent(new Event('input', { bubbles: true }))

    await waitFor(() => caption().includes('n’est pas visible'))
    // Plus longtemps que le souffle entre deux manches : rien ne doit repartir.
    await new Promise((resolve) => setTimeout(resolve, PAUSE_MS + 150))
    expect(capturedPorts).not.toContain('C')
    expect(capturedPorts.every((name) => name === 'COM8')).toBe(true)
  })

  it('ne dit rien du port écouté tant que la configuration n’est pas lue', async () => {
    // « Aucun port n'est indiqué » était affirmé pendant la lecture, à trois
    // centimètres de « cette page ne déclare rien de ce poste ».
    await open(nominalHealth(), false)

    expect(caption()).not.toContain('Aucun port n’est indiqué')
    expect(caption()).toContain('Lecture de la configuration')
    expect(capturedPorts).toHaveLength(0)
  })
})

describe('« Détecter automatiquement »', () => {
  it('annonce son avancement et se désarme pendant la boucle', async () => {
    ports = [
      { name: 'COM1', description: '', vid: '', pid: '' },
      { name: 'COM3', description: '', vid: '', pid: '' },
      { name: 'COM8', description: 'USB-Serial CH340', vid: '1A86', pid: '7523' },
    ]
    holding = true
    await open()

    button('Détecter automatiquement').click()
    await waitFor(() => held.length > 0)

    const armed = button('Détection')
    expect(armed.disabled).toBe(true)
    expect((armed.textContent ?? '').replace(/\s+/gu, ' ')).toContain('port 1 sur 3')

    held.shift()?.()
    await waitFor(() => held.length > 0)
    expect((button('Détection').textContent ?? '').replace(/\s+/gu, ' ')).toContain('port 2 sur 3')

    holding = false
    for (const release of held.splice(0)) release()
    await waitFor(() => host.querySelectorAll('[data-verdict]').length === 3)
    expect(button('Détecter automatiquement').disabled).toBe(false)
  })

  it('attend que l’écoute RENDE LE PORT avant d’en ouvrir un seul', async () => {
    // Sous Windows un port est exclusif : une capture en vol fait répondre « il est
    // déjà utilisé » au balayage, sur le seul port où il y avait quelque chose à
    // trouver. La manche dure trois secondes, le clic dure un dixième.
    holdingCaptures = true
    await open()
    await waitFor(() => heldCaptures.length === 1)

    button('Détecter automatiquement').click()
    await settle(6)
    expect(countOf('/admin/api/scale/detect')).toBe(0)
    expect((button('Détection').textContent ?? '')).toContain('port se libère')

    holdingCaptures = false
    for (const release of heldCaptures.splice(0)) release()
    await waitFor(() => host.querySelectorAll('[data-verdict]').length === 1)
    expect(capturesDuringDetect).toBe(0)
  })

  it('n’avale plus le refus d’un port : chacun laisse sa ligne', async () => {
    ports = [
      { name: 'COM1', description: '', vid: '', pid: '' },
      { name: 'COM8', description: 'USB-Serial CH340', vid: '1A86', pid: '7523' },
    ]
    answers.set('COM1', {
      status: 502,
      message: 'le port COM1 ne peut pas être ouvert : il est déjà utilisé.',
    })
    answers.set('COM8', { status: 200, message: 'COM8 : 12 trame(s) valide(s) — GRAM XFOC.' })
    await open()

    button('Détecter automatiquement').click()
    await waitFor(() => host.querySelectorAll('[data-verdict]').length === 2)

    expect(text()).toContain('il est déjà utilisé')
    expect(text()).toContain('COM8 : 12 trame(s) valide(s)')
  })

  it('est un acte PROTÉGÉ : le mot de passe est demandé, puis le balayage est rejoué', async () => {
    guarded = true
    answers.set('COM8', { status: 200, message: 'COM8 : 12 trame(s) valide(s) — GRAM XFOC.' })
    await open()

    button('Détecter automatiquement').click()
    await waitFor(() => admin.pending !== null)
    expect(admin.pending?.kind).toBe('password')

    await admin.answerPassword('openscale')
    await waitFor(() => host.querySelectorAll('[data-verdict]').length === 1)
    expect(text()).toContain('COM8 : 12 trame(s) valide(s)')
  })
})

/**
 * Ce que « Détecter automatiquement » laissait faire À LA MAIN.
 *
 * Sur un poste neuf, l'installation écrit `scale.present = false` et `scale.type = ""` —
 * la seule forme qui n'oppose pas « scale.options.port : option exigée par le driver » à
 * un poste qui n'a encore rien de branché. Remettre la balance en service demandait alors
 * trois gestes dont deux sont cachés : décocher la case, déplier les réglages série et
 * retaper le protocole DE MÉMOIRE. Or le rapport de détection NOMME ce protocole.
 */
describe('remettre en service une balance que la détection vient de reconnaître', () => {
  /** Ce qu'un port sur lequel une balance parle rend au balayage. */
  const RECOGNISED: Answer = {
    status: 200,
    message: 'COM8 : 12 trame(s) valide(s) en 3s — GRAM XFOC +, GRAM XFOC RS.',
    driver: 'gram-xfoc-plus',
    frames: 12,
  }
  /** Ce que rend un port où rien ne répond : une phrase, et aucun protocole. */
  const MUTE: Answer = {
    status: 200,
    message: 'COM1 : aucun octet reçu en 3s. La balance est-elle allumée ?',
  }

  /**
   * Joue un banc sur un poste tel que l'installation le laisse, puis rend la configuration
   * du fichier : les autres bancs de ce fichier partent d'un poste dont la balance est
   * déjà déclarée.
   *
   * @param banc - ce qu'il y a à observer, la page montée et le balayage passé.
   */
  async function fromFactory(banc: () => Promise<void> | void): Promise<void> {
    const declared = CONFIG.scale
    CONFIG.scale = { type: '', present: false, options: { port: '', baud_rate: 9600 } }
    try {
      ports = [
        { name: 'COM1', description: '', vid: '', pid: '' },
        { name: 'COM8', description: 'USB-Serial CH340', vid: '1A86', pid: '7523' },
      ]
      answers.set('COM1', MUTE)
      answers.set('COM8', RECOGNISED)
      await open()
      button('Détecter automatiquement').click()
      await waitFor(() => host.querySelectorAll('[data-verdict]').length === 2)
      await banc()
    } finally {
      CONFIG.scale = declared
    }
  }

  /** Le geste que la ligne de ce port porte, ou null quand elle n'en porte aucun. */
  function gestureOn(port: string): HTMLButtonElement | null {
    const row = [...host.querySelectorAll('[data-verdict]')].find(
      (candidate) => (candidate.querySelector('.what')?.textContent ?? '').trim() === port,
    )
    if (row === undefined) throw new Error(`aucun verdict pour ${port} à l'écran`)
    return row.querySelector('button')
  }

  /** La case « Ce poste n’a pas de balance », telle qu'elle est cochée à l'instant. */
  function declaredWithoutScale(): boolean {
    return host.querySelector<HTMLInputElement>('input[type="checkbox"]')?.checked ?? false
  }

  it('offre le geste sur le port RECONNU, et sur aucun autre', async () => {
    await fromFactory(() => {
      const offered = gestureOn('COM8')
      expect(offered, 'le port où la balance a répondu n’offre aucun geste').not.toBeNull()
      expect((offered?.textContent ?? '').replace(/\s+/gu, ' ')).toContain('Utiliser cette balance')

      // Un bouton inerte sur un port muet est pire que pas de bouton : il fait chercher
      // ce qui s'est cassé, là où l'absence dit que ce port n'a pas de balance.
      expect(gestureOn('COM1'), 'un port muet propose un geste qui ne peut rien régler').toBeNull()
    })
  })

  it('écrit les TROIS champs dans le brouillon, et rien ne part vers le poste', async () => {
    await fromFactory(async () => {
      gestureOn('COM8')?.click()
      await settle()

      expect(draft.flag('scale.present'), 'le poste se déclare toujours sans balance').toBe(true)
      // Le protocole que les TRAMES ont nommé, jamais la première entrée d'un registre.
      expect(draft.text('scale.type')).toBe('gram-xfoc-plus')
      expect(draft.text('scale.options.port')).toBe('COM8')
      // « Enregistrer » reste le seul geste de cette page qui parte vers le poste.
      expect(calls.filter((line) => line.startsWith('PUT'))).toEqual([])
    })
  })

  it('décoche « Ce poste n’a pas de balance »', async () => {
    await fromFactory(async () => {
      expect(declaredWithoutScale(), 'le poste d’usine ne se déclarait pas sans balance').toBe(true)

      gestureOn('COM8')?.click()
      await settle()

      expect(
        declaredWithoutScale(),
        'la case reste cochée sur un poste dont la balance vient d’être mise en service',
      ).toBe(false)
    })
  })

  it('reste désarmé tant que la configuration n’est pas lue', async () => {
    // `draft.set` jette EN SILENCE ce qu'on écrit dans un document qui n'est pas encore
    // là : le geste laisserait croire qu'il a réglé le poste, et il n'aurait rien réglé.
    ports = [{ name: 'COM8', description: 'USB-Serial CH340', vid: '1A86', pid: '7523' }]
    answers.set('COM8', RECOGNISED)
    await open(nominalHealth(), false)

    button('Détecter automatiquement').click()
    await waitFor(() => host.querySelectorAll('[data-verdict]').length === 1)

    expect(gestureOn('COM8')?.disabled).toBe(true)
  })
})

describe('les actes protégés de l’imprimante', () => {
  it('« Rechercher l’imprimante » demande le mot de passe et rejoue la recherche', async () => {
    // `POST /admin/api/printers/discover` est protégée : un bandeau « cette adresse
    // demande une session ouverte » sans porte pour la régler ne mène nulle part.
    guarded = true
    discovered = [{ name: '10.0.0.9:9100', key: 'address', detail: 'répond', default: false }]
    await open()
    // Le balayage rend des ADRESSES : la page ne les propose que sous le transport qui les
    // lit (ADR-025), et c'est celui-là qu'un exploitant qui cherche une imprimante réseau
    // vient de choisir.
    await tap(folded('Réglages de l’imprimante'))
    await pick('printer.options.transport', 'tcp')

    button('Rechercher l’imprimante').click()
    await waitFor(() => admin.pending !== null)
    expect(admin.pending?.kind).toBe('password')

    await admin.answerPassword('openscale')
    await waitFor(() => text().includes('10.0.0.9:9100'))
    expect(countOf('/admin/api/printers/discover')).toBe(2)
  })

  it('les auto-tests demandent le mot de passe et sortent l’étiquette une fois rejoués', async () => {
    guarded = true
    await open()

    button('Auto-test : étiquette').click()
    await waitFor(() => admin.pending !== null)

    await admin.answerPassword('openscale')
    await waitFor(() => countOf('/admin/api/printer/test?what=label') === 2)
    expect(admin.notice).toContain('auto-test')
  })

  it('désarme les commandes pendant qu’un auto-test sort une étiquette', async () => {
    // Chaque appui sort une étiquette pour de bon : deux appuis impatients, deux
    // étiquettes, et rien à l'écran ne disait que la première était partie.
    holdingTests = true
    await open()

    button('Auto-test : réglette').click()
    await waitFor(() => heldTests.length === 1)

    expect(button('Auto-test : réglette').disabled).toBe(true)
    expect(button('Lister les files').disabled).toBe(true)
    button('Auto-test : réglette').click()
    await settle()
    expect(countOf('/admin/api/printer/test')).toBe(1)

    holdingTests = false
    for (const release of heldTests.splice(0)) release()
    await waitFor(() => !button('Auto-test : réglette').disabled)
  })

  it('n’offre que les auto-tests que le driver en service honore', async () => {
    // La page portait les trois quel que soit le driver. Sur un poste en `preview` — celui
    // sur lequel une configuration d'usine se replie (§11.3) — « alignement » et
    // « réglette » répondaient un refus APRÈS le clic, devant quelqu'un qui cherchait déjà
    // pourquoi rien ne sort. Un bouton dont la seule réponse possible est un refus n'est
    // pas un choix (ADR-025).
    await open(nominalHealth({ printer_self_tests: ['label'] }))

    expect(button('Auto-test : étiquette')).toBeDefined()
    expect(() => button('Auto-test : alignement')).toThrow()
    expect(() => button('Auto-test : réglette')).toThrow()
    // Et le bouton absent se lit comme une déclaration, jamais comme une panne.
    expect(text()).toContain('ne les imprime pas')
  })

  it('dit qu’un poste sans auto-test n’en a aucun, au lieu de se taire', async () => {
    await open(nominalHealth({ printer_self_tests: [] }))

    expect(() => button('Auto-test :')).toThrow()
    expect(text()).toContain('n’imprime aucun auto-test')
  })

  it('ne sert JAMAIS trente files d’impression en toutes lettres', async () => {
    // `Field` imprime `allowed` en entier — « Valeurs acceptées : … » — dès qu'un
    // contrôle de §11.3 refuse la clé, et un poste porte PDF, OneNote, télécopie…
    printers = Array.from({ length: 30 }, (_, index) => ({
      name: `File ${String(index + 1)}`,
      key: 'queue',
      detail: '',
      default: false,
    }))
    await open()

    button('Lister les files').click()
    await waitFor(() => host.querySelector('[data-printers-count]') !== null)

    const offered = host.querySelectorAll('#field-printer-options-queue-allowed option')
    expect(offered.length).toBeGreaterThan(0)
    expect(offered.length).toBeLessThanOrEqual(12)
    expect(text()).toContain('seules les 12 premières')
  })
})

describe('ce que la page dit de l’imprimante et de la balance', () => {
  it('traduit printer.health : aucun jeton anglais n’atteint le bénévole', async () => {
    await open(
      nominalHealth({
        state: nominalState({
          printer: {
            health: 'consumable',
            detail: '',
            pending_jobs_count: 0,
            observed_at: '2026-07-24T12:00:00.000Z',
          },
        }),
      }),
    )

    expect(text()).not.toMatch(/\b(ready|consumable|faulted|unknown)\b/u)
    expect(text()).toContain('fin de vie')
  })

  it('n’invente aucun état tant que la configuration n’est pas arrivée', async () => {
    // `draft.flag('scale.present')` vaut FAUX avant la lecture, ce qui affichait
    // « ce poste n'a pas de balance » coché sur un poste qui en a une.
    await open(nominalHealth(), false)

    const box = host.querySelector<HTMLInputElement>('input[type="checkbox"]')
    expect(box === null || !box.checked).toBe(true)
    expect(text()).toContain('Lecture de la configuration')

    // Et les champs sont désarmés : `draft.set` jette en silence ce qu'on écrirait
    // dans un document qui n'est pas encore là.
    const fields = [...host.querySelectorAll<HTMLInputElement>('input[type="text"]')]
    expect(fields.length).toBeGreaterThan(0)
    expect(fields.every((field) => field.disabled)).toBe(true)
  })
})

/**
 * Les volets repliés, et le sondage d'état qui passe dessous toutes les trois secondes.
 *
 * L'ouverture d'un volet appartient au BÉNÉVOLE : c'est lui qui l'ouvre, c'est lui qui le
 * referme, et rien d'autre. La page l'ouvrait à sa place — et la rouvrait, et la
 * refermait — à chaque tour du tableau de bord, sous les doigts de quelqu'un qui tapait un
 * nom de port dedans. Les deux seuls champs qui nomment un port série vivent là.
 */
describe('le transport de l’imprimante, et où il fait écrire', () => {
  it('se choisit dans la liste que le POSTE déclare, jamais en texte libre', async () => {
    await open()
    await tap(folded('Réglages de l’imprimante'))
    const list = choice('printer.options.transport')
    const offered = [...list.options].map((option) => option.value)
    expect(offered).toEqual(['winspool', 'devfile', 'tcp', 'file'])
    // Le libellé est celui du poste, pas un mot que l'écran s'invente.
    expect(list.options[2]?.textContent).toContain('Imprimante réseau')
    expect(list.value).toBe('winspool')
  })

  it('montre le champ de l’appareil QUE le transport choisi lit', async () => {
    await open()
    await tap(folded('Réglages de l’imprimante'))
    expect(shown('printer.options.queue')).toBe(true)
    expect(shown('printer.options.address')).toBe(false)

    await pick('printer.options.transport', 'tcp')
    expect(draft.text('printer.options.transport')).toBe('tcp')
    // Le champ de la file DISPARAÎT : le laisser à l'écran sous un transport qui ne le lit
    // pas est exactement ce qui a fait saisir une adresse IP dans printer.options.queue.
    expect(shown('printer.options.queue')).toBe(false)
    expect(shown('printer.options.address')).toBe(true)
    expect(text()).toContain('Adresse réseau')

    await pick('printer.options.transport', 'devfile')
    expect(shown('printer.options.path')).toBe(true)
    expect(shown('printer.options.address')).toBe(false)
  })

  it('écrit dans la clé du transport choisi, et dans aucune autre', async () => {
    await open()
    await tap(folded('Réglages de l’imprimante'))
    await pick('printer.options.transport', 'tcp')
    const box = field('printer.options.address')
    box.value = '192.168.0.43:9100'
    box.dispatchEvent(new Event('input', { bubbles: true }))
    await settle()
    expect(draft.text('printer.options.address')).toBe('192.168.0.43:9100')
    // La file garde ce qu'elle avait : revenir à `winspool` ne doit pas coûter la saisie.
    expect(draft.text('printer.options.queue')).toBe('Étiqueteuse')
  })

  it('n’affiche pas une liste vide quand le poste ne déclare aucun transport', async () => {
    // C'est l'état d'un binaire sans registre de transports : la page retombe sur un champ
    // libre plutôt que d'offrir une liste déroulante sans une seule valeur dedans.
    await open(nominalHealth({ printer_transports: [] }))
    await tap(folded('Réglages de l’imprimante'))
    expect(() => choice('printer.options.transport')).toThrow()
    expect(field('printer.options.transport').value).toBe('winspool')
  })

  it('garde à l’écran une valeur que ce poste ne connaît pas, en le disant', async () => {
    // Un fichier écrit à la main, ou venu d'un binaire plus récent. Une liste qui se
    // rabattrait en silence sur son premier choix afficherait un transport que le poste
    // n'applique pas, et rien à l'écran ne dirait lequel tourne vraiment.
    CONFIG.printer.options.transport = 'smb'
    try {
      await open()
      await tap(folded('Réglages de l’imprimante'))
      const list = choice('printer.options.transport')
      expect(list.value).toBe('smb')
      expect(text()).toContain('inconnu de ce poste')
    } finally {
      CONFIG.printer.options.transport = 'winspool'
    }
  })
})

describe('les destinations listées, et la clé où chacune va', () => {
  it('écrit une file d’impression dans la file', async () => {
    printers = [{ name: 'SATO WS408_2', key: 'queue', detail: 'file locale', default: true }]
    await open()
    button('Lister les files').click()
    await waitFor(() => text().includes('SATO WS408_2'))
    button('SATO WS408_2').click()
    await settle()
    expect(draft.text('printer.options.queue')).toBe('SATO WS408_2')
  })

  it('écrit une imprimante réseau découverte dans l’ADRESSE, jamais dans la file', async () => {
    // Le défaut lui-même. « Rechercher l'imprimante » rend des hôtes qui répondent sur le
    // port 9100 ; la page les écrivait dans printer.options.queue, que seul `winspool` lit.
    // Le fichier était accepté — rien ne lie une clé à un transport — et le poste
    // n'imprimait pas, sans que l'écran ait dit un mot.
    discovered = [
      { name: '192.168.0.43:9100', key: 'address', detail: 'répond sur le port 9100', default: false },
    ]
    await open()
    await tap(folded('Réglages de l’imprimante'))
    await pick('printer.options.transport', 'tcp')
    button('Rechercher l’imprimante').click()
    await waitFor(() => text().includes('192.168.0.43:9100'))
    button('192.168.0.43:9100').click()
    await settle()
    expect(draft.text('printer.options.address')).toBe('192.168.0.43:9100')
    expect(draft.text('printer.options.queue')).toBe('Étiqueteuse')
  })

  it('n’offre pas une destination que le transport choisi ne peut pas ouvrir', async () => {
    // Même règle que pour les auto-tests (ADR-025) : un clic dont le seul résultat possible
    // est un poste qui n'imprime pas n'est pas un choix. Et l'écart se DIT — une liste qui
    // rétrécit en silence se lit comme une recherche qui n'a rien trouvé.
    discovered = [
      { name: '192.168.0.43:9100', key: 'address', detail: 'répond', default: false },
    ]
    await open()
    button('Rechercher l’imprimante').click()
    await waitFor(() => host.querySelector('[data-unreachable]') !== null)
    expect(text()).not.toContain('192.168.0.43:9100')
    expect(text()).toContain('Imprimante réseau, port 9100')
  })

  it('écrit un nœud d’impression dans le chemin', async () => {
    printers = [
      { name: '/dev/usb/lp0', key: 'path', detail: 'nœud d’impression', default: false },
    ]
    await open()
    await tap(folded('Réglages de l’imprimante'))
    await pick('printer.options.transport', 'devfile')
    button('Lister les files').click()
    // La LISTE, et non le texte de la page : l'aide du champ « Fichier de périphérique »
    // cite /dev/usb/lp0 elle aussi, et l'attente se terminait avant que la liste n'arrive.
    await waitFor(() => host.querySelector('[data-printers-count]') !== null)
    button('/dev/usb/lp0').click()
    await settle()
    expect(draft.text('printer.options.path')).toBe('/dev/usb/lp0')
    expect(draft.text('printer.options.queue')).toBe('Étiqueteuse')
  })
})

describe('les volets repliés de la page Matériel', () => {
  it('garde ouvert le volet des réglages série quand le sondage d’état passe', async () => {
    await open()
    const settings = folded('Réglages série de la balance')
    await tap(settings)
    expect(settings.open, 'le titre touché n’a pas ouvert le volet').toBe(true)

    await poll()
    await poll()

    expect(settings.open, 'le sondage d’état a refermé le volet ouvert à la main').toBe(true)
  })

  it('garde ouvert le volet des réglages de l’imprimante, qui est le même objet', async () => {
    // Les deux volets partagent l'effet de gabarit du fragment : ce qui referme l'un
    // referme l'autre, et un seul test ne prouverait rien du second.
    await open()
    const settings = folded('Réglages de l’imprimante')
    await tap(settings)
    expect(settings.open, 'le titre touché n’a pas ouvert le volet').toBe(true)

    await poll()

    expect(settings.open, 'le sondage d’état a refermé le volet ouvert à la main').toBe(true)
  })

  it('laisse le champ « Port série » atteignable après un tour de sondage', async () => {
    // C'est la conséquence qui compte pour le bénévole, et elle ne se déduit pas de
    // l'attribut : un volet refermé emporte le champ avec lui.
    await open()
    await tap(folded('Réglages série de la balance'))

    await poll()

    const port = field('scale.options.port')
    expect(host.contains(port), 'le champ a quitté la page').toBe(true)
    expect(
      port.closest('details')?.open,
      'le champ est retombé sous un volet que le sondage a refermé',
    ).toBe(true)
  })

  it('ouvre le volet tout seul quand un contrôle refuse un de ses champs', async () => {
    await open()
    const settings = folded('Réglages série de la balance')
    expect(settings.open, 'le volet était déjà ouvert alors que rien n’était refusé').toBe(false)

    draft.faults = [
      { field: 'scale.options.port', message: 'COM99 n’est pas visible depuis ce poste.' },
    ]
    flushSync()

    expect(
      settings.open,
      'un refus de contrôle n’a pas ouvert le volet qui porte le champ fautif',
    ).toBe(true)
  })

  it('laisse refermé un volet refermé à la main, même si le refus tient toujours', async () => {
    // Le même défaut par l'autre bout : un volet que la page rouvrirait à chaque tour
    // tant qu'un refus dure serait aussi impossible à refermer qu'il l'était à ouvrir.
    await open()
    const settings = folded('Réglages série de la balance')
    draft.faults = [
      { field: 'scale.options.port', message: 'COM99 n’est pas visible depuis ce poste.' },
    ]
    flushSync()
    expect(settings.open, 'le refus n’a pas ouvert le volet').toBe(true)

    await tap(settings)
    expect(settings.open, 'le titre touché n’a pas refermé le volet').toBe(false)

    await poll()

    expect(settings.open, 'le volet s’est rouvert tout seul alors que rien n’avait bougé').toBe(
      false,
    )
  })

  it('ne perd pas la saisie en cours quand le sondage passe', async () => {
    await open()
    await tap(folded('Réglages série de la balance'))
    const port = field('scale.options.port')
    port.value = 'COM3'
    port.dispatchEvent(new Event('input', { bubbles: true }))
    await settle()

    await poll()

    expect(field('scale.options.port').value, 'la saisie en cours a disparu au sondage').toBe(
      'COM3',
    )
    expect(
      field('scale.options.port').closest('details')?.open,
      'la saisie en cours est retombée sous un volet refermé',
    ).toBe(true)
  })
})
