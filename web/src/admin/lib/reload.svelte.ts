import type { HealthDTO, ReloadDTO } from './dto'
import { importOutcomeSentence, noImportSentence, noJournalSentence } from './inventory'

/**
 * L'attente de l'issue d'un « Recharger le catalogue ».
 *
 * L'import est asynchrone par conception : la route répond un 202 et c'est la veille, sur
 * son propre fil, qui lit le fichier, le qualifie, l'applique et l'acquitte. La réponse
 * HTTP ne peut donc pas porter l'issue, et rendre la route synchrone reconstruirait le
 * second chemin d'import que le poste s'interdit.
 *
 * Le seul canal dont l'administration dispose est le sondage du tableau de bord, toutes
 * les trois secondes, et l'identifiant de l'import en service est ce qui dit que l'attente
 * est finie. Ce mécanisme existait déjà sur la page Catalogue ; il vit ici pour que les
 * DEUX portes de cet acte en rendent exactement la même phrase.
 */

/** Ce que le service écrit dans `journal_rows_count` quand ce poste n'a pas de journal. */
const NO_JOURNAL = -1

/**
 * Combien de sondages l'écran attend avant de dire ce qu'il SAIT.
 *
 * Dix tours de trois secondes font trente secondes, plus du double du pire cas de la
 * veille elle-même : cinq secondes de balayage, deux observations identiques avant de
 * lire, puis la lecture. Un plafond plus court dirait « rien ne s'est passé » pendant que
 * l'import tourne, ce qui est exactement le mensonge que ce travail supprime.
 */
const WAITED_POLLS = 10

/** L'attente d'une relecture, du moment de l'appui à la phrase qui la conclut. */
export class ReloadWatch {
  /** La phrase à l'écran, vide tant qu'il n'y a rien à dire. */
  sentence = $state('')

  /** L'import en service au moment de l'appui : l'écran attend qu'il change. */
  #baseline = 0
  /** Ce que le poste surveille, tel que la réponse l'a nommé. */
  #watched = ''
  /** Combien de sondages il reste à attendre. Zéro veut dire « on n'attend plus ». */
  #polls = 0

  /**
   * Arme l'attente sur ce que la route vient de répondre.
   *
   * @param answer - la réponse de « Recharger le catalogue ».
   */
  begin(answer: ReloadDTO): void {
    this.sentence = ''
    this.#baseline = answer.last_import_id
    this.#watched = answer.watched
    this.#polls = WAITED_POLLS
  }

  /** Oublie l'attente : un autre acte a pris l'écran, et sa phrase avec. */
  forget(): void {
    this.sentence = ''
    this.#polls = 0
  }

  /**
   * Confronte l'attente au tableau de bord que le sondage vient de relire.
   *
   * @param health - le tableau de bord, tel qu'il vient d'être lu.
   */
  observe(health: HealthDTO): void {
    if (this.#polls === 0) return
    if (health.counters.journal_rows_count === NO_JOURNAL) {
      this.#settle(noJournalSentence(this.#watched))
      return
    }
    const record = health.catalog
    if (record !== null && record.id !== this.#baseline) {
      this.#settle(importOutcomeSentence(record, health.catalog_motives))
      return
    }
    this.#polls -= 1
    if (this.#polls === 0) this.sentence = noImportSentence(this.#watched)
  }

  /** Pose la phrase qui conclut, et cesse d'attendre. */
  #settle(sentence: string): void {
    this.sentence = sentence
    this.#polls = 0
  }
}
