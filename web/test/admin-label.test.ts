import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Label from '../src/admin/pages/Label.svelte'
import { Draft } from '../src/admin/lib/draft.svelte'
import { Admin } from '../src/admin/lib/session.svelte'

/**
 * La page Étiquette de §14.4, et ce qu'elle affirmait sans le faire.
 *
 *  - les flèches ±1 dot écrivaient dans le BROUILLON, tandis que l'aperçu rend le
 *    décalage EN SERVICE : quatre boutons sans effet visible, sur le seul écran dont tout
 *    l'intérêt est de rendre un réglage visible ;
 *  - la flèche gauche écrivait -1, que le contrôle 38 refuse TOUJOURS — l'écran promettait
 *    un enregistrement qui allait être rejeté ;
 *  - le gabarit était lu et écrit sous `printer.options.template`, clé que rien ne lit :
 *    le champ restait vide sur un poste correctement réglé et le refus du contrôle 29,
 *    qui nomme `printer.template`, ne pouvait jamais s'apparier ;
 *  - le bandeau d'écart disparaissait au changement de page, là où il sert ;
 *  - l'auto-test d'impression, route PROTÉGÉE, passait par `admin.run` : un 401 nu, sans
 *    panneau de mot de passe et sans rejeu, et deux boutons qu'on pouvait appuyer deux
 *    fois pendant qu'une étiquette sortait ;
 *  - `Number('')` vaut 0, donc vider « Exemplaires » écrivait `copies: 0`.
 */

/**
 * La configuration d'un poste dont l'étiquette est réglée au dot près.
 *
 * `template` est un champ de `PrinterConfig` et NON une entrée de `printer.options`
 * (`internal/domain/config.go`) : c'est le document réel qui fait foi ici.
 */
const CONFIG = {
  printer: {
    type: 'raster',
    template: 'weighing_identical',
    options: {
      offset_x: 0,
      offset_y: 0,
      darkness: 10,
      speed: 4,
      copies: 1,
    },
  },
}

/**
 * Le gabarit livré que la route de géométrie renvoie comme point de départ (décision
 * 12). Un fragment suffit : cette page ne lit que `name`, et copie le reste tel quel.
 */
const STARTER_TEMPLATE: Record<string, unknown> & { name: string } = {
  name: 'cooperative',
  media: { width_um: 40_000, height_um: 25_000 },
  elements: [],
}

let host: HTMLElement
let admin: Admin
let draft: Draft
/** Toutes les requêtes passées, dans l'ordre : « méthode route ». */
let calls: string[] = []
/** Ce que l'aperçu répond quand on le redemande en JSON : un refus, ou rien à dire. */
let previewRefusal: { status: number; body: string } | null = null
/** Ce que l'auto-test d'impression répond. */
let printerTest: { status: number; body: string } = {
  status: 200,
  body: JSON.stringify({ message: 'Mire imprimée.' }),
}
/** Ce qui retient l'auto-test en vol, quand un test veut le voir « en cours ». */
let holdPrinterTest: Promise<void> | null = null
/** Vrai quand la relecture de la configuration EN SERVICE échoue. */
let configUnreadable = false
/** Les bornes de décalage que `GET /admin/api/label/geometry` répond. */
let geometryBounds: { min_x: number; max_x: number; min_y: number; max_y: number } | null = {
  min_x: 0,
  max_x: 40,
  min_y: 0,
  max_y: 16,
}
/** Vrai quand la route de géométrie échoue. */
let geometryFails = false
let component: unknown

beforeEach(() => {
  calls = []
  previewRefusal = null
  printerTest = { status: 200, body: JSON.stringify({ message: 'Mire imprimée.' }) }
  holdPrinterTest = null
  configUnreadable = false
  geometryBounds = { min_x: 0, max_x: 40, min_y: 0, max_y: 16 }
  geometryFails = false
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

/** Le service, réduit aux routes que cette page touche. */
async function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  const method = init?.method ?? 'GET'
  calls.push(`${method} ${route}`)

  if (route.startsWith('/admin/api/label/preview.png')) {
    if (previewRefusal !== null) {
      return new Response(previewRefusal.body, { status: previewRefusal.status })
    }
    return new Response('', { status: 200 })
  }
  if (route.startsWith('/admin/api/printer/test')) {
    if (holdPrinterTest !== null) await holdPrinterTest
    return new Response(printerTest.body, { status: printerTest.status })
  }
  if (route === '/admin/api/label/geometry') {
    if (geometryFails) return new Response('', { status: 500 })
    return json({ starter: STARTER_TEMPLATE, offset_bounds: geometryBounds })
  }
  if (route === '/admin/api/config' && method === 'PUT') {
    // Ce que le service renvoie est ce qu'il vient d'appliquer : le document envoyé.
    const applied = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>
    return json({
      config: applied,
      config_fingerprint: 'b2c3d4e5',
      retired_keys: [],
      pending_confirmation: null,
    })
  }
  if (route === '/admin/api/config') {
    if (configUnreadable) {
      return new Response(JSON.stringify({ message: 'Le poste ne répond pas.' }), { status: 503 })
    }
    return json({
      config: structuredClone(CONFIG),
      config_fingerprint: 'a1b2c3d4',
      retired_keys: [],
      pending_confirmation: null,
    })
  }
  return json({})
}

/** Une réponse 200 portant un corps JSON. */
function json(body: unknown): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

/**
 * Monte la page Étiquette.
 *
 * @param readConfig - faux pour l'observer AVANT que la configuration n'arrive.
 */
async function open(readConfig = true): Promise<void> {
  admin = new Admin(60_000)
  draft = new Draft(admin)
  if (readConfig) await draft.load()
  component = mount(Label, { target: host, props: { admin, draft } })
  await settle()
}

/**
 * Ce que fait App.svelte à CHAQUE navigation : la page est démontée, puis remontée.
 *
 * Le brouillon, lui, survit — c'est un seul document pour les six pages expertes.
 */
async function remount(): Promise<void> {
  unmount(component as Parameters<typeof unmount>[0])
  component = mount(Label, { target: host, props: { admin, draft } })
  await settle()
}

/** Laisse ce qui est en vol se terminer, puis met le DOM à jour. */
async function settle(rounds = 4): Promise<void> {
  for (let round = 0; round < rounds; round += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
    flushSync()
  }
}

/** Le texte de la page, espaces normalisés. */
function text(): string {
  return (host.textContent ?? '').replace(/\s+/gu, ' ')
}

/** L'aperçu lui-même. */
function image(): HTMLImageElement {
  const found = host.querySelector('img')
  if (found === null) throw new Error('aucun aperçu à l’écran')
  return found
}

/** L'adresse que l'aperçu demande en ce moment. */
function source(): string {
  return image().getAttribute('src') ?? ''
}

/** Le bouton dont le libellé contient ce fragment. */
function button(fragment: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  return found
}

/** Le champ de la clé nommée, tel que `Field` le rend. */
function field(path: string): HTMLInputElement {
  const id = `field-${path.replace(/\./gu, '-')}`
  const found = host.querySelector<HTMLInputElement>(`#${id}`)
  if (found === null) throw new Error(`aucun champ « ${path} » à l'écran`)
  return found
}

/**
 * Tape une valeur dans un champ, comme un exploitant le ferait.
 *
 * L'événement REMONTE : Svelte 5 délègue `oninput` à la racine, et un événement qui ne
 * bulle pas n'atteindrait aucun gestionnaire — le test passerait sans rien exercer.
 */
function type(path: string, value: string): void {
  const input = field(path)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
}

describe('les flèches ±1 dot, et ce que l’aperçu montre vraiment', () => {
  it('dit ce que l’image porte VRAIMENT : le gabarit en cours, le décalage enregistré', async () => {
    await open()

    // `template` EST un paramètre de l'adresse de l'aperçu, et il est lu dans le
    // BROUILLON : « l'image ne rend jamais le réglage en cours » était faux de ce
    // champ-là. Seul le décalage est celui de la configuration en service.
    expect(source()).toContain('template=weighing_identical')
    expect(text()).toContain('gabarit en cours d’édition')
    expect(text()).toContain('décalage ENREGISTRÉ')
  })

  it('annonce l’écart dès qu’une flèche est touchée, avec les deux décalages', async () => {
    await open()
    expect(host.querySelector('[data-preview-stale]')).toBeNull()

    button('1 dot →').click()
    flushSync()

    expect(draft.value('printer.options.offset_x')).toBe(1)
    expect(host.querySelector('[data-offset-x]')?.textContent).toContain('1 dot')
    const said = text()
    // Ce que l'image porte, et ce que le réglage vaut : les deux chiffres, côte à côte.
    expect(said).toContain('0 dot en horizontal')
    expect(said).toContain('1 dot')
    expect(said).toContain('enregistrez la configuration')
  })

  it('n’écrit jamais un décalage négatif : le contrôle 38 le refuse toujours', async () => {
    await open()

    // `offset ∈ [0, max]` (contrôle 38) : un décalage négatif est refusé QUEL QUE SOIT le
    // gabarit. La flèche qui l'écrirait n'est donc pas armée, et le plancher tient même
    // quand deux appuis arrivent avant que l'écran n'ait été redessiné.
    expect(button('← 1 dot').disabled).toBe(true)
    expect(button('↑ 1 dot').disabled).toBe(true)

    button('1 dot →').click()
    flushSync()
    expect(button('← 1 dot').disabled).toBe(false)

    button('← 1 dot').click()
    button('← 1 dot').click()
    flushSync()

    expect(draft.value('printer.options.offset_x')).toBe(0)
  })

  it('ne redemande PAS l’image qu’elle sait identique, et la redemande après l’enregistrement', async () => {
    await open()
    const before = source()

    button('1 dot ↓').click()
    await settle()

    // Redemander le PNG à chaque flèche coûtait un aller-retour pour redessiner le même
    // bitmap : c'est précisément ce qui faisait passer les flèches pour cassées.
    expect(source()).toBe(before)
    expect(calls.filter((call) => call.includes('preview.png'))).toEqual([])

    await draft.save()
    await settle()

    // L'enregistrement, lui, change ce que le poste rend : l'image repart la chercher.
    expect(source()).not.toBe(before)
    expect(host.querySelector('[data-preview-stale]')).toBeNull()
    expect(host.querySelector('[data-offset-y]')?.textContent).toContain('1 dot')
  })

  it('ne demande l’image qu’UNE fois à l’ouverture', async () => {
    await open()

    // jsdom ne va jamais chercher le `src` d'une `<img>` : ce qui se compte ici est le
    // nombre d'ADRESSES demandées, et le jeton `t` les distingue. L'effet posait l'image à
    // `t=1` puis la redemandait aussitôt à `t=2`, un aller-retour par montage de page pour
    // redessiner exactement le même bitmap.
    expect(source()).toContain('t=1')
  })

  it('n’arme pas les flèches tant que la configuration n’est pas arrivée', async () => {
    await open(false)

    expect(button('1 dot →').disabled).toBe(true)
    expect(text()).toContain('Lecture de la configuration')
  })
})

describe('le plancher du décalage, calculé sur la géométrie du gabarit (ADR-063)', () => {
  it('arme la flèche verticale et écrit -1 quand le gabarit laisse une marge en haut', async () => {
    geometryBounds = { min_x: 0, max_x: 40, min_y: -16, max_y: 16 }
    await open()

    // La marge de 16 dots en haut rend -16 recevable au contrôle 38 : la flèche n'est
    // désarmée qu'à CETTE borne, plus basse que zéro.
    expect(button('↑ 1 dot').disabled).toBe(false)

    button('↑ 1 dot').click()
    flushSync()

    expect(draft.value('printer.options.offset_y')).toBe(-1)
    expect(text()).toContain('16 dots')
  })

  it('relit la géométrie au remontage même sur un brouillon sale, et garde la flèche armée', async () => {
    geometryBounds = { min_x: 0, max_x: 40, min_y: -16, max_y: 16 }
    await open()

    button('↑ 1 dot').click()
    flushSync()
    expect(draft.value('printer.options.offset_y')).toBe(-1)

    await remount()

    // Le brouillon reste SALE au remontage — document commun aux six pages, qu'App.svelte
    // démonte à chaque navigation — et la route de géométrie lit la configuration EN
    // SERVICE, pas le brouillon : elle doit donc être relue quel que soit `dirty`, sans
    // quoi la flèche se désarmait et le texte annonçait « jusqu'à 0 dot » à côté d'un
    // décalage déjà à -1.
    expect(button('↑ 1 dot').disabled).toBe(false)
    expect(text()).toContain('16 dots')
  })

  it('le plus prudent reste zéro quand la route de géométrie échoue, sans rien annoncer', async () => {
    geometryFails = true
    await open()

    // Aucune marge connue : les deux flèches qui descendraient sous zéro restent
    // désarmées, exactement comme sur le gabarit livré, et rien ne dit que la route a
    // échoué — ce n'est pas un refus à afficher, c'est l'absence prudente d'une marge.
    expect(button('← 1 dot').disabled).toBe(true)
    expect(button('↑ 1 dot').disabled).toBe(true)
    expect(host.querySelector('[data-preview-refused]')).toBeNull()
    expect(host.querySelector('[data-preview-stale]')).toBeNull()
  })
})

describe('le bouton « Partir du gabarit livré » (décision 12)', () => {
  it('copie le gabarit livré dans le brouillon et le sélectionne, sans PUT', async () => {
    await open()

    button('Partir du gabarit livré').click()
    flushSync()

    expect(draft.value('printer.custom_template')).toEqual(STARTER_TEMPLATE)
    expect(draft.value('printer.template')).toBe('cooperative')
    expect(draft.dirty).toBe(true)
    expect(calls.some((call) => call.startsWith('PUT'))).toBe(false)
  })

  it('reste désarmé quand le brouillon porte déjà un gabarit de coopérative', async () => {
    await open()
    draft.set('printer.custom_template', { name: 'cooperative', media: {}, elements: [] })
    flushSync()

    expect(button('Partir du gabarit livré').disabled).toBe(true)
    expect(text()).toContain('ne l’écrase pas')
  })

  it('redemande l’image après le bouton puis l’enregistrement', async () => {
    await open()

    // Le bouton pose `printer.template = cooperative` et demande déjà l'image une
    // première fois, pour un gabarit qui n'est pas encore en service (422 « gabarit
    // inconnu » côté moteur). Sans redemander l'image une seconde fois après
    // l'enregistrement, ce refus restait affiché alors que le gabarit est désormais en
    // service — le paragraphe du bouton promet exactement le contraire.
    button('Partir du gabarit livré').click()
    flushSync()
    const beforeSave = source()

    await draft.save()
    await settle()

    expect(source()).not.toBe(beforeSave)
  })
})

describe('le bandeau d’écart, quand on quitte la page et qu’on revient', () => {
  it('retrouve les deux chiffres après un aller-retour sur une autre page', async () => {
    await open()
    button('1 dot →').click()
    flushSync()
    expect(host.querySelector('[data-preview-stale]')).not.toBeNull()

    await remount()

    // Le brouillon est SALE au remontage : le document a été écrit par-dessus, donc le
    // décalage en service se relit sur le poste plutôt que de se deviner. Sans cela,
    // l'écran affichait « 1 dot » à côté d'une image qui en porte 0, sans un mot.
    const said = text()
    expect(host.querySelector('[data-preview-stale]')).not.toBeNull()
    expect(said).toContain('0 dot en horizontal')
    expect(said).toContain('1 dot')
  })

  it('dit qu’il ne sait pas encore, plutôt que d’inventer le décalage en service', async () => {
    await open()
    button('1 dot →').click()
    flushSync()

    configUnreadable = true
    await remount()

    expect(text()).toContain('ne sait pas encore')
  })
})

describe('l’aperçu qui ne se rend pas', () => {
  it('affiche la phrase du 422 — celle du moteur, pas une phrase de l’écran', async () => {
    previewRefusal = {
      status: 422,
      body: JSON.stringify({
        code: '',
        message: 'Le décalage vertical sort de la découpe : la première ligne serait coupée.',
      }),
    }
    await open()

    image().dispatchEvent(new Event('error'))
    await settle()

    expect(text()).toContain('sort de la découpe')
    expect(host.querySelector('[data-preview-refused]')).not.toBeNull()
  })

  it('dit quand même quelque chose quand le refus ne porte aucune phrase', async () => {
    previewRefusal = { status: 500, body: 'une page HTML, comme un mandataire en insère' }
    await open()

    image().dispatchEvent(new Event('error'))
    await settle()

    expect(text()).toContain('L’aperçu n’a pas pu être rendu')
  })

  it('n’accuse pas le poste d’un refus qu’il n’a pas prononcé', async () => {
    // Le poste rend cette adresse parfaitement : c'est le navigateur qui n'a rien
    // affiché. La phrase était posée AVANT la réponse et laissée en place par le chemin
    // du succès — « l'aperçu n'a pas pu être rendu par le poste » à propos d'une adresse
    // qu'il venait de rendre.
    await open()

    image().dispatchEvent(new Event('error'))
    await settle()

    expect(text()).not.toContain('n’a pas pu être rendu par le poste')
    expect(text()).toContain('le navigateur ne l’a pas affiché')
  })

  it('efface le refus dès que l’image revient', async () => {
    previewRefusal = { status: 422, body: JSON.stringify({ code: '', message: 'Refusé.' }) }
    await open()
    image().dispatchEvent(new Event('error'))
    await settle()
    expect(host.querySelector('[data-preview-refused]')).not.toBeNull()

    image().dispatchEvent(new Event('load'))
    flushSync()

    expect(host.querySelector('[data-preview-refused]')).toBeNull()
  })
})

describe('ce que la page écrit dans le document', () => {
  it('lit et écrit le gabarit là où le document le porte : printer.template', async () => {
    await open()

    // `printer.options.template` n'est lu par personne : le champ restait vide sur un
    // poste correctement réglé, et l'écran ajoutait une option que le service ignore.
    expect(field('printer.template').value).toBe('weighing_identical')

    type('printer.template', 'weighing_wide')

    expect(draft.value('printer.template')).toBe('weighing_wide')
    expect(draft.value('printer.options.template')).toBeUndefined()
  })

  it('n’écrit pas 0 dans « Exemplaires » quand le champ est vidé', async () => {
    await open()

    type('printer.options.copies', '')

    // `Number('')` vaut 0 : un poste qui n'imprime plus rien, enregistré par une frappe
    // qui ressemblait à une rature.
    expect(draft.value('printer.options.copies')).toBe(1)

    type('printer.options.copies', '2')
    expect(draft.value('printer.options.copies')).toBe(2)
  })

  it('garde la densité de l’administration : aucune cible de 72 px sur cette page', async () => {
    await open()

    // ADR-033 : 72 px pour ce qui est destructeur ou irréversible, et rien ici ne change
    // ce que le poste vend ni la façon dont il pèse.
    expect(host.querySelectorAll('.touch-target').length).toBe(0)
    expect(host.querySelectorAll('button').length).toBeGreaterThan(0)
  })
})

describe('les refus des 47 contrôles, sur les clés que cette page édite', () => {
  it('montre le refus de chaque clé À CÔTÉ d’elle, décalages compris', async () => {
    await open()

    // Le contrôle 29 nomme `printer.template`, le contrôle 38 nomme les deux décalages —
    // et les deux décalages étaient les seules clés de la page dont le refus ne
    // s'affichait pas ici. L'aperçu ne peut pas les porter : il rend le décalage EN
    // SERVICE, qui a déjà passé le contrôle 38.
    draft.faults = [
      { field: 'printer.template', message: 'gabarit inconnu « weighing_x ».' },
      {
        field: 'printer.options.offset_x',
        message: '80 dots hors bornes [0, 40] pour le gabarit « weighing_identical ».',
      },
    ]
    flushSync()

    expect(text()).toContain('gabarit inconnu')
    expect(host.querySelector('[data-fault-offset-x]')?.textContent).toContain('hors bornes')
  })
})

describe('les deux auto-tests d’impression', () => {
  it('demande le mot de passe au lieu d’afficher un 401 nu', async () => {
    printerTest = {
      status: 401,
      body: JSON.stringify({ code: '', message: 'Session absente ou expirée.' }),
    }
    await open()

    // `POST /admin/api/printer/test` est dans la table `guarded` de `internal/web/server.go` :
    // passé par `admin.run`, un poste dont la session a expiré répondait un 401 dans le
    // bandeau, sans panneau de mot de passe et sans rejeu.
    expect(button('mire d’alignement').textContent).toContain('clé')

    button('mire d’alignement').click()
    await settle()

    expect(admin.pending?.kind).toBe('password')
  })

  it('désarme les deux boutons pendant qu’une étiquette sort', async () => {
    let release = (): void => {}
    holdPrinterTest = new Promise<void>((resolve) => {
      release = resolve
    })
    await open()

    button('mire d’alignement').click()
    await settle(1)

    // Une impression prend des secondes : sans état « en cours », le deuxième appui sort
    // une deuxième étiquette. La phrase est celle d'`Act`, la même sur les six pages.
    expect(text()).toContain('En cours…')
    expect(button('Imprimer la réglette').disabled).toBe(true)

    release()
    await settle()

    expect(button('mire d’alignement').disabled).toBe(false)
    expect(admin.notice).toBe('Mire imprimée.')
  })
})

describe('le bandeau chiffré de Diagnose(), que §14.4 demande', () => {
  it('dit que les chiffres ne sont pas servis plutôt que d’en inventer', async () => {
    await open()

    // Aucune route ne les porte et aucun DTO n'a de champ pour eux : la troncature reste
    // annoncée (ADR-003), les chiffres sont annoncés ABSENTS. Le renvoi, lui, vit dans ce
    // commentaire-ci et plus dans la phrase : l'écran dit le fait, pas où il est décidé.
    expect(host.querySelector('[data-diagnose-absent]')).not.toBeNull()
    expect(text()).toContain('volontairement tronqué')
  })
})
