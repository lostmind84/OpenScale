import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * The painting order of the client screen, read from the stylesheets themselves.
 *
 * Vitest applies no component style and jsdom lays nothing out (`vite.config.ts`),
 * so no mounted test can ever observe one layer covering another: in jsdom a click
 * on a button buried under an opaque overlay still reaches the button. What can be
 * held is the DECLARED order, and that is what this file reads.
 *
 * It matters because of one station state. A station out of the installer starts in
 * OutOfService, the overlay of `FullScreen.svelte` covers the whole window, and the
 * settings key of the bottom bar is the only way into the administration (ADR-032:
 * no URL to type, no way out of the kiosk). Painted under the overlay, that key
 * leaves a brand new station in the single state where it cannot be configured.
 *
 * The fix is one layer deep on purpose. The overlay keeps doing its work — a
 * customer must not weigh on a station that cannot serve — and the settings key
 * alone goes through it.
 */

const SOURCE_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../src')

/** Reads one front source file, named relative to `web/src`. */
function source(...parts: string[]): string {
  return readFileSync(join(SOURCE_DIR, ...parts), 'utf8')
}

/** The declarations of the first `selector { … }` rule of a stylesheet. */
function rule(css: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')
  const found = new RegExp(String.raw`(?:^|[\s,;}])${escaped}\s*\{([^{}]*)\}`, 'u').exec(css)
  if (found === null) throw new Error(`la règle « ${selector} » a disparu de sa feuille de style`)
  return found[1] as string
}

/** The `z-index` a rule declares, or `null` when it declares none. */
function depth(declarations: string): number | null {
  const found = /(?:^|[\s;{])z-index:\s*(-?\d+)/u.exec(declarations)
  return found === null ? null : Number(found[1])
}

/** The `position` a rule declares; `static` is what the absence of one means. */
function position(declarations: string): string {
  return /(?:^|[\s;{])position:\s*([a-z]+)/u.exec(declarations)?.[1] ?? 'static'
}

/** Declaring any one of these opens a stacking context, wherever the rule sits. */
const CONTEXT_MAKERS = [
  'transform',
  'filter',
  'opacity',
  'will-change',
  'isolation',
  'contain',
  'mix-blend-mode',
  'backdrop-filter',
  'perspective',
]

/** The first stacking-context property a rule declares, or an empty string. */
function contextMaker(declarations: string): string {
  const pattern = String.raw`(?:^|[\s;{])(${CONTEXT_MAKERS.join('|')})\s*:`
  return new RegExp(pattern, 'u').exec(declarations)?.[1] ?? ''
}

const veil = rule(source('components', 'FullScreen.svelte'), '.screen')
const settingsKey = rule(source('components', 'StatusBar.svelte'), '.admin')
const bottomBar = rule(source('components', 'StatusBar.svelte'), '.bar')
const reprint = rule(source('components', 'StatusBar.svelte'), '.reprint')

/**
 * Every layer of the two screens, bottom to top, in the order they must keep.
 *
 * Each line is a reason, not a number: the overlay hides a station that cannot
 * serve, the settings key is the way to repair it, the administration replaces the
 * client screen, the password panel gates what writes, and the ERR-UI-01 screen is
 * what is left when everything else is broken.
 */
const LAYERS = [
  { name: 'le voile de panne', declarations: veil },
  { name: 'la touche Réglages', declarations: settingsKey },
  { name: 'l’administration', declarations: rule(source('admin', 'App.svelte'), '.admin') },
  {
    name: 'le panneau de mot de passe',
    declarations: rule(source('admin', 'components', 'PasswordPanel.svelte'), '.scrim'),
  },
  { name: 'l’écran fatal ERR-UI-01', declarations: rule(source('app.css'), '.fatal') },
] as const

/** `le voile de panne = 10 · la touche Réglages = aucun · …`, for a readable failure. */
function reading(): string {
  return LAYERS.map((layer) => `${layer.name} = ${depth(layer.declarations) ?? 'aucun'}`).join(' · ')
}

describe('l’empilement des couches, du voile à l’écran fatal', () => {
  it('donne une profondeur déclarée à chacune des cinq couches', () => {
    const silent = LAYERS.filter((layer) => depth(layer.declarations) === null).map((l) => l.name)
    expect(silent, `ces couches ne déclarent aucun z-index — ${reading()}`).toEqual([])
  })

  it('les empile dans l’ordre strict de leur raison d’être', () => {
    const stack = LAYERS.map((layer) => depth(layer.declarations) ?? -1)
    expect(stack, `l’ordre lu est faux — ${reading()}`).toEqual([...stack].sort((a, b) => a - b))
    expect(new Set(stack).size, `deux couches partagent une profondeur — ${reading()}`).toBe(
      stack.length,
    )
  })
})

describe('le voile de panne, et la seule chose qui le traverse', () => {
  it('laisse la touche Réglages passer au-dessus du voile', () => {
    expect(
      depth(settingsKey) ?? -1,
      'la touche Réglages est peinte sous le voile : un poste neuf, qui démarre hors service, n’est réglable par aucun chemin prévu',
    ).toBeGreaterThan(depth(veil) ?? 0)
  })

  it('lui donne une position qui rende cette profondeur effective', () => {
    expect(
      position(settingsKey),
      'un z-index sur un élément statique ne compte pas : la touche resterait sous le voile',
    ).not.toBe('static')
  })

  it('garde « Réimprimer » sous le voile : un poste en panne ne réimprime rien', () => {
    expect(
      depth(reprint),
      'seule l’entrée vers l’administration traverse le voile, pas la barre entière',
    ).toBeNull()
  })

  it('garde le voile sur toute la fenêtre : aucune tuile ne redevient touchable', () => {
    expect(position(veil)).toBe('fixed')
    expect(veil, 'un voile raccourci rendrait la grille touchable sur un poste en panne').toMatch(
      /(?:^|[\s;{])inset:\s*0\s*;/u,
    )
  })

  /**
   * The silent way the fix could come undone.
   *
   * `.bar` is a flex item at `z-index: auto`, so it creates no stacking context and
   * the settings key climbs into the root one. Give the bar a `z-index`, an
   * `opacity` below 1, a `transform`, a `filter` or an `isolation`, and the key is
   * trapped inside a context that is itself painted under the overlay — with the
   * declared numbers still reading exactly right.
   */
  it('laisse la barre basse sans contexte d’empilement propre', () => {
    expect(
      depth(bottomBar),
      'une profondeur sur la barre enfermerait la touche Réglages sous le voile, en silence',
    ).toBeNull()
    expect(
      contextMaker(bottomBar),
      'cette propriété ouvre un contexte d’empilement sur la barre, et y enferme la touche Réglages',
    ).toBe('')
  })
})
