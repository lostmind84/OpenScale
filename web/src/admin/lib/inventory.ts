import type { ImportDTO, MotiveDTO } from './dto'
import { frenchDate } from './format'

/**
 * L'inventaire du catalogue, écrit MOT POUR MOT comme §14.4 le donne.
 *
 * C'est la phrase qui décide si un bénévole s'inquiète ou non, et elle est ici pour
 * être testable seule : les chiffres viennent de l'API, jamais d'un gabarit, et cette
 * fonction ne fait que les mettre dans les mots du document.
 *
 * ```
 * Catalogue du 24/07/2026 — 355 produits reçus
 *   331 pesables            (181 avec photo, 174 sans)
 *     8 non pesables        préemballés (7), code interne 0490 (1)
 *    16 anomalies           à corriger dans Odoo         [voir les 16 lignes]
 *   + 1 unité divergente    pesable, unité à corriger    [voir la ligne]
 * ```
 *
 * **Jamais « 46 produits en erreur ».** C'est faux — un boulgour préemballé n'est pas
 * en erreur, il ne relève pas de la balance —, c'est alarmant sans action possible pour
 * 39 de ces 46 lignes, et cela noie le seul chiffre qui doit attirer l'œil : les lignes
 * réparables. D'où quatre lignes distinctes et aucun total.
 */

/** Une ligne de l'inventaire : un chiffre, ce qu'il compte, et sa note. */
export interface InventoryLine {
  /** Le chiffre tel qu'il s'écrit, « 331 » ou « + 1 ». */
  count: string
  /** Ce que le chiffre compte : « pesables », « non pesables », « anomalies ». */
  label: string
  /** La précision qui suit, vide quand la donnée ne la porte pas. */
  note: string
  /** Le libellé du lien vers les lignes concernées, vide quand il n'y a rien à voir. */
  link: string
}

/** L'inventaire complet d'un import. */
export interface Inventory {
  /** « Catalogue du 24/07/2026 — 355 produits reçus ». */
  headline: string
  lines: InventoryLine[]
  /**
   * La même chose sur UNE ligne, telle que §14.4 l'écrit pour `flv_1.csv` :
   * « 153 reçus · 107 pesables · 39 non pesables · 7 anomalies · 5 unités divergentes ».
   */
  oneLine: string
}

/**
 * Compose l'inventaire d'un import à partir des chiffres servis par l'API.
 *
 * @param record - l'import, tel que `GET /admin/api/health` le donne.
 * @param motives - la répartition des non-pesables par motif, ou une liste vide.
 * @returns les lignes de §14.4, dans l'ordre du document.
 */
export function inventoryOf(record: ImportDTO, motives: MotiveDTO[]): Inventory {
  const received = record.rows_read_count
  const withPhoto = record.images_decoded_count
  // « 181 avec photo, 174 sans » porte sur les 355 REÇUS, et 181 + 174 = 355 le
  // confirme : §14.2 le dit dans les mêmes termes (« 181 produits sur 355 »). Accrocher
  // la parenthèse aux 331 pesables afficherait un nombre qui n'existe pas.
  const withoutPhoto = received - withPhoto

  const lines: InventoryLine[] = [
    {
      count: integer(record.weighable_count),
      label: 'pesables',
      note: `(${integer(withPhoto)} avec photo, ${integer(withoutPhoto)} sans)`,
      link: '',
    },
    {
      count: integer(record.not_weighable_count),
      label: 'non pesables',
      note: motivesSentence(motives),
      link: '',
    },
  ]
  if (record.anomalies_count > 0) {
    lines.push({
      count: integer(record.anomalies_count),
      label: 'anomalies',
      note: 'à corriger dans Odoo',
      link: rowLink(record.anomalies_count),
    })
  }
  if (record.unit_mismatches_count > 0) {
    const divergent = record.unit_mismatches_count
    lines.push({
      count: '+ ' + integer(divergent),
      label: divergent === 1 ? 'unité divergente' : 'unités divergentes',
      note: 'pesable, unité à corriger',
      link: rowLink(divergent),
    })
  }

  return {
    headline: `Catalogue du ${frenchDate(record.occurred_at)} — ${integer(received)} produits reçus`,
    lines,
    oneLine: oneLineOf(record),
  }
}

/**
 * La phrase d'une ligne, celle que §14.4 écrit pour `flv_1.csv`.
 *
 * Elle existe parce que le tableau de bord annonce « l'inventaire du dernier import, en
 * une ligne » : le bloc aligné est la forme longue, celle-ci la forme courte, et les
 * deux sortent des mêmes chiffres pour ne jamais pouvoir se contredire.
 */
function oneLineOf(record: ImportDTO): string {
  const parts = [
    `${integer(record.rows_read_count)} reçus`,
    `${integer(record.weighable_count)} pesables`,
    `${integer(record.not_weighable_count)} non pesables`,
    `${integer(record.anomalies_count)} anomalies`,
  ]
  const divergent = record.unit_mismatches_count
  if (divergent > 0) {
    parts.push(`${integer(divergent)} ${divergent === 1 ? 'unité divergente' : 'unités divergentes'}`)
  }
  return parts.join(' · ')
}

/** Le libellé du lien vers les lignes concernées, au singulier comme au pluriel. */
function rowLink(count: number): string {
  return count === 1 ? 'voir la ligne' : `voir les ${integer(count)} lignes`
}

/**
 * La répartition des non-pesables : « préemballés (7), code interne 0490 (1) ».
 *
 * Une liste vide rend une note VIDE, et c'est voulu : un poste dont le dernier import
 * n'a pas de signalement enregistré n'a pas de répartition, et inventer « préemballés »
 * pour meubler la ligne serait inventer un chiffre.
 */
function motivesSentence(motives: MotiveDTO[]): string {
  return motives
    .map((motive) => `${motiveLabel(motive)} (${integer(motive.count)})`)
    .join(', ')
}

/**
 * Le mot français d'un motif de non-pesabilité (§10.3).
 *
 * « code interne 0490 » et non « code interne » : le préfixe EST le numéro que
 * quelqu'un doit corriger dans Odoo, et c'est la forme que §14.4 cite.
 */
function motiveLabel(motive: MotiveDTO): string {
  switch (motive.code) {
    case 'PREPACKAGED_PRODUCT':
      return 'préemballés'
    case 'INTERNAL_CODE_NOT_WEIGHABLE':
      return motive.value === '' ? 'code interne' : 'code interne ' + motive.value
    case 'NO_BARCODE':
      return 'sans code-barres'
    default:
      // Un motif que ce front ne connaît pas encore s'affiche sous son code plutôt que
      // de disparaître : un chiffre manquant dans la répartition est un chiffre faux.
      return motive.code
  }
}

/** Un entier dans la forme où il se lit sur cet écran. */
function integer(value: number): string {
  return String(value)
}
