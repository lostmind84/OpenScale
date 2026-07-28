import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../src/admin/App.svelte'
import Field from '../src/admin/components/Field.svelte'
import {
  BLOCK_LABELS,
  FIELD_LABELS,
  LOG_SOURCE_LABELS,
  blockLabelOf,
  labelOf,
  logSourceLabelOf,
} from '../src/admin/lib/fields'
import { preferences } from '../src/admin/lib/preferences.svelte'
import type { ConfirmationDTO } from '../src/admin/lib/dto'
import { nominalHealth } from './fixtures/health'

/**
 * Ce que l'écran d'administration DIT, et le nom français de chaque clé qu'il édite.
 *
 * Deux sujets dans un fichier parce qu'ils sont le même : masquer les clés techniques
 * n'était possible qu'une fois qu'un libellé français existait pour les remplacer, et
 * couper les renvois au dossier de conception n'a de sens que si la phrase qui reste
 * suffit à agir.
 */

const ADMIN_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin')

/** Les fichiers de l'administration dont le nom correspond, récursivement. */
function sources(dir: string, matching: RegExp): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sources(path, matching)
    return matching.test(entry) ? [path] : []
  })
}

/** Chaque fichier avec son contenu, pour que l'échec nomme le fichier fautif. */
function withText(paths: string[]): { path: string; text: string }[] {
  return paths.map((path) => ({ path, text: readFileSync(path, 'utf8') }))
}

/**
 * Les composants et les pages SEULS pour la règle des renvois.
 *
 * Un module `.ts` ne montre rien : il est entièrement du code et des commentaires, et la
 * règle du dépôt garde les renvois `§` et `ADR-` dans les commentaires — ce sont eux qui
 * rattachent une décision à sa justification pour qui ouvre le fichier. Les passer à
 * `visibleText` reviendrait à exiger le contraire de ce que le projet demande.
 */
const screens = withText(sources(ADMIN_DIR, /\.svelte$/u))

/** Tout ce que l'administration porte, pour la couverture de l'index. */
const files = withText(sources(ADMIN_DIR, /\.(svelte|ts)$/u))

/**
 * Le markup seul : les commentaires du code gardent leurs renvois, qui sont ce qui
 * rattache une décision à sa justification pour qui ouvre le fichier.
 */
function visibleText(source: string): string {
  return source
    .replace(/<script[\s\S]*?<\/script>/gu, '')
    .replace(/<style[\s\S]*?<\/style>/gu, '')
    .replace(/<!--[\s\S]*?-->/gu, '')
}

/**
 * Les trois chemins qui nomment un BLOC et non un champ.
 *
 * La page Poste les liste pour dire ce qu'un export sans matériel laisse sur place, et
 * chacun y porte déjà son nom français juste à côté. Aucun contrôle ne refuse un bloc —
 * les fautes sortent toujours en `bloc.clé` —, donc leur donner une entrée dans l'index
 * créerait un second nom français pour le même chemin, que personne ne lirait.
 */
const BLOCK_PATHS = new Set(['scale.options', 'printer.options', 'catalog.options'])

describe('ce que l’écran montre ne cite plus le dossier de conception', () => {
  it.each(screens)('$path', ({ text }) => {
    const visible = visibleText(text)
    expect(visible).not.toMatch(/§\d/u)
    expect(visible).not.toMatch(/ADR-\d/u)
  })
})

/**
 * Le code d'un fichier, ses commentaires retirés.
 *
 * `visibleText` ne voit que le markup, et une phrase montrée au bénévole n'y arrive pas
 * toujours par là : les six consignes des feux sont des CHAÎNES de `lib/lights.ts`, et
 * plusieurs explications de champ sont assemblées dans un `<script>`. Trier sur la balise
 * laissait donc « pas une panne (ADR-007) » lisible sur le tableau de bord.
 *
 * Le tri se fait ici sur les COMMENTAIRES : eux gardent leurs renvois, et ce qui reste
 * d'un fichier une fois ses commentaires ôtés est du code — où un renvoi ne peut être que
 * dans une chaîne, donc à l'écran. Une chaîne qui contient `//` fait perdre la fin de sa
 * ligne : ce test rate alors un renvoi, il n'en invente jamais.
 */
function composedText({ path, text }: { path: string; text: string }): string {
  const blocks = path.endsWith('.ts')
    ? [text]
    : [...text.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/gu)].map((match) => match[1] ?? '')
  return blocks
    .join('\n')
    .replace(/\/\*[\s\S]*?\*\//gu, '')
    .replace(/\/\/.*$/gmu, '')
}

describe('ni les phrases que le code compose avant de les montrer', () => {
  it.each(files)('$path', (file) => {
    const composed = composedText(file)
    expect(composed).not.toMatch(/§\d/u)
    expect(composed).not.toMatch(/ADR-\d/u)
  })
})

/**
 * Ce que l'œil lit d'une page : les accolades Svelte, puis les balises, en moins.
 *
 * `visibleText` laisse encore du code dans le markup — `path="pricing.primary_code"` dans
 * une balise, `{@render toggle('barcode.…')}` dans une expression. Ni l'un ni l'autre
 * n'arrive à l'écran : le premier est une propriété que `Field` ne montre que si
 * l'interrupteur est coché, le second un appel dont on ne sait rien statiquement.
 *
 * Les accolades partent AVANT les balises, et l'ordre n'est pas indifférent : une flèche
 * `=>` dans un `onchange={…}` porte un `>` qui refermerait la balise trop tôt et rendrait
 * visible la moitié de ses propriétés.
 */
function markupText(source: string): string {
  let text = visibleText(source)
  let previous = ''
  while (text !== previous) {
    previous = text
    text = text.replace(/\{[^{}]*\}/gu, ' ')
  }
  return text.replace(/<[^>]*>/gu, ' ')
}

/**
 * Les chaînes littérales d'un morceau de code, le `${…}` des gabarits en moins.
 *
 * Le tri s'arrête au bout de la ligne pour les guillemets simples et doubles : une
 * apostrophe droite égarée fait perdre la fin de SA ligne, jamais la suite du fichier —
 * ce banc rate alors une chaîne, il n'en invente aucune.
 */
function literals(code: string): string[] {
  const quoted = /'((?:[^'\\\n]|\\.)*)'|"((?:[^"\\\n]|\\.)*)"|`((?:[^`\\]|\\.)*)`/gu
  return [...code.matchAll(quoted)].map((match) =>
    (match[1] ?? match[2] ?? match[3] ?? '').replace(/\$\{[^}]*\}/gu, ' '),
  )
}

/**
 * Les clés de l'index qu'un texte laisse voir.
 *
 * Une chaîne qui n'est QUE la clé est un argument — le chemin d'un champ, celui d'un
 * interrupteur — et le composant qui la reçoit décide de la montrer ou non. Dès qu'une
 * phrase l'entoure, plus personne ne décide : elle part à l'écran telle quelle.
 *
 * Le tri se fait sur les clés de l'index et non sur un motif « mot.mot », qui prendrait
 * « 10.5 » et « flv_2.csv » pour des réglages.
 */
function keysShown(text: string): string[] {
  const trimmed = text.trim()
  return Object.keys(FIELD_LABELS)
    .filter((key) => trimmed !== key && text.includes(key))
    .map((key) => `${key} dans « ${trimmed} »`)
}

/**
 * La clé technique est derrière l'interrupteur, ou elle n'est pas.
 *
 * Deux notes de la page Règles renvoyaient au seuil d'un autre garde-fou en l'appelant
 * par sa clé, et le tableau de bord nommait le pilote d'impression de la même façon :
 * décoché, l'interrupteur ne cachait donc rien de ces trois phrases-là. Elles ne passent
 * par aucune balise ni aucun composant — ce sont des chaînes du code —, ce qui est
 * exactement ce que les deux bancs ci-dessus ne regardaient pas.
 */
describe('ni les clés de configuration, que l’interrupteur est seul à montrer', () => {
  it.each(screens)('à l’écran de $path', ({ text }) => {
    expect(keysShown(markupText(text))).toEqual([])
  })

  it.each(files)('dans les phrases composées par $path', (file) => {
    expect(literals(composedText(file)).flatMap(keysShown)).toEqual([])
  })
})

/**
 * Les deux écrans, et non l'administration seule.
 *
 * Un renvoi vers une page n'est pas un sujet d'écran d'expert : l'écran client porte lui
 * aussi des phrases écrites pour un bénévole. Le banc regarde donc `src` entier, pour que
 * la règle ne s'arrête pas à la frontière du dossier où la faute a été trouvée.
 */
const WEB_SRC = resolve(dirname(fileURLToPath(import.meta.url)), '../src')

/** Tout ce que les deux écrans portent, pour la règle des renvois morts. */
const everything = withText(sources(WEB_SRC, /\.(svelte|ts)$/u))

/**
 * L'onglet supprimé le 27/07/2026, sous toutes ses casses.
 *
 * `\s+` et non une espace : deux chaînes concaténées coupent la phrase en deux, et c'est
 * exactement ainsi que le tableau de bord l'écrivait — « puis la cadence dans » suivi de
 * « Réglages avancés → Matériel. ».
 */
const REMOVED_SCREEN = /réglages\s+avancés/iu

/**
 * Ce qu'un fichier MONTRE : le markup d'une page, et les chaînes que son code compose.
 *
 * Les commentaires en sont exclus, et c'est délibéré : plusieurs disent que cet onglet a
 * été retiré, et pourquoi. Interdire le mot partout effacerait cette mémoire au lieu du
 * renvoi mort — un banc doit pousser à corriger la phrase, pas à taire l'histoire.
 */
function shownText(file: { path: string; text: string }): string {
  const markup = file.path.endsWith('.svelte') ? markupText(file.text) : ''
  return [markup, ...literals(composedText(file))].join('\n')
}

/**
 * Le renvoi vers un écran qui n'existe plus.
 *
 * « Réglages avancés » était une porte ; les neuf pages s'ouvrent maintenant d'emblée.
 * Une consigne qui y envoie est pire que pas de consigne du tout : le bénévole cherche
 * un onglet absent au lieu de faire le geste, et le tableau de bord est la page que tout
 * le monde ouvre en premier.
 */
describe('aucune phrase ne renvoie vers l’onglet supprimé', () => {
  it.each(everything)('$path', (file) => {
    expect(shownText(file)).not.toMatch(REMOVED_SCREEN)
  })

  it('a bien lu les deux écrans, et pas un dossier vide', () => {
    expect(everything.length).toBeGreaterThan(40)
  })
})

describe('l’index des champs', () => {
  it('nomme en français tout chemin qu’une page édite', () => {
    const unknown = new Set<string>()
    for (const { text } of files) {
      for (const match of text.matchAll(/path[:=]\s*['"]([a-z_]+(?:\.[a-z_0-9]+)+)['"]/gu)) {
        const path = match[1] as string
        if (!BLOCK_PATHS.has(path) && FIELD_LABELS[path] === undefined) unknown.add(path)
      }
    }
    expect([...unknown]).toEqual([])
  })

  it('rend le chemin lui-même quand il ne connaît pas la clé — un refus reste lisible', () => {
    expect(labelOf('bloc.inconnu')).toBe('bloc.inconnu')
  })

  it('nomme les clés que le poste refuse le plus souvent', () => {
    expect(labelOf('station.number')).toBe('Numéro du poste')
    expect(labelOf('limits.max_weight_g')).toBe('Poids maximum accepté')
    expect(labelOf('catalog.options.directory')).toBe('Répertoire surveillé')
  })
})

/** Le fichier Go qui décide, seul, quels blocs peuvent apparaître dans le bandeau. */
const CHANGED_BLOCKS_SOURCE = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../internal/web/config.go',
)

/**
 * Les blocs que le poste compare, lus dans la fonction Go qui les déclare.
 *
 * La liste ne vit pas dans le navigateur : `changedBlocks` la porte, et un treizième bloc
 * ajouté là-bas sortirait en jeton anglais dans un bandeau français sans que rien ne le
 * dise. Ce banc lit la source à la place de l'œil.
 */
function blocksTheStationCompares(): string[] {
  const source = readFileSync(CHANGED_BLOCKS_SOURCE, 'utf8')
  const body = /func changedBlocks\([\s\S]*?\n\}/u.exec(source)?.[0] ?? ''
  return [...body.matchAll(/\{"([a-z_]+)",/gu)].map((match) => match[1] as string)
}

describe('l’index des blocs', () => {
  it('nomme en français chaque bloc que le poste sait déclarer changé', () => {
    const blocks = blocksTheStationCompares()

    // Le banc ne vaut que s'il a vraiment lu le fichier : douze blocs, pas zéro.
    expect(blocks).toHaveLength(12)
    expect(blocks.filter((block) => BLOCK_LABELS[block] === undefined)).toEqual([])
  })

  it('ne porte aucun nom que l’index des champs ne connaisse pas comme bloc', () => {
    expect(Object.keys(BLOCK_LABELS).sort()).toEqual(blocksTheStationCompares().sort())
  })

  it('rend le jeton lui-même quand il ne connaît pas le bloc — le bandeau nomme toujours', () => {
    expect(blockLabelOf('bloc-de-demain')).toBe('bloc-de-demain')
  })
})

/** Le fichier Go qui déclare, seul, les origines que le journal technique peut porter. */
const LOG_SOURCES_SOURCE = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../internal/store/technical.go',
)

/**
 * Les origines que le poste écrit, lues dans les constantes Go qui les déclarent.
 *
 * Même raison que pour les blocs : la liste ne vit pas dans le navigateur. Une huitième
 * origine ajoutée côté service sortirait en jeton anglais au milieu d'une phrase
 * française, et personne ne le verrait avant qu'un bénévole ne le lise.
 */
function logSourcesTheStationWrites(): string[] {
  const source = readFileSync(LOG_SOURCES_SOURCE, 'utf8')
  return [...source.matchAll(/LogSource[A-Za-z]+\s*=\s*"([a-z]+)"/gu)].map(
    (match) => match[1] as string,
  )
}

/**
 * L'index des origines du journal technique.
 *
 * Il existe parce que DEUX écrans lisent le même journal — le tableau de bord en montre
 * dix lignes, le Journal cinquante — et que la table était privée à l'un des deux : on
 * lisait « catalogue » sur une page et « catalog » sur l'autre, pour le même événement.
 * Celle des deux qui montrait le jeton brut était le tableau de bord, page ouverte par
 * défaut et sans mot de passe.
 */
describe('l’index des origines du journal technique', () => {
  it('nomme en français chacune des origines que le poste écrit', () => {
    const sources = logSourcesTheStationWrites()

    // Le banc ne vaut que s'il a vraiment lu le fichier : sept origines, pas zéro.
    expect(sources).toHaveLength(7)
    expect(sources.filter((source) => LOG_SOURCE_LABELS[source] === undefined)).toEqual([])
  })

  it('n’en porte aucune que le service n’écrive plus', () => {
    expect(Object.keys(LOG_SOURCE_LABELS).sort()).toEqual(logSourcesTheStationWrites().sort())
  })

  it('dit « origine inconnue » plutôt qu’un mot que l’écran ne sait pas traduire', () => {
    expect(logSourceLabelOf('origine-de-demain')).toBe('origine inconnue')
  })
})

/**
 * Les deux écrans qui lisent le journal technique disent-ils la même chose ?
 *
 * Le constat qui a motivé ce banc : le tableau de bord rendait `{event.source}` brut là
 * où le Journal traduisait la même colonne du même journal. Aucun banc ne l'avait vu —
 * ils cherchaient des clés pointées et des codes ERR-xxx-nn, pas un jeton d'un seul mot.
 */
describe('les deux écrans qui lisent le journal technique', () => {
  it('ne laissent aucun jeton d’origine brut dans leur markup', () => {
    for (const page of ['Dashboard.svelte', 'Journal.svelte']) {
      const source = readFileSync(join(ADMIN_DIR, 'pages', page), 'utf8')
      expect(source).not.toMatch(/\{(event|line)\.source\}/u)
    }
  })
})

/**
 * L'interrupteur lui-même, avant que le rail ne le montre et que le champ ne l'écoute.
 *
 * Ce qui se vérifie ici n'est pas l'affichage — c'est la tâche suivante — mais la
 * mémoire : une préférence qu'il faut recocher à chaque ouverture de l'écran est une
 * préférence que personne ne coche.
 */
describe('la préférence des noms techniques', () => {
  beforeEach(() => {
    globalThis.localStorage.clear()
    vi.resetModules()
  })

  it('est décochée tant que personne ne l’a demandée', async () => {
    const { preferences } = await import('../src/admin/lib/preferences.svelte')
    expect(preferences.showTechnicalNames).toBe(false)
  })

  it('s’en souvient d’une ouverture de l’écran à la suivante', async () => {
    const { preferences } = await import('../src/admin/lib/preferences.svelte')
    preferences.toggleTechnicalNames()

    vi.resetModules()
    const reopened = await import('../src/admin/lib/preferences.svelte')
    expect(reopened.preferences.showTechnicalNames).toBe(true)
  })

  it('reste un écran qui marche quand le navigateur refuse le stockage local', async () => {
    const real = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new Error('stockage local refusé')
      },
    })
    try {
      const { preferences } = await import('../src/admin/lib/preferences.svelte')
      expect(preferences.showTechnicalNames).toBe(false)
      preferences.toggleTechnicalNames()
      expect(preferences.showTechnicalNames).toBe(true)
    } finally {
      if (real === undefined) delete (globalThis as { localStorage?: Storage }).localStorage
      else Object.defineProperty(globalThis, 'localStorage', real)
    }
  })
})

/** La clé que ce banc fait refuser au poste : elle est dans l'index, et c'est le sujet. */
const REFUSED_FIELD = 'limits.max_weight_g'

/** Le nom français que l'index lui donne. */
const REFUSED_LABEL = 'Poids maximum accepté'

/** Une clé que ce binaire ne connaît plus, telle que le contrôle 20 la remonte. */
const RETIRED_KEY = 'barcode.weight_decimals'

let host: HTMLElement
let component: unknown
/** Ce que le fichier du poste porte de périmé, pour le bandeau qui le dit. */
let retired: string[] = []
/** La confirmation que le poste attend, quand un test veut voir le bandeau des 60 s. */
let pending: ConfirmationDTO | null = null

/**
 * L'interrupteur en service : ce qu'il cache, où on le trouve, et ce qui reste sans lui.
 *
 * Ce n'est pas une préférence d'affichage parmi d'autres. Masquer la clé sans mettre le
 * nom français à sa place laisserait « 99999 hors bornes [1, 50000] » tout seul au-dessus
 * du bouton d'enregistrement, sans dire de quel champ il parle : le masquage et l'index
 * des libellés ne tiennent que l'un par l'autre.
 */
describe('l’interrupteur des noms techniques', () => {
  beforeEach(() => {
    globalThis.localStorage.clear()
    preferences.showTechnicalNames = false
    retired = []
    pending = null
    host = document.createElement('div')
    document.body.append(host)
    vi.stubGlobal('fetch', fakeFetch)
  })

  afterEach(() => {
    if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
    component = undefined
    host.remove()
    vi.unstubAllGlobals()
  })

  it('cache la clé sous un champ, et la rend quand on le coche', () => {
    component = mount(Field, {
      target: host,
      props: {
        label: REFUSED_LABEL,
        path: REFUSED_FIELD,
        value: '99999',
        onchange: () => {},
      },
    })
    flushSync()
    expect(host.textContent).toContain(REFUSED_LABEL)
    expect(host.textContent).not.toContain(REFUSED_FIELD)

    preferences.showTechnicalNames = true
    flushSync()
    expect(host.textContent).toContain(REFUSED_FIELD)
  })

  it('se coche depuis le rail, sous l’identité du poste', async () => {
    await openAdmin()

    const rail = host.querySelector('.rail')
    expect(rail?.textContent).toContain('Montrer les noms techniques')
    const box = rail?.querySelector<HTMLInputElement>('input[type="checkbox"]') ?? null
    expect(box, 'aucun interrupteur dans le rail').not.toBeNull()
    expect(box?.checked).toBe(false)

    box?.click()
    flushSync()

    expect(preferences.showTechnicalNames).toBe(true)
  })

  it('laisse la barre de refus nommer le champ en français, la clé restant dessous', async () => {
    await openAdmin()
    await press('Matériel')
    await changeAField()
    await press('Enregistrer la configuration')

    const refusals = host.querySelector('[data-faults]')
    expect(refusals, 'aucune barre de refus après un enregistrement refusé').not.toBeNull()
    expect(refusals?.textContent).toContain(REFUSED_LABEL)
    expect(refusals?.textContent).toContain('99999 hors bornes [1, 50000].')
    expect(refusals?.textContent).not.toContain(REFUSED_FIELD)

    preferences.showTechnicalNames = true
    flushSync()

    expect(host.querySelector('[data-faults]')?.textContent).toContain(REFUSED_FIELD)
  })

  it('nomme en français les blocs du bandeau de confirmation, les jetons sous l’interrupteur', async () => {
    pending = {
      changed_blocks: ['scale', 'printer'],
      confirm_before: '2026-07-28T09:03:12Z',
      seconds_left: 42,
    }
    await openAdmin()
    // Le bandeau vient du BROUILLON, que seule l'ouverture d'une page de réglages lit.
    await press('Matériel')

    const banner = host.querySelector('[data-pending]')
    expect(banner, 'aucun bandeau de confirmation').not.toBeNull()
    expect(banner?.textContent).toContain('la balance, l’imprimante')
    // Les jetons du service : ce qu'un bénévole lisait avant, et qui ne lui disait rien.
    expect(banner?.textContent).not.toContain('scale')
    expect(banner?.textContent).not.toContain('printer')
    // Ce que le bandeau garde en toutes circonstances : sur quoi porte le compte à rebours.
    expect(banner?.textContent).toContain('42 secondes')

    preferences.showTechnicalNames = true
    flushSync()

    expect(host.querySelector('[data-pending] [data-blocks]')?.textContent).toBe('scale, printer')
    expect(host.querySelector('[data-pending]')?.textContent).toContain('la balance, l’imprimante')
  })

  it('dit d’une clé périmée ce qu’elle est, sans cesser de nommer celle qu’on retire', async () => {
    retired = [RETIRED_KEY]
    await openAdmin()
    await press('Matériel')

    const bar = host.querySelector('footer')?.textContent ?? ''
    expect(bar).toContain('des réglages que cette version du poste ne connaît plus')
    expect(bar).not.toContain('binaire')
    // Le bouton, lui, supprime CETTE clé-là : il la nomme, interrupteur ou pas.
    expect(bar).toContain(`retirer ${RETIRED_KEY}`)
  })
})

/** Le poste, réduit à ce que cette barre de refus demande : une lecture, puis un 422. */
function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  if (route === '/admin/api/health') return json(nominalHealth())
  if (route === '/admin/api/config' && init?.method === 'PUT') {
    return json(
      {
        code: '',
        message: 'Cette configuration ne peut pas être appliquée.',
        faults: [{ field: REFUSED_FIELD, message: '99999 hors bornes [1, 50000].' }],
      },
      422,
    )
  }
  if (route.startsWith('/admin/api/config')) {
    return json({
      config: { station: { number: 2 } },
      config_fingerprint: 'a1b2c3d4',
      retired_keys: retired,
      pending_confirmation: pending,
    })
  }
  return json({ ports: [], printers: [], weighings: [], entries: [], versions: [] })
}

/** Une réponse du service, telle que `problem` l'écrit quand elle refuse. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/** Monte l'écran d'administration entier et laisse le tableau de bord se dessiner. */
async function openAdmin(): Promise<void> {
  component = mount(App, { target: host, props: {} })
  flushSync()
  await settle()
}

/**
 * Laisse ce qui est en vol se terminer, puis met le DOM à jour.
 *
 * Aucune horloge métier n'est en jeu : la règle « aucun test ne dort » porte sur l'horloge
 * injectée du poste, pas sur le tour de boucle d'événements d'un navigateur simulé.
 */
async function settle(): Promise<void> {
  for (let round = 0; round < 3; round += 1) {
    await new Promise((done) => setTimeout(done, 0))
    flushSync()
  }
}

/** Touche le bouton dont le libellé contient ce fragment, et laisse l'acte se terminer. */
async function press(fragment: string): Promise<void> {
  const found = [...host.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  found.click()
  await settle()
}

/**
 * Modifie un champ de la page ouverte, pour armer le bouton d'enregistrement.
 *
 * Il est désarmé tant que rien n'a bougé — « Aucune modification à enregistrer » — et un
 * enregistrement qui ne part pas ne rapporte aucun refus à afficher.
 */
async function changeAField(): Promise<void> {
  const field = host.querySelector<HTMLInputElement>('main input[type="text"]')
  expect(field, 'aucun champ à modifier sur cette page').not.toBeNull()
  if (field === null) return
  field.value = 'COM9'
  field.dispatchEvent(new Event('input', { bubbles: true }))
  await settle()
}
