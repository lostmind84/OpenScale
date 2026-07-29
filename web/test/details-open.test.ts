import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * Un volet replié n'apprend jamais son ouverture du flux de rendu.
 *
 * Le défaut que ce banc interdit a tenu deux volets de la page Matériel, et il ne se
 * voyait dans aucun test : `open={uneExpression}` sur un `<details>` ne compile PAS comme
 * un attribut ordinaire. `open` est une propriété du DOM, et le compilateur émet alors
 * `details.open = …` — une affectation directe, sans la mémoïsation que porte l'écriture
 * d'attribut. Cette affectation est fondue dans l'effet de gabarit du fragment, avec tous
 * les autres textes de la page : dès qu'UN SEUL d'entre eux dépend du tableau de bord,
 * l'effet se rejoue au sondage — toutes les trois secondes — et réécrit `open` par-dessus
 * ce que le doigt venait de faire. Rien d'autre à l'écran ne bougeait, et c'est pourquoi
 * le symptôme ressemblait à un re-rendu alors que le nœud n'était jamais recréé.
 *
 * D'où la règle, qui ne coûte rien à respecter : l'ouverture d'un volet est un état de
 * l'écran, elle se lie par `bind:open` à un état local — le compilateur en fait alors un
 * effet isolé, et l'événement `toggle` réinjecte dans cet état ce que le doigt a fait. Ce
 * qui doit ouvrir un volet tout seul l'ouvre en ÉCRIVANT cet état, jamais en le pilotant.
 *
 * Le banc regarde `src` entier et pas la seule page fautive : la faute est une CLASSE, et
 * le troisième volet du projet n'est pas encore écrit.
 */

const WEB_SRC = resolve(dirname(fileURLToPath(import.meta.url)), '../src')

/** Les fichiers d'écran dont le nom correspond, récursivement. */
function sources(dir: string, matching: RegExp): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sources(path, matching)
    return matching.test(entry) ? [path] : []
  })
}

/** Chaque écran avec son contenu, pour que l'échec nomme le fichier fautif. */
const screens = sources(WEB_SRC, /\.svelte$/u).map((path) => ({
  path: relative(WEB_SRC, path).replace(/\\/gu, '/'),
  text: readFileSync(path, 'utf8'),
}))

/**
 * Les balises `<details>` ouvrantes d'un fichier, chacune avec sa ligne.
 *
 * Le tri s'arrête au premier `>`, ce qui suffit ici : aucune balise du dépôt ne porte de
 * `>` dans une de ses expressions avant `open`, et une flèche `=>` égarée ferait couper la
 * balise trop tôt — le banc raterait alors un volet, il n'en inventerait aucun.
 */
function detailsTags(source: string): { line: number; tag: string }[] {
  return [...source.matchAll(/<details\b[^>]*>/gu)].map((match) => ({
    line: source.slice(0, match.index).split('\n').length,
    tag: match[0],
  }))
}

/**
 * Les volets dont l'ouverture est PILOTÉE par une expression, s'il en reste.
 *
 * `bind:open` est retiré d'abord, sans quoi il porterait lui-même le motif cherché. Ce qui
 * reste est ce que la règle refuse : un `open=` en écriture seule.
 *
 * @param source - le texte d'un écran.
 */
function drivenOpen(source: string): string[] {
  return detailsTags(source)
    .filter(({ tag }) => /\bopen\s*=/u.test(tag.replace(/\bbind:open\s*=\s*\{[^}]*\}/gu, ' ')))
    .map(({ line, tag }) => `ligne ${String(line)} : ${tag}`)
}

describe('aucun volet ne confie son ouverture au flux de rendu', () => {
  it.each(screens)('$path', ({ text }) => {
    expect(
      drivenOpen(text),
      'ce <details> reçoit son ouverture d’une expression : le prochain rafraîchissement ' +
        'la réécrira sous le doigt. Liez-la par bind:open à un état local.',
    ).toEqual([])
  })

  it('a bien lu les deux écrans, et pas un dossier vide', () => {
    expect(screens.length).toBeGreaterThan(20)
    expect(screens.flatMap(({ text }) => detailsTags(text)).length).toBeGreaterThan(0)
  })

  it('reconnaît le motif qu’il interdit, et laisse passer celui qu’il demande', () => {
    // Sans cette paire, un banc qui ne trouverait plus AUCUN <details> passerait au vert
    // en ne prouvant rien — et c'est l'état dans lequel il finirait par se figer.
    expect(drivenOpen('<details class="folded" open={scaleRefused}>')).toHaveLength(1)
    expect(drivenOpen('<details class="folded" bind:open={scaleSettingsOpen}>')).toEqual([])
  })
})
