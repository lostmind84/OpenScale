/**
 * What the Catalogue page says of what it has NOT read.
 *
 * Three values and not two, for the reason `Admin.load` gives about itself: an empty
 * array with no explanation reads as « there is none », which is false. A station with no
 * journal (ADR-013) answers 503 « ce poste n'a pas d'historique d'imports » to every read,
 * and four panels of that page used to answer it with « Aucune anomalie sur le dernier
 * import. » — permanently, and reassuringly.
 */
export type ReadState = 'loading' | 'read' | 'unread'

/**
 * What the three lists of findings say as long as they have not been read.
 *
 * @param state - where the read of the import history stands.
 */
export function findingsUnknownSentence(state: ReadState): string {
  if (state === 'loading') return 'Lecture des signalements du dernier import…'
  return (
    'Les signalements du dernier import n’ont pas pu être lus : cet écran ne sait pas ce ' +
    'qu’ils disent.'
  )
}

/**
 * What the history table says as long as it has not been read.
 *
 * @param state - where the read of the import history stands.
 */
export function historyUnknownSentence(state: ReadState): string {
  if (state === 'loading') return 'Lecture de l’historique des imports…'
  return 'L’historique des imports n’a pas pu être lu : cet écran ne sait pas ce qu’il contient.'
}

/**
 * The name of a product, or an honest sentence when the screen cannot give one.
 *
 * THREE cases and not two. `unread` used to answer like `read`, so a failed read of the
 * client catalog made a page state, of every decision in force, that its product had left
 * the catalog — having never managed to open the catalog. « Produit absent du catalogue »
 * is an ACCUSATION: it says the shop sells a product its own catalog does not know, and it
 * may only be said once the catalog has actually answered.
 *
 * The lookup is left to the caller — one screen holds a `Map`, the other a `Record` — and
 * so is the wording of the failed read: the Catalogue page and the Rules page do not say
 * it in the same words today, and this parameter is where that shows.
 *
 * @param name - what the catalog holds for that id, `undefined` when it holds nothing.
 * @param state - where the read of the client catalog stands.
 * @param unread - what to say when the catalog itself could not be read.
 */
export function productNameOf(
  name: string | undefined,
  state: ReadState,
  unread: string,
): string {
  if (name !== undefined) return name
  if (state === 'loading') return 'Lecture du nom…'
  if (state === 'unread') return unread
  return 'Produit absent du catalogue en service'
}
