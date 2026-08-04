import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { createClassComponent } from 'svelte/legacy'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Catalog from '../src/admin/pages/Catalog.svelte'
import { Draft } from '../src/admin/lib/draft.svelte'
import type { DecisionDTO, FindingDTO, HealthDTO, ImportDTO } from '../src/admin/lib/dto'
import {
  importOutcomeSentence,
  importResultWord,
  importSourceWord,
} from '../src/admin/lib/inventory'
import { Admin } from '../src/admin/lib/session.svelte'
import { GRID_COLUMNS_AUTO, gridTemplateColumns } from '../src/lib/grid'
import { FLV_1_IMPORT, FLV_IMPORT, nominalHealth } from './fixtures/health'

/**
 * La page Catalogue dit-elle la vérité, et le mot de passe est-il demandé au bon moment ?
 *
 * Douze choses sont tenues ici, et chacune est un écart constaté à §14.4 ou un défaut :
 *
 *  1. aucune liste sans plafond, et aucun plafond muet — la troncature à vingt de la
 *     recherche ne disait rien, et un fichier de 116 anomalies affichait « 20 » sans un
 *     mot, ce qui fait croire le travail fini à qui corrige les vingt ;
 *  2. « Le proposer de nouveau » est ATTEIGNABLE : la route client ne sert pas un produit
 *     qu'un humain a retiré, donc la recherche ne pouvait plus le trouver, et le seul
 *     bouton qui défait un retrait était inatteignable au moment précis où il servait ;
 *  3. le retrait et la dérogation sont DEUX actes sur deux colonnes d'une même ligne : le
 *     seul appel d'avant effaçait la dérogation à chaque retrait ;
 *  4. le dépôt d'un CSV et les décisions de catalogue passent par `admin.protect` :
 *     ADR-033 protège ce qui change ce que le poste vend, et `admin.run` répondait 401 à
 *     l'écran au lieu de demander le mot de passe ;
 *  5. les produits retirés depuis l'import précédent sont annoncés (§10.9), et datés du
 *     dernier import APPLIQUÉ — un fichier refusé n'a jamais rien mis en service ;
 *  6. aucun jeton anglais du service — `applied`, `unchanged`, `local_drop` — n'est
 *     affiché tel quel ;
 *  7. un fichier déposé n'est PAS annoncé comme appliqué : la route répond un inventaire
 *     dont le résultat est vide, et c'est la veille qui tranchera quelques secondes plus
 *     tard ;
 *  8. rien n'est affirmé de ce qui n'a pas été LU : un poste sans journal répond 503 à
 *     toute lecture de l'historique, et quatre phrases « Aucun… » disaient le contraire —
 *     et la page S'OUVRE sur un poste qui n'a jamais reçu de catalogue, l'état de tout
 *     poste installé le matin même ;
 *  9. les signalements sont RELUS quand le poste change d'import ;
 * 10. aucun bouton n'est armé pour un refus certain : le motif est exigé par le service ;
 * 11. la zone de dépôt obéit au même verrou que le reste de la page, et le sélecteur de
 *     fichier se réarme, sans quoi le MÊME fichier ne peut pas être redéposé ;
 * 12. les trois listes de signalements NOMMENT les produits : « 4412 » est un numéro qu'il
 *     faut chercher dans Odoo avant de commencer, et le nom vient de l'import lui-même, pas
 *     du catalogue en service.
 */

const HERE = dirname(fileURLToPath(import.meta.url))

/** Le noyau, seule source des jetons de résultat et de source d'un import. */
const JOURNAL_GO = readFileSync(resolve(HERE, '../../internal/domain/journal.go'), 'utf8')

/** Ce qui fabrique la réponse au dépôt : c'est lui qui laisse le résultat VIDE. */
const CATALOG_ADMIN_GO = readFileSync(
  resolve(HERE, '../../cmd/openscale/catalogadmin.go'),
  'utf8',
)

/** La page elle-même, pour les garanties qui portent sur la source. */
const PAGE = readFileSync(resolve(HERE, '../src/admin/pages/Catalog.svelte'), 'utf8')

/** Ce que `receivedImport` fabrique, du mot-clé jusqu'à l'accolade fermante. */
const RECEIVED_IMPORT = goFunction(CATALOG_ADMIN_GO, 'func receivedImport')

/**
 * Le corps d'une fonction Go, de sa signature à l'accolade fermante en colonne zéro.
 *
 * @param source - le fichier Go.
 * @param signature - le début de la ligne `func`.
 */
function goFunction(source: string, signature: string): string {
  const body = source.slice(source.indexOf(signature))
  return body.slice(0, body.indexOf('\n}'))
}

/**
 * La phrase que `POST /admin/api/catalog/import` écrit dans `reason`, LUE DANS LE SERVICE.
 *
 * Recopiée à la main, elle aurait dérivé du jour où quelqu'un la retouche ; lue ici, le
 * banc ne peut pas mentir sur ce que la route répond. Et c'est cette phrase-là — la seule
 * qui dise ce qu'il reste à attendre — que la page jetait.
 */
const RECEIVED_REASON = goLiteral(RECEIVED_IMPORT, 'Reason:')

/**
 * Recolle un littéral Go que le formatage a coupé en plusieurs morceaux.
 *
 * @param source - le code Go à lire.
 * @param field - le champ dont on veut la valeur, « Reason: ».
 */
function goLiteral(source: string, field: string): string {
  const tail = source.slice(source.indexOf(field) + field.length)
  const whole = tail.slice(0, tail.indexOf('",') + 1)
  return [...whole.matchAll(/"([^"]*)"/gu)].map((piece) => piece[1] ?? '').join('')
}

/**
 * Ce que « Recharger le catalogue » répond, dans la forme exacte du service.
 *
 * La réponse est un 202 : l'import est asynchrone par conception, et ce que la route rend
 * n'est pas l'issue mais de quoi la reconnaître — ce qui est surveillé, et l'import en
 * service à l'instant de l'appui.
 */
const RELOAD_ANSWER = {
  done: true,
  message:
    'Aucun fichier flv_2.csv dans C:\\ProgramData\\OpenScale\\catalog\\incoming : il n’y a rien à relire.',
  watched: 'dépôt local, flv_2.csv dans C:\\ProgramData\\OpenScale\\catalog\\incoming',
  last_import_id: FLV_IMPORT.id,
  last_import_at: FLV_IMPORT.occurred_at,
}

let host: HTMLElement
let component: unknown
/** La page montée avec un tableau de bord MODIFIABLE, quand un test en a besoin. */
let live: { $set: (props: Record<string, unknown>) => void; $destroy: () => void } | undefined

/** Ce que le banc a vu passer : la route, la méthode et le corps. */
interface Call {
  url: string
  method: string
  body: unknown
}

let calls: Call[] = []
/** Les signalements que la route d'imports rend pour le dernier import. */
let findings: FindingDTO[] = []
/** L'historique que la route d'imports rend. */
let history: ImportDTO[] = []
/** Les produits que la route CLIENT sert : elle ne sert jamais un produit retiré. */
let catalogProducts: { id: string; name: string; search: string; mode: string }[] = []
/** Combien de fois encore une décision doit être refusée en 401 avant d'être acceptée. */
let refusalsLeft = 0
/** Vrai quand ce poste n'a pas d'historique d'imports (ADR-013) : la route répond 503. */
let historyFails = false
/** Vrai quand la route CLIENT du catalogue ne répond pas. */
let catalogFails = false

beforeEach(() => {
  calls = []
  findings = []
  history = [FLV_IMPORT]
  catalogProducts = []
  refusalsLeft = 0
  historyFails = false
  catalogFails = false
  host = document.createElement('div')
  document.body.appendChild(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  if (live !== undefined) live.$destroy()
  live = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/** Les routes que cette page touche, et rien d'autre. */
function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const url = String(input)
  const method = init?.method ?? 'GET'
  const raw = init?.body
  calls.push({ url, method, body: typeof raw === 'string' ? JSON.parse(raw) : raw })

  if (url.startsWith('/admin/api/imports')) {
    if (historyFails) {
      return json({ code: '', message: 'Ce poste n’a pas d’historique d’imports.' }, 503)
    }
    return json({ imports: history, findings })
  }
  if (url === '/api/v1/catalog') {
    if (catalogFails) {
      return json({ code: '', message: 'Le catalogue n’est pas lisible.' }, 503)
    }
    // La PRÉSENTATION et les tarifs voyagent, parce que le service les sert : l'aperçu de
    // la grille dessine une vraie tuile, et une tuile porte les paliers et l'interrupteur
    // des prix de ce poste. Un banc qui les taisait faisait retomber l'aperçu sur des
    // défauts au lieu d'exercer ce que le poste publie.
    return json({
      products: catalogProducts.map((product) => ({
        category_code: 'vegetables',
        unit_price_cents: 250,
        unit_price_text: '2,50',
        price_suffix: ' €/kg',
        image_url: '',
        prices: [{ code: 'A', text: '2,50' }],
        ...product,
      })),
      categories: [],
      tiers: [],
      pricing: { primary_code: 'A', primary_label: 'Adhérent', tiers: [] },
      presentation: {
        show_grid_prices: true,
        show_by_unit_products: false,
        grid_columns: 0,
        idle_timeout_s: 60,
        reprint_window_s: 120,
        sound: true,
      },
    })
  }
  if (url === '/admin/api/health') {
    return json(nominalHealth())
  }
  if (url.includes('/decision')) {
    if (refusalsLeft > 0) {
      refusalsLeft -= 1
      return json({ code: '', message: 'Session absente ou expirée.' }, 401)
    }
    return json({ done: true, message: 'La décision est enregistrée.' })
  }
  if (url === '/admin/api/catalog/reload') {
    return json(RELOAD_ANSWER, 202)
  }
  if (url === '/admin/api/catalog/import') {
    // EXACTEMENT ce que la route répond : un inventaire SANS résultat, et la phrase qui
    // dit ce qu'il reste à attendre. Le banc rendait `result: 'applied'` sur une route
    // qui ne remplit jamais ce champ, et couvrait ainsi une branche morte.
    return json(
      { ...FLV_IMPORT, id: 8, file_name: 'flv_2.csv', result: '', reason: RECEIVED_REASON },
      202,
    )
  }
  return json({ done: true, message: 'Fait.' })
}

/** Une réponse JSON, comme le service en écrit. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/**
 * Le bloc catalogue tel que le poste le sert, réduit à ce que le panneau de source lit.
 *
 * Ce fichier ne teste PAS ce panneau — `admin-source.test.ts` s'en charge —, mais la page
 * l'édite désormais et un brouillon vide lui ferait dire « ce poste ne déclare aucune
 * source », ce qui n'est le cas d'aucun poste en service.
 */
function localDropConfig(): Record<string, unknown> {
  return { catalog: { type: 'local_drop', options: {} } }
}

/** Monte la page sur un tableau de bord, et rend l'objet de session. */
function open(health: HealthDTO = nominalHealth()): Admin {
  const admin = new Admin()
  const draft = new Draft(admin)
  draft.config = localDropConfig()
  component = mount(Catalog, { target: host, props: { admin, draft, health } })
  flushSync()
  return admin
}

/**
 * Monte la page sur un tableau de bord qui CHANGE, comme le sondage le fait.
 *
 * `mount()` reçoit un objet ordinaire : la page ne verrait jamais un `health` remplacé,
 * alors que c'est exactement ce qui arrive toutes les trois secondes. `createClassComponent`
 * est la seule API publique qui rende une prop réactive depuis un fichier `.ts`, et elle ne
 * sert qu'à cela ici.
 *
 * @param health - le premier tableau de bord.
 * @returns de quoi en poser un autre.
 */
function openLive(health: HealthDTO): (next: HealthDTO) => void {
  const admin = new Admin()
  const draft = new Draft(admin)
  draft.config = localDropConfig()
  live = createClassComponent({
    component: Catalog,
    target: host,
    props: { admin, draft, health },
  }) as unknown as { $set: (props: Record<string, unknown>) => void; $destroy: () => void }
  flushSync()
  return (next: HealthDTO) => {
    live?.$set({ health: next })
    flushSync()
  }
}

/** Laisse les lectures se terminer, puis met le DOM à jour. */
async function settle(): Promise<void> {
  for (let round = 0; round < 6; round += 1) {
    await new Promise((done) => setTimeout(done, 0))
    flushSync()
  }
}

/** Réduit les blancs et ramène toute apostrophe à une seule forme. */
function collapse(text: string): string {
  return text.replace(/\s+/gu, ' ').replace(/[’´`]/gu, "'").trim()
}

/** Le texte de la page entière, prêt à être cherché. */
function pageText(): string {
  return collapse(host.textContent ?? '')
}

/** Un bouton, retrouvé par le début de son libellé — la pastille « clé » suit le texte. */
function buttonNamed(label: string): HTMLButtonElement {
  const wanted = collapse(label)
  const found = [...host.querySelectorAll('button')].find((button) =>
    collapse(button.textContent ?? '').startsWith(wanted),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${label} »`)
  return found
}

/** Vrai quand un bouton portant ce libellé existe. */
function hasButton(label: string): boolean {
  try {
    buttonNamed(label)
    return true
  } catch {
    return false
  }
}

/** Un bouton d'acte, retrouvé par l'acte qu'il porte — son libellé, lui, change. */
function act(name: string): HTMLButtonElement {
  const found = host.querySelector<HTMLButtonElement>(`button[data-act="${name}"]`)
  if (found === null) throw new Error(`aucun bouton d'acte « ${name} »`)
  return found
}

/** Lâche un fichier sur la zone de dépôt, comme un exploitant le fait. */
function drop(name: string): void {
  const zone = host.querySelector('[aria-label="Zone de dépôt du catalogue"]')
  if (zone === null) throw new Error('aucune zone de dépôt')
  const dropped = new Event('drop', { bubbles: true, cancelable: true })
  Object.defineProperty(dropped, 'dataTransfer', {
    value: { files: { item: () => new File(['nom;prix'], name) } },
  })
  zone.dispatchEvent(dropped)
  flushSync()
}

/** Ce que le poste a reçu sur la route de dépôt. */
function importPosts(): Call[] {
  return calls.filter((call) => call.url === '/admin/api/catalog/import')
}

/** La phrase que la zone de dépôt affiche, ramenée à une seule forme d'apostrophe. */
function reportText(): string {
  return collapse(host.querySelector('[data-report]')?.textContent ?? '')
}

/** Tape dans un champ, exactement comme un exploitant le fait. */
function type(selector: string, value: string): void {
  const field = host.querySelector<HTMLInputElement>(selector)
  if (field === null) throw new Error(`aucun champ ${selector}`)
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
}

/** Les corps des décisions envoyées au poste. */
function decisionBodies(): Record<string, unknown>[] {
  return calls
    .filter((call) => call.url.includes('/decision'))
    .map((call) => call.body as Record<string, unknown>)
}

/** N signalements d'anomalie, tous sur des lignes différentes. */
function manyAnomalies(count: number): FindingDTO[] {
  return Array.from({ length: count }, (_unused, index) => ({
    csv_line: index + 2,
    product_id: `id-${String(index)}`,
    product_name: `TOMATE ${String(index)}`,
    code: 'INVALID_BARCODE',
    issue: 'anomaly',
    message: 'Corrigez ce code dans Odoo.',
    value: '0493100100006',
  }))
}

/** Un signalement, dans la forme exacte que la route d'imports sert. */
function finding(overrides: Partial<FindingDTO> = {}): FindingDTO {
  return {
    csv_line: 42,
    product_id: '4412',
    product_name: 'AIL VIOLET SAF',
    code: 'RESERVED_ZONE_NOT_EMPTY',
    issue: 'anomaly',
    message: 'Corrigez le code-barres dans Odoo.',
    value: '0493021012365',
    ...overrides,
  }
}

/** Les noms que dessine une des listes de signalements. */
function namesIn(list: string): string[] {
  return [...host.querySelectorAll(`[data-rows="${list}"] [data-name]`)].map(
    (span) => span.textContent ?? '',
  )
}

/** Une décision humaine en vigueur. */
function decision(overrides: Partial<DecisionDTO> = {}): DecisionDTO {
  return {
    product_id: '4242',
    offered: true,
    min_weight_g: null,
    reason: 'hors saison',
    decided_by: 'bénévole',
    decided_at: '2026-07-20T09:00:00.000Z',
    ...overrides,
  }
}

describe('aucune liste sans plafond, et aucun plafond muet', () => {
  it('annonce ce que la troncature des anomalies cache', async () => {
    findings = manyAnomalies(116)
    open()
    await settle()

    // 50 dessinées sur 116 : le chiffre caché est DIT, sans quoi celui qui corrige les
    // cinquante premières repart en croyant le fichier propre.
    expect(pageText()).toContain('50 lignes affichées sur 116 anomalies.')
    expect(host.querySelectorAll('[data-rows="anomalies"] li')).toHaveLength(50)
  })

  it('annonce ce que la troncature de la recherche cache', async () => {
    catalogProducts = Array.from({ length: 47 }, (_unused, index) => ({
      id: `p${String(index)}`,
      name: `TOMATE ${String(index)}`,
      search: `tomate ${String(index)}`,
      mode: 'by_weight',
    }))
    open()
    await settle()
    type('#product-search', 'tomate')

    expect(pageText()).toContain('20 produits affichés sur 47 trouvés')
    expect(host.querySelectorAll('[data-rows="matches"] li')).toHaveLength(20)
  })

  it('borne chaque liste dans son propre conteneur défilant', () => {
    // Le corps de la page ne défile jamais à cause d'une liste : c'est la liste qui défile.
    expect(PAGE).toMatch(/\.scroll\s*\{[^}]*max-height:/u)
    expect(PAGE).toMatch(/\.scroll\s*\{[^}]*overflow:\s*auto/u)
  })

  it('ne prend pas le numéro de ligne du CSV pour une clé', async () => {
    // Une même ligne porte autant de signalements qu'elle a de problèmes : un prix illisible
    // ET un code faux, c'est deux signalements sur la ligne 42. Une boucle clée par
    // `csv_line` lèverait `each_key_duplicate` et emporterait tout l'écran.
    findings = [
      finding({ code: 'ZERO_PRICE', message: 'a', value: '0' }),
      finding({ code: 'INVALID_BARCODE', message: 'b', value: '1' }),
    ]
    open()
    await settle()

    expect(host.querySelectorAll('[data-rows="anomalies"] li')).toHaveLength(2)
  })
})

describe('les listes de signalements nomment les produits', () => {
  it('nomme le produit dans les trois listes, et pas seulement son identifiant Odoo', async () => {
    // « 4412 » est un numéro qu'il faut d'abord chercher dans Odoo ; « AIL VIOLET SAF » est
    // le produit que celui qui corrige le fichier reconnaît. Les trois listes sont dessinées
    // par le MÊME extrait, et c'est pour ça qu'elles sont vérifiées ensemble : une seule
    // d'entre elles qui nommerait le produit serait un extrait dupliqué quelque part.
    findings = [
      finding({ product_name: 'AIL VIOLET SAF' }),
      finding({
        code: 'UNIT_MISMATCH',
        issue: 'info',
        product_id: '5209',
        product_name: 'OEUFS PLEIN AIR',
        message: 'Corrigez l’unité dans Odoo.',
      }),
      finding({
        code: 'PREPACKAGED_PRODUCT',
        issue: 'info',
        product_id: '77',
        product_name: 'BOULGOUR GROS 5 KG',
        message: 'Rien à corriger : c’est un code fournisseur.',
      }),
    ]
    open()
    await settle()

    expect(namesIn('anomalies')).toEqual(['AIL VIOLET SAF'])
    expect(namesIn('mismatches')).toEqual(['OEUFS PLEIN AIR'])
    expect(namesIn('not-weighable')).toEqual(['BOULGOUR GROS 5 KG'])
    // Le nom N'A PAS remplacé l'identifiant : c'est lui qui ouvre la fiche dans Odoo.
    expect(host.querySelector('[data-rows="anomalies"] .id')?.textContent).toBe('4412')
  })

  it('n’invente aucun libellé pour un signalement sans nom', async () => {
    // Deux signalements ne portent pas de nom : celui qui ne porte sur aucun produit, et une
    // ligne trop abîmée pour en avoir un — ce qu'UNREADABLE_ROW dit déjà dans son message.
    // Un « nom inconnu » écrit à leur place serait un fait que cette page n'a pas lu.
    findings = [
      finding({
        code: 'UNREADABLE_ROW',
        product_id: '90',
        product_name: '',
        message: 'Corrigez la ligne : elle doit porter un identifiant et un nom.',
      }),
    ]
    open()
    await settle()

    expect(host.querySelectorAll('[data-rows="anomalies"] li')).toHaveLength(1)
    expect(namesIn('anomalies')).toEqual([])
  })
})

describe('« Le proposer de nouveau », et le chemin qui y mène', () => {
  it('reprend un produit retiré depuis les décisions en vigueur', async () => {
    // La route CLIENT ne sert JAMAIS un produit retiré : la recherche ne peut donc pas le
    // retrouver, et c'est exactement le produit sur lequel on veut revenir.
    catalogProducts = []
    const health = nominalHealth({ decisions: [decision({ offered: false })] })
    open(health)
    await settle()

    buttonNamed('Reprendre cette décision').click()
    flushSync()

    expect(hasButton('Le proposer de nouveau')).toBe(true)
    buttonNamed('Le proposer de nouveau').click()
    await settle()

    expect(decisionBodies()).toHaveLength(1)
    expect(decisionBodies()[0]).toMatchObject({ offered: true })
  })

  it('n’offre « Ne plus proposer » qu’à un produit qui est encore proposé', async () => {
    const health = nominalHealth({ decisions: [decision({ offered: false })] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    expect(hasButton('Ne plus proposer ce produit')).toBe(false)
  })
})

describe('le retrait et la dérogation sont deux actes', () => {
  it('retire un produit SANS toucher à sa dérogation', async () => {
    const health = nominalHealth({ decisions: [decision({ offered: true, min_weight_g: 8 })] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    // Le motif est une CONDITION du service, pas un commentaire : sans lui, le poste
    // refuse en 422 et le bouton reste désarmé.
    type('#decision-reason', 'hors saison')
    buttonNamed('Ne plus proposer ce produit').click()
    await settle()

    // La dérogation en vigueur repart telle quelle : elle était lue dans le formulaire, et
    // un champ vide effaçait donc une dérogation à chaque retrait.
    expect(decisionBodies()).toHaveLength(1)
    expect(decisionBodies()[0]).toMatchObject({ offered: false, min_weight_g: 8 })
  })

  it('enregistre une dérogation SANS remettre le produit dans la grille', async () => {
    const health = nominalHealth({ decisions: [decision({ offered: false })] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    type('#decision-reason', 'petits pois de printemps')
    type('#decision-waiver', '8')
    buttonNamed('Enregistrer la dérogation').click()
    await settle()

    expect(decisionBodies()).toHaveLength(1)
    expect(decisionBodies()[0]).toMatchObject({ offered: false, min_weight_g: 8 })
  })

  it('refuse d’envoyer une dérogation illisible', async () => {
    const health = nominalHealth({ decisions: [decision()] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    // `Number('')` vaut 0 et `Number('abc')` vaut NaN, que JSON écrit `null` : les deux
    // écriraient une décision que personne n'a demandée.
    type('#decision-waiver', ' ')
    expect(buttonNamed('Enregistrer la dérogation').disabled).toBe(true)
    expect(decisionBodies()).toHaveLength(0)
  })
})

describe('les actes protégés d’ADR-033', () => {
  it('demande le mot de passe sur 401, puis REJOUE la décision', async () => {
    refusalsLeft = 1
    const health = nominalHealth({ decisions: [decision({ offered: false })] })
    const admin = open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    buttonNamed('Le proposer de nouveau').click()
    await settle()

    // `admin.run` aurait affiché « Session absente ou expirée. » et perdu le geste.
    expect(admin.pending).not.toBeNull()
    await admin.answerPassword('openscale')
    await settle()

    expect(decisionBodies()).toHaveLength(2)
    expect(decisionBodies()[1]).toMatchObject({ offered: true })
  })

  it('fait passer les cinq actes de la page par admin.protect', () => {
    for (const name of ['reload', 'quarantine', 'offered']) {
      expect(PAGE, name).toContain(`guarded('${name}'`)
    }
    // Les deux actes de dérogation portent DEUX étiquettes de travail, et c'est le sujet
    // du test « En cours… » plus bas : ils passent tous deux par le même `guarded`.
    expect(PAGE).toContain("guarded(grams === null ? 'waiver-off' : 'waiver'")
    expect(PAGE).toContain('admin.protect(() => api.importCatalog(file))')
    // Aucun acte de cette page ne passe plus par `admin.run`, qui ne sait pas rejouer.
    expect(PAGE).not.toContain('admin.run(')
  })

  it('montre « En cours… » sur le bouton qui travaille, et sur lui seul', async () => {
    // « Retirer la dérogation » posait `working = 'waiver'`, donc c'est « Enregistrer la
    // dérogation », dessiné juste à côté, qui affichait l'avancement de l'autre acte.
    const health = nominalHealth({ decisions: [decision({ offered: true, min_weight_g: 8 })] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    act('waiver-off').click()
    flushSync()

    expect(collapse(act('waiver-off').textContent ?? '')).toContain('En cours…')
    expect(collapse(act('waiver').textContent ?? '')).not.toContain('En cours…')
    await settle()
  })

  it('annonce la clé AVANT le clic', async () => {
    // Un bénévole qui n'a pas le mot de passe doit savoir ce qui lui est accessible sans
    // aller chercher quelqu'un (§14.4).
    open()
    await settle()
    expect(collapse(buttonNamed('Oublier la quarantaine').textContent ?? '')).toContain('clé')
  })
})

describe('le dépôt d’un CSV, que §14.4 place sur cette page', () => {
  it('accepte un fichier déposé et l’envoie par la route protégée', async () => {
    open()
    await settle()
    drop('flv_2.csv')
    await settle()

    expect(importPosts()).toHaveLength(1)
    expect(importPosts()[0]?.method).toBe('POST')
  })

  it('ne branche pas sur un résultat que la route de dépôt ne remplit JAMAIS', () => {
    // `receivedImport` n'écrit pas de `Result` — le fichier l'explique en toutes lettres,
    // « Why the record it returns carries no result ». La branche « REFUSÉ » de la page
    // était donc morte, et tout fichier déposé était annoncé comme accepté.
    expect(RECEIVED_IMPORT).not.toMatch(/^\s*Result:/mu)
    expect(RECEIVED_REASON).toMatch(/^Fichier déposé dans le répertoire surveillé/u)
    expect(PAGE).not.toContain("record.result === 'rejected'")
  })

  it('n’annonce pas comme appliqué un fichier que le poste n’a fait que RECEVOIR', async () => {
    open()
    await settle()
    drop('flv_2.csv')
    await settle()

    // L'inventaire des octets déposés est certain ; la mise en service ne l'est pas, et
    // c'est le poste qui écrit la phrase qui le dit.
    expect(reportText()).toContain('355 lignes lues, 331 pesables.')
    expect(reportText()).toContain(collapse(RECEIVED_REASON))
    expect(reportText()).not.toContain('appliquera')
  })

  it('ne prend pas un second fichier pendant qu’un acte est en vol', async () => {
    open()
    await settle()

    drop('flv_2.csv')
    // Pas de `settle` : le premier import est en vol, et c'est tout le sujet.
    drop('flv_3.csv')

    expect(reportText()).toContain("flv_3.csv n'a pas été déposé")
    await settle()
    expect(importPosts()).toHaveLength(1)
  })

  it('réarme le sélecteur, pour que le MÊME fichier puisse être redéposé', async () => {
    open()
    await settle()

    const chooser = host.querySelector<HTMLInputElement>('.choose input')
    expect(chooser).not.toBeNull()
    let cleared = false
    Object.defineProperty(chooser, 'value', {
      configurable: true,
      get: () => '',
      set: (written: string) => (cleared = written === ''),
    })
    Object.defineProperty(chooser, 'files', {
      configurable: true,
      value: { item: () => new File(['nom;prix'], 'flv_2.csv') },
    })
    chooser?.dispatchEvent(new Event('change', { bubbles: true }))
    await settle()

    // Sans cette remise à zéro, `change` ne repart pas pour le même chemin : rechoisir le
    // fichier après « Oublier la quarantaine » ne faisait STRICTEMENT rien.
    expect(cleared).toBe(true)
    expect(importPosts()).toHaveLength(1)
  })

  it('efface ce que l’acte précédent a laissé à l’écran', async () => {
    const admin = open()
    await settle()
    admin.notice = 'La décision est enregistrée.'
    admin.actionError = 'Un refus d’il y a une minute.'

    drop('flv_2.csv')

    expect(admin.notice).toBe('')
    expect(admin.actionError).toBe('')
    await settle()
  })
})

describe('les produits retirés depuis l’import précédent (§10.9)', () => {
  it('nomme leur nombre et l’import dont ils datent', async () => {
    history = [
      { ...FLV_IMPORT, id: 7, products_withdrawn_count: 4 },
      { ...FLV_IMPORT, id: 6, occurred_at: '2026-03-12T08:06:00.000Z' },
    ]
    open(nominalHealth({ catalog: { ...FLV_IMPORT, products_withdrawn_count: 4 } }))
    await settle()

    const said = collapse(host.querySelector('[data-withdrawn]')?.textContent ?? '')
    expect(said).toContain('4 produits retirés depuis l\'import du 12/03/2026')
  })

  it('dit qu’aucun produit n’a été retiré plutôt que de se taire', async () => {
    open()
    await settle()
    expect(pageText()).toContain('Aucun produit retiré par le dernier import.')
  })

  it('les date du dernier import APPLIQUÉ, jamais d’un fichier refusé', async () => {
    // L'historique porte aussi les refusés, les échecs et les identiques. Dater un retrait
    // d'un fichier que le poste a écarté nomme un import qui n'a rien mis en service ; le
    // retrait se mesure contre le dernier import appliqué (`withdrawUnseen`).
    history = [
      { ...FLV_IMPORT, id: 9, products_withdrawn_count: 4 },
      { ...FLV_IMPORT, id: 8, result: 'rejected', occurred_at: '2026-03-14T08:06:00.000Z' },
      { ...FLV_IMPORT, id: 7, result: 'applied', occurred_at: '2026-03-12T08:06:00.000Z' },
    ]
    open(nominalHealth({ catalog: { ...FLV_IMPORT, id: 9, products_withdrawn_count: 4 } }))
    await settle()

    const said = collapse(host.querySelector('[data-withdrawn]')?.textContent ?? '')
    expect(said).toContain("4 produits retirés depuis l'import du 12/03/2026")
    expect(said).not.toContain('14/03/2026')
  })
})

describe('rien n’est affirmé de ce qui n’a pas été lu', () => {
  it('ne conclut « aucun » sur AUCUNE des quatre listes que l’historique alimente', async () => {
    // Cas nominal, pas théorique : un poste sans journal (ADR-013) reçoit un 503 « ce poste
    // n'a pas d'historique d'imports » et lisait ces quatre phrases fausses en permanence.
    historyFails = true
    open()
    await settle()

    const said = pageText()
    expect(said).not.toContain('Aucune anomalie sur le dernier import.')
    expect(said).not.toContain('Aucune unité divergente sur le dernier import.')
    expect(said).not.toContain('Aucun produit non pesable sur le dernier import.')
    expect(said).not.toContain("Aucun import dans l'historique.")
    expect(host.querySelectorAll('[data-unread]')).toHaveLength(4)
    expect(said).toContain("Les signalements du dernier import n'ont pas pu être lus")
    expect(said).toContain("L'historique des imports n'a pas pu être lu")
  })

  it('dit « lecture en cours » avant que la première réponse arrive', () => {
    open()
    // Aucun `settle` : les lectures sont en vol, et l'écran ne sait encore rien.
    expect(pageText()).toContain('Lecture des signalements du dernier import…')
    expect(pageText()).toContain("Lecture de l'historique des imports…")
  })

  it('s’ouvre sur un poste qui n’a jamais reçu de catalogue', async () => {
    // L'état d'un poste installé ce matin, et celui du mode démonstration avant le premier
    // dépôt. Aucun import, donc aucun identifiant à nommer, donc `GET /admin/api/imports`
    // lu SANS `?id=` — et la route répondait alors `"findings": null`. Le premier filtre
    // levait, le filet ERR-UI-01 rechargeait, et l'administration se fermait toute seule.
    // Ce que le service répond désormais est tenu par `TestNoListEverComesBackAsNull`,
    // internal/web/dto_test.go ; ce que la page en fait est tenu ici.
    history = []
    findings = []
    catalogProducts = []
    open(
      nominalHealth({
        catalog: null,
        catalog_findings_id: 0,
        catalog_motives: [],
        catalog_source: null,
      }),
    )
    await settle()

    const said = pageText()
    expect(said).toContain('Aucun import enregistré sur ce poste.')
    expect(said).toContain('Aucune anomalie sur le dernier import.')
    expect(said).toContain("Aucun import dans l'historique.")
    expect(said).toContain('Aucune décision locale')
    expect(said).toContain("Aucun import enregistré : rien n'a encore pu être retiré.")
  })

  it('ne dit pas d’un produit qu’il est ABSENT quand le catalogue n’a pas pu être lu', async () => {
    // `'read'` et `'unread'` rendaient la même phrase : un échec de lecture faisait donc
    // affirmer de CHAQUE décision en vigueur que son produit avait quitté le catalogue.
    catalogFails = true
    open(nominalHealth({ decisions: [decision()] }))
    await settle()

    expect(pageText()).toContain("Nom inconnu : le catalogue n'a pas pu être lu")
    expect(pageText()).not.toContain('Produit absent du catalogue en service')
  })
})

describe('les signalements suivent l’import en vigueur', () => {
  it('les relit quand le poste change d’import', async () => {
    findings = manyAnomalies(16)
    const setHealth = openLive(nominalHealth())
    await settle()
    expect(pageText()).toContain('16 anomalies.')

    // Le bénévole corrige Odoo, le producteur redépose, la veille applique : le tableau de
    // bord change d'import. L'encadré du haut le disait déjà ; les anomalies, elles,
    // restaient celles de l'import précédent, et le travail fait se lisait comme vain.
    findings = manyAnomalies(3)
    setHealth(
      nominalHealth({ catalog: { ...FLV_IMPORT, id: 8 }, catalog_findings_id: 8 }),
    )
    await settle()

    expect(pageText()).toContain('3 anomalies.')
    expect(calls.filter((call) => call.url.startsWith('/admin/api/imports'))).toHaveLength(2)
  })

  it('lit ceux du catalogue EN SERVICE, pas ceux d’une ligne « inchangé »', async () => {
    // Le producteur redépose son export à l'identique, ce qu'ADR-015 tient pour un
    // événement NOMINAL : la ligne est enregistrée « inchangé », elle ne bascule rien et
    // elle ne réécrit AUCUN signalement — ils appartiennent à l'import qui a produit la
    // grille, une ligne plus haut. La page lisait ceux du dernier import : l'encadré du
    // haut continuait d'annoncer seize anomalies au-dessus de trois listes qui répondaient
    // toutes « Aucune anomalie sur le dernier import. », et définitivement.
    findings = manyAnomalies(16)
    history = [{ ...FLV_IMPORT, id: 8, result: 'unchanged' }, FLV_IMPORT]
    open(
      nominalHealth({
        catalog: { ...FLV_IMPORT, id: 8, result: 'unchanged' },
        catalog_findings_id: FLV_IMPORT.id,
      }),
    )
    await settle()

    const read = calls.filter((call) => call.url.startsWith('/admin/api/imports'))
    expect(read.map((call) => call.url)).toEqual([
      `/admin/api/imports?id=${String(FLV_IMPORT.id)}`,
    ])
    expect(pageText()).toContain('16 anomalies.')
    expect(pageText()).not.toContain('Aucune anomalie sur le dernier import.')
  })

  it('ne relit rien tant que l’import en vigueur est le même', async () => {
    const setHealth = openLive(nominalHealth())
    await settle()
    setHealth(nominalHealth({ counters: { unlogged_weighings_count: 3, journal_rows_count: 9 } }))
    await settle()

    // Le sondage remplace `health` toutes les trois secondes : une relecture à chaque fois
    // serait vingt lectures par minute pour rien.
    expect(calls.filter((call) => call.url.startsWith('/admin/api/imports'))).toHaveLength(1)
  })
})

describe('le motif, que le poste EXIGE', () => {
  it('désarme les actes que le poste refuserait faute de motif', async () => {
    const health = nominalHealth({ decisions: [decision({ offered: true, min_weight_g: 8 })] })
    open(health)
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    // `internal/web/admin.go` répond 422 « Indiquez le motif de cette décision. » : armer
    // ces boutons, c'est promettre un acte qui finira en bannière rouge — après avoir
    // éventuellement demandé le mot de passe.
    expect(act('offered').disabled).toBe(true)
    type('#decision-waiver', '10')
    expect(act('waiver').disabled).toBe(true)

    type('#decision-reason', 'hors saison')
    expect(act('offered').disabled).toBe(false)
    expect(act('waiver').disabled).toBe(false)
  })

  it('laisse passer sans motif le seul acte qui EFFACE la décision', async () => {
    // « Le proposer de nouveau » sans dérogation en vigueur passe par `ClearDecision` :
    // la ligne disparaît, et une ligne qui n'existe plus n'a rien à expliquer.
    open(nominalHealth({ decisions: [decision({ offered: false, min_weight_g: null })] }))
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    expect(act('offered').disabled).toBe(false)
    act('offered').click()
    await settle()
    expect(decisionBodies()).toHaveLength(1)
  })

  it('dit à l’écran que le motif est une condition, et laquelle', async () => {
    open(nominalHealth({ decisions: [decision()] }))
    await settle()
    buttonNamed('Reprendre cette décision').click()
    flushSync()

    expect(pageText()).toContain('Sans motif, le poste refuse la décision.')
    type('#decision-reason', 'hors saison')
    expect(pageText()).not.toContain('Sans motif, le poste refuse la décision.')
  })
})

describe('ce que le mouvement réduit doit éteindre', () => {
  it('neutralise le retour d’appui du sélecteur de fichier', () => {
    // L'échappatoire globale de `app.css` ne neutralise que `button:active` — et ce
    // sélecteur est un `<label>`, qui n'en est pas un.
    expect(PAGE).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.choose:active[\s\S]*?transform:\s*none/u,
    )
  })
})

describe('aucun jeton anglais du service à l’écran', () => {
  // Le vocabulaire des imports a quitté cette page pour `lib/inventory.ts` : trois écrans
  // le lisent — le tableau de bord, cet historique, et la phrase que « Recharger le
  // catalogue » laisse derrière lui —, et trois copies auraient fini par diverger. La
  // garantie suit le code : c'est le module partagé qui doit couvrir les jetons du noyau.
  it('traduit les quatre résultats d’import du noyau', () => {
    const tokens = [...JOURNAL_GO.matchAll(/\bImport[A-Za-z]+\s*=\s*"(\w+)"/gu)].map((m) => m[1])
    expect(tokens).toHaveLength(4)
    for (const token of tokens) {
      expect(importResultWord(token as string), token).not.toBe('résultat inconnu')
    }
  })

  it('traduit les trois sources de catalogue du noyau', () => {
    const tokens = [...JOURNAL_GO.matchAll(/\bCatalogSource[A-Za-z]+\s*=\s*"(\w+)"/gu)].map(
      (m) => m[1],
    )
    expect(tokens).toHaveLength(3)
    for (const token of tokens) {
      expect(importSourceWord(token as string), token).not.toBe('source inconnue')
    }
  })

  it('annonce l’issue d’un rechargement dans les MÊMES mots que la page Dépannage', async () => {
    // Un acte ne peut pas s'annoncer différemment selon l'écran d'où on l'atteint : c'est
    // la règle déjà écrite à propos de la pastille « clé » sur la même zone de dépôt.
    const applied = { ...FLV_1_IMPORT, id: 9, result: 'applied' }
    const poll = openLive(nominalHealth())
    await settle()

    act('reload').click()
    await settle()
    poll(nominalHealth({ catalog: applied }))
    await settle()

    expect(pageText()).toContain(
      collapse(importOutcomeSentence(applied, nominalHealth().catalog_motives)),
    )
  })

  it('écrit « identique au précédent » là où le service écrit `unchanged`', async () => {
    history = [{ ...FLV_IMPORT, result: 'unchanged' }]
    open()
    await settle()

    const cell = collapse(host.querySelector('[data-result]')?.textContent ?? '')
    expect(cell).toBe('identique au précédent')
    expect(pageText()).not.toContain('unchanged')
    expect(pageText()).not.toContain('local_drop')
  })
})

/**
 * Le panneau qui dit ce que la grille montre, et ce qu'elle ne montre pas.
 *
 * Sans le nombre RÉEL de produits concernés, l'écart entre « 331 pesables » de
 * l'inventaire et « 316 produits pesables » du bandeau client n'est explicable par
 * personne au téléphone. Ce nombre est donc DÉRIVÉ du catalogue en service : un chiffre
 * écrit en dur passerait un test de rendu sans broncher, et mentirait au premier import.
 */
describe('ce que la grille montre des produits vendus à l’unité', () => {
  /** Un catalogue en service : quinze produits à l'unité parmi des produits au poids. */
  function mixedCatalog(): void {
    catalogProducts = [
      ...Array.from({ length: 15 }, (_unused, index) => ({
        id: `u${String(index)}`,
        name: `MELON ${String(index)}`,
        search: `melon ${String(index)}`,
        mode: 'by_unit',
      })),
      ...Array.from({ length: 20 }, (_unused, index) => ({
        id: `w${String(index)}`,
        name: `TOMATE ${String(index)}`,
        search: `tomate ${String(index)}`,
        mode: 'by_weight',
      })),
    ]
  }

  it('écrit le réglage dans le brouillon, sans toucher au bloc catalogue', async () => {
    mixedCatalog()
    const admin = new Admin()
    const draft = new Draft(admin)
    draft.config = localDropConfig()
    component = mount(Catalog, { target: host, props: { admin, draft, health: nominalHealth() } })
    flushSync()
    await settle()

    const box = host.querySelector<HTMLInputElement>(
      '[data-flag="ui.show_by_unit_products"] input',
    )
    expect(box, 'aucun interrupteur pour les produits vendus à l’unité').not.toBeNull()
    expect(box?.checked).toBe(false)

    box?.click()
    flushSync()

    expect(draft.flag('ui.show_by_unit_products')).toBe(true)
    // Le bloc `catalog` n'a pas bougé : un changement là-bas relance la sonde disque et
    // redémarre la source du catalogue, pour un simple réglage d'affichage.
    expect(draft.config?.catalog).toEqual(localDropConfig().catalog)
  })

  it('compte les produits vraiment concernés par le dernier import', async () => {
    mixedCatalog()
    open()
    await settle()

    expect(pageText()).toContain("15 produits vendus à l'unité sont masqués sur ce poste")
  })

  it('dit qu’ils sont montrés quand le poste les montre', async () => {
    mixedCatalog()
    const admin = new Admin()
    const draft = new Draft(admin)
    draft.config = { ...localDropConfig(), ui: { show_by_unit_products: true } }
    component = mount(Catalog, { target: host, props: { admin, draft, health: nominalHealth() } })
    flushSync()
    await settle()

    expect(pageText()).toContain("15 produits vendus à l'unité sont montrés dans la grille")
  })

  it('n’affirme aucun nombre quand le catalogue n’a pas pu être lu', async () => {
    catalogFails = true
    open()
    await settle()

    const said = pageText()
    expect(said).not.toMatch(/\d+ produits vendus à l'unité/u)
    expect(said).toContain("Le catalogue en service n'a pas pu être lu")
  })

  it('dit ce qu’un poste perd en masquant ces produits', async () => {
    mixedCatalog()
    open()
    await settle()

    // Ce n'est pas de l'ornement : une tuile à l'unité imprime une étiquette SANS jamais
    // lire la balance, et c'est le seul geste que ce réglage retire.
    expect(pageText()).toContain('sans jamais lire la balance')
  })
})

/**
 * Le nombre de colonnes de la grille, et la phrase qui dit ce qu'il donne.
 *
 * Ce qui se vérifie ici n'est PAS le compte : jsdom ne fait aucune mise en page — ni
 * `clamp()`, ni `auto-fill`, ni `gridTemplateColumns` résolu, ni `clientWidth` de sonde —
 * et les trois sondes de cette page répondent donc zéro, exactement comme `document.fonts`
 * est absent pour la grille du client. Ce qui se vérifie est le REPLI : la phrase se
 * réduit à ce qu'elle sait, et elle n'écrit jamais « 0 colonnes × 0 rangées ».
 *
 * Les nombres, eux, se mesurent au navigateur : c'est le seul endroit où ils existent.
 */
describe('les colonnes de la grille, réglables sans cesser d’être automatiques', () => {
  /** Le groupe des onze choix, ou null quand la page ne le dessine pas. */
  function choices(): HTMLInputElement[] {
    return [...host.querySelectorAll<HTMLInputElement>('[role="radiogroup"] input[name="grid-columns"]')]
  }

  /** Ce que la phrase chiffrée dit, ramenée à une seule forme d'apostrophe. */
  function countText(): string {
    return collapse(host.querySelector('[data-grid-count]')?.textContent ?? '')
  }

  /** Monte la page sur un brouillon donné, et rend ce brouillon. */
  function openWith(ui: Record<string, unknown>): Draft {
    const admin = new Admin()
    const draft = new Draft(admin)
    draft.config = { ...localDropConfig(), ui }
    component = mount(Catalog, { target: host, props: { admin, draft, health: nominalHealth() } })
    flushSync()
    return draft
  }

  it('offre exactement les valeurs que le noyau accepte, sans les recopier de tête', () => {
    // Les bornes vivent dans `internal/domain/config.go` — c'est lui qui refuse, par le
    // contrôle 49, ce que cet écran ne devrait pas proposer. Elles étaient jusqu'ici
    // écrites une seconde fois ici, sans rien pour les relier : le jour où la mesure
    // déplace la borne haute, un écran qui offre encore l'ancienne valeur produit un choix
    // que l'enregistrement REFUSE — un bouton dont la seule réponse possible est un refus,
    // ce qu'ADR-049 nomme comme un contrôle qui n'aurait pas dû exister.
    //
    // Lire le Go depuis un banc du front est le geste que ce fichier emploie déjà pour les
    // jetons d'import (JOURNAL_GO plus haut).
    const configGo = readFileSync(resolve(HERE, '../../internal/domain/config.go'), 'utf8')
    const bound = (name: string): number => {
      const found = new RegExp(String.raw`\b${name}\s*=\s*(\d+)`, 'u').exec(configGo)
      if (found === null) throw new Error(`${name} introuvable dans config.go`)
      return Number(found[1])
    }

    const declared = /GRID_COLUMNS_CHOICES = \[([\d, ]+)\]/u.exec(PAGE)?.[1]
    if (declared === undefined) throw new Error('GRID_COLUMNS_CHOICES introuvable dans la page')
    const offered = declared.split(',').map((n) => Number(n.trim()))

    const min = bound('MinGridColumns')
    const max = bound('MaxGridColumns')
    const expected = Array.from({ length: max - min + 1 }, (_, i) => min + i)
    expect(offered).toEqual(expected)

    // Et « Automatique » est le zéro du noyau, pas un treizième choix inventé par l'écran.
    expect(bound('GridColumnsAutomatic')).toBe(0)
    expect(offered).not.toContain(0)
  })

  it('offre onze choix visibles d’un coup, et non une glissière', async () => {
    open()
    await settle()

    // Ce sont des entiers qu'on nomme, et « Automatique » n'est pas un cran de plus au
    // bout d'une course : c'est la grille qui décide toute seule, sur n'importe quel écran.
    expect(choices()).toHaveLength(11)
    expect(host.querySelector('input[type="range"]')).toBeNull()
    expect(pageText()).toContain('Automatique')
    expect(choices().map((box) => box.value)).toEqual([
      '0', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12',
    ])
  })

  it('s’ouvre sur « Automatique » quand le fichier ne porte pas la clé', async () => {
    // Le défaut n'est pas un nombre, c'est un COMPORTEMENT : une coopérative qui n'y
    // touche jamais garde la densité continue, et le 4K continue d'en montrer dix.
    open()
    await settle()

    expect(choices()[0]?.checked).toBe(true)
    expect(choices().filter((box) => box.checked)).toHaveLength(1)
  })

  it('écrit le nombre choisi dans le brouillon, sans toucher au bloc catalogue', async () => {
    const draft = openWith({})
    await settle()

    const seven = choices().find((box) => box.value === '7')
    expect(seven, 'aucun choix à 7 colonnes').not.toBeUndefined()
    seven?.click()
    flushSync()

    expect(draft.number('ui.grid_columns')).toBe(7)
    // Un changement dans le bloc `catalog` relance la sonde disque et redémarre la source :
    // un prix à payer sans rapport avec le nombre de colonnes d'une grille.
    expect(draft.config?.catalog).toEqual(localDropConfig().catalog)
  })

  it('revient à « Automatique » en écrivant zéro, et non en effaçant la clé', async () => {
    // Zéro EST une valeur du contrat : un fichier qui porte `0` dit « la grille décide »,
    // et c'est ce que le poste relit. Effacer la clé dirait la même chose au poste et
    // rien du tout à celui qui ouvre le fichier.
    const draft = openWith({ grid_columns: 7 })
    await settle()

    choices()[0]?.click()
    flushSync()

    expect(draft.number('ui.grid_columns')).toBe(0)
  })

  it('relit le réglage en vigueur plutôt que de partir du premier choix', async () => {
    openWith({ grid_columns: 9 })
    await settle()

    expect(choices().find((box) => box.value === '9')?.checked).toBe(true)
    expect(choices().filter((box) => box.checked)).toHaveLength(1)
  })

  it('n’écrit jamais « 0 colonnes × 0 rangées » quand rien n’a pu être mesuré', async () => {
    // Le repli, et c'est LE sujet de ce fichier : une sonde qui répond zéro n'est pas une
    // grille vide, c'est un navigateur qui n'a pas répondu. Les deux phrases se réduisent
    // à ce qu'elles savent.
    openWith({ grid_columns: 7 })
    await settle()

    const said = countText()
    expect(said).not.toContain('0 colonnes')
    expect(said).not.toContain('0 rangées')
    expect(said).not.toMatch(/× 0/u)
    expect(said).not.toContain('tuiles d\'un coup')
  })

  it('dit tout de même les 7 colonnes, qui sont le contrat et non une mesure', async () => {
    openWith({ grid_columns: 7 })
    await settle()

    // « 7 colonnes » veut dire 7 partout : ce nombre-là ne se mesure pas, il se lit dans
    // le réglage. Ce qui manque en repli, ce sont les rangées, et la phrase le DIT.
    expect(countText()).toContain('7 colonnes sur tous les écrans')
    expect(countText()).toContain('ne sait pas dire combien de rangées')
  })

  it('n’affirme aucun nombre sur « Automatique » quand rien n’a pu être mesuré', async () => {
    // Là, aucun nombre n'est connu de personne : ni de l'écran, ni du fichier. La phrase
    // garde ce qui reste vrai — que la grille suit la largeur de l'écran.
    open()
    await settle()

    const said = countText()
    expect(said).toContain('la grille suit la largeur de l\'écran')
    expect(said).toContain('sans qu\'on y revienne')
    expect(said).not.toMatch(/\d+ colonnes ×/u)
  })

  it('ne prétend pas mesurer l’écran du poste depuis un portable', async () => {
    // `admin_on_lan` permet d'ouvrir cette page depuis ailleurs, et le compte vaut alors
    // pour l'écran qu'on lit. Sans mesure il n'y a aucun compte à qualifier, donc rien à
    // dire : la mention naît avec le nombre qu'elle relativise.
    open()
    await settle()

    expect(host.querySelector('[data-other-screen]')).toBeNull()
  })

  it('porte la déclaration de grille du BROUILLON sur sa sonde, et pas une autre', () => {
    // La sonde et la grille du client tirent la MÊME chaîne du même module : deux
    // littéraux dans deux fichiers, c'est ainsi qu'une sonde finit par compter les
    // colonnes d'une grille que personne ne dessine. Le mode automatique doit rester le
    // mode automatique à l'octet près — c'est ce que la densité continue a acheté.
    expect(PAGE).toContain("from '../../lib/grid'")
    expect(PAGE).toContain('gridBox.style.gridTemplateColumns = gridTemplateColumns(columns)')
    expect(gridTemplateColumns(GRID_COLUMNS_AUTO)).toBe(
      'repeat(auto-fill, minmax(var(--tile-min), 1fr))',
    )
    // `minmax(0, 1fr)` et jamais `1fr`, qui vaut `minmax(auto, 1fr)` : une piste `auto` ne
    // descend pas sous la largeur min-content de « CRANBERRY/CANNEBERGES ».
    expect(gridTemplateColumns(7)).toBe('repeat(7, minmax(0, 1fr))')
  })

  it('lit les hauteurs des trois barres dans leurs jetons, jamais en pixels', () => {
    // Le jour où l'une d'elles change de hauteur, ce compte suit sans qu'on y pense.
    expect(PAGE).toMatch(
      /100vh - var\(--banner-height\) - var\(--category-height\) - var\(--status-height\)/u,
    )
    expect(PAGE).toContain('grid-auto-rows: minmax(var(--tile-height), auto)')
    expect(PAGE).toContain('width: var(--tile-min)')
  })

  it('mesure une TUILE et jamais un jeton de hauteur', () => {
    // La campagne du 01/08 a mesuré l'écart : `--tile-height` laisse le bloc des prix
    // hors de son calcul, dont les corps ne rétrécissent pas avec la tuile. 189 px
    // annoncés contre 245 dessinés à 7 colonnes sur 1920 — l'écran aurait promis trois
    // rangées d'une grille qui en montre deux. La sonde porte donc une vraie tuile.
    expect(PAGE).toContain('<Tile')
    expect(PAGE).toContain('offsetHeight')
    // Et le bloc des prix suit l'interrupteur du poste, qu'aucun écran n'édite : prix
    // masqués, la même grille rend 7 × 3 ; prix affichés, 7 × 2.
    expect(PAGE).toContain("draftedFlag(\n                'ui.show_grid_prices',")
  })

  it('ne tombe pas sur un poste qui publie moins que ce que le contrat déclare', async () => {
    // Un binaire plus ancien peut ne pas servir le bloc de présentation, et lire au
    // travers emporterait TOUTE la page d'administration pour un aperçu. C'est le même
    // garde-fou que le `?? []` du brouillon sur une liste que le service ne sert plus.
    vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/v1/catalog') {
        return json({ products: [{ id: 'p1', name: 'TOMATE', search: 'tomate', mode: 'by_weight' }] })
      }
      return fakeFetch(input, init)
    })
    open()
    await settle()

    expect(pageText()).toContain('Dernier import')
    expect(host.querySelector('[data-grid-count]')).not.toBeNull()
  })

  it('n’écrit aucun plancher typographique en dur : il vient du module qui le déclare', () => {
    // Le plancher est en cours de mesure — 18 px aujourd'hui, 16 px attendu. Recopié ici,
    // il mentirait dans la phrase le jour où il descend.
    expect(PAGE).toContain('NAME_SIZE_MIN_PX')
    expect(PAGE).not.toMatch(/plancher de 1[468] px/u)
  })
})

/**
 * Le seuil de puce (ADR-059), réglé juste à côté des colonnes de la grille.
 *
 * Un champ nombre et non des boutons radio : contrairement aux colonnes, ce seuil n'a
 * pas de plafond, donc rien à énumérer. Ce qui se vérifie ici est le même geste que pour
 * les colonnes — le brouillon, et lui seul, change — et non une mesure de mise en page.
 */
describe('le seuil de puce, réglable à côté des colonnes de la grille', () => {
  /** Monte la page sur un brouillon donné, et rend ce brouillon. */
  function openWith(ui: Record<string, unknown>): Draft {
    const admin = new Admin()
    const draft = new Draft(admin)
    draft.config = { ...localDropConfig(), ui }
    component = mount(Catalog, { target: host, props: { admin, draft, health: nominalHealth() } })
    flushSync()
    return draft
  }

  it('écrit le seuil tapé dans le brouillon, sans toucher au bloc catalogue', async () => {
    const draft = openWith({})
    await settle()

    type('#chip-threshold', '3')

    expect(draft.number('ui.min_products_for_chip')).toBe(3)
    // Un changement dans le bloc `catalog` relance la sonde disque et redémarre la
    // source : un prix à payer sans rapport avec un seuil d'affichage.
    expect(draft.config?.catalog).toEqual(localDropConfig().catalog)
  })

  it('relit le seuil en vigueur plutôt que d’afficher zéro', async () => {
    openWith({ min_products_for_chip: 7 })
    await settle()

    expect(host.querySelector<HTMLInputElement>('#chip-threshold')?.value).toBe('7')
  })
})

/**
 * La phrase chiffrée, sur une mise en page FEINTE.
 *
 * Ce banc ne mesure rien et ne prétend rien mesurer : jsdom ne fait aucune mise en page,
 * donc les sondes reçoivent ici une réponse écrite à la main — sept pistes de 260 px, une
 * gouttière de 8 px, une zone de grille de 700 px, une tuile de 220 px. Ce qui est tenu
 * est la RÉDACTION de ce qui en sort : l'ordre colonnes × rangées, les accords, et le
 * fait que la mention « cet écran n'est pas celui du poste » naisse avec le nombre
 * qu'elle relativise.
 *
 * Les vrais nombres se mesurent au navigateur, sur les trois résolutions du dossier de
 * conception, et nulle part ailleurs.
 */
describe('ce que la phrase dit quand le navigateur, lui, a répondu', () => {
  /** La réponse feinte des sondes : elle ne dépend d'aucun réglage, et c'est voulu. */
  const LAYOUT = {
    tracks: Array.from({ length: 7 }, () => '260px').join(' '),
    rowGap: '8px',
    calibrationWidth: 352,
    viewportHeight: 700,
    /** La hauteur de la CELLULE qui porte la tuile — bloc des prix compris. */
    rowHeight: 220,
    nameBoxWidth: 200,
    nameBoxHeight: 90,
  }

  /** Ce que la vraie fenêtre déclare, pour la remettre en place après coup. */
  let realLocation: PropertyDescriptor | undefined

  beforeEach(() => {
    const real = window.getComputedStyle.bind(window)
    vi.stubGlobal('getComputedStyle', (element: Element, pseudo?: string | null) => {
      if (element.classList.contains('grid-probe')) {
        return {
          gridTemplateColumns: LAYOUT.tracks,
          rowGap: LAYOUT.rowGap,
        } as unknown as CSSStyleDeclaration
      }
      return real(element as Element, pseudo)
    })
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
      configurable: true,
      get(this: HTMLElement): number {
        if (this.classList.contains('calibration-probe')) return LAYOUT.calibrationWidth
        if (this.classList.contains('name-box')) return LAYOUT.nameBoxWidth
        return 0
      },
    })
    Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
      configurable: true,
      get(this: HTMLElement): number {
        if (this.classList.contains('viewport-probe')) return LAYOUT.viewportHeight
        if (this.classList.contains('name-box')) return LAYOUT.nameBoxHeight
        return 0
      },
    })
    // La hauteur de RANGÉE vient de la cellule qui porte une vraie tuile, et non plus
    // d'une sonde de `var(--tile-height)` : ce jeton laisse le bloc des prix hors de son
    // calcul, et la campagne du 01/08 l'a mesuré 30 % sous ce que le navigateur dessine.
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
      configurable: true,
      get(this: HTMLElement): number {
        return this.classList.contains('row-probe') ? LAYOUT.rowHeight : 0
      },
    })
  })

  afterEach(() => {
    // Les deux propriétés sont déclarées en lecture seule et vivent sur `Element` : ce
    // qui est posé ci-dessus est une propriété PROPRE de `HTMLElement` qui les masque, et
    // la retirer rend jsdom à ce qu'il était pour les autres bancs du fichier.
    Reflect.deleteProperty(HTMLElement.prototype, 'clientWidth')
    Reflect.deleteProperty(HTMLElement.prototype, 'clientHeight')
    Reflect.deleteProperty(HTMLElement.prototype, 'offsetHeight')
    if (realLocation !== undefined) Object.defineProperty(window, 'location', realLocation)
    realLocation = undefined
  })

  /** Fait croire à la page qu'elle est lue depuis un portable du réseau. */
  function readFromTheLan(): void {
    realLocation = Object.getOwnPropertyDescriptor(window, 'location')
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { hostname: '192.168.1.20' },
    })
  }

  /** Cent tuiles au poids : de quoi que « tient en N écrans » dise quelque chose. */
  function hundredTiles(): void {
    catalogProducts = Array.from({ length: 100 }, (_unused, index) => ({
      id: `w${String(index)}`,
      name: `TOMATE ${String(index)}`,
      search: `tomate ${String(index)}`,
      mode: 'by_weight',
    }))
  }

  /** Monte la page sur un brouillon donné. */
  function openWith(ui: Record<string, unknown>): void {
    const admin = new Admin()
    const draft = new Draft(admin)
    draft.config = { ...localDropConfig(), ui }
    component = mount(Catalog, { target: host, props: { admin, draft, health: nominalHealth() } })
    flushSync()
  }

  /** Ce que la phrase chiffrée dit, ramenée à une seule forme d'apostrophe. */
  function countText(): string {
    return collapse(host.querySelector('[data-grid-count]')?.textContent ?? '')
  }

  it('dit les colonnes AVANT les rangées, et ce que cela fait de tuiles', async () => {
    hundredTiles()
    openWith({ grid_columns: 7 })
    await settle()

    // C'est le vocabulaire dans lequel la demande est arrivée, et donc celui dans lequel
    // elle se vérifie : 7 × 3, et 21 tuiles d'un coup.
    expect(countText()).toContain('7 colonnes × 3 rangées — 21 tuiles d\'un coup')
  })

  it('dit en combien d’écrans la grille entière tient', async () => {
    hundredTiles()
    openWith({ grid_columns: 7 })
    await settle()

    expect(countText()).toContain('Les 100 tuiles de la grille tiennent en 5 écrans.')
  })

  it('compte les tuiles que le BROUILLON montre, l’interrupteur d’à côté compris', async () => {
    // Les deux réglages de ce panneau agissent sur la même grille : annoncer cent tuiles
    // au-dessus d'un interrupteur qui vient d'en retirer quinze serait faux avant même
    // d'être enregistré.
    catalogProducts = [
      ...Array.from({ length: 85 }, (_unused, index) => ({
        id: `w${String(index)}`,
        name: `TOMATE ${String(index)}`,
        search: `tomate ${String(index)}`,
        mode: 'by_weight',
      })),
      ...Array.from({ length: 15 }, (_unused, index) => ({
        id: `u${String(index)}`,
        name: `MELON ${String(index)}`,
        search: `melon ${String(index)}`,
        mode: 'by_unit',
      })),
    ]
    openWith({ grid_columns: 7 })
    await settle()

    expect(countText()).toContain('Les 85 tuiles de la grille tiennent en 5 écrans.')
  })

  it('dit sur « Automatique » ce que l’écran fait ET que ce n’est pas figé', async () => {
    hundredTiles()
    openWith({})
    await settle()

    const said = countText()
    expect(said).toContain('Automatique : 7 colonnes × 3 rangées sur cet écran.')
    expect(said).toContain('Un écran plus large en montrera davantage sans qu\'on y revienne.')
  })

  it('ne nomme pas l’écran du poste quand la page est lue sur celui du poste', async () => {
    hundredTiles()
    openWith({ grid_columns: 7 })
    await settle()

    expect(host.querySelector('[data-other-screen]')).toBeNull()
  })

  it('dit que le compte vaut pour l’écran qu’on lit, quand ce n’est pas celui du poste', async () => {
    // L'administration s'ouvre depuis un portable du réseau, et le compte est alors celui
    // du portable. Le cas est NOMMÉ plutôt que subi : zéro donnée en plus, zéro route en
    // plus, et une erreur de réglage se voit sur le poste et se répare en revenant ici.
    readFromTheLan()
    hundredTiles()
    openWith({ grid_columns: 7 })
    await settle()

    expect(collapse(host.querySelector('[data-other-screen]')?.textContent ?? '')).toBe(
      "Cet écran n'est pas celui du poste : ce compte vaut pour l'écran que vous lisez.",
    )
  })
})
