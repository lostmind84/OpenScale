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
 * Met en forme l'instant où le catalogue est entré en service.
 *
 * Écrit à la main plutôt que par `Intl.DateTimeFormat` : ce poste affiche cette
 * ligne en permanence, et la sortie d'`Intl` dépend de la locale du système, qui
 * n'est pas un réglage du poste. Un poste installé avec un Windows anglais
 * afficherait `7/27/2026` sur un écran par ailleurs entièrement français.
 *
 * L'instant est rendu dans le fuseau du poste : c'est l'heure qu'il est dans le
 * magasin, la seule qu'un bénévole puisse comparer à sa montre.
 *
 * @param iso - `updated_at` du catalogue, RFC 3339, ou une chaîne vide.
 * @returns `27/07/2026 08:06:48`, ou une chaîne vide si rien n'est daté.
 * @example
 * catalogStamp('2026-07-27T08:06:48Z') // '27/07/2026 10:06:48' à Paris en été
 */
export function catalogStamp(iso: string): string {
  if (iso === '') return ''
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''
  const pad = (n: number): string => String(n).padStart(2, '0')
  const day = `${pad(at.getDate())}/${pad(at.getMonth() + 1)}/${at.getFullYear()}`
  return `${day} ${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`
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
