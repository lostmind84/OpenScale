import { frenchDateTime, frenchInteger } from './format'

/**
 * What a CAPPED list says of itself.
 *
 * A cap that does not say what it hides is a lie by omission: a screen reading « 20
 * anomalies » on a file carrying 116 makes whoever fixes the twenty believe the work
 * done. Every list of the Catalogue page and of the Matériel page goes through here, so
 * no two of them can announce their ceiling in two different sets of words.
 *
 * The sentences live in a module rather than in the pages because they are arithmetic and
 * grammar — a plural, a ceiling, a comparison — and both are testable with no DOM at all.
 */

/**
 * « 50 lignes affichées sur 116 anomalies. », or « 16 anomalies. » when nothing is hidden.
 *
 * The worst case this shape was written for is the field-by-field diff of the Station
 * page: a hundred and thirty rows, with the button that acts on them below the fold.
 *
 * @param shown - how many rows are drawn.
 * @param total - how many there are.
 * @param singular - what one of them is called.
 * @param plural - what several of them are called.
 */
export function tally(shown: number, total: number, singular: string, plural: string): string {
  const noun = total > 1 ? plural : singular
  if (shown >= total) return `${frenchInteger(total)} ${noun}.`
  return `${frenchInteger(shown)} lignes affichées sur ${frenchInteger(total)} ${noun}.`
}

/**
 * « 20 produits affichés sur 47 — précisez votre recherche. »
 *
 * The search is the one list whose ceiling is ACTED upon rather than merely read: what
 * fixes it is typing one more word, and the sentence says so. It used to be silent on
 * both counts — the search truncated at twenty without a word.
 *
 * @param shown - how many products the search draws.
 * @param found - how many it retained before the cap.
 */
export function matchTally(shown: number, found: number): string {
  if (found > shown) {
    return (
      `${frenchInteger(shown)} produits affichés sur ` +
      `${frenchInteger(found)} trouvés — précisez votre recherche.`
    )
  }
  return `${frenchInteger(found)} ${found > 1 ? 'produits trouvés' : 'produit trouvé'}.`
}

/**
 * « 7 imports affichés : le poste n'en publie jamais plus de vingt. »
 *
 * The ceiling is the STATION'S here and not the screen's, which is why the sentence names
 * it: nothing typed on this page will ever show a twenty-first import.
 *
 * @param count - how many imports the route served.
 */
export function importTally(count: number): string {
  return (
    `${frenchInteger(count)} ` +
    `${count > 1 ? 'imports affichés' : 'import affiché'} : ` +
    'le poste n’en publie jamais plus de vingt.'
  )
}

/**
 * Le total d'une liste, et son plafond quand elle en a un.
 *
 * The other shape a ceiling takes, and it is the Matériel page's: there, the total is the
 * point and the ceiling is a footnote to it. On the Catalogue page it is the other way
 * round, which is why {@link tally} leads with what is drawn.
 *
 * Aucune liste de la page Matériel n'est servie entière sans le dire : un poste peut
 * porter trente files d'impression — PDF, OneNote, télécopie — et une liste tronquée en
 * silence est une liste qui ment.
 *
 * @param singular - le nom au singulier, accord compris.
 * @param plural - le même au pluriel.
 * @param total - combien il y en a vraiment.
 * @param cap - combien de lignes sont affichées au plus.
 */
export function census(singular: string, plural: string, total: number, cap: number): string {
  const head = `${frenchInteger(total)} ${total > 1 ? plural : singular}`
  if (total <= cap) return head + '.'
  // « lignes » et non le nom compté : l'accord reste juste quel que soit ce qu'on liste.
  return `${head} — seules les ${frenchInteger(cap)} premières lignes sont affichées.`
}

/**
 * « 4 produits retirés depuis l'import du 24/07/2026 à 11:02. »
 *
 * The date is that of the last APPLIED import and never of the last one recorded: the
 * history carries the refused, the failed and the unchanged too, and dating a withdrawal
 * from a file the station discarded names an import that never served anything.
 *
 * @param count - how many products the last import withdrew.
 * @param previousImportAt - when the previous applied import happened, empty when there
 *   is none to date it from.
 */
export function withdrawnSentence(count: number, previousImportAt: string): string {
  const said = `${frenchInteger(count)} ${count > 1 ? 'produits retirés' : 'produit retiré'}`
  if (previousImportAt === '') return said + '.'
  return `${said} depuis l’import du ${frenchDateTime(previousImportAt)}.`
}
