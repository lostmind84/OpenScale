import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { FIELD_LABELS, labelOf } from '../src/admin/lib/fields'

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
