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

/**
 * Une chaîne qu'on ne trouve que dans l'administration : la route de son tableau de bord.
 *
 * Le budget se mesure en octets, mais un total sous le plafond ne prouve pas que la
 * séparation existe : le jour où le code d'administration atterrit dans un morceau que
 * `index.html` précharge, le chiffre monte sans que rien ne dise POURQUOI. Ce marqueur, lui,
 * le dit.
 */
const ADMIN_MARKER = '/admin/api/health'

/**
 * La chaîne que porte le runtime SERVEUR de Svelte, et qu'aucun bundle ne doit contenir.
 *
 * Elle est le symptôme exact d'un défaut qui n'a fait aucun bruit : avec
 * `resolve.conditions` privé de `browser`, `svelte` se résout sur `src/index-server.js`, où
 * `mount()` est `function(){ throw lifecycle_function_unavailable }`. Le bundle se construit,
 * pèse le bon poids, passe le budget — et ne monte AUCUN écran. Pire, esbuild supprime les
 * arguments de cette fonction sans paramètre, si bien que le morceau d'administration est
 * sorti à 288 octets, vidé de son propre code, sans un avertissement.
 */
const SERVER_RUNTIME_MARKER = 'lifecycle_function_unavailable'

/** Lit un fichier en texte, ou une chaîne vide pour une police ou une image. */
function text(file) {
  return /\.(js|css|html)$/u.test(file) ? readFileSync(file, 'utf8') : ''
}

const leaking = client.filter((file) => text(file).includes(ADMIN_MARKER))
if (leaking.length > 0) {
  console.error(
    `\nÉCHEC : l’administration est dans le chemin de chargement de l’écran client — ` +
      `${leaking.map((file) => relative(DIST, file)).join(', ')}. La séparation en deux ` +
      `entrées porte sur le POIDS et le CHARGEMENT (§14.1) : un poste qui n’ouvre jamais ` +
      `l’administration ne doit en télécharger, analyser et exécuter aucun octet.`,
  )
  process.exit(1)
}

const adminFiles = excluded.filter((file) => text(file).includes(ADMIN_MARKER))
if (adminFiles.length === 0) {
  console.error(
    `\nÉCHEC : le bundle d’administration ne contient pas son propre code — aucun fichier ` +
      `hors budget ne porte « ${ADMIN_MARKER} ». Un morceau vide passe le budget en ` +
      `silence et ne sert aucun écran.`,
  )
  process.exit(1)
}

const server = walk(DIST).filter((file) => text(file).includes(SERVER_RUNTIME_MARKER))
if (server.length > 0) {
  console.error(
    `\nÉCHEC : le runtime SERVEUR de Svelte est dans le bundle — ` +
      `${server.map((file) => relative(DIST, file)).join(', ')}. ` +
      `« mount() » y lève lifecycle_function_unavailable : aucun écran ne démarre. ` +
      `Vérifiez resolve.conditions dans vite.config.ts : il REMPLACE les valeurs par ` +
      `défaut de Vite, il ne s’y ajoute pas.`,
  )
  process.exit(1)
}

console.log(
  `\nSéparation vérifiée : « ${ADMIN_MARKER} » est dans ` +
    `${adminFiles.map((file) => relative(DIST, file).replace(/\\/g, '/')).join(', ')} ` +
    `et dans aucun octet chargé par l’écran client.`,
)
