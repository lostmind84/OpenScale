import type { DecisionDTO } from './dto'
import { frenchInteger } from './format'

/**
 * What a human decided about ONE product, and what the station will accept.
 *
 * Two columns of one row of `local_decisions`, and the whole point of this module is that
 * they stay two: withdrawing a product and letting it weigh less are separate acts, and a
 * single call carrying both erased a waiver every time somebody withdrew a product.
 *
 * The rules live here rather than in the page because they are the answer to « will the
 * station take this? », which is worth knowing BEFORE arming a button — and worth testing
 * without a browser.
 */

/**
 * The form as the screen holds it, before anything travels.
 *
 * `typedWaiver` is the FIGURE and not the text: `Number('')` is 0 and `Number('abc')` is
 * NaN, which `JSON.stringify` turns into `null` — an unusable field would silently write
 * « this product may weigh 0 g » or silently drop a waiver somebody meant to grant.
 */
export interface DecisionForm {
  /** The motive as it will TRAVEL: trimmed, because a space is not an explanation. */
  motive: string
  /** Whether the product is offered today, read from the decision IN FORCE. */
  offeredInForce: boolean
  /** The waiver the product already carries, read from the decision in force. */
  waiverInForce: number | null
  /** The waiver typed in the form, `null` when the field is empty or unreadable. */
  typedWaiver: number | null
}

/** Whether each of the four acts of a decision can travel as things stand. */
export interface DecisionActs {
  canWithdraw: boolean
  canOfferAgain: boolean
  canSaveWaiver: boolean
  canDropWaiver: boolean
}

/**
 * Whether an act writing these two columns needs a motive to be ACCEPTED.
 *
 * The station requires one for every decision it WRITES. The single act exempt is the one
 * that writes nothing: offered again AND no waiver ERASES the row (`ClearDecision`,
 * internal/web/admin.go), and a row that no longer exists needs no explaining.
 *
 * @param offered - whether the product would go back into the grid.
 * @param grams - the waiver that would travel with it.
 */
export function needsMotive(offered: boolean, grams: number | null): boolean {
  return !(offered && grams === null)
}

/**
 * The waiver a field holds, or null when it holds nothing usable.
 *
 * @param typed - what was typed in the grams field.
 */
export function waiverTyped(typed: string): number | null {
  if (typed.trim() === '') return null
  return Number(typed)
}

/**
 * Whether a typed waiver can travel.
 *
 * @param grams - the figure the field gave, or null.
 */
export function waiverIsUsable(grams: number | null): boolean {
  return grams !== null && Number.isFinite(grams) && grams > 0
}

/**
 * Which of the four acts the station would accept, as the form stands.
 *
 * The motive is a CONDITION and not a comment, and the field used to present it as one:
 * the station answers 422 « Indiquez le motif de cette décision. » to every decision it
 * writes. Arming a button it is certain to refuse — after asking for the password, which
 * is worse — is a promise the screen cannot keep.
 *
 * @param form - the decision in force and what the form holds.
 */
export function decisionActs(form: DecisionForm): DecisionActs {
  const explained = form.motive !== ''
  return {
    canWithdraw: explained || !needsMotive(false, form.waiverInForce),
    canOfferAgain: explained || !needsMotive(true, form.waiverInForce),
    canSaveWaiver:
      waiverIsUsable(form.typedWaiver) &&
      (explained || !needsMotive(form.offeredInForce, form.typedWaiver)),
    canDropWaiver: explained || !needsMotive(form.offeredInForce, null),
  }
}

/**
 * What one decision in force says, in one French line.
 *
 * @param decision - the row as the station serves it.
 */
export function decisionSentence(decision: DecisionDTO): string {
  const parts: string[] = []
  if (!decision.offered) parts.push('retiré de la grille')
  if (decision.min_weight_g !== null) {
    parts.push(`peut peser à partir de ${frenchInteger(decision.min_weight_g)} g`)
  }
  return parts.length === 0 ? 'aucune restriction' : parts.join(' · ')
}
