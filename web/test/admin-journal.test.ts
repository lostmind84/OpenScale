import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Journal from '../src/admin/pages/Journal.svelte'
import type { TechnicalLineDTO, WeighingDTO } from '../src/admin/lib/dto'
import { preferences } from '../src/admin/lib/preferences.svelte'
import { Admin } from '../src/admin/lib/session.svelte'

/**
 * La page Journal de §14.4, et ce qu'elle faisait de travers.
 *
 * C'est la page du RAPPROCHEMENT et du diagnostic : on y vient parce qu'une caisse a
 * scanné une étiquette qui ne passe pas, ou parce qu'un client dit avoir pesé et n'avoir
 * rien eu. Ce que ce fichier tient :
 *
 *  - le tableau tient dans son propre conteneur défilant, en-tête figé — il faisait
 *    ~17 000 px de haut, et ses en-têtes quittaient l'écran après onze lignes ;
 *  - le détail s'ouvre SOUS LA LIGNE CLIQUÉE, et non sous la table entière ;
 *  - le filtre n'offre plus `printed`, qui n'existe nulle part dans le service et ne
 *    sélectionnait donc rien un jour où le poste avait imprimé toute la journée ;
 *  - `result`, `source` et `stability` n'atteignent jamais l'écran en anglais ;
 *  - « Rejouer cette trame » passe par `admin.protect` : le mot de passe est demandé à
 *    l'acte, et l'acte est REJOUÉ (ADR-033) ;
 *  - AUCUNE PHRASE DE CETTE PAGE N'AFFIRME CE QU'ELLE NE PEUT PAS TENIR — ni un plafond
 *    qui appartiendrait au poste, ni un export qui emporterait « les précédentes », ni
 *    une raison « écrite en haut de l'écran » qu'une lecture réussie vient d'effacer ;
 *  - et tant qu'une lecture est en vol, la page dit « je ne sais pas encore » plutôt que
 *    de laisser à l'écran les lignes, le décompte et l'export de la lecture d'avant.
 */

/** Le fichier de la page, lu tel quel : la mise en page est du CSS, pas du DOM. */
const HERE = dirname(fileURLToPath(import.meta.url))
const SOURCE = readFileSync(resolve(HERE, '../src/admin/pages/Journal.svelte'), 'utf8')
/** L'ossature, lue pour une seule raison : elle porte l'autre moitié d'un même nombre. */
const SHELL = readFileSync(resolve(HERE, '../src/admin/App.svelte'), 'utf8')

/** Une trame GRAM telle que le journal l'a enregistrée. */
const FRAME = 'ST,GS,+  1.236KG'

let host: HTMLElement
let component: unknown
let admin: Admin
/** Toutes les requêtes passées, dans l'ordre : « méthode route ». */
let calls: string[] = []
/** Ce que le poste répond sur le journal des pesées. */
let rows: WeighingDTO[] = []
/** Vrai tant que le rejeu doit répondre 401 : le premier acte protégé le prend. */
let guarded = false
/** Vrai quand ce poste n'a pas de journal du tout (`internal/web/admin.go`). */
let journalless = false
/** Vrai quand ce poste n'a pas non plus de journal TECHNIQUE : les deux peuvent refuser. */
let technicalless = false
/** Combien de lignes le journal technique répond. */
let technicalCount = 1
/** Combien de fois `POST /admin/api/replay` a été appelé. */
let replayCalls = 0
/** Ce qui retient la réponse du journal, quand un test veut REGARDER la lecture. */
let gate: Promise<void> | null = null

beforeEach(() => {
  calls = []
  rows = [weighing(1, 'sent'), weighing(2, 'rejected'), weighing(3, 'failed')]
  guarded = false
  journalless = false
  technicalless = false
  technicalCount = 1
  replayCalls = 0
  gate = null
  // La préférence est un singleton de module : un test qui la coche la laisserait cochée
  // pour tous les suivants, et « aucun jeton anglais n'atteint un bénévole » passerait
  // alors sur un écran qui les montre tous.
  preferences.showTechnicalNames = false
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
 * Une pesée journalisée, dans la forme exacte de `internal/web/admin.go`.
 *
 * @param id - l'identifiant de la ligne.
 * @param result - comment la pesée s'est terminée, dans le jeton du service.
 */
function weighing(id: number, result: string): WeighingDTO {
  return {
    id,
    occurred_at: '2026-07-24T09:02:11.000Z',
    station: 1,
    job_id: `01J-${String(id)}`,
    product_id: '894',
    product_name: 'POMME GOLDEN',
    reference: '0493894012365',
    mode: 'by_weight',
    gross_g: 1436,
    tare_g: 200,
    net_g: 1236,
    quantity: 1,
    barcode: '0493894012365',
    source: 'scale',
    stability: 'unstable',
    rate_ms: 400,
    frame: FRAME,
    result,
    detail: '',
    duration_ms: 2500,
    lines: [],
  }
}

/**
 * Une ligne du journal technique, dans la forme exacte de `internal/web/admin.go`.
 *
 * @param id - l'identifiant de la ligne.
 */
function technicalLine(id: number): TechnicalLineDTO {
  return {
    id,
    occurred_at: '2026-07-24T09:02:12.000Z',
    level: 'warn',
    source: 'scale',
    code: 'ERR-SCL-07',
    message: 'La balance a émis une trame illisible.',
    detail: '',
  }
}

/** Le service, réduit aux quatre routes que cette page touche. */
async function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  const method = init?.method ?? 'GET'
  calls.push(`${method} ${route}`)

  if (route === '/admin/api/session' && method === 'POST') {
    guarded = false
    return json({ expires_at: '', session_minutes: 30 })
  }
  if (route === '/admin/api/replay' && method === 'POST') {
    replayCalls += 1
    if (guarded) return refusal(401, 'Session expirée ou absente.')
    return json({ done: true, message: 'La trame a été rejouée.' })
  }
  if (route.startsWith('/admin/api/journal')) {
    if (gate !== null) await gate
    if (journalless) return unavailable('ce poste n’a pas de journal')
    return json({ weighings: rows })
  }
  if (route.startsWith('/admin/api/technical')) {
    if (technicalless) return unavailable('ce poste n’a pas de journal technique')
    return json({
      entries: Array.from({ length: technicalCount }, (_, index) => technicalLine(index + 1)),
    })
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

/**
 * Le refus d'une route que ce binaire n'a pas câblée, mot pour mot (`unavailable`).
 *
 * 501 et non 404 : la route EXISTE, c'est le poste qui n'a pas la capacité.
 *
 * @param what - ce qui manque à ce poste.
 */
function unavailable(what: string): Promise<Response> {
  return refusal(501, `Cette fonction n’est pas disponible sur ce poste : ${what}.`)
}

/**
 * Retient la prochaine réponse du journal des pesées.
 *
 * C'est la seule façon de regarder l'écran PENDANT qu'il lit — le moment où il n'a pas
 * encore le droit d'affirmer quoi que ce soit.
 *
 * @returns de quoi laisser partir la réponse.
 */
function holdJournal(): () => void {
  let release = (): void => {}
  gate = new Promise<void>((resolve) => {
    release = (): void => {
      gate = null
      resolve()
    }
  })
  return (): void => {
    release()
  }
}

/** Monte la page Journal et laisse ses deux lectures se terminer. */
async function open(): Promise<void> {
  admin = new Admin(60_000)
  component = mount(Journal, { target: host, props: { admin } })
  flushSync()
  await settle()
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

/**
 * Le texte d'un élément, espaces normalisés.
 *
 * La normalisation n'est pas un confort : `frenchInteger` pose des espaces INSÉCABLES,
 * et « 5 000 » tapé ici n'en est pas une.
 *
 * @param selector - ce qu'on veut lire.
 */
function textOf(selector: string): string {
  return (host.querySelector(selector)?.textContent ?? '').replace(/\s+/gu, ' ').trim()
}

/** Le bouton dont le libellé contient ce fragment. */
function button(fragment: string): HTMLButtonElement {
  const found = buttons(fragment)[0]
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  return found
}

/**
 * Tous les boutons dont le libellé contient ce fragment — souvent aucun, ce qui est
 * justement ce qu'un test veut pouvoir constater.
 *
 * @param fragment - un morceau de libellé.
 */
function buttons(fragment: string): HTMLButtonElement[] {
  return [...host.querySelectorAll('button')].filter((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
}

/** Choisit un résultat dans le filtre, comme un doigt le ferait. */
function chooseFilter(value: string): void {
  const select = host.querySelector('select') as HTMLSelectElement
  select.value = value
  // `bubbles` : Svelte 5 délègue « change » à la racine du montage, et un événement qui
  // ne remonte pas n'atteint jamais le gestionnaire de la page.
  select.dispatchEvent(new Event('change', { bubbles: true }))
}

/**
 * Ouvre le détail de la n-ième PESÉE, comme un doigt le ferait.
 *
 * `:not(.detail-row)` parce qu'un détail ouvert est lui-même une ligne du tableau : sans
 * cela, la deuxième ouverture viserait le détail de la première.
 *
 * @param index - le rang de la pesée dans le tableau.
 */
async function openDetailOfRow(index: number): Promise<HTMLTableRowElement> {
  const row = [...host.querySelectorAll('tbody tr:not(.detail-row)')][index] as HTMLTableRowElement
  const toggle = row.querySelector('button')
  if (toggle === null) throw new Error('cette ligne ne porte aucun bouton de détail')
  toggle.click()
  await settle()
  return row
}

describe('le détail s’ouvre LÀ OÙ L’ON A CLIQUÉ', () => {
  it('pose le détail dans la ligne qui suit celle qu’on a touchée', async () => {
    await open()
    const clicked = await openDetailOfRow(1)

    const detail = host.querySelector('[data-detail]')
    expect(detail).not.toBeNull()
    // Il s'ouvrait sous la TABLE ENTIÈRE : cliquer la ligne 3 envoyait la réponse à
    // 16 000 px de l'endroit touché, ce qui se lit comme un bouton qui ne fait rien.
    expect(detail?.previousElementSibling).toBe(clicked)
    expect(detail?.parentElement?.tagName).toBe('TBODY')
  })

  it('marque la ligne ouverte, et n’en ouvre jamais deux', async () => {
    await open()
    await openDetailOfRow(0)
    await openDetailOfRow(2)

    expect(host.querySelectorAll('[data-detail]').length).toBe(1)
    expect(host.querySelector('[data-detail]')?.getAttribute('data-detail')).toBe('3')
    expect(host.querySelectorAll('tbody tr.open').length).toBe(1)
  })

  it('écrit l’identifiant de la pesée SANS séparateur de milliers', async () => {
    rows = [weighing(1236, 'sent')]
    await open()
    await openDetailOfRow(0)

    // `frenchInteger` est fait pour les QUANTITÉS (« 1 236 pesées »). Un identifiant se
    // dicte au téléphone et se cherche dans l'export, qui l'écrit 1236 : « Pesée 1 236 »,
    // avec son espace insécable, ne correspond ni à l'un ni à l'autre.
    expect(host.querySelector('[data-detail] h3')?.textContent).toBe('Pesée 1236')
  })

  it('ne dépense aucun jeton d’ÉTAT pour marquer la ligne ouverte', async () => {
    await open()
    await openDetailOfRow(0)
    expect(host.querySelectorAll('tbody tr.open').length).toBe(1)

    // `--waiting-wash` est le lavis de `--waiting`, « weight not latched yet » (app.css) :
    // un état de l'écran CLIENT. §14.2 : la couleur porte du sens, jamais de la
    // décoration — et une ligne sélectionnée n'est pas un état de la pesée. Le jeton peut
    // être NOMMÉ dans un commentaire ; ce qui est interdit est de le DÉPENSER.
    expect(SOURCE).not.toContain('var(--waiting-wash)')
  })
})

describe('le filtre n’offre que des valeurs qui existent', () => {
  it('a perdu « printed », que le service n’écrit nulle part', async () => {
    await open()
    const values = [...host.querySelectorAll('option')].map((option) => option.value)

    // `internal/domain/journal.go` : « There is no 'ok' [...] a successful weighing is
    // 'sent' ». `printed` ne sélectionnait donc RIEN un jour où le poste avait imprimé
    // toute la journée, ce qui se lit comme un journal en panne.
    expect(values).not.toContain('printed')
    expect(values).toEqual(['', 'sent', 'rejected', 'failed', 'reprint'])
    expect(text()).not.toContain('printed')
  })

  it('emporte le filtre choisi dans la requête ET dans l’export', async () => {
    await open()
    chooseFilter('sent')
    await settle()

    const journalCalls = calls.filter((call) => call.includes('/admin/api/journal?'))
    expect(journalCalls.at(-1)).toContain('result=sent')
    const link = host.querySelector('a[download]') as HTMLAnchorElement
    expect(link.getAttribute('href')).toContain('result=sent')
  })

  it('n’accuse aucun filtre quand aucun filtre n’est posé', async () => {
    rows = []
    await open()

    // « Aucune pesée ne correspond à ce filtre » s'affichait aussi sur « toutes » : un
    // poste neuf qui n'a simplement pas encore pesé se voyait accusé d'un filtre qui
    // n'existe pas.
    expect(textOf('[data-empty="weighings"]')).toBe('Le journal ne contient aucune pesée.')
  })

  it('nomme le filtre quand il y en a un, plutôt que de dire « ce filtre »', async () => {
    rows = []
    await open()
    chooseFilter('rejected')
    await settle()

    expect(textOf('[data-empty="weighings"]')).toBe(
      'Aucune pesée ne correspond au filtre « refusées ».',
    )
  })
})

describe('aucun jeton anglais n’atteint un bénévole', () => {
  it('traduit le résultat, l’origine du poids et la stabilité', async () => {
    await open()
    expect(host.querySelector('td[data-result]')?.textContent?.trim()).toBe(
      'envoyée à l’imprimante',
    )

    await openDetailOfRow(0)
    expect(host.querySelector('[data-stability]')?.textContent).toContain('instable')
    expect(host.querySelector('[data-source]')?.textContent?.trim()).toBe('balance')
    // Les jetons de `internal/domain/journal.go` et de `internal/store/technical.go`
    // n'apparaissent nulle part, y compris dans le journal technique.
    expect(text()).not.toMatch(/\b(sent|rejected|failed|reprint|unstable|scale|warn)\b/u)
  })
})

/**
 * Le code d'un événement est un NOM TECHNIQUE, et il en suit la règle.
 *
 * §2.3 de la conception confie quatre choses au même interrupteur, et celle-ci en est
 * une : `ERR-SCL-07` n'apprend rien à qui n'ouvrira jamais le source, et le message
 * français à côté dit déjà ce qui s'est passé. Le masquer ne le met pas hors de portée —
 * l'interrupteur est dans le rail, et `technical.csv` du fichier de diagnostic porte la
 * colonne `code` quoi que l'écran montre (internal/diag/archive.go).
 *
 * Le banc va DANS LES DEUX SENS : un masquage qu'on ne sait pas défaire vaudrait une
 * suppression, et c'est ce code-là qu'on lit au téléphone à qui dépanne.
 */
describe('le code technique d’un événement passe sous l’interrupteur', () => {
  it('ne le montre pas tant que personne ne l’a demandé', async () => {
    await open()

    expect(textOf('[data-scroll="technical"]')).toContain(
      'La balance a émis une trame illisible.',
    )
    expect(text()).not.toContain('ERR-SCL-07')
  })

  it('le rend dès qu’on coche, où il était', async () => {
    await open()

    preferences.showTechnicalNames = true
    flushSync()

    expect(textOf('[data-scroll="technical"]')).toContain('ERR-SCL-07')
  })
})

describe('« Rejouer cette trame » est un acte PROTÉGÉ (ADR-033)', () => {
  it('demande le mot de passe sur 401, puis rejoue la trame', async () => {
    await open()
    guarded = true
    await openDetailOfRow(0)

    button('Rejouer cette trame').click()
    await waitFor(() => admin.pending !== null)
    // `admin.run` affichait un 401 nu : le bénévole lisait « Session expirée ou absente »
    // sans qu'aucun champ ne s'ouvre pour y répondre.
    expect(admin.pending?.kind).toBe('password')

    await admin.answerPassword('openscale')
    await settle()

    expect(replayCalls).toBe(2)
    expect(admin.notice).toBe('La trame a été rejouée.')
  })

  it('porte sa marque AVANT le clic, et ses 72 px', async () => {
    await open()
    await openDetailOfRow(0)
    const replay = button('Rejouer cette trame')

    expect(replay.textContent).toContain('clé')
    // Le rejeu pousse une mesure dans le poste EN SERVICE : rien ne le remet comme il
    // était. C'est la seule commande de cette page à garder les 72 px (§3.3).
    expect(replay.classList.contains('touch-target')).toBe(true)
    expect([...host.querySelectorAll('.touch-target')].length).toBe(1)
  })

  it('ne propose rien à rejouer sur une ligne sans trame, et LE DIT', async () => {
    rows = [{ ...weighing(1, 'rejected'), frame: '' }]
    await open()
    await openDetailOfRow(0)

    // Un bouton grisé sur une condition qu'il ne nomme pas n'explique rien, et le refus
    // écrit dans `actionError` était inatteignable : la même condition avait déjà désarmé
    // le bouton. Une phrase, donc, et pas de bouton du tout.
    expect(buttons('Rejouer cette trame')).toEqual([])
    expect(textOf('[data-no-frame]')).toContain('rien à rejouer')
  })
})

describe('aucune liste sans plafond, et le tableau tient dans sa boîte', () => {
  it('attribue le plafond à la PAGE, et jamais au poste', async () => {
    rows = Array.from({ length: 200 }, (_, index) => weighing(index + 1, 'sent'))
    technicalCount = 50
    await open()

    const said = textOf('[data-tally="weighings"]')
    expect(said).toContain('200 pesées')
    expect(said).toContain('c’est ce que cette page demande au poste')
    // `GET /admin/api/journal` a pour défaut les MÊMES 200 et ne borne rien : `intParam`
    // rend le défaut, jamais un plafond (internal/web/admin.go). Le poste sert ce qu'on
    // lui demande, et un écran qui lui attribuerait ce nombre enverrait chercher un mur
    // qui n'existe pas.
    expect(said).not.toContain('jamais plus de')

    const technical = textOf('[data-tally="technical"]')
    expect(technical).toContain('50 lignes')
    expect(technical).not.toContain('jamais plus de')
    // `archivedTechnical` = 500 (internal/diag/archive.go) sur les 2 000 que le poste
    // garde : le fichier de diagnostic ne porte pas « les précédentes », il en porte 500.
    expect(technical).toContain('500')
  })

  it('propose un export qui descend PLUS BAS que le tableau', async () => {
    rows = Array.from({ length: 200 }, (_, index) => weighing(index + 1, 'sent'))
    await open()

    const href = host.querySelector('a[download]')?.getAttribute('href') ?? ''
    // L'export partait avec le `limit=200` du tableau : il emportait EXACTEMENT les
    // lignes déjà à l'écran. `maxPageSize` (internal/store/journal.go) vaut 5 000 et
    // égale le `journal.max_rows` livré : le journal entier EST la plus grande page.
    expect(href).toContain('limit=5000')
    expect(href).not.toContain('limit=200')
    expect(textOf('[data-tally="weighings"]')).toContain('5 000')
  })

  it('ne lit JAMAIS un journal illisible comme un journal vide', async () => {
    journalless = true
    await open()

    const said = textOf('[data-empty="weighings"]')
    expect(said).toContain('n’a pas pu être lu')
    expect(said).not.toContain('Aucune pesée')
    // Et l'export n'est pas proposé : un `<a download>` sur une route qui refuse pose du
    // JSON de refus dans un kiosque dont on ne sait pas revenir.
    expect(host.querySelector('a[download]')).toBeNull()
  })

  it('fige l’en-tête, borne la hauteur et sort de la colonne de 68rem', async () => {
    await open()
    const box = host.querySelector('[data-scroll="weighings"]')
    expect(box?.querySelector('table')).not.toBeNull()

    // Le CSS ne s'évalue pas sous jsdom : ce qui est vérifié ici est la DÉCLARATION.
    expect(SOURCE).toMatch(/\bth\s*\{[^}]*position:\s*sticky/u)
    expect(SOURCE).toMatch(/\.table-box\s*\{[^}]*max-height/u)
    expect(SOURCE).toMatch(/\.table-box,?[^{]*\{[^}]*overflow:\s*auto/u)
    // La table large sort de la colonne de lecture bornée par `App.svelte` (§14.4).
    expect(SOURCE).toMatch(/div\.journal\s*\{[^}]*max-width:\s*none/u)
  })

  it('garde sa colonne de lecture égale à celle de l’ossature', async () => {
    await open()

    // Deux nombres qui doivent bouger ensemble : `--reading-column` est une COPIE du
    // `max-width` que `App.svelte` impose aux autres pages, et aucun jeton d'`app.css` ne
    // porte cette mesure. Faute de quoi les tenir, ils divergent en silence.
    const shell = /\.page\s*:global\(>\s*\*\)\s*\{[^}]*max-width:\s*([\d.]+rem)/u.exec(SHELL)
    const mine = /--reading-column:\s*([\d.]+rem)/u.exec(SOURCE)
    expect(shell?.[1]).toBeDefined()
    expect(mine?.[1]).toBe(shell?.[1])
  })
})

describe('la page ne dit rien qu’elle ne sache encore', () => {
  it('vide tout ce qu’elle montrait avant d’attendre la lecture suivante', async () => {
    await open()
    expect(host.querySelectorAll('tbody tr').length).toBe(3)

    const release = holdJournal()
    rows = [weighing(9, 'rejected')]
    chooseFilter('rejected')
    await settle()

    // Seule la PREMIÈRE lecture était honnête : choisir « refusées » laissait à l'écran
    // les lignes `sent` de la lecture précédente, leur décompte, et un select qui disait
    // déjà « refusées ». La page affirmait un filtre qu'elle ne montrait pas.
    expect(host.querySelectorAll('tbody tr').length).toBe(0)
    expect(textOf('[data-empty="weighings"]')).toBe('Lecture du journal…')
    expect(host.querySelector('[data-tally="weighings"]')).toBeNull()

    release()
    await settle(6)
    expect(host.querySelectorAll('tbody tr:not(.detail-row)').length).toBe(1)
  })

  it('ne propose pas l’export tant que le journal n’a pas répondu', async () => {
    const release = holdJournal()
    admin = new Admin(60_000)
    component = mount(Journal, { target: host, props: { admin } })
    flushSync()
    await settle()

    // Le `{:else}` attrapait 'loading' autant que 'read' : sur un poste sans journal, le
    // lien restait à l'écran pendant toute la lecture, et le toucher dans cette fenêtre
    // posait le JSON d'un refus dans un kiosque dont on ne sait pas revenir.
    expect(host.querySelector('a[download]')).toBeNull()
    expect(text()).toContain('L’export sera proposé quand le journal aura répondu.')

    release()
    await settle(6)
    expect(host.querySelector('a[download]')).not.toBeNull()
  })
})

describe('un refus est écrit SOUS le journal qui a refusé', () => {
  it('n’envoie plus vers une bannière que la lecture suivante efface', async () => {
    journalless = true
    await open()

    // `admin.load` fait `actionError = ''` sur son chemin de SUCCÈS (session.svelte.ts) :
    // la lecture technique qui suit effaçait la bannière un instant après qu'elle a été
    // dessinée. Le journal des pesées porte donc sa raison lui-même.
    expect(textOf('[data-empty="weighings"]')).toContain('pas de journal.')
    expect(admin.actionError).toBe('')
  })

  it('donne à chaque journal SA raison quand les deux refusent', async () => {
    journalless = true
    technicalless = true
    await open()

    expect(textOf('[data-empty="weighings"]')).toContain('pas de journal.')
    const technical = textOf('[data-empty="technical"]')
    // Il disait « la raison est écrite en haut de l'écran » ; quand les deux lectures
    // échouaient, la seule bannière restante portait la raison de l'AUTRE journal, au
    // dessus du panneau des pesées.
    expect(technical).toContain('pas de journal technique.')
    expect(technical).not.toContain('en haut de l’écran')
    expect(admin.actionError).toBe('')
  })
})
