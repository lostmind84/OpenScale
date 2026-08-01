import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { NAME_SIZE_MIN_PX } from '../src/lib/typography'

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
  '--action': '#17518f',
  '--danger': '#a11f19',
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
 * `--surface` sert de texte partout où il est posé SUR une couleur, et jamais sur du
 * clair : l'initiale posée sur la couleur de catégorie — dont le fond ne vient PAS des
 * jetons mais de `catalog.categories[].color`, donc de la configuration du poste, et s'y
 * vérifie à la validation — et les deux fonds pleins de l'administration, `--action` et
 * `--danger`, que portent `Act`, `BigButton` et les deux sélecteurs de fichier des zones
 * de dépôt. Leur contraste est mesuré par le describe « les deux fonds pleins ».
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

describe('les deux fonds pleins de l’administration', () => {
  /**
   * La couleur y est un FOND et l'encre est blanche : la règle « aucune couleur ne
   * porte de lettres » interdit d'écrire EN --warning ou EN --fault sur fond clair,
   * ce qui reste vrai. Ce qui est vérifié ici est l'autre sens, et il est mesuré.
   */
  it.each([
    ['--action', 'ce qui écrit'],
    ['--danger', 'ce qui est irréversible'],
  ])('%s porte l’encre blanche à au moins 7:1 — %s', (token) => {
    expect(contrast(TOKENS[token] as string, TOKENS['--surface'] as string)).toBeGreaterThanOrEqual(
      7,
    )
  })

  it('les déclare dans app.css et pas seulement dans ce test', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    for (const token of ['--action', '--danger']) {
      expect(css).toContain(`${token}: ${TOKENS[token] as string}`)
    }
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

/** La valeur déclarée d'un jeton de `app.css`, jusqu'au point-virgule. */
function declarationOf(css: string, token: string): string {
  return new RegExp(`${token}:\\s*([^;]+);`, 'u').exec(css)?.[1] ?? ''
}

describe('la densité de grille (ADR-035)', () => {
  it('la densité de grille est continue : plus de palier data-tile-size', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    expect(css).not.toMatch(/\[data-tile-size/)
    expect(css).toMatch(/--tile-min:\s*clamp\(/)
    expect(css).toMatch(/--tile-height:\s*calc\(/)
  })
})

describe('le facteur d’échelle de la tuile', () => {
  const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')

  it('déclare les jetons mis à l’échelle pour :root ET pour qui porte une échelle', () => {
    // Une propriété personnalisée voit ses `var()` substitués À L'ÉLÉMENT QUI LA
    // DÉCLARE : écrits sur `:root` seul, ces jetons se figeraient contre le
    // facteur de la racine, et une sonde qui pose le sien plus bas hériterait
    // d'une valeur déjà calculée contre 1 — sans que rien ne bouge, en silence.
    const block = /:root,\s*\[data-tile-scale\]\s*\{([\s\S]*?)\n\}/u.exec(css)?.[1] ?? ''
    for (const token of ['--tile-media', '--tile-name', '--tile-pad', '--tile-height']) {
      expect(block).toContain(`${token}:`)
    }
  })

  it('déclare --tile-scale à 1 dans :root, sans quoi la grille casserait AU REPOS', () => {
    // Un `calc()` qui porte une propriété personnalisée non définie est invalide
    // à la valeur calculée : les trois jetons ci-dessous perdraient leur valeur,
    // et c'est le poste qui ne règle rien qui le paierait.
    expect(declarationOf(css, '--tile-scale')).toBe('1')
  })

  it.each(['--tile-media', '--tile-name', '--tile-pad'])('%s porte var(--tile-scale)', (token) => {
    expect(declarationOf(css, token)).toMatch(
      /^calc\(\s*clamp\([^)]*\)\s*\*\s*var\(--tile-scale\)\s*\)$/u,
    )
  })

  it('--tile-min ne le porte PAS : il calibre l’échelle et sert encore auto-fill', () => {
    const declared = declarationOf(css, '--tile-min')
    expect(declared).toBe('clamp(15rem, 19vw, 22rem)')
    expect(declared).not.toContain('--tile-scale')
  })

  it('--tile-height se recompose des jetons mis à l’échelle et n’en porte pas un de plus', () => {
    expect(declarationOf(css, '--tile-height')).not.toContain('--tile-scale')
  })

  it('ne met à l’échelle ni l’interligne plaque-nom, ni les deux bordures', () => {
    // Une bordure de 0,7 px n'est pas une bordure : ces deux littéraux restent
    // absolus dans le `calc` de --tile-height.
    const height = declarationOf(css, '--tile-height')
    expect(height).toContain('0.5rem')
    expect(height).toContain('2px')
    expect(height).not.toMatch(/(?:0\.5rem|2px)\s*\*/u)
  })
})

describe('le plancher de lisibilité, écrit des deux côtés et jamais deux fois différent', () => {
  const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')

  it('déclare --text-min à la valeur EXACTE de NAME_SIZE_MIN_PX', () => {
    // Le CSS dessine le prix, le TypeScript choisit le corps du nom : le même
    // plancher vit donc aux deux endroits. Ce cas est ce qui interdit qu'il
    // descende à 16 d'un seul côté — un compteur recopié finit par mentir.
    const declared = /--text-min:\s*([\d.]+)rem/u.exec(css)
    expect(declared).not.toBeNull()
    expect(Number(declared?.[1]) * 16).toBe(NAME_SIZE_MIN_PX)
  })

  it('ne le met pas à l’échelle : une distance de lecture n’est pas une proportion', () => {
    expect(declarationOf(css, '--text-min')).not.toContain('--tile-scale')
  })
})

describe('le bloc des prix suit la tuile (mesure du 01/08/2026)', () => {
  const tile = readFileSync(join(SOURCE_DIR, 'components/Tile.svelte'), 'utf8')

  // Laissé constant, le prix sortait de sa tuile dès 10 colonnes sur 1920 et
  // donnait à l'écran client une barre de défilement HORIZONTALE — 66 px sur
  // 1366, avec « 20,09 €/kg » rogné en « 20,09 €/ ». Un kiosque n'en a pas.
  it.each([
    ['--price-size', '1.75rem'],
    ['--price-size-secondary', '1.25rem'],
  ])('%s descend avec l’échelle et s’arrête au plancher', (token, nominal) => {
    expect(tile).toMatch(
      new RegExp(`${token}:\\s*max\\(\\s*var\\(--text-min\\),\\s*${nominal}\\s*\\*\\s*var\\(--tile-scale\\)\\s*\\)`, 'u'),
    )
  })

  it('PLANCHER SUR LES DEUX PRIX : il ne connaît pas d’exception selon qui paie', () => {
    // Au seul rapport, le second tarif sortait à 15,1 px à 7 colonnes sur 1920,
    // sous un plancher qui existe pour dire « en dessous, personne ne lit ». Un
    // plancher qui vaut pour un prix sur deux n'est pas un plancher. Ce que les
    // deux corps réunis coûtent est UN signal sur quatre : l'encre, la graisse
    // et le badge portent la hiérarchie d'ADR-036 intacte.
    const block = /\.price\s*\{[\s\S]*?\.price\.secondary\s*\{[^}]*\}/u.exec(tile)?.[0] ?? ''
    expect(block).not.toBe('')
    expect(block).not.toMatch(/font-size:\s*[\d.]+rem/u)
    expect(block).toContain('font-size: var(--price-size)')
    expect(block).toContain('font-size: var(--price-size-secondary)')
  })

  it('garde les trois autres signaux de la hiérarchie, que le plancher n’aplatit pas', () => {
    const secondary = /\.price\.secondary\s*\{[^}]*\}/u.exec(tile)?.[0] ?? ''
    expect(secondary).toContain('color: var(--ink-muted)')
    expect(secondary).toContain('font-weight: 400')
    expect(tile).toMatch(/\.abbrev\.hollow\s*\{/u)
  })

  it('met à l’échelle les espacements du bloc, et laisse le filet à 1px', () => {
    const block = /\.prices\s*\{[^}]*\}/u.exec(tile)?.[0] ?? ''
    expect(block).toMatch(/gap:\s*calc\([\d.]+rem\s*\*\s*var\(--tile-scale\)\)/u)
    expect(block).toMatch(/padding-top:\s*calc\([\d.]+rem\s*\*\s*var\(--tile-scale\)\)/u)
    expect(block).toContain('border-top: 1px solid var(--border)')
  })
})
