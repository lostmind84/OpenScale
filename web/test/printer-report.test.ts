import { describe, expect, it } from 'vitest'
import { diagnosisReport } from '../src/admin/lib/printer-report'
import type { DiagnosisStepDTO, PrinterDiagnosisDTO } from '../src/admin/lib/dto'

/**
 * Le texte copiable du diagnostic de l'imprimante (§14.4, décision 4).
 *
 * Il doit rester lisible une fois collé sur le forum des coopératives — texte brut,
 * sans balise ni mise en forme — et il doit se construire même quand l'étape 5 est
 * restée sans réponse (décision 10) : un bénévole qui copie avant d'avoir regardé
 * l'imprimante ne doit pas se voir refuser son rapport.
 */

/** Un pas, avec le peu que ce test a besoin de préciser par-dessus les valeurs vides. */
function step(
  partial: Partial<DiagnosisStepDTO> & Pick<DiagnosisStepDTO, 'number' | 'title' | 'state'>,
): DiagnosisStepDTO {
  return { observed: '', remedy: '', doc: '', ...partial }
}

/** Le parcours du rapport de terrain : rien n'est sorti, l'étape 3 reste muette (décision 2). */
const RUN: PrinterDiagnosisDTO = {
  target: "nœud d'impression /dev/usb/lp0",
  steps: [
    step({
      number: 1,
      title: 'Le périphérique existe',
      state: 'pass',
      observed: '« /dev/usb/lp0 » existe.',
    }),
    step({ number: 2, title: 'Le poste a le droit d’y écrire', state: 'pass' }),
    step({
      number: 3,
      title: "L'imprimante répond quand on lui demande son état",
      state: 'unknown',
    }),
    step({ number: 4, title: 'Une étiquette de test est envoyée en entier', state: 'pass' }),
    step({ number: 5, title: 'L’étiquette est-elle sortie ?', state: 'unanswered' }),
  ],
}

describe('diagnosisReport', () => {
  it('commence par le titre, et nomme l’imprimante', () => {
    const report = diagnosisReport(RUN)
    expect(report.startsWith("Diagnostic de l'imprimante — OpenScale")).toBe(true)
    expect(report).toContain("Imprimante : nœud d'impression /dev/usb/lp0")
  })

  it('porte chaque étape, avec son mot français exact', () => {
    const report = diagnosisReport(RUN)
    expect(report).toContain("3. L'imprimante répond quand on lui demande son état : inconnu")
    expect(report).toContain('5. L’étiquette est-elle sortie ? : sans réponse')
  })

  it('ne dit jamais « échoué » quand aucune étape n’a échoué', () => {
    expect(diagnosisReport(RUN)).not.toContain('échoué')
  })

  it('ajoute le remède et la documentation d’une étape en échec', () => {
    const failing: PrinterDiagnosisDTO = {
      target: "nœud d'impression /dev/usb/lp0",
      steps: [
        step({
          number: 1,
          title: 'Le périphérique existe',
          state: 'fail',
          observed: "« /dev/usb/lp0 » n'existe pas sur ce poste.",
          remedy: 'Vérifier que le câble USB est bien branché.',
          doc: 'https://lostmind84.github.io/OpenScale/imprimante/#etape-1',
        }),
        step({ number: 2, title: 'Le poste a le droit d’y écrire', state: 'not_applicable' }),
        step({
          number: 3,
          title: "L'imprimante répond quand on lui demande son état",
          state: 'not_applicable',
        }),
        step({
          number: 4,
          title: 'Une étiquette de test est envoyée en entier',
          state: 'not_applicable',
        }),
        step({ number: 5, title: 'L’étiquette est-elle sortie ?', state: 'not_applicable' }),
      ],
    }
    const report = diagnosisReport(failing)
    expect(report).toContain('   À faire : Vérifier que le câble USB est bien branché.')
    expect(report).toContain(
      '   Documentation : https://lostmind84.github.io/OpenScale/imprimante/#etape-1',
    )
  })

  it('reste du texte brut : aucune balise, aucune astérisque de mise en forme', () => {
    const report = diagnosisReport(RUN)
    expect(report).not.toContain('<')
    expect(report).not.toContain('*')
  })
})
