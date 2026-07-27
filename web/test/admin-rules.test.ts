import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Rules from '../src/admin/pages/Rules.svelte'
import { Draft } from '../src/admin/lib/draft.svelte'
import type { DecisionDTO, HealthDTO } from '../src/admin/lib/dto'
import { Admin } from '../src/admin/lib/session.svelte'
import type { DiagnosticDTO } from '../src/lib/dto'
import { nominalHealth, nominalState } from './fixtures/health'

/**
 * La page Règles dit-elle la vérité, et la dit-elle en français ?
 *
 * Ce qui est tenu ici est, à chaque fois, un écart constaté à §14.4 ou un défaut :
 *
 *  1. les QUATORZE garde-fous de §6.4 sont à l'écran, dans l'ordre d'évaluation, et ce
 *     sont ceux du noyau — la liste et les messages sont relus dans
 *     `internal/domain/safeguard.go`, pour qu'aucun ne soit inventé ni oublié ;
 *  2. les BORNES sont dites comme le noyau les écrit : un net ÉGAL au plancher est
 *     refusé, et l'écran ne doit pas laisser croire qu'il passe ;
 *  3. un champ numérique VIDÉ n'écrit pas zéro : `Number('')` vaut 0, et effacer
 *     « Dénominateur » posait une division par zéro dans le prix de tous les produits —
 *     et la case retrouve la valeur du fichier au lieu de rester vide à l'écran ;
 *  4. deux tarifs sans code ne s'écrasent plus l'un l'autre ;
 *  5. le bloc code-barres que §14.4 place sur cette page existe ;
 *  6. la page ne maquille pas en conformité l'écart sur le message : la sévérité est en
 *     lecture seule par conception, le message devrait être modifiable et ne l'est pas ;
 *  7. les dérogations NOMMENT leur produit, la liste est bornée, son total est annoncé,
 *     et une dérogation que le garde-fou 14 précède est dite sans effet ;
 *  8. rien n'est AFFIRMÉ pendant une lecture qui n'a pas répondu : un catalogue muet
 *     donne « Nom non lu », jamais « produit absent du catalogue » ;
 *  9. le verdict 13 dit QUEL produit il a laissé passer — c'est sa raison d'être ;
 * 10. aucun jeton anglais du service — `blocking`, `info` — n'est affiché tel quel.
 */

/** Le noyau, seule source de la liste des garde-fous et de leurs messages. */
const SAFEGUARD_GO = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../../internal/domain/safeguard.go'),
  'utf8',
)

let host: HTMLElement
let component: unknown

beforeEach(() => {
  catalogProducts = []
  catalogFails = false
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

/** Le catalogue que la route client sert à ce banc : c'est lui qui NOMME les produits. */
let catalogProducts: { id: string; name: string }[] = []
/** Vrai quand le catalogue refuse de répondre : la page doit le dire, pas se taire. */
let catalogFails = false

/** La route client `GET /api/v1/catalog`, la seule que cette page appelle. */
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  if (catalogFails) return Promise.reject(new Error('le poste n’a pas répondu'))
  if (String(input) === '/api/v1/catalog') {
    return Promise.resolve(new Response(JSON.stringify({ products: catalogProducts }), { status: 200 }))
  }
  return Promise.resolve(new Response('{}', { status: 200 }))
}

/**
 * La configuration de `testdata/config-lacagette.json`, réduite aux blocs de cette page.
 *
 * Les valeurs sont celles du fichier réel : un test qui inventerait ses seuils ne dirait
 * rien de ce qu'un exploitant voit.
 */
function laCagetteConfig(): Record<string, unknown> {
  return {
    pricing: {
      tiers: [
        { code: 'MEMBER', label: 'Adhérent', abbrev: 'A', coef_num: 9, coef_den: 10, rank: 1 },
        { code: 'SOLIDARITY', label: 'Solidaire', abbrev: 'S', coef_num: 1, coef_den: 1, rank: 2 },
      ],
      primary_code: 'MEMBER',
      reference_code: 'SOLIDARITY',
      amount_rounding: 'half_up',
      unit_price_rounding: 'half_up',
    },
    barcode: { verify_reference_check_digit: true },
    limits: {
      empty_max_g: 5,
      basket_check_enabled: true,
      basket_min_g: -282,
      basket_max_g: -270,
      min_weight_g: 10,
      max_weight_g: 99999,
      max_tare_g: 9999,
      min_units: 1,
      max_units: 99,
      max_amount_cents: 99999,
    },
  }
}

/** Monte la page sur une configuration et un tableau de bord, et rend le brouillon. */
function open(
  config: Record<string, unknown> = laCagetteConfig(),
  health: HealthDTO = nominalHealth(),
): Draft {
  const draft = new Draft(new Admin())
  draft.config = config
  component = mount(Rules, { target: host, props: { draft, health } })
  flushSync()
  return draft
}

/**
 * Laisse la lecture du catalogue se terminer, puis met le DOM à jour.
 *
 * Aucune horloge métier n'est en jeu : c'est le tour de boucle d'un navigateur simulé.
 */
async function settle(): Promise<void> {
  for (let round = 0; round < 3; round += 1) {
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

/** Les quatorze codes du noyau, dans l'ordre d'évaluation où la constante les écrit. */
function domainCodes(): string[] {
  return [...SAFEGUARD_GO.matchAll(/\b(?:Code[A-Za-z]+)\s*=\s*"([A-Z_]+)"/gu)].map(
    (match) => match[1] as string,
  )
}

/** Les messages livrés, par code : `defaultMessages` relu dans le noyau. */
function domainMessages(): Map<string, string> {
  const codeOf = new Map<string, string>()
  for (const match of SAFEGUARD_GO.matchAll(/\b(Code[A-Za-z]+)\s*=\s*"([A-Z_]+)"/gu)) {
    codeOf.set(match[1] as string, match[2] as string)
  }
  const messages = new Map<string, string>()
  for (const match of SAFEGUARD_GO.matchAll(/\b(Code[A-Za-z]+):\s*"([^"]*)"/gu)) {
    const code = codeOf.get(match[1] as string)
    if (code !== undefined) messages.set(code, match[2] as string)
  }
  return messages
}

/** Le champ d'un seuil, retrouvé par le chemin de sa clé — c'est ainsi que `Field` l'écrit. */
function fieldOf(path: string): HTMLInputElement {
  const id = `field-${path.replace(/\./gu, '-')}`
  const found = host.querySelector<HTMLInputElement>(`#${id}`)
  if (found === null) throw new Error(`aucun champ pour ${path}`)
  return found
}

/** Le champ d'une colonne du tableau des tarifs, retrouvé par son libellé accessible. */
function tierField(label: string): HTMLInputElement {
  const found = host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)
  if (found === null) throw new Error(`aucun champ « ${label} »`)
  return found
}

/** Tape une valeur dans un champ, exactement comme un exploitant le fait. */
function type(field: HTMLInputElement, value: string): void {
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
  field.dispatchEvent(new Event('change', { bubbles: true }))
  flushSync()
}

/** Un tableau de bord dont la pesée en cours porte les verdicts donnés. */
function healthWithVerdicts(diagnostics: DiagnosticDTO[]): HealthDTO {
  return nominalHealth({ state: nominalState({ diagnostics }) })
}

/** Une dérogation de poids minimum sur un produit, telle que §10.6 l'enregistre. */
function waiver(id: string, grams: number): DecisionDTO {
  return {
    product_id: id,
    offered: true,
    min_weight_g: grams,
    reason: 'se vend par petites quantités',
    decided_by: 'bénévole',
    decided_at: '2026-07-24T12:00:00.000Z',
  }
}

/** Le panneau dont le titre porte ce mot : c'est ainsi qu'on en parle au téléphone. */
function panelAbout(word: string): string {
  const panel = [...host.querySelectorAll('.panel')].find((section) =>
    collapse(section.querySelector('h2')?.textContent ?? '').includes(word),
  )
  if (panel === undefined) throw new Error(`aucun panneau « ${word} »`)
  return collapse(panel.textContent ?? '')
}

/** Quitte un champ, comme un exploitant qui va cliquer ailleurs. */
function leave(field: HTMLInputElement): void {
  field.dispatchEvent(new FocusEvent('focusout', { bubbles: true }))
  flushSync()
}

describe('les quatorze garde-fous de §6.4', () => {
  it('affiche les QUATORZE codes du noyau, dans l’ordre d’évaluation', () => {
    open()

    const codes = domainCodes()
    expect(codes).toHaveLength(14)
    const shown = [...host.querySelectorAll('.rule')].map((rule) => rule.getAttribute('data-code'))
    // L'ordre est normatif : le premier verdict bloquant décide du message affiché.
    expect(shown).toEqual(codes)
  })

  it('n’invente aucun garde-fou : chaque code affiché existe dans le noyau', () => {
    open()

    const known = new Set(domainCodes())
    const shown = [...host.querySelectorAll('.rule')].map((rule) => rule.getAttribute('data-code'))
    // La longueur est vérifiée ici aussi : une page vide ne satisferait pas « aucun code
    // inventé » par la seule vertu de ne rien afficher.
    expect(shown).toHaveLength(14)
    expect(shown.filter((code) => code === null || !known.has(code))).toEqual([])
  })

  it('cite les messages livrés, mot pour mot, depuis internal/domain/safeguard.go', () => {
    open()

    const text = pageText()
    for (const [code, message] of domainMessages()) {
      if (message === '') continue
      expect(text.includes(collapse(message)), `${code} : « ${message} »`).toBe(true)
    }
  })

  it('offre les DIX seuils de `limits`, et chacun une seule fois', () => {
    open()

    for (const key of [
      'empty_max_g',
      'min_weight_g',
      'max_weight_g',
      'max_tare_g',
      'min_units',
      'max_units',
      'max_amount_cents',
      'basket_min_g',
      'basket_max_g',
    ]) {
      expect(host.querySelectorAll(`#field-limits-${key}`), key).toHaveLength(1)
    }
    // Le dixième est un booléen : il n'a pas de champ mais un interrupteur.
    expect(host.querySelector('[data-flag="limits.basket_check_enabled"]')).not.toBeNull()
  })

  it('dit la vérité sur ce qui est modifiable, et ne s’abrite pas derrière §6.4', () => {
    open()

    const text = pageText()
    expect(text).toContain('Le seuil est modifiable')
    // Seule la SÉVÉRITÉ est en lecture seule par conception (§6.4, ADR-025). §6.4 écrit
    // au contraire que les messages « sont éditables depuis l'écran Règles » : citer
    // cette section à l'appui de l'inverse transformait un écart en conformité.
    expect(text).toContain('La sévérité est en lecture seule')
    expect(text).not.toContain('la sévérité et le message sont en lecture seule')
    expect(text).not.toContain('Le seuil et le message sont modifiables')
    // Et ce qui manque est nommé pour ce que c'est : la clé, pas la validation, que le
    // service porte déjà (`domain.CheckMessage`, `domain.MessagePlaceholders`).
    expect(text).toContain('il manque la clé de configuration')
  })

  it('dit la borne du garde-fou 8 comme le noyau l’écrit : au plancher, c’est refusé', () => {
    // Le noyau refuse sur `net <= floor` : un net ÉGAL au plancher est bloqué. Un
    // exploitant qui règle min_weight_g = 10 doit lire que 10 g est refusé, pas admis.
    expect(SAFEGUARD_GO).toContain('net > 0 && net <= floor')

    open()

    const rule = collapse(host.querySelector('[data-code="WEIGHT_TOO_LOW"]')?.textContent ?? '')
    expect(rule).toContain('ne dépasse pas le plancher')
    expect(rule).not.toContain("n'atteint pas le plancher")
  })
})

describe('un champ vidé n’écrit pas zéro', () => {
  it('garde le dénominateur du fichier quand l’exploitant efface la case', () => {
    const draft = open()
    expect(draft.value('pricing.tiers.0.coef_den')).toBe(10)

    type(tierField('Dénominateur du tarif 1'), '')

    // `Number('')` vaut 0 : sans garde, cette frappe écrivait `coef_den: 0`, c'est-à-dire
    // une division par zéro dans le prix de TOUS les produits, enregistrée sans un mot.
    expect(draft.value('pricing.tiers.0.coef_den')).toBe(10)
    expect(draft.dirty).toBe(false)
  })

  it('garde le seuil du fichier quand un garde-fou est vidé', () => {
    const draft = open()

    type(fieldOf('limits.max_tare_g'), '')

    expect(draft.value('limits.max_tare_g')).toBe(9999)
    expect(draft.dirty).toBe(false)
  })

  it('écrit bien la valeur quand il y en a une : le garde ne bloque pas la saisie', () => {
    const draft = open()

    type(tierField('Dénominateur du tarif 1'), '4')
    type(fieldOf('limits.max_tare_g'), '5000')

    expect(draft.value('pricing.tiers.0.coef_den')).toBe(4)
    expect(draft.value('limits.max_tare_g')).toBe(5000)
    expect(draft.dirty).toBe(true)
  })

  it('remet la valeur du fichier dans la case quand on quitte un seuil vidé', () => {
    const draft = open()
    const field = fieldOf('limits.max_tare_g')

    type(field, '')
    // `Field` est piloté par `value=` : rien n'ayant changé dans le brouillon, rien ne se
    // redessine, et la case RESTAIT vide à l'écran pendant que la configuration portait
    // 9 999. L'écran montrait un seuil que personne n'avait posé.
    leave(field)

    expect(field.value).toBe('9999')
    expect(draft.value('limits.max_tare_g')).toBe(9999)
  })

  it('remet aussi la valeur du fichier dans une case vidée de la grille de tarifs', () => {
    const draft = open()
    const field = tierField('Dénominateur du tarif 1')

    type(field, '')
    leave(field)

    expect(field.value).toBe('10')
    expect(draft.value('pricing.tiers.0.coef_den')).toBe(10)
  })

  it('le dit à l’écran, à côté des SEUILS comme à côté des tarifs', () => {
    open()

    // Les neuf champs de seuils ont exactement le même comportement silencieux que les
    // deux colonnes de la grille : la phrase ne pouvait pas rester dans le seul panneau
    // des tarifs.
    expect(panelAbout('garde-fous')).toContain('garde la valeur du fichier')
    expect(panelAbout('Grille de tarifs')).toContain('garde la valeur du fichier')
  })
})

describe('la grille de tarifs', () => {
  it('dessine DEUX lignes pour deux tarifs sans code', () => {
    // `tiersOf` remplace un code manquant par la chaîne vide. Clé d'itération, les deux
    // tarifs entraient en collision et Svelte n'en dessinait qu'un : la moitié de la
    // grille disparaissait de l'écran sans que rien ne le dise.
    open({
      pricing: {
        tiers: [
          { label: 'Adhérent', abbrev: 'A', coef_num: 9, coef_den: 10, rank: 1 },
          { label: 'Solidaire', abbrev: 'S', coef_num: 1, coef_den: 1, rank: 2 },
        ],
      },
    })

    expect(host.querySelectorAll('tbody tr')).toHaveLength(2)
    const text = pageText()
    expect(text).toContain('2 tarifs déclarés')
  })

  it('écrit dans le tarif de la LIGNE touchée, et pas dans un autre', () => {
    const draft = open()

    type(tierField('Libellé du tarif 2'), 'Solidaire renforcé')

    expect(draft.value('pricing.tiers.1.label')).toBe('Solidaire renforcé')
    expect(draft.value('pricing.tiers.0.label')).toBe('Adhérent')
  })
})

describe('le bloc code-barres de §14.4', () => {
  it('porte la vérification de la clé de contrôle de la référence', () => {
    const draft = open()

    const toggle = host.querySelector<HTMLElement>(
      '[data-flag="barcode.verify_reference_check_digit"]',
    )
    expect(toggle).not.toBeNull()
    expect(toggle?.getAttribute('data-on')).toBe('true')

    const box = toggle?.querySelector<HTMLInputElement>('input[type="checkbox"]')
    expect(box).not.toBeNull()
    if (box === null || box === undefined) return
    box.checked = false
    box.dispatchEvent(new Event('change', { bubbles: true }))
    flushSync()

    expect(draft.value('barcode.verify_reference_check_digit')).toBe(false)
  })

  it('dit pourquoi le plan de numérotation n’est PAS un réglage (ADR-028)', () => {
    open()
    expect(pageText()).toContain('constante du binaire')
  })
})

describe('ce que les garde-fous disent de la pesée en cours', () => {
  it('ne prétend plus qu’aucune pesée n’a été soumise depuis le démarrage', () => {
    open(laCagetteConfig(), healthWithVerdicts([]))

    // Ces verdicts portent sur le CYCLE EN VOL, que la machine remet à zéro en revenant
    // au repos : la phrase d'avant était fausse dès la deuxième seconde d'exploitation.
    const text = pageText()
    expect(text).not.toContain("aucune pesée n'a encore été soumise")
    expect(text).not.toContain('depuis le démarrage')
    expect(host.querySelector('[data-verdicts="0"]')?.textContent).toContain('EN COURS')
  })

  it('traduit le verdict : ni le code ni la sévérité ne sortent en anglais brut', () => {
    open(
      laCagetteConfig(),
      healthWithVerdicts([
        {
          code: 'WEIGHT_TOO_LOW',
          severity: 'blocking',
          message: 'La balance doit être retarée, ou l’emballage est trop lourd.',
          blocking: true,
          product_id: '',
        },
      ]),
    )

    const row = host.querySelector('.verdicts li')
    const text = collapse(row?.textContent ?? '')
    expect(text).toContain('Poids trop faible')
    expect(text).toContain('Bloquant')
    // Le jeton du service reste lisible en second, jamais À LA PLACE du libellé français.
    expect(text).not.toMatch(/\bblocking\b/u)
    expect(text).not.toMatch(/\binfo\b/u)
  })

  it('nomme le produit du verdict 13, le seul verdict qui en porte un', async () => {
    catalogProducts = [{ id: '4021', name: 'CURCUMA EN POUDRE' }]
    open(
      laCagetteConfig(),
      healthWithVerdicts([
        {
          code: 'LIGHT_PRODUCT_ALLOWED',
          severity: 'info',
          message: '',
          blocking: false,
          product_id: '4021',
        },
      ]),
    )
    await settle()

    // Son message est VIDE : sans l'id, la ligne se réduisait à « Produit léger autorisé »
    // sans dire lequel — c'est-à-dire sans l'information pour laquelle la règle existe.
    const row = collapse(host.querySelector('.verdicts li')?.textContent ?? '')
    expect(row).toContain('Produit léger autorisé')
    expect(row).toContain('CURCUMA EN POUDRE')
    expect(row).toContain('4021')
  })
})

describe('les dérogations de poids minimum', () => {
  it('NOMME le produit au lieu de montrer son identifiant seul', async () => {
    catalogProducts = [{ id: '4021', name: 'CURCUMA EN POUDRE' }]
    open(laCagetteConfig(), nominalHealth({ decisions: [waiver('4021', 8)] }))
    await settle()

    const row = collapse(host.querySelector('.waivers li')?.textContent ?? '')
    expect(row).toContain('CURCUMA EN POUDRE')
    // La borne est celle du noyau : le garde-fou 8 refuse à 8 g AUSSI. « À partir de
    // 8 g » promettait le contraire sur le gramme même que la dérogation vise.
    expect(row).toContain('plancher 8 g')
    expect(row).toContain('refusé à 8 g et en dessous')
  })

  it('borne la liste et annonce son total', async () => {
    catalogProducts = []
    const many = Array.from({ length: 25 }, (_unused, index) => waiver(`p${String(index)}`, 8))
    open(laCagetteConfig(), nominalHealth({ decisions: many }))
    await settle()

    expect(host.querySelectorAll('.waivers li')).toHaveLength(20)
    const total = collapse(host.querySelector('[data-waiver-total]')?.textContent ?? '')
    expect(total).toContain('20')
    expect(total).toContain('25')
    expect(total).toContain('Catalogue')
  })

  it('dit que les noms manquent quand le catalogue n’a pas répondu', async () => {
    catalogFails = true
    open(laCagetteConfig(), nominalHealth({ decisions: [waiver('4021', 8)] }))
    await settle()
    catalogFails = false

    const text = pageText()
    expect(text).toContain("Les noms de produits n'ont pas pu être lus")
    // La ligne reste : l'identifiant Odoo suffit à retrouver le produit.
    expect(host.querySelectorAll('.waivers li')).toHaveLength(1)
    expect(text).toContain('4021')
  })

  it('n’accuse AUCUN produit d’être absent quand le catalogue n’a pas répondu', async () => {
    catalogFails = true
    open(laCagetteConfig(), nominalHealth({ decisions: [waiver('4021', 8)] }))
    await settle()
    catalogFails = false

    // Le bandeau disait « les noms n'ont pas pu être lus » et les lignes en dessous
    // disaient le contraire : « produit absent du catalogue en service » est une
    // accusation, et le catalogue n'a jamais répondu.
    const row = collapse(host.querySelector('.waivers li')?.textContent ?? '')
    expect(row).not.toContain('absent du catalogue')
    expect(row).toContain('Nom non lu')
  })

  it('dit qu’une dérogation sur un produit retiré ne sert à rien', async () => {
    catalogProducts = [
      { id: '4021', name: 'CURCUMA EN POUDRE' },
      { id: '4412', name: 'SAFRAN' },
    ]
    const withdrawn: DecisionDTO = { ...waiver('4412', 6), offered: false }
    open(laCagetteConfig(), nominalHealth({ decisions: [waiver('4021', 8), withdrawn] }))
    await settle()

    // Les deux décisions vivent dans la MÊME ligne de `local_decisions` (§10.6) : le
    // garde-fou 14 refuse le produit avant que le 8 ait un sens.
    const rows = [...host.querySelectorAll('.waivers li')]
    expect(rows).toHaveLength(2)
    expect(rows[1]?.getAttribute('data-withdrawn')).toBe('true')
    const row = collapse(rows[1]?.textContent ?? '')
    expect(row).toContain('Produit retiré')
    expect(row).toContain('garde-fou 14')

    const total = collapse(host.querySelector('[data-waiver-total]')?.textContent ?? '')
    expect(total).not.toContain('2 dérogations en vigueur')
    expect(total).toContain('1 sur un produit retiré')
  })
})
