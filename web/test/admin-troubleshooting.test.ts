import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync } from 'svelte'
import { createClassComponent } from 'svelte/legacy'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Troubleshooting from '../src/admin/pages/Troubleshooting.svelte'
import type { HealthDTO } from '../src/admin/lib/dto'
import { Admin } from '../src/admin/lib/session.svelte'
import { FLV_1_IMPORT, FLV_IMPORT, nominalHealth } from './fixtures/health'

/**
 * Les neuf gros boutons du Dépannage sont-ils toujours GROS ?
 *
 * `tokens.test.ts` garantissait les 72 px de §14.2 sur chaque bouton de `src/**`, y
 * compris ceux de l'administration. ADR-033 l'a restreint à l'écran client : la règle
 * vient d'une contrainte physique — 20 mm sous un doigt — et une page de réglages à 45
 * champs conduite à la souris n'y est pas soumise.
 *
 * Cette page-ci, si. §14.4 la veut faite de « 9 gros boutons », et elle se touche debout
 * devant un poste en panne, parfois avec un sac dans l'autre main. Ce fichier est la
 * garantie qui remplace celle qui a été retirée : sans lui, personne ne remarquerait le
 * jour où ces boutons rétrécissent.
 */

const ADMIN = resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin')

const bigButton = readFileSync(resolve(ADMIN, 'components/BigButton.svelte'), 'utf8')
const page = readFileSync(resolve(ADMIN, 'pages/Troubleshooting.svelte'), 'utf8')

/** Le gros bouton dont le libellé porte ce fragment, du `<BigButton` à son `/>`. */
function buttonSaying(fragment: string): string {
  const found = [...page.matchAll(/<BigButton\b[\s\S]*?\/>/gu)]
    .map((match) => match[0])
    .find((markup) => markup.includes(fragment))
  if (found === undefined) throw new Error(`aucun gros bouton « ${fragment} »`)
  return found
}

/** La famille que ce bouton déclare — « read » quand il n'en déclare aucune. */
function kindOf(markup: string): string {
  return /kind="(\w+)"/u.exec(markup)?.[1] ?? 'read'
}

describe('les gros boutons de §14.4', () => {
  it('déclarent la cible tactile, et une hauteur au-delà', () => {
    expect(bigButton).toContain('touch-target')
    const height = /min-height:\s*([\d.]+)rem/u.exec(bigButton)
    expect(height).not.toBeNull()
    // 6rem = 96 px, au-delà des 72 px de §14.2 : ces boutons se touchent sans lunettes.
    expect(Number(height?.[1]) * 16).toBeGreaterThanOrEqual(72)
  })

  it('sont le SEUL composant de la page à porter des boutons', () => {
    // Un <button> écrit à la main dans la page échapperait à la garantie ci-dessus.
    const handWritten = [...page.matchAll(/<button\b[^>]*>/gsu)].filter(
      (match) => !match[0].includes('touch-target'),
    )
    expect(handWritten.map((m) => m[0])).toEqual([])
  })
})

describe('ce que chaque bouton dit avant et pendant', () => {
  it('donne un état « en cours » à CHACUNE des neuf actions', () => {
    // Trois d'entre elles n'en avaient pas — les deux tests de matériel et l'import —
    // parce qu'elles passent par `admin.load`, qui ne lève pas `busy` contrairement à
    // `admin.run`. On appuyait, le port s'ouvrait pendant trois secondes, rien ne
    // bougeait, et on appuyait de nouveau.
    const wired = [...page.matchAll(/busy=\{working === '(\w+)'\}/gu)].map((m) => m[1])
    for (const action of ['scale', 'printer', 'label', 'reprint', 'reload', 'manual', 'roll']) {
      expect(wired, action).toContain(action)
    }
    expect(page).toContain("begin('import')")
  })

  it('annonce AVANT le clic les deux actes qui demandent le mot de passe', () => {
    // Un bénévole qui n'a pas le mot de passe doit savoir lesquels lui sont accessibles
    // sans aller chercher quelqu'un (ADR-033).
    expect(page).toContain('protected')
    expect(bigButton).toContain('guarded')
  })

  it('fait passer les deux actes protégés par admin.protect, et eux seuls', () => {
    expect(page).toContain("guarded('manual'")
    expect(page).toContain('admin.protect(() => api.importCatalog(file))')
    // Les sept autres ne demandent rien : ADR-033 protège ce qui change ce que le poste
    // vend ou la façon dont il pèse, et tester une imprimante ne change ni l'un ni l'autre.
    for (const free of ['api.testScale', 'api.testPrinter', 'api.reprintLast', 'api.rollChanged']) {
      expect(page).toContain(free)
    }
  })

  it('annonce un import REFUSÉ comme refusé', () => {
    // « La veille l'appliquera dans la seconde » se lisait sous un import que le service
    // venait d'écarter : le bénévole repartait en croyant son catalogue à jour.
    expect(page).toContain('REFUSÉ')
    expect(page).toContain("record.result === 'rejected'")
  })
})

/**
 * La couleur d'un gros bouton dit CE QU'IL FAIT AU POSTE, et rien d'autre : neutre quand
 * il l'interroge, bleu quand il l'écrit, rouge quand rien ne le défait d'un clic. C'est
 * la seule information qu'un bénévole lit sans légende, et elle ne vaut que si elle est
 * exacte sur les neuf.
 */
describe('la couleur dit la nature de l’acte', () => {
  it.each([
    ['Tester la balance', 'read'],
    ['Tester l’imprimante', 'read'],
    ['Imprimer une étiquette de test', 'read'],
    ['Réimprimer la dernière', 'read'],
    ['Recharger le catalogue', 'write'],
    ['J’ai changé le rouleau', 'write'],
    ['imprimante du poste voisin', 'write'],
    ['Basculer en saisie manuelle', 'destructive'],
  ])('« %s » est un acte « %s »', (fragment, kind) => {
    expect(kindOf(buttonSaying(fragment))).toBe(kind)
  })

  it('donne à BigButton les deux fonds pleins, et une encre qui tient dessus', () => {
    expect(bigButton).toContain('background: var(--action)')
    expect(bigButton).toContain('background: var(--danger)')
    // L'explication passe en encre claire : --ink-muted disparaîtrait dans le fond plein.
    expect(bigButton).toMatch(/\.destructive \.hint[\s\S]*?color:\s*var\(--surface\)/u)
  })

  it('met le rouge sur la zone de dépôt sans en faire un bouton', () => {
    // C'est un `<label>` habillant un `<input type="file">` : en faire un bouton casserait
    // le sélecteur de fichier. Il prend le jeton, pas le composant.
    expect(page).toMatch(/\.choose\s*\{[^}]*background:\s*var\(--danger\)/u)
  })
})

/**
 * « Recharger le catalogue » rend-il enfin quelque chose ?
 *
 * Le bouton répondait « Le catalogue va être relu. » — une promesse au futur, écrite en
 * dur avant tout accès au support — puis se taisait définitivement quand le fichier
 * n'était pas là, parce que la veille qui ne trouve rien revient sans un mot. Le poste,
 * lui, sait tout dire : le fichier surveillé, le résultat, l'heure, l'inventaire. Tout
 * était déjà en base et déjà servi par le tableau de bord.
 *
 * L'import est ASYNCHRONE et le reste : la réponse est un 202, la veille fait le travail,
 * et le seul canal dont l'administration dispose est le sondage de trois secondes.
 * L'identifiant de l'import en service est ce qui dit que l'attente est finie.
 */

/** Ce que la route de rechargement répond, dans la forme exacte du service. */
const RELOAD_ANSWER = {
  done: true,
  message: 'Aucun fichier flv_2.csv dans C:\\ProgramData\\OpenScale\\catalog\\incoming : il n’y a rien à relire.',
  watched: 'dépôt local, flv_2.csv dans C:\\ProgramData\\OpenScale\\catalog\\incoming',
  last_import_id: FLV_IMPORT.id,
  last_import_at: FLV_IMPORT.occurred_at,
}

let host: HTMLElement
let live: { $set: (props: Record<string, unknown>) => void; $destroy: () => void } | undefined

beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  if (live !== undefined) live.$destroy()
  live = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/** Les deux routes que cette page touche pendant un rechargement, et rien d'autre. */
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  const url = String(input)
  if (url === '/admin/api/troubleshooting/reload-catalog') {
    return json(RELOAD_ANSWER, 202)
  }
  if (url === '/admin/api/health') return json(nominalHealth())
  return json({ done: true, message: 'Fait.' })
}

/** Une réponse JSON, comme le service en écrit. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/**
 * Monte la page sur un tableau de bord qui CHANGE, comme le sondage le fait.
 *
 * `mount()` reçoit un objet ordinaire : la page ne verrait jamais un `health` remplacé,
 * alors que c'est exactement ce qui arrive toutes les trois secondes.
 *
 * @param health - le premier tableau de bord.
 * @returns de quoi en poser un autre.
 */
function openLive(health: HealthDTO = nominalHealth()): (next: HealthDTO) => void {
  const admin = new Admin()
  live = createClassComponent({
    component: Troubleshooting,
    target: host,
    props: { admin, health },
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

/** Appuie sur le gros bouton dont le libellé commence par ce fragment. */
function press(label: string): void {
  const wanted = collapse(label)
  const found = [...host.querySelectorAll('button')].find((button) =>
    collapse(button.textContent ?? '').startsWith(wanted),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${label} »`)
  found.click()
  flushSync()
}

describe('« Recharger le catalogue » rend compte de ce qu’il a déclenché', () => {
  it('écrit l’issue dès que le poste change d’import : fichier, résultat, heure, inventaire', async () => {
    const poll = openLive()
    press('Recharger le catalogue')
    await settle()

    // Le sondage rapporte un import DIFFÉRENT : c'est ce qui dit que l'attente est finie.
    poll(nominalHealth({ catalog: { ...FLV_1_IMPORT, id: 9, result: 'applied' } }))
    await settle()

    const said = pageText()
    expect(said).toContain('flv_1.csv')
    expect(said).toContain('appliqué')
    expect(said).toContain('24/07/2026')
    expect(said).toContain('153 reçus')
    expect(said).toContain('107 pesables')
  })

  it('garde son mot au fichier identique au précédent, qui ne met rien en service', async () => {
    const poll = openLive()
    press('Recharger le catalogue')
    await settle()

    poll(nominalHealth({ catalog: { ...FLV_IMPORT, id: 9, result: 'unchanged' } }))
    await settle()

    // « appliqué » ferait croire à un bénévole qu'un nouveau catalogue est en service.
    expect(pageText()).toContain('identique au précédent')
  })

  it('dit ce qu’il SAIT quand rien n’arrive, et jamais « catalogue rechargé »', async () => {
    const poll = openLive()
    press('Recharger le catalogue')
    await settle()

    // Le poste ne bouge pas : le fichier n'était pas là, et la veille se tait.
    for (let round = 0; round < 12; round += 1) {
      poll(nominalHealth())
      await settle()
    }

    const said = pageText()
    expect(said).toContain('Aucun nouvel import enregistré')
    expect(said).toContain('C:\\ProgramData\\OpenScale\\catalog\\incoming')
    expect(said).not.toContain('rechargé')
  })

  it('ne promet aucune issue sur un poste sans journal, qui n’en écrira jamais', async () => {
    // Il attendrait un identifiant qui n'arrivera jamais, puis accuserait le fichier.
    const noJournal = nominalHealth({
      counters: { unlogged_weighings_count: 0, journal_rows_count: -1 },
      catalog: null,
    })
    const poll = openLive(noJournal)
    press('Recharger le catalogue')
    await settle()
    poll(nominalHealth({ ...noJournal }))
    await settle()

    expect(pageText()).toContain('journal')
    expect(pageText()).not.toContain('Aucun nouvel import enregistré')
  })
})

describe('ce que la page de dépannage montre en permanence', () => {
  it('nomme la source surveillée sans qu’on ait rien à toucher', async () => {
    openLive()
    await settle()
    const shown = host.querySelector('[data-source]')?.textContent ?? ''
    expect(collapse(shown)).toContain('flv_2.csv')
    expect(collapse(shown)).toContain('C:\\ProgramData\\OpenScale\\catalog\\incoming')
  })

  it('montre le dernier fichier écarté, qu’il ait été refusé ou qu’il ait échoué', async () => {
    for (const result of ['rejected', 'failed']) {
      openLive(
        nominalHealth({
          catalog: { ...FLV_IMPORT, result, reason: 'La base a refusé l’écriture.' },
        }),
      )
      await settle()
      expect(pageText(), result).toContain("La base a refusé l'écriture.")
      live?.$destroy()
      live = undefined
      host.textContent = ''
    }
  })
})
