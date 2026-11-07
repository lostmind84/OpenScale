import type { PrinterDiagnosisDTO } from './dto'

/**
 * L'état d'une étape du diagnostic pas à pas de l'imprimante (§14.4), côté écran.
 *
 * Les cinq valeurs sont celles que `printing.StepState` déclare en Go
 * (`internal/printing/diagnosis.go`) : ce fichier ne les réinvente pas, il leur donne
 * le mot français que l'écran et le rapport copiable montrent. `DiagnosisStepDTO.state`
 * (`dto.ts`) voyage en `string` plutôt qu'en `StepState` pour éviter une dépendance de
 * `dto.ts` vers ce fichier ; c'est ici, au moment d'indexer {@link STATE_WORDS}, que
 * l'état reprend ce type.
 */
export type StepState = 'pass' | 'fail' | 'unknown' | 'not_applicable' | 'unanswered'

/**
 * Le mot français de chaque état, celui que l'écran et le rapport copiable montrent.
 *
 * « sans objet » et « sans réponse » ne sont délibérément pas « échoué » (décision 2,
 * décision 3) : un silence ou une question encore ouverte ne sont pas une panne.
 */
export const STATE_WORDS: Record<StepState, string> = {
  pass: 'réussi',
  fail: 'échoué',
  unknown: 'inconnu',
  not_applicable: 'sans objet',
  unanswered: 'sans réponse',
}

/**
 * Le remède d'une étape tel que l'écran et le rapport le montrent.
 *
 * « À faire : » ne le précède que sur une étape en échec : le remède d'une étape
 * inconnue — le silence de l'étape 3 — dit qu'on continue, et l'annoncer comme une
 * action le ferait lire comme une panne à corriger (décision 2).
 *
 * @param state - l'état de l'étape, tel que le service l'a rendu.
 * @param remedy - le remède, non vide.
 */
export function remedyLine(state: string, remedy: string): string {
  return state === 'fail' ? `À faire : ${remedy}` : remedy
}

/**
 * Construit le texte brut que « Copier le rapport » dépose dans le presse-papiers.
 *
 * Texte brut, sans balise ni mise en forme (décision 4) : c'est ce qu'un bénévole colle
 * sur le forum des coopératives, et une étoile ou un chevron n'y survivrait pas intact.
 * Le rapport se construit même quand l'étape 5 est restée « sans réponse » (décision 10) :
 * rien ici n'attend que le bénévole ait regardé l'imprimante.
 *
 * @param d - le parcours tel que le service l'a rendu, étape 5 répondue ou non.
 */
export function diagnosisReport(d: PrinterDiagnosisDTO): string {
  const lines = ["Diagnostic de l'imprimante — OpenScale", '', `Imprimante : ${d.target}`]
  for (const step of d.steps) {
    const word = STATE_WORDS[step.state as StepState] ?? step.state
    lines.push('', `${String(step.number)}. ${step.title} : ${word}`)
    if (step.observed !== '') lines.push(`   Observé : ${step.observed}`)
    if (step.remedy !== '') lines.push(`   ${remedyLine(step.state, step.remedy)}`)
    if (step.doc !== '') lines.push(`   Documentation : ${step.doc}`)
  }
  return lines.join('\n')
}
