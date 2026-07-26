import type { LabelDTO } from './dto'

/**
 * Le peu de mise en forme qui reste du côté du navigateur.
 *
 * Le service envoie un jumeau `_text` de toute valeur qu'un humain lit — `net_text`,
 * `unit_price_text`, `amount_text` — précisément « pour qu'aucun front ne
 * réimplémente la virgule décimale française » (`internal/web/dto.go`). Il n'y a
 * donc ici ni conversion de centimes, ni conversion de grammes : seulement de la
 * COMPOSITION de chaînes déjà mises en forme.
 */

/**
 * Compose le prix d'une tuile : le montant servi et son suffixe.
 *
 * @param text - le prix déjà mis en forme, `unit_price_text`.
 * @param suffix - le suffixe servi avec le produit, espace initiale comprise.
 * @returns par exemple `5,32 €/kg`.
 */
export function unitPrice(text: string, suffix: string): string {
  return `${text}${suffix}`
}

/**
 * Compose la phrase de la barre de réimpression permanente.
 *
 * @param label - la dernière étiquette telle que le flux la donne.
 * @returns par exemple `ail 1,236 kg` ou `Œufs 3 unités`.
 * @example
 * lastLabelSummary({ product_name: 'ail', net_text: '1,236', mode: 'by_weight', … })
 */
export function lastLabelSummary(label: LabelDTO): string {
  if (label.mode === 'by_unit') {
    return `${label.product_name} ${label.quantity} ${label.quantity > 1 ? 'unités' : 'unité'}`
  }
  return `${label.product_name} ${label.net_text} kg`
}
