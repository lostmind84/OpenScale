import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Station from '../src/admin/pages/Station.svelte'
import type { ConfigVersionDTO, FaultDTO } from '../src/admin/lib/dto'
import { Draft } from '../src/admin/lib/draft.svelte'
import { Admin } from '../src/admin/lib/session.svelte'
import { nominalHealth } from './fixtures/health'

/**
 * La page Poste de §14.4, et les cinq choses qu'elle disait de faux ou pas du tout.
 *
 * C'est la page du CLONAGE de §11.5 : un exploitant y exporte le poste de référence, porte
 * le fichier sur les trois autres, et vérifie que l'empreinte affichée est la même. Ce que
 * ce fichier tient :
 *
 *  1. la colonne « En service » montre la configuration EN SERVICE, et non le brouillon —
 *     comparer un fichier apporté à sa propre saisie non enregistrée fait lire « rien ne
 *     change » sur le champ qui est précisément sur le point de changer ;
 *  2. le diff SE RELIT : il était calculé une fois, à l'import, et gardé tel quel — après
 *     la recopie il montrait encore un « en service » qui n'existait plus nulle part ;
 *  3. l'export est un acte PROTÉGÉ (ADR-033, §4.2 : il emporte encore l'empreinte du mot
 *     de passe). Une ancre `<a download>` n'a aucun moyen de voir un 401 ;
 *  4. l'import et la restauration d'une version sont protégés eux aussi, et se REJOUENT
 *     après le mot de passe ;
 *  5. le diff est BORNÉ et annonce son total : une configuration porte plus de cent
 *     feuilles, et un clone diffère sur presque toutes.
 *
 * Et huit choses qu'une relecture adversariale y a trouvées de FAUX :
 *
 *  6. « identique » et « rien à quoi comparer » sont deux états. Le diff est vide dès que
 *     la configuration en service n'a pas pu être lue, et le pavé vert rassurant
 *     s'affichait sous le bandeau rouge qui disait le contraire ;
 *  7. les cinq versions ont un troisième état : la liste vide était à la fois « aucune
 *     version » et « je n'ai pas encore lu » — puis « on m'a refusé la lecture » ;
 *  8. `modified_at` n'est PAS comparé : chaque poste écrit le sien, deux postes que §11.5
 *     déclare identiques différaient donc toujours d'un champ ;
 *  9. l'export sans le matériel retire SIX choses, pas trois, et l'écran nomme celles que
 *     le fichier ne porte pas avant que « Recopier » n'en recopie le vide ;
 * 10. un contrôle qui refuse dit AUSSI les valeurs qui marcheraient (§11.4 étape 1) ;
 * 11. le retour tactile ne répond pas à un geste mort ;
 * 12. la page n'affirme pas un téléchargement qu'elle ne peut pas constater, et ne libère
 *     pas l'URL de l'objet dans le tour de boucle du clic — c'est ce qui l'annule ;
 * 13. les trois champs éditables allument la faute qui les nomme (§11.3).
 */

/** La page elle-même : une règle CSS ne s'observe pas dans jsdom, elle se lit. */
const STATION_SVELTE = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin/pages/Station.svelte'),
  'utf8',
)

/** L'instant que le poste cible a écrit dans son propre fichier. */
const SERVED_STAMP = '2026-07-24T09:12:00.000Z'
/** Celui que porte le fichier apporté du poste de référence. Il ne sera jamais le même. */
const FILE_STAMP = '2026-01-19T16:40:00.000Z'

let host: HTMLElement
let component: unknown

/** Ce que le banc a vu passer : la route, la méthode et le corps. */
interface Call {
  url: string
  method: string
  body: unknown
}

let calls: Call[] = []
/** La configuration EN SERVICE, celle que `GET /admin/api/config` sert. */
let servedConfig: Record<string, unknown> = {}
/** Le document que `POST /admin/api/config/import` dit avoir lu dans le fichier. */
let importedConfig: Record<string, unknown> = {}
/** Ce que les 47 contrôles de §11.3 disent du fichier importé. */
let importFaults: FaultDTO[] = []
/** Les versions restaurables que le poste publie. */
let versions: ConfigVersionDTO[] = []
/** Combien de refus 401 chaque acte protégé doit encore opposer. */
let refusals: Record<string, number> = {}
/** Ce que le navigateur a été prié d'enregistrer : un nom de fichier par téléchargement. */
let downloads: string[] = []
/** Les URL d'objet libérées, dans l'ordre. */
let revoked: string[] = []
/** Combien d'URL étaient déjà libérées quand le navigateur a pris le téléchargement. */
let revokedAtHandover: number[] = []
/** Vrai quand `GET /admin/api/config` refuse : la configuration en service est illisible. */
let configUnreadable = false
/** Vrai quand la lecture des versions ne répond JAMAIS : l'écran est en cours de lecture. */
let versionsSilent = false
/** Vrai quand la lecture des versions est refusée : l'écran n'a pas lu, il a été refusé. */
let versionsRefused = false

beforeEach(() => {
  calls = []
  servedConfig = configWithPort('COM8')
  // Le fichier vient d'un AUTRE poste : il porte l'instant où cet autre poste a été écrit.
  importedConfig = configWithPort('COM3', FILE_STAMP)
  importFaults = []
  versions = []
  refusals = {}
  downloads = []
  revoked = []
  revokedAtHandover = []
  configUnreadable = false
  versionsSilent = false
  versionsRefused = false
  host = document.createElement('div')
  document.body.appendChild(host)
  vi.stubGlobal('fetch', fakeFetch)
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation((address: string) => {
    revoked.push(address)
  })
  // Un téléchargement est un clic sur une ancre jetable : on l'intercepte plutôt que de
  // laisser jsdom tenter une navigation qu'il n'implémente pas.
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloads.push(this.download)
    // Un navigateur ne télécharge pas DANS le tour de boucle du clic : il met le
    // téléchargement en file et lit l'adresse après. Ce compteur est donc ce que lui
    // verra, et non ce qu'on voit d'ici.
    queueMicrotask(() => revokedAtHandover.push(revoked.length))
  })
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

/**
 * Une configuration de poste, dont seuls le port série et l'instant d'écriture varient.
 *
 * Elle porte `modified_at`, `network` et `catalog` parce que le document comparé est le
 * `domain.Config` ENTIER : la charge qui ne les portait pas laissait passer un diff qui
 * ne pouvait jamais être vide entre deux postes.
 */
function configWithPort(port: string, modifiedAt = SERVED_STAMP): Record<string, unknown> {
  return {
    modified_at: modifiedAt,
    station: { number: 2, name: 'Poste 2 — fruits', coop: 'La Cagette' },
    network: { listen: '127.0.0.1:8080', admin_on_lan: false },
    scale: { type: 'gram_xfoc', present: true, options: { port, baud_rate: 9600 } },
    printer: { type: 'raster', options: { transport: 'local', queue: 'Étiqueteuse' } },
    catalog: { source: 'webdav', options: { url: 'https://dav.local/flv', user: 'poste1' } },
    limits: { min_weight_g: 10, max_weight_g: 15_000 },
  }
}

/**
 * La configuration en service telle que le POSTE la sert : le mot de passe WebDAV blanchi.
 *
 * `configPayload` remplace le secret par une chaîne vide et ne retire pas la clé — elle
 * doit survivre à l'aller-retour, sans quoi le bloc `catalog` aurait bougé sans que
 * personne n'y touche. C'est cette chaîne vide que le diff comparait à une clé absente.
 */
function configWithBlankedSecret(): Record<string, unknown> {
  const config = configWithPort('COM8')
  const catalog = config.catalog as Record<string, unknown>
  catalog.options = { url: 'https://dav.local/flv', user: 'poste1', password: '' }
  return config
}

/**
 * Un export, tel que le poste le rend : `Config.Export` SUPPRIME la clé du mot de passe,
 * que `hardware` vaille 0 ou 1. Deux secrets ne quittent jamais le poste (§11.5).
 */
function exportWithoutTheSecret(): Record<string, unknown> {
  const config = configWithPort('COM3', FILE_STAMP)
  const catalog = config.catalog as Record<string, unknown>
  catalog.options = { url: 'https://dav.local/flv', user: 'poste1' }
  return config
}

/**
 * Un export « sans le matériel », tel que le poste le REND après l'avoir relu.
 *
 * `Config.Export(false)` vide six choses et `importConfig` n'en rétablit qu'une, le
 * numéro : le nom du poste revient à vide, le réseau à vide, et les trois cartes
 * d'options à `null` — compte WebDAV compris.
 */
function hardwareFreeExport(): Record<string, unknown> {
  return {
    modified_at: FILE_STAMP,
    station: { number: 2, name: '', coop: 'La Cagette' },
    network: { listen: '', admin_on_lan: false },
    scale: { type: 'gram_xfoc', present: true, options: null },
    printer: { type: 'raster', options: null },
    catalog: { source: 'webdav', options: null },
    limits: { min_weight_g: 10, max_weight_g: 15_000 },
  }
}

/** Une configuration à 137 feuilles, pour voir ce que fait un diff qui ne tient pas. */
function configWithManyFields(value: number): Record<string, unknown> {
  const limits: Record<string, number> = {}
  for (let index = 0; index < 137; index += 1) limits[`threshold_${String(index)}`] = value
  return { limits }
}

/** Les routes que cette page touche, et rien d'autre. */
function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const url = String(input)
  const method = init?.method ?? 'GET'
  const raw = init?.body
  calls.push({ url, method, body: typeof raw === 'string' ? JSON.parse(raw) : raw })

  if (url.startsWith('/admin/api/config/export')) {
    if (refused('export')) return problem()
    return json(servedConfig)
  }
  if (url === '/admin/api/config/import') {
    if (refused('import')) return problem()
    return json({ ...payload(importedConfig), faults: importFaults, changed_blocks: ['scale'] })
  }
  if (url === '/admin/api/config/restore') {
    if (refused('restore')) return problem()
    return json(payload(servedConfig))
  }
  if (url === '/admin/api/config/versions') {
    // Une lecture qui ne répond jamais : c'est l'instant qu'aucun état de chargement ne
    // couvrait, et pendant lequel la page affirmait un fait sur le passé du poste.
    if (versionsSilent) return new Promise<Response>(() => undefined)
    if (versionsRefused) return problem()
    return json({ versions })
  }
  if (url === '/admin/api/config') {
    if (configUnreadable) return json({ code: '', message: 'Configuration illisible.' }, 500)
    return json(payload(servedConfig))
  }
  if (url === '/admin/api/health') return json(nominalHealth())
  return json({ expires_at: '', session_minutes: 30 })
}

/** Vrai quand cet acte doit encore prendre un 401, et le décompte. */
function refused(act: string): boolean {
  const left = refusals[act] ?? 0
  if (left <= 0) return false
  refusals[act] = left - 1
  return true
}

/** La charge de `GET /admin/api/config`, telle que `internal/web/config.go` l'écrit. */
function payload(config: Record<string, unknown>): Record<string, unknown> {
  return {
    config,
    config_fingerprint: 'a1b2c3d4',
    retired_keys: [],
    pending_confirmation: null,
  }
}

/** Une réponse JSON, comme le service en écrit. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/** Le refus d'une session absente, dans la forme exacte de `guard.go`. */
function problem(): Promise<Response> {
  return json({ code: '', message: 'Session expirée ou absente.' }, 401)
}

/** Monte la page sur une configuration déjà lue, et rend la session et le brouillon. */
async function open(): Promise<{ admin: Admin; draft: Draft }> {
  const admin = new Admin(60_000)
  const draft = new Draft(admin)
  await draft.load()
  component = mount(Station, {
    target: host,
    props: { admin, draft, health: nominalHealth() },
  })
  flushSync()
  await settle()
  return { admin, draft }
}

/** Laisse les lectures se terminer, puis met le DOM à jour. */
async function settle(): Promise<void> {
  for (let round = 0; round < 8; round += 1) {
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
  const wanted = collapse(label).toLowerCase()
  const found = [...host.querySelectorAll('button')].find((button) =>
    collapse(button.textContent ?? '')
      .toLowerCase()
      .startsWith(wanted),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${label} »`)
  return found
}

/**
 * Les deux valeurs qu'une ligne du diff affiche pour un chemin de clé.
 *
 * La ligne se retrouve par son `data-path` et non par le chemin écrit dedans : la colonne
 * « Champ » nomme désormais le champ en français, et le chemin n'est à l'écran que sous
 * l'interrupteur des noms techniques. Ce que ce banc vérifie est ce que la ligne COMPARE,
 * pas comment elle se nomme — c'est le sujet de `admin-technical-names.test.ts`.
 */
function diffRow(path: string): { before: string; after: string } {
  const rows = [...host.querySelectorAll('[data-diff] tbody tr')]
  const found = rows.find((row) => row.getAttribute('data-path') === path)
  if (found === undefined) throw new Error(`aucune ligne « ${path} » dans le diff`)
  const cells = [...found.querySelectorAll('td')].map((cell) => collapse(cell.textContent ?? ''))
  return { before: cells[1] ?? '', after: cells[2] ?? '' }
}

/** Choisit un fichier de configuration, exactement comme un exploitant le fait. */
function chooseFile(name: string, body: unknown): void {
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')
  if (input === null) throw new Error('aucun sélecteur de fichier sur cette page')
  const file = new File([JSON.stringify(body)], name, { type: 'application/json' })
  Object.defineProperty(input, 'files', {
    value: { item: () => file, length: 1 },
    configurable: true,
  })
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

/** Combien de fois une route exacte a été appelée. */
function timesCalled(url: string): number {
  return calls.filter((call) => call.url === url).length
}

/** Une version restaurable, telle que `GET /admin/api/config/versions` la publie. */
function version(number: number): ConfigVersionDTO {
  return {
    version: number,
    modified_at: '2026-07-24T12:00:00.000Z',
    config_fingerprint: `fingerp${String(number)}`,
  }
}

describe('la colonne « En service » montre ce qui est en service', () => {
  it('compare le fichier apporté au poste, et jamais au brouillon', async () => {
    const { draft } = await open()
    // L'exploitant a tapé COM9 sans enregistrer : c'est son brouillon, pas le poste.
    draft.set('scale.options.port', 'COM9')
    flushSync()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    // « En service » est COM8 : ce que le poste sert. Comparé au brouillon, cette ligne
    // aurait annoncé COM9 — une valeur qui n'est en service nulle part.
    expect(diffRow('scale.options.port')).toEqual({ before: 'COM8', after: 'COM3' })
  })
})

describe('le diff se relit', () => {
  it('ne garde pas l’instantané pris au moment de l’import', async () => {
    await open()
    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()
    expect(diffRow('scale.options.port')).toEqual({ before: 'COM8', after: 'COM3' })

    // Le poste change sous l'écran : une version restaurée ailleurs, un enregistrement.
    // Le fichier apporté décrit désormais exactement ce qui tourne.
    servedConfig = configWithPort('COM3')
    buttonNamed('Recopier').click()
    await settle()

    expect(pageText()).toContain('décrit la même configuration que celle en service')
    expect(host.querySelectorAll('[data-diff] tbody tr')).toHaveLength(0)
  })
})

describe('l’export est un acte protégé (ADR-033)', () => {
  it('n’expose plus d’ancre nue, qui n’a aucun moyen de voir un refus', async () => {
    await open()
    expect(host.querySelector('a[download]')).toBeNull()
  })

  it('demande le mot de passe sur 401, puis REJOUE l’export', async () => {
    refusals = { export: 1 }
    const { admin } = await open()

    buttonNamed('Exporter tout').click()
    await settle()

    expect(admin.pending).not.toBeNull()
    await admin.answerPassword('openscale')
    await settle()

    expect(calls.filter((call) => call.url.startsWith('/admin/api/config/export'))).toHaveLength(2)
    // Le fichier arrive quand même : personne n'a eu à recommencer le geste.
    expect(downloads).toHaveLength(1)
    expect(downloads[0]).toContain('poste2')
  })

  it('nomme distinctement l’export sans le matériel, qui n’est pas le même fichier', async () => {
    await open()

    buttonNamed('Exporter sans le matériel').click()
    await settle()

    expect(downloads[0]).toContain('sans-materiel')
  })
})

describe('l’import est un acte protégé, et le poste lit le fichier', () => {
  it('demande le mot de passe sur 401, puis REJOUE l’import', async () => {
    refusals = { import: 1 }
    const { admin } = await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    expect(admin.pending).not.toBeNull()
    await admin.answerPassword('openscale')
    await settle()

    expect(timesCalled('/admin/api/config/import')).toBe(2)
    expect(diffRow('scale.options.port')).toEqual({ before: 'COM8', after: 'COM3' })
  })

  it('annonce les contrôles qui refuseraient ce fichier, avant de le recopier', async () => {
    importFaults = [
      { field: 'scale.options.port', message: 'Ce port n’existe pas sur ce poste.' },
    ]
    await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    // `collapse` ramène toute apostrophe à la forme droite : la page, elle, écrit la
    // française, celle que le service a envoyée.
    expect(pageText()).toContain("Ce port n'existe pas sur ce poste.")
  })
})

describe('restaurer une version', () => {
  it('demande le mot de passe sur 401, puis REJOUE la restauration', async () => {
    refusals = { restore: 1 }
    versions = [version(1), version(2)]
    const { admin } = await open()

    buttonNamed('Remettre').click()
    await settle()

    expect(admin.pending).not.toBeNull()
    await admin.answerPassword('openscale')
    await settle()

    expect(timesCalled('/admin/api/config/restore')).toBe(2)
  })

  it('garde ses 72 px : c’est le seul geste de la page qui change le poste tout de suite', async () => {
    versions = [version(1)]
    await open()

    expect(buttonNamed('Remettre').classList.contains('touch-target')).toBe(true)
  })
})

describe('aucune liste sans plafond', () => {
  it('borne le diff et annonce ce que la troncature cache', async () => {
    servedConfig = configWithManyFields(0)
    importedConfig = configWithManyFields(1)
    await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    expect(host.querySelectorAll('[data-diff] tbody tr')).toHaveLength(40)
    expect(pageText()).toContain('40 lignes affichées sur 137 champs qui changent.')
  })
})

describe('« identique » et « rien à quoi comparer » sont deux états', () => {
  it('n’annonce pas une configuration identique quand elle n’a pas lu celle en service', async () => {
    configUnreadable = true
    const { admin } = await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    // Le pavé vert disait « il n'y a rien à recopier » sous le bandeau rouge qui disait
    // que la colonne « En service » ne pouvait rien affirmer. Des deux, la rassurante
    // était la fausse — dans le geste même où l'on décide de ne pas recopier.
    expect(host.querySelector('[data-same]')).toBeNull()
    expect(host.querySelector('[data-uncompared]')).not.toBeNull()
    expect(pageText()).toContain("Ce fichier n'a été comparé à rien")
    expect(admin.notice).toContain('rien ne peut être comparé')
  })

  it('ne garde pas une colonne « En service » qu’une relecture ratée a périmée', async () => {
    await open()
    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()
    expect(diffRow('scale.options.port')).toEqual({ before: 'COM8', after: 'COM3' })

    // La relecture qui suit la recopie échoue : ce que la page tenait n'est plus
    // attestable, et une lecture ratée ne fabrique pas une colonne.
    configUnreadable = true
    buttonNamed('Recopier').click()
    await settle()

    expect(host.querySelectorAll('[data-diff] tbody tr')).toHaveLength(0)
    expect(host.querySelector('[data-uncompared]')).not.toBeNull()
  })
})

describe('la date d’écriture n’est pas une différence', () => {
  it('laisse le clonage atteindre l’état vert, que `modified_at` rendait inatteignable', async () => {
    // Deux postes que §11.5 déclare identiques : même configuration, deux instants
    // d'écriture. Chaque poste stampe le sien, et l'empreinte de §11.5 l'exclut.
    servedConfig = configWithPort('COM8')
    importedConfig = configWithPort('COM8', FILE_STAMP)
    await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    expect(host.querySelector('[data-diff]')).toBeNull()
    expect(pageText()).toContain('décrit la même configuration que celle en service')
    expect(pageText()).not.toContain('Recopier ce champ dans le brouillon')
  })
})

describe('le mot de passe WebDAV n’est pas un champ du diff', () => {
  it('ne compare pas un secret que ni le poste ni le fichier ne portent en clair', async () => {
    servedConfig = configWithBlankedSecret()
    importedConfig = exportWithoutTheSecret()
    await open()

    chooseFile('config-poste1-2026-07-24.json', importedConfig)
    await settle()

    // La ligne existait : « » d'un côté, « — » de l'autre. Elle ne comparait rien de vrai
    // — le poste blanchit son secret et l'export le supprime — et « Recopier » la traitait
    // comme n'importe quelle autre.
    expect(() => diffRow('catalog.options.password')).toThrow()
    expect(diffRow('scale.options.port')).toEqual({ before: 'COM8', after: 'COM3' })
  })

  it('ne fait pas disparaître la clé du brouillon quand « Recopier » passe', async () => {
    servedConfig = configWithBlankedSecret()
    importedConfig = exportWithoutTheSecret()
    const { draft } = await open()

    chooseFile('config-poste1-2026-07-24.json', importedConfig)
    await settle()
    buttonNamed('Recopier').click()
    await settle()

    // `JSON.stringify` supprime une propriété valant `undefined` : recopier le « — » du
    // fichier faisait donc partir un PUT dont `catalog.options` n'avait plus de clé
    // `password`, et le compte WebDAV de la coopérative disparaissait du fichier.
    const sent = JSON.parse(JSON.stringify(draft.config)) as Record<string, unknown>
    const options = (sent.catalog as Record<string, unknown>).options as Record<string, unknown>
    expect(Object.keys(options)).toContain('password')
  })
})

describe('ce que l’export sans le matériel ne porte pas', () => {
  it('nomme les blocs retirés avant que « Recopier » n’en recopie le vide', async () => {
    importedConfig = hardwareFreeExport()
    await open()

    chooseFile('config-poste1-sans-materiel-2026-07-24.json', importedConfig)
    await settle()

    const warning = host.querySelector('[data-stripped]')
    expect(warning).not.toBeNull()
    const said = collapse(warning?.textContent ?? '')
    expect(said).toContain('le nom du poste')
    expect(said).toContain("l'adresse d'écoute")
    expect(said).toContain('les réglages de la balance')
    expect(said).toContain("les réglages de l'imprimante")
    // Le compte WebDAV part avec `catalog.options` : c'est le vide le plus coûteux.
    expect(said).toContain('la source du catalogue, compte compris')
  })

  it('promet exactement ce que §11.5 retire, et pas trois éléments sur six', async () => {
    await open()

    const promise = pageText()
    expect(promise).toContain('le numéro et le nom du poste')
    expect(promise).toContain('les réglages de la balance')
    expect(promise).toContain("ceux de l'imprimante")
    expect(promise).toContain('la source du catalogue')
    expect(promise).toContain('le réseau')
  })
})

describe('un contrôle qui refuse dit aussi ce qui marcherait', () => {
  it('affiche les valeurs acceptées à côté de la clé refusée (§11.4 étape 1)', async () => {
    importFaults = [
      {
        field: 'scale.options.port',
        message: 'Ce port n’existe pas sur ce poste.',
        allowed: ['COM1', 'COM8'],
      },
    ]
    await open()

    chooseFile('config-station1-2026-07-24.json', importedConfig)
    await settle()

    expect(pageText()).toContain('Valeurs acceptées : COM1, COM8.')
  })
})

describe('les cinq versions ont trois états, pas deux', () => {
  it('ne dit pas « ce poste n’a jamais été reconfiguré » avant d’avoir lu quoi que ce soit', async () => {
    versionsSilent = true
    await open()

    expect(pageText()).not.toContain("n'a jamais été reconfiguré")
    expect(host.querySelector('[data-versions="reading"]')).not.toBeNull()
  })

  it('ne transforme pas une lecture refusée en fait historique sur le poste', async () => {
    versionsRefused = true
    await open()

    expect(pageText()).not.toContain("n'a jamais été reconfiguré")
    expect(pageText()).toContain("Les versions enregistrées n'ont pas pu être lues")
  })
})

describe('le retour tactile ne répond pas à un geste mort', () => {
  it('exclut le sélecteur de fichier désarmé de la compression sous le doigt', () => {
    // Un `<label>` n'a pas d'état désactivé : c'est l'`<input>` qu'il porte qui l'est, et
    // c'est `.off` qui le grise. La règle avait perdu cette garde et répondait à un geste
    // qui n'ouvrait rien. Depuis que les boutons de la page sont des `Act`, le sélecteur
    // est le seul à porter la classe, et `:disabled` n'a donc plus personne à écarter.
    expect(STATION_SVELTE).toContain('.choose:active:not(.off)')
    expect(STATION_SVELTE).not.toMatch(/^\s*\.choose:active\s*\{/mu)
  })
})

describe('un export remis au navigateur n’est pas un export enregistré', () => {
  it('dit ce qu’elle peut constater, et libère l’URL après le tour de boucle du clic', async () => {
    const { admin } = await open()

    buttonNamed('Exporter tout').click()
    await settle()

    // Rien n'atteste que le fichier est écrit : un navigateur peut demander où, ou refuser.
    expect(admin.notice).toContain('remis au navigateur')
    expect(admin.notice).not.toContain('est enregistré')
    // L'URL de l'objet tient encore quand le navigateur prend le téléchargement en charge.
    // Libérée dans le même tour, c'est elle qui annule le téléchargement annoncé.
    expect(revokedAtHandover).toEqual([0])
    // Et elle finit par être libérée : ce n'est pas une fuite non plus.
    expect(revoked).toHaveLength(1)
  })
})

describe('les trois champs éditables portent leur refus', () => {
  it('allume la clé que les 47 contrôles ont nommée, pas seulement la bannière', async () => {
    const { draft } = await open()

    // Ce que §11.3 renvoie sur un enregistrement refusé, tel que `draft.save` le range.
    draft.faults = [
      {
        field: 'station.number',
        message: 'Le numéro de poste doit être entre 1 et 4.',
        allowed: ['1', '2', '3', '4'],
      },
    ]
    flushSync()

    expect(pageText()).toContain('Le numéro de poste doit être entre 1 et 4.')
    expect(pageText()).toContain('Valeurs acceptées : 1, 2, 3, 4.')
  })
})
