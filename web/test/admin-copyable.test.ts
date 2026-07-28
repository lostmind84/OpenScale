import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * L'administration se lit-elle comme un document ?
 *
 * L'écran client n'en est pas un : on ne sélectionne rien, on ne zoome pas sur un double
 * appui, aucun menu contextuel ne s'ouvre — c'est §14.2, et cela reste vrai. Mais
 * l'administration s'ouvre dans LA MÊME fenêtre (§14.1), elle héritait de ces trois
 * interdits, et rien n'y était copiable : ni le détail d'une erreur du journal technique,
 * ni une trame brute, ni un chemin, ni une empreinte de configuration. Ce sont exactement
 * les lignes qu'on recopie dans un message au support ou dans Odoo, et les retaper à la
 * main est le meilleur moyen d'appeler pour rien.
 *
 * Trois choses tenues ici, parce qu'aucune ne se voit dans un test de rendu :
 *
 *  1. la règle du kiosque EXISTE toujours — l'exception ne l'a pas supprimée ;
 *  2. la sélection est rendue à l'administration, et refusée à ses boutons : un appui qui
 *     glisse un peu ne doit pas étaler une sélection en travers de l'écran ;
 *  3. le menu contextuel s'ouvre dans l'administration, sinon « copier » n'a plus de
 *     geste sur un poste conduit à la souris.
 */

const SOURCE = resolve(dirname(fileURLToPath(import.meta.url)), '../src')

/** La feuille globale : c'est elle qui pose l'interdit, et donc elle qui le lève. */
const APP_CSS = readFileSync(join(SOURCE, 'app.css'), 'utf8')

/** Le point d'entrée de l'écran client, où les trois gestes du navigateur sont refusés. */
const MAIN_TS = readFileSync(join(SOURCE, 'main.ts'), 'utf8')

describe('l’écran client reste un kiosque', () => {
  it('interdit toujours la sélection sur le corps du document', () => {
    expect(APP_CSS).toMatch(/body\s*\{[^}]*user-select:\s*none/su)
  })
})

describe('l’administration est un document', () => {
  it('rend la sélection à tout l’écran d’administration', () => {
    expect(APP_CSS).toMatch(/\[data-admin\]\s*\{[^}]*user-select:\s*text/su)
  })

  it('la refuse à ses boutons, qui se pressent et ne se lisent pas', () => {
    expect(APP_CSS).toMatch(/\[data-admin\]\s+button\s*\{[^}]*user-select:\s*none/su)
  })

  it('déclare cette exception AU MÊME endroit que la règle qu’elle lève', () => {
    // Une page qui redéclarerait la sienne rouvrirait le défaut d'à côté : la sélection
    // marcherait sur ce qu'un auteur a pensé à couvrir, et nulle part ailleurs.
    for (const file of sourcesUnder(join(SOURCE, 'admin'))) {
      expect(readFileSync(file, 'utf8'), file).not.toContain('user-select')
    }
  })

  it('y laisse le menu contextuel s’ouvrir', () => {
    // Le refus est global sur le document : sans exemption, « Copier » n'existe plus, et
    // une sélection qu'on ne peut pas copier ne sert à rien.
    expect(MAIN_TS).toMatch(/contextmenu/u)
    expect(MAIN_TS).toMatch(/insideAdmin\(e\.target\)/u)
    expect(MAIN_TS).toMatch(/closest\('\[data-admin\]'\)/u)
  })
})

/**
 * Les fichiers de source d'un répertoire, sans descendre dans ce qui n'en est pas.
 *
 * @param directory - la racine à parcourir.
 */
function sourcesUnder(directory: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry)
    if (statSync(path).isDirectory()) {
      found.push(...sourcesUnder(path))
    } else if (/\.(svelte|css|ts)$/u.test(entry)) {
      found.push(path)
    }
  }
  return found
}
