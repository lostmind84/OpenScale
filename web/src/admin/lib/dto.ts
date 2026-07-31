import type { StateDTO } from '../../lib/dto'

/**
 * Le contrat JSON des routes d'administration, recopié champ pour champ.
 *
 * La source de vérité est `internal/web/health.go`, `internal/web/admin.go`,
 * `internal/web/config.go` et `internal/web/troubleshooting.go`. Ce fichier n'est que
 * la même chose en TypeScript : il ne décide de rien et il n'ajoute aucun champ.
 *
 * `StateDTO` est importé de l'écran client et non recopié : c'est le MÊME instantané
 * que le flux SSE diffuse, et deux définitions du même objet finiraient par diverger.
 * Un `import type` disparaît à la compilation : l'admin n'emporte pas un octet de
 * l'écran client pour cela.
 */

/** Ce qu'une action de dépannage répond : ce qui a été fait, en français. */
export interface ActionDTO {
  done: boolean
  message: string
}

/**
 * Ce que « Recharger le catalogue » répond, sur les deux routes qui y mènent.
 *
 * Un type à lui, et non un `ActionDTO` élargi : celui-là sert sept routes et son format
 * est figé. Et cette réponse-ci ne peut pas porter ce que tout le monde veut — l'import
 * est asynchrone par conception, la veille fait le travail sur son propre fil, et la
 * réponse est un 202. Ce qu'elle porte est donc de quoi RECONNAÎTRE l'issue quand elle
 * arrivera, par le sondage de trois secondes.
 */
export interface ReloadDTO {
  done: boolean
  /** Ce que le poste a VU, jamais une promesse de ce qu'il va faire. */
  message: string
  /** La ligne permanente du catalogue. Vide quand ce poste ne la publie pas. */
  watched: string
  /**
   * L'import en service à l'instant de l'appui, et 0 quand ce poste n'a pas de journal.
   *
   * C'est ce que l'écran compare au tableau de bord : un identifiant différent, trois
   * secondes plus tard, est l'issue qui arrive.
   */
  last_import_id: number
  last_import_at: string
}

/** Un refus de la couche HTTP. `code` est vide quand aucun ERR-xxx-nn n'est alloué. */
export interface ProblemDTO {
  code: string
  message: string
  faults?: FaultDTO[]
}

/** Un des 47 contrôles de configuration qui a échoué (§11.3). */
export interface FaultDTO {
  field: string
  message: string
  /** Les valeurs qui MARCHERAIENT, quand le contrôle les connaît. */
  allowed?: string[]
}

/** L'inventaire d'un import, tel que §14.4 le lit à voix haute. */
export interface ImportDTO {
  id: number
  occurred_at: string
  source: string
  file_name: string
  result: string
  code: string
  reason: string
  rows_read_count: number
  unreadable_rows_count: number
  weighable_count: number
  not_weighable_count: number
  anomalies_count: number
  unit_mismatches_count: number
  images_decoded_count: number
  images_rejected_count: number
  products_withdrawn_count: number
  duration_ms: number
}

/** Un motif de non-pesabilité et le nombre de lignes qui le partagent (§10.3). */
export interface MotiveDTO {
  code: string
  /** Le préfixe à quatre chiffres quand le motif en est un, sinon vide. */
  value: string
  count: number
}

/** La ligne permanente du catalogue : la source, le chemin ou l'URL, le compte. */
export interface CatalogSourceDTO {
  type: string
  label: string
}

/** Le compteur de rouleau de §8.5, tel que le feu « rouleau » le lit. */
export interface RollDTO {
  printed_count: number
  capacity_count: number
  /** Peut être NÉGATIF : un rouleau changé sans que personne l'ait dit (§8.5). */
  remaining_count: number
  level: string
  message: string
  known: boolean
}

/** La place qui reste là où le poste écrit, avec son seuil à côté (§10.4). */
export interface DiskDTO {
  path: string
  free_bytes: number
  total_bytes: number
  alert_mb: number
}

/** « Redémarrage sans intervention : OK / NON CONFIGURÉ » (bloquant-7). */
export interface RestartDTO {
  configured: boolean
  /** Faux quand la question n'a pas pu être posée : ce n'est pas « non configuré ». */
  known: boolean
  detail: string
  remedy: string
}

/** Sur quelle imprimante les étiquettes sortent (§8.4). */
export interface RoutingDTO {
  fallback_available: boolean
  on_fallback: boolean
  name: string
  banner: string
}

/** Une ligne du journal technique. */
export interface TechnicalLineDTO {
  id: number
  occurred_at: string
  level: string
  source: string
  code: string
  message: string
  detail: string
}

/** Une décision humaine en vigueur sur un produit (§10.6, ADR-017). */
export interface DecisionDTO {
  product_id: string
  offered: boolean
  min_weight_g: number | null
  reason: string
  decided_by: string
  decided_at: string
}

/** La charge de `GET /admin/api/health` : le tableau de bord de §14.4. */
export interface HealthDTO {
  version: string
  config_fingerprint: string
  station: number
  station_name: string
  coop: string
  alive: boolean
  state: StateDTO
  /** Le poste déclare-t-il une balance ? Sinon le feu s'ÉTEINT (§11.2). */
  scale_present: boolean
  /**
   * Les auto-tests de §8.6 que le driver EN SERVICE honore, par leur jeton de route.
   *
   * C'est là-dessus, et sur rien d'autre, que la page Matériel dessine ses boutons. Elle
   * portait la liste des trois elle-même : un poste sur `preview` proposait « Mire
   * d'alignement » et « Réglette millimétrée », qui répondaient un refus au clic. Un
   * bouton dont la seule réponse possible est un refus n'est pas un choix (ADR-025).
   *
   * Toujours une liste, jamais `null` — comme toute liste de §14.5. Vide quand le binaire
   * ne déclare aucun driver d'impression, ce qui n'arrive pas sur un poste en service.
   */
  printer_self_tests: string[]
  /**
   * Les transports d'octets que CE BINAIRE porte (§8.4), et pour chacun la clé de
   * `printer.options` dans laquelle il désigne son appareil.
   *
   * La page Matériel y prend les deux choses à la fois : ce que la liste déroulante
   * « Transport » propose, et **où le champ d'appareil en dessous écrit**. Elle n'avait ni
   * l'une ni l'autre — `transport` était un champ de texte libre, et l'unique champ
   * d'appareil était câblé sur `queue` quoi qu'on tape au-dessus. Un poste réglé sur `tcp`
   * enregistrait donc l'adresse de son imprimante dans `printer.options.queue` : rien ne
   * le refusait, aucun transport ne la lisait, et le poste n'imprimait pas.
   *
   * Toujours une liste, jamais `null`. Vide quand le binaire ne déclare aucun transport,
   * et la page retombe alors sur un champ libre plutôt que sur une liste sans valeur.
   */
  printer_transports: TransportDTO[]
  counters: {
    unlogged_weighings_count: number
    /** -1 quand ce poste n'a pas de journal (ADR-013). */
    journal_rows_count: number
  }
  events: TechnicalLineDTO[]
  catalog: ImportDTO | null
  /**
   * L'import dont les signalements DÉCRIVENT LE CATALOGUE EN SERVICE, et ce n'est pas
   * toujours celui du dessus. Zéro quand ce poste n'en a aucun.
   *
   * Un export redéposé à l'identique est enregistré « inchangé » et n'écrit aucun
   * signalement — ils appartiennent à l'import qui a produit la grille, une ligne plus
   * haut —, et un lot que la base a refusé n'en écrit pas davantage. Lire ceux de la
   * dernière ligne vidait les trois listes de la page Catalogue sur l'événement le plus
   * ordinaire qui soit, pendant que l'encadré du haut continuait d'annoncer seize
   * anomalies à corriger.
   */
  catalog_findings_id: number
  catalog_motives: MotiveDTO[]
  catalog_source: CatalogSourceDTO | null
  decisions: DecisionDTO[]
  roll: RollDTO | null
  disk: DiskDTO | null
  unattended_restart: RestartDTO | null
  printing: RoutingDTO | null
  /**
   * La version publiée plus récente que celle qui tourne, ou une chaîne vide.
   *
   * Elle voyage ici, dans la charge utile que le tableau de bord lit déjà, et non sur
   * une route à elle : la page bénévole s'ouvre sans mot de passe et n'appelle qu'une
   * route, ce qu'un test tient. Vide aussi quand le service n'a pas pu lire — la
   * pastille est une courtoisie, pas un diagnostic.
   */
  new_version: string
}

/** Une ligne de tarif d'une pesée journalisée. */
export interface JournalLineDTO {
  tier_code: string
  unit_price_cents: number
  amount_cents: number
}

/** Une pesée du journal (§14.4, page Journal). */
export interface WeighingDTO {
  id: number
  occurred_at: string
  station: number
  job_id: string
  product_id: string
  product_name: string
  reference: string
  mode: string
  gross_g: number
  tare_g: number
  net_g: number
  quantity: number
  barcode: string
  source: string
  stability: string
  rate_ms: number
  /** La trame brute, corpus vivant du driver de rejeu (§15.4). */
  frame: string
  result: string
  detail: string
  duration_ms: number
  lines: JournalLineDTO[]
}

/** Une ligne qu'un import a eu quelque chose à dire sur (§10.3 bis). */
export interface FindingDTO {
  csv_line: number
  product_id: string
  /**
   * Le nom commercial TEL QUE CET IMPORT L'A LU, instantané d'affichage.
   *
   * Vide dans les deux cas où le fichier n'en donne pas : un signalement qui ne porte sur
   * aucun produit, et une ligne trop abîmée pour porter un nom.
   */
  product_name: string
  code: string
  issue: string
  message: string
  value: string
}

/** Un port série énuméré, avec la description USB qui le rend reconnaissable. */
export interface PortDTO {
  name: string
  description: string
  vid: string
  pid: string
}

/** Un transport d'octets, et l'endroit où le choisir fait écrire (§8.4). */
export interface TransportDTO {
  /** La valeur qui va dans `printer.options.transport` : `winspool`, `tcp`… */
  id: string
  /** Le libellé français de la liste déroulante. Il vient du poste, jamais de l'écran. */
  label: string
  /** La clé de `printer.options` par laquelle ce transport désigne son appareil. */
  key: string
}

/** Une destination d'impression que la plateforme connaît : file, nœud ou hôte. */
export interface PrinterDeviceDTO {
  name: string
  /**
   * La clé de `printer.options` où cette destination va : `queue`, `path` ou `address`.
   *
   * C'est l'énumération qui le dit, parce qu'elle seule le sait : les deux routes servent
   * la même liste à l'écran et « 192.168.0.43:9100 » ne ressemble pas moins à un nom de
   * file que « SATO WS408_2 ».
   */
  key: string
  detail: string
  default: boolean
}

/** Ce qu'un port a répondu quand on lui a appliqué les parseurs (§14.4). */
export interface DetectionDTO {
  port: string
  driver: string
  valid_frames_count: number
  frames: string[]
  message: string
}

/** Une version restaurable du fichier de configuration (§11.4, cinq d'entre elles). */
export interface ConfigVersionDTO {
  version: number
  modified_at: string
  config_fingerprint: string
}

/** Le compte à rebours de 60 s de §11.4. */
export interface ConfirmationDTO {
  changed_blocks: string[]
  confirm_before: string
  seconds_left: number
}

/** `GET /admin/api/config` : le document, sans ses deux secrets. */
export interface ConfigDTO {
  config: Record<string, unknown>
  config_fingerprint: string
  retired_keys: string[]
  pending_confirmation: ConfirmationDTO | null
}

/** Ce qu'une session ouverte répond. */
export interface SessionDTO {
  expires_at: string
  session_minutes: number
  /**
   * Absent d'ordinaire. Posé par une réinitialisation qui n'a pas pu écrire le
   * nouveau mot de passe sur le disque — le fichier porte encore une clé que ce
   * binaire refuse (ADR-034) — pour dire que la session s'ouvre quand même, mais que
   * ce mot de passe ne survivra pas à un redémarrage tant que le fichier n'est pas
   * corrigé. Français : c'est la phrase lue telle quelle.
   */
  warning?: string
}

/**
 * Les quatre issues d'une bascule, telles qu'`update.ps1` les écrit.
 *
 * Quatre et non deux : « annulée, le poste fonctionne » n'appelle personne, « le poste ne
 * répond pas » demande quelqu'un tout de suite, et « rien n'a été remplacé » veut dire
 * qu'on peut recommencer.
 */
export type UpdateStatus =
  | 'succeeded'
  | 'rolled-back'
  | 'rolled-back-unhealthy'
  | 'not-started'

/** La dernière bascule tentée par ce poste. */
export interface UpdateOutcomeDTO {
  status: UpdateStatus
  from: string
  to: string
  reason: string
  finished_at: string
}

/** Ce que la page Mise à jour dessine. */
export interface UpdateDTO {
  /** La version qui tourne en ce moment. */
  running: string
  /** Le dépôt suivi, sous la forme propriétaire/projet. */
  repository: string
  /** Faux là où la bascule n'existe pas : la page le dit au lieu d'un bouton mort. */
  supported: boolean
  /** Vrai quand la version publiée est plus récente que celle qui tourne. */
  available: boolean
  latest: string
  published_at: string
  html_url: string
  checked_at: string
  /**
   * `null` et non un objet vide : « aucune bascule n'a jamais été tentée » et « une
   * bascule a été tentée et n'a rien fait » sont deux phrases différentes à l'écran.
   */
  outcome: UpdateOutcomeDTO | null
}
