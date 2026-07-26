/**
 * Le contrat JSON que le service sert, recopié champ pour champ.
 *
 * La source de vérité est `internal/web/dto.go` et `internal/web/catalog.go`, dont
 * un golden Go gèle la sortie. Ce fichier n'est que la même chose en TypeScript :
 * il ne décide de rien, et il n'ajoute aucun champ.
 *
 * Deux conventions y sont visibles et méritent d'être respectées d'ici :
 *
 *  - l'unité fait partie du nom (`_g`, `_ms`, `_cents`, `_count`) ;
 *  - un jumeau `_text` accompagne toute valeur qu'un humain LIT, « pour qu'aucun
 *    front ne réimplémente la virgule décimale française ». Quand un `_text`
 *    existe, l'écran l'affiche — il ne recalcule pas.
 */

/** Les 16 états de `domain.State`, tels que `State.String()` les épelle. */
export type StationState =
  | 'initializing'
  | 'idle'
  | 'product_armed'
  | 'weight_present'
  | 'weight_stable'
  | 'awaiting_stability'
  | 'entering_tare'
  | 'entering_weight'
  | 'manual_mode'
  | 'validating'
  | 'printing'
  | 'succeeded'
  | 'rejected'
  | 'faulted'
  | 'scale_lost'
  | 'out_of_service'

/** Ce que le bandeau haut dit du plateau. */
export interface WeightDTO {
  /** Distingue « aucune trame n'est jamais arrivée » de « le plateau lit zéro ». */
  available: boolean
  /** La mesure est plus vieille que la péremption DÉRIVÉE : l'écran masque le poids. */
  expired: boolean
  gross_g: number
  tare_g: number
  net_g: number
  quantity: number
  /** Le net en kilogrammes, virgule française, trois décimales. À afficher tel quel. */
  net_text: string
  stability: string
  /** Le figeage a tenu : un INDICATEUR, pas une condition d'impression (A3). */
  latched: boolean
  seq: number
  age_ms: number
  expiry_ms: number
}

/** La tuile sélectionnée, telle que le flux la renvoie. */
export interface SelectedProductDTO {
  id: string
  name: string
  category_code: string
  mode: string
  unit_price_cents: number
  unit_price_text: string
  price_suffix: string
  image_url: string
}

/** Une ligne de tarif d'une étiquette. */
export interface PriceDTO {
  code: string
  label: string
  abbrev: string
  unit_price_cents: number
  unit_price_text: string
  amount_cents: number
  amount_text: string
}

/** Ce que porte une étiquette, calculé une seule fois par le chemin unique. */
export interface LabelDTO {
  job_id: string
  barcode: string
  product_id: string
  product_name: string
  mode: string
  gross_g: number
  tare_g: number
  net_g: number
  net_text: string
  quantity: number
  prices: PriceDTO[]
  primary_code: string
  reference_code: string
}

/** L'état de la barre basse PERMANENTE (§8.5, §14.3). */
export interface ReprintDTO {
  available: boolean
  job_id: string
  printed_at: string
}

/** La ligne du bandeau. Le texte est FRANÇAIS et déjà interpolé. */
export interface MessageDTO {
  level: string
  code: string
  text: string
  expires_at: string
}

/** Un verdict de garde-fou. Tous voyagent ; la machine agit sur le premier bloquant. */
export interface DiagnosticDTO {
  code: string
  severity: string
  message: string
  blocking: boolean
  product_id: string
}

/** Ce que le poste sait de sa balance sans la lui demander. */
export interface ScaleDTO {
  connected: boolean
  median_ms: number
  observations_count: number
  provisional: boolean
  too_slow: boolean
}

/** La dernière chose que le superviseur a vue de l'imprimante. */
export interface PrinterDTO {
  health: string
  detail: string
  pending_jobs_count: number
  observed_at: string
}

/** Pourquoi le poste tourne en mode dégradé, et DEPUIS QUAND (§11.4). */
export interface DegradationDTO {
  since: string
  code: string
  reason: string
}

/** La charge de l'événement SSE nommé `state`, un par changement (ADR-014). */
export interface StateDTO {
  revision: number
  at: string
  state: StationState
  station: number
  weight: WeightDTO
  product: SelectedProductDTO | null
  label: LabelDTO | null
  last_label: LabelDTO | null
  reprint: ReprintDTO
  message: MessageDTO | null
  sound: string
  diagnostics: DiagnosticDTO[]
  fault_code: string
  arming_expires_at: string
  scale: ScaleDTO
  printer: PrinterDTO
  degraded: DegradationDTO | null
  /** Combien de tuiles la grille porte — les tuiles, elles, viennent du catalogue. */
  catalog_count: number
  unlogged_weighings_count: number
}

/** Le corps de `POST /api/v1/weigh`, envoyé sur un seul toucher de tuile. */
export interface WeighRequest {
  product_id: string
  tare_g: number
  units: number
  /** Chemin dégradé : une masse tapée à la main. Zéro = « lis le plateau ». */
  manual_weight_g: number
  /** Le poids brut que le client REGARDAIT en touchant. Le serveur le vérifie. */
  seen_weight_g: number
  measurement_seq: number
  /** Une clé d'idempotence par toucher : double-tap, rejeu, retry ⇒ une étiquette. */
  key: string
}
