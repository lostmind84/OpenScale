/**
 * L'aperçu du diff champ par champ que §14.4 demande à l'import de configuration.
 *
 * Il est ici, en fonction pure, pour deux raisons. Un import n'applique RIEN : il montre ce
 * qui changerait, et c'est l'enregistrement, geste distinct, qui applique — un fichier qui
 * s'appliquerait tout seul serait un poste reconfiguré par un double-clic. Et la comparaison
 * porte sur des CHEMINS de clés et non sur des blocs : « le bloc printer a changé » ne dit
 * pas si c'est la file d'impression ou le noircissement, et c'est exactement la différence
 * entre relire un diff et le signer les yeux fermés.
 */

/** Un champ qui change, et ses deux valeurs telles qu'elles se lisent. */
export interface Difference {
  /** Le chemin pointé de la clé, « printer.options.queue ». */
  path: string
  before: string
  after: string
}

/**
 * Compare deux documents de configuration, feuille par feuille.
 *
 * @param current - la configuration en service.
 * @param candidate - celle que le fichier importé décrit.
 * @returns un champ par différence, dans l'ordre des clés du document en service puis des
 * clés que seul le fichier importé porte.
 */
export function differences(
  current: Record<string, unknown>,
  candidate: Record<string, unknown>,
): Difference[] {
  const found: Difference[] = []
  for (const path of [...leaves(current), ...leaves(candidate)]) {
    if (found.some((entry) => entry.path === path)) continue
    const before = render(valueAt(current, path))
    const after = render(valueAt(candidate, path))
    if (before !== after) found.push({ path, before, after })
  }
  return found
}

/**
 * Lit une valeur par son chemin pointé.
 *
 * @param document - le document à lire.
 * @param path - le chemin de la clé.
 */
export function valueAt(document: Record<string, unknown>, path: string): unknown {
  let node: unknown = document
  for (const key of path.split('.')) {
    if (node === null || typeof node !== 'object') return undefined
    node = (node as Record<string, unknown>)[key]
  }
  return node
}

/**
 * Les chemins de toutes les feuilles d'un document.
 *
 * Un tableau est une FEUILLE et non un sous-arbre : la grille de tarifs, les catégories et
 * les seuils se lisent en entier ou pas du tout, et un diff qui annoncerait
 * « pricing.tiers.2.discount_percent » à un bénévole nommerait un indice que personne ne voit à
 * l'écran.
 */
function leaves(node: unknown, prefix = ''): string[] {
  if (node === null || typeof node !== 'object' || Array.isArray(node)) {
    return prefix === '' ? [] : [prefix]
  }
  const paths: string[] = []
  for (const [key, value] of Object.entries(node)) {
    paths.push(...leaves(value, prefix === '' ? key : `${prefix}.${key}`))
  }
  return paths
}

/** Une valeur telle qu'elle se lit dans un diff : jamais « [object Object] ». */
function render(value: unknown): string {
  if (value === undefined) return '—'
  if (value === null) return 'vide'
  if (typeof value === 'boolean') return value ? 'oui' : 'non'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
