import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

/**
 * Mesure le poids de l'écran CLIENT et refuse le budget de §14.1.
 *
 * « Budget client : < 110 ko gzip, premier rendu < 400 ms sur un i3 de 2015 —
 * mesuré en CI. » Ce script mesure la première moitié : tout ce qu'un navigateur
 * télécharge AVANT le premier rendu — le document, ses modules préchargés, ses
 * feuilles de style et la police, qui est bloquante (`font-display: block`).
 *
 * Ce qu'il exclut est tout l'intérêt de la séparation en deux entrées : les
 * modules de l'administration ne sont ni téléchargés, ni analysés, ni exécutés
 * tant que personne ne fait l'appui long de trois secondes (§14.1, §14.3).
 *
 * La réachabilité est lue dans le HTML produit — `<script type="module">`,
 * `<link rel="modulepreload">`, `<link rel="stylesheet">` — puis dans les `url()`
 * des feuilles de style. Un `import()` dynamique n'y figure jamais : c'est
 * exactement ce que le budget doit ignorer.
 */

const BUDGET_BYTES = 110 * 1024
const DIST = resolve(dirname(fileURLToPath(import.meta.url)), '../../internal/web/dist')

/**
 * Liste récursivement les fichiers d'un répertoire.
 *
 * @param {string} dir répertoire à parcourir.
 * @returns {string[]} chemins absolus.
 */
function walk(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    return statSync(path).isDirectory() ? walk(path) : [path]
  })
}

/**
 * Ressources bloquantes d'un document.
 *
 * @param {string} document nom du fichier HTML dans `dist`.
 * @returns {string[]} chemins absolus, document compris.
 */
function blockingAssets(document) {
  const html = readFileSync(join(DIST, document), 'utf8')
  const assets = new Set([join(DIST, document)])

  for (const match of html.matchAll(/(?:src|href)="\/([^"]+)"/g)) {
    assets.add(join(DIST, match[1]))
  }
  for (const asset of [...assets]) {
    if (!asset.endsWith('.css')) continue
    for (const match of readFileSync(asset, 'utf8').matchAll(/url\(\/([^)]+)\)/g)) {
      assets.add(join(DIST, match[1]))
    }
  }
  return [...assets].sort()
}

/** Affiche une ligne de tableau : taille gzip et chemin relatif. */
function line(file) {
  const size = gzipSync(readFileSync(file)).length
  console.log(`  ${String(size).padStart(7)} o gzip  ${relative(DIST, file).replace(/\\/g, '/')}`)
  return size
}

const client = blockingAssets('index.html')
console.log('Budget de l’écran client — §14.1 : < 110 ko gzip\n')
const total = client.reduce((sum, file) => sum + line(file), 0)

console.log(`\n  ${String(total).padStart(7)} o gzip  TOTAL CLIENT`)
console.log(`  ${String(BUDGET_BYTES).padStart(7)} o gzip  budget`)
console.log(
  `  ${String(BUDGET_BYTES - total).padStart(7)} o gzip  marge — ${((100 * total) / BUDGET_BYTES).toFixed(1)} % du budget\n`,
)

const excluded = walk(DIST).filter((file) => !client.includes(file))
console.log('Hors budget client — rien de tout cela n’est chargé pour afficher la grille :')
let rest = 0
for (const file of excluded.sort()) rest += line(file)
console.log(`  ${String(rest).padStart(7)} o gzip  TOTAL HORS ÉCRAN CLIENT`)

if (total > BUDGET_BYTES) {
  console.error(`\nÉCHEC : ${total} o gzip dépassent le budget de ${BUDGET_BYTES} o.`)
  process.exit(1)
}
