import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * Les jetons tiennent-ils les deux règles physiques de §14.2 ?
 *
 * 1. Contrastes AAA (≥ 7:1) sur tout texte ≥ 24 px, AA (≥ 4,5:1) partout ailleurs.
 * 2. Toute cible touchable déclare au moins 72 px, en unités relatives — une
 *    dérive de calibration tactile de 5 mm, cas réel après un changement d'écran,
 *    doit rester sans effet. C'est une mitigation par le design, pas par la
 *    procédure, et son mode de défaillance est silencieux : un bouton un peu trop
 *    petit marche encore pour celui qui l'a testé.
 */

const SOURCE_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../src')

/** Les jetons de couleur de §14.2, recopiés depuis `src/app.css`. */
const TOKENS: Record<string, string> = {
  '--bg': '#f7f6f3',
  '--surface': '#ffffff',
  '--border': '#e2dfd8',
  '--ink': '#1c1b19',
  '--ink-muted': '#5b5850',
  '--waiting': '#8a867c',
  '--ready': '#1e8e4e',
  '--warning': '#c8641b',
  '--fault': '#b3261e',
  '--focus': '#1e5fa8',
}

/**
 * L'inventaire des couleurs employées COMME TEXTE, avec le corps le plus petit
 * auquel chacune apparaît. Un test plus bas interdit qu'une couleur employée
 * comme texte échappe à cette table.
 */
const TEXT_PAIRS = [
  { fg: '--ink', bg: '--bg', px: 18 },
  { fg: '--ink', bg: '--surface', px: 18 },
  { fg: '--ink-muted', bg: '--bg', px: 18 },
  { fg: '--ink-muted', bg: '--surface', px: 24 },
] as const

/**
 * `--surface` sert de texte à un seul endroit : l'initiale posée sur la couleur de
 * catégorie. Ce fond ne vient PAS des jetons mais de `catalog.categories[].color`,
 * donc de la configuration du poste : le contraste s'y vérifie à la validation de
 * la configuration, pas ici.
 */
const OUT_OF_TOKEN_SCOPE = new Set(['--surface'])

/** Composante linéarisée d'un canal sRGB, formule WCAG 2.1. */
function channel(value: number): number {
  const c = value / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

/** Luminance relative d'une couleur `#rrggbb`. */
function luminance(hex: string): number {
  const n = Number.parseInt(hex.slice(1), 16)
  return (
    0.2126 * channel((n >> 16) & 255) +
    0.7152 * channel((n >> 8) & 255) +
    0.0722 * channel(n & 255)
  )
}

/** Rapport de contraste entre deux couleurs, entre 1 et 21. */
function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)]
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
}

/** Tous les fichiers de source du front, récursivement. */
function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sources(path)
    return /\.(svelte|css)$/u.test(entry) ? [path] : []
  })
}

const styles = sources(SOURCE_DIR).map((path) => readFileSync(path, 'utf8'))

/**
 * L'ÉCRAN CLIENT seul, pour la règle des cibles tactiles.
 *
 * Cette règle vient d'une contrainte physique — 20 mm sous un doigt, à 60-80 cm — et
 * l'administration n'y est pas soumise : elle se conduit à la souris, sur des pages de
 * réglages qui portent 45 champs, et imposer 72 px à chacun donnait une page Règles de
 * 1 900 px de haut (ADR-033). Ce que ce test garde est donc l'écran que le CLIENT touche,
 * où la contrainte s'applique vraiment.
 *
 * Les neuf gros boutons du mode bénévole, que §14.4 veut gros et qu'on touche au
 * comptoir, ont leur propre test dans `admin-troubleshooting.test.ts`.
 */
const clientStyles = sources(SOURCE_DIR)
  .filter((path) => !path.includes(`${sep}admin`))
  .map((path) => readFileSync(path, 'utf8'))

describe('les contrastes de §14.2', () => {
  it.each(TEXT_PAIRS)('$fg sur $bg, à $px px', ({ fg, bg, px }) => {
    const ratio = contrast(TOKENS[fg] as string, TOKENS[bg] as string)
    expect(ratio).toBeGreaterThanOrEqual(px >= 24 ? 7 : 4.5)
  })

  it('n’emploie comme texte que des couleurs inventoriées ci-dessus', () => {
    const declared = new Set<string>([...TEXT_PAIRS.map((p) => p.fg), ...OUT_OF_TOKEN_SCOPE])
    const used = new Set<string>()
    for (const style of styles) {
      for (const match of style.matchAll(/(?:^|[;{\s])color:\s*var\((--[a-z-]+)\)/gu)) {
        used.add(match[1] as string)
      }
    }
    expect([...used].filter((token) => !declared.has(token))).toEqual([])
  })

  it('ÉCART DOCUMENTAIRE : --warning et --fault ne peuvent pas être du texte', () => {
    // §14.2 fixe la palette ET la règle « AAA (≥ 7:1) sur tout texte ≥ 24 px ».
    // Les deux ne tiennent pas ensemble : sur --surface, --warning plafonne à
    // 3,97:1 et --fault à 6,54:1. Les employer comme couleur de lettres à 28 px
    // violerait la règle qui les déclare — d'où le liseré, jamais l'encre.
    expect(contrast(TOKENS['--warning'] as string, TOKENS['--surface'] as string)).toBeLessThan(7)
    expect(contrast(TOKENS['--fault'] as string, TOKENS['--surface'] as string)).toBeLessThan(7)
    expect(contrast(TOKENS['--fault'] as string, TOKENS['--bg'] as string)).toBeLessThan(7)
  })
})

describe('les cibles tactiles de §14.2', () => {
  it('déclare --touch-min à 4,5rem, soit 72 px sur la base de 16 px', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    const declared = /--touch-min:\s*([\d.]+)rem/u.exec(css)
    expect(declared).not.toBeNull()
    expect(Number(declared?.[1]) * 16).toBeGreaterThanOrEqual(72)
  })

  it('donne la classe .touch-target à CHAQUE bouton de l’écran client', () => {
    const offenders: string[] = []
    for (const [index, style] of clientStyles.entries()) {
      for (const match of style.matchAll(/<button\b[^>]*>/gsu)) {
        if (!match[0].includes('touch-target')) offenders.push(`${index}: ${match[0]}`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('espace les cibles d’au moins 8 px, ce qui absorbe 5 mm de dérive tactile', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    const gap = /--touch-gap:\s*([\d.]+)rem/u.exec(css)
    expect(Number(gap?.[1]) * 16).toBeGreaterThanOrEqual(8)
  })
})

describe('la densité de grille (ADR-035)', () => {
  it('la densité de grille est continue : plus de palier data-tile-size', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    expect(css).not.toMatch(/\[data-tile-size/)
    expect(css).toMatch(/--tile-min:\s*clamp\(/)
    expect(css).toMatch(/--tile-height:\s*calc\(/)
  })
})
