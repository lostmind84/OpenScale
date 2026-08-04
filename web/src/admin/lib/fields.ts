/**
 * The French name of every configuration key.
 *
 * It exists because the station's refusals are NOT self-carrying: the service answers a
 * key plus a message, and « attendu : nombre entier » names nothing on its own. As long
 * as the screen displayed the key, the sentence was enough by itself; once the key is
 * hidden, something else has to take its place.
 *
 * This is the ONLY source of those names: the pages read it to draw their fields, and the
 * refusal bar reads it to name what the station turned down. A test checks that every
 * path edited by a page appears here.
 *
 * A label here is longer than the one a page puts under its own heading: « Port série »
 * reads fine beneath the « Balance » panel, but the refusal bar has no panel above it —
 * there, the name has to be enough on its own.
 */
export const FIELD_LABELS: Readonly<Record<string, string>> = {
  'station.number': 'Numéro du poste',
  'station.name': 'Nom du poste',
  'station.coop': 'Nom de la coopérative',

  'network.listen': 'Adresse d’écoute',
  'network.admin_on_lan': 'Administration accessible depuis le réseau',

  'ui.language': 'Langue',
  'ui.sound': 'Son',
  'ui.idle_timeout_s': 'Retour à l’accueil après (secondes)',
  'ui.reprint_window_s': 'Réimpression possible pendant (secondes)',
  'ui.show_grid_prices': 'Afficher les prix sur les tuiles',
  'ui.show_by_unit_products': 'Afficher les produits vendus à l’unité',
  'ui.grid_columns': 'Colonnes de la grille',
  'ui.min_products_for_chip': 'Articles minimum pour afficher une catégorie',

  'scale.type': 'Protocole de la balance',
  'scale.present': 'Ce poste a une balance',
  'scale.manual_entry_allowed': 'Saisie manuelle autorisée',
  'scale.degrade_after_s': 'Passer en mode dégradé après (secondes)',
  'scale.options.port': 'Port série',
  'scale.options.baud': 'Vitesse (bauds)',
  'scale.options.bits': 'Bits de données',
  'scale.options.parity': 'Parité',
  'scale.options.stop': 'Bits d’arrêt',
  'scale.options.backoff_min_ms': 'Attente minimale avant réessai (ms)',
  'scale.options.backoff_max_ms': 'Attente maximale avant réessai (ms)',

  'printer.type': 'Pilote d’impression',
  'printer.template': 'Gabarit d’étiquette',
  'printer.options.transport': 'Transport',
  'printer.options.queue': 'File d’impression',
  'printer.options.path': 'Fichier de périphérique',
  'printer.options.address': 'Adresse réseau',
  'printer.options.darkness': 'Noircissement',
  'printer.options.speed': 'Vitesse d’impression',
  'printer.options.offset_x': 'Décalage horizontal (dots)',
  'printer.options.offset_y': 'Décalage vertical (dots)',
  'printer.options.invert_bits': 'Inverser les points',
  'printer.options.copies': 'Exemplaires',
  'printer.options.roll_capacity': 'Étiquettes par rouleau',

  'pricing.amount_rounding': 'Arrondi du montant',
  'pricing.unit_price_rounding': 'Arrondi du prix au kilo',
  'pricing.primary_code': 'Tarif principal',
  'pricing.reference_code': 'Tarif de référence',

  'barcode.verify_reference_check_digit': 'Vérifier la clé de contrôle',

  'limits.empty_max_g': 'Plateau considéré vide en dessous de (g)',
  'limits.basket_check_enabled': 'Vérifier la présence du panier',
  'limits.basket_min_g': 'Poids du panier, borne basse (g)',
  'limits.basket_max_g': 'Poids du panier, borne haute (g)',
  'limits.min_weight_g': 'Poids minimum accepté',
  'limits.max_weight_g': 'Poids maximum accepté',
  'limits.max_tare_g': 'Tare maximale',
  'limits.min_units': 'Unités minimum',
  'limits.max_units': 'Unités maximum',
  'limits.max_amount_cents': 'Montant maximum (centimes)',

  'stability.mode': 'Exigence de stabilité',
  'stability.min_duration_ms': 'Durée de stabilité (ms)',
  'stability.tolerance_g': 'Tolérance de stabilité (g)',
  'stability.timeout_ms': 'Délai d’attente de stabilité (ms)',
  'stability.on_timeout': 'Au bout du délai',
  'stability.min_latch_rate': 'Taux d’accroche minimal',
  'stability.latch_rate_window_ms': 'Fenêtre de mesure du taux (ms)',
  'stability.expiry_floor_ms': 'Péremption, plancher (ms)',
  'stability.expiry_ceiling_ms': 'Péremption, plafond (ms)',
  'stability.expiry_factor': 'Péremption, facteur',

  'catalog.type': 'Où le poste va chercher le catalogue',
  'catalog.options.directory': 'Répertoire surveillé',
  'catalog.options.url': 'Adresse du serveur',
  'catalog.options.username': 'Compte',
  'catalog.options.password': 'Mot de passe',
  'catalog.options.separator': 'Séparateur du CSV',
  'catalog.options.poll_interval_s': 'Vérifier toutes les (secondes)',
  'catalog.options.stable_polls': 'Vérifications avant lecture',
  'catalog.options.max_file_size_mb': 'Taille maximale du fichier (Mo)',
  'catalog.options.max_image_size_kb': 'Taille maximale d’une image (Ko)',
  'catalog.options.min_readable_ratio': 'Part minimale de lignes lisibles',
  'catalog.options.max_weighable_drop': 'Baisse maximale des produits pesables',
  'catalog.options.max_archives': 'Archives conservées',
  'catalog.options.archive_days': 'Archives conservées (jours)',
  'catalog.options.failures_before_reject': 'Échecs avant mise en quarantaine',
  'catalog.images.source': 'Origine des photos',
  'catalog.images.path': 'Répertoire des photos',
  'catalog.fallback_category': 'Rayon par défaut',

  'journal.max_rows': 'Pesées conservées',
  'journal.max_days': 'Pesées conservées (jours)',
  'journal.max_technical': 'Événements techniques conservés',

  'admin.session_minutes': 'Durée d’une session (minutes)',
  'admin.attempts_per_minute': 'Tentatives par minute',

  'maintenance.weekly_integrity_check': 'Contrôle d’intégrité hebdomadaire',
  'maintenance.disk_alert_mb': 'Alerte disque en dessous de (Mo)',
}

/**
 * The French name of a key, or the key itself.
 *
 * The fallback is not a stopgap: a refusal coming from a check that no page edits must
 * stay readable to someone on the telephone, rather than vanish altogether.
 *
 * @param path - the dotted path of the key, as the service names it.
 */
export function labelOf(path: string): string {
  return FIELD_LABELS[path] ?? path
}

/**
 * The French name of every BLOCK of the configuration.
 *
 * It exists for the same reason as the field index, one notch above it: the confirmation
 * banner of §11.4 lists the blocks that moved, and the station names them in English —
 * `changedBlocks` in `internal/web/config.go` answers `scale`, `printer`, `catalog`.
 * Those are exactly the tokens the « Montrer les noms techniques » switch has to hide,
 * and hiding them with nothing in their place would leave a banner announcing a sixty
 * second countdown without saying what it is about.
 *
 * The twelve entries are the twelve blocks `changedBlocks` compares, no more and no less.
 * The `admin` block is absent because that comparison never looks at it: it carries only
 * secrets and the length of a session, and it triggers no confirmation at all. A test
 * bench reads the Go file and fails if a thirteenth block appears there with no French
 * name.
 *
 * The labels carry their article: they are listed inside a sentence — « ce qui a changé :
 * la balance, le catalogue » — where a field name instead titles a box.
 */
export const BLOCK_LABELS: Readonly<Record<string, string>> = {
  station: 'l’identité du poste',
  network: 'le réseau',
  ui: 'l’écran client',
  scale: 'la balance',
  printer: 'l’imprimante',
  pricing: 'les tarifs',
  barcode: 'le code-barres',
  limits: 'les garde-fous',
  stability: 'la stabilité',
  catalog: 'le catalogue',
  journal: 'le journal',
  maintenance: 'la maintenance',
}

/**
 * The French name of a block, or the token itself.
 *
 * Same fallback as for the fields, and for the same reason: a block this screen does not
 * know must still be named, even if only in English. A banner announcing an unconfirmed
 * configuration without saying what it covers is worth less than an unreadable token.
 *
 * @param block - the name of the block, as the service writes it.
 */
export function blockLabelOf(block: string): string {
  return BLOCK_LABELS[block] ?? block
}

/**
 * The French name of the part of the station that wrote a technical line.
 *
 * The seven tokens of `LogSource*`, internal/store/technical.go. They live here rather
 * than on the Journal page because TWO screens read the same technical log — the
 * Dashboard shows its last ten lines, the Journal its last fifty — and the table stayed
 * private to one of them: a volunteer read « catalogue » on one page and « catalog » on
 * the other, for the very same event.
 */
export const LOG_SOURCE_LABELS: Readonly<Record<string, string>> = {
  scale: 'balance',
  printer: 'imprimante',
  catalog: 'catalogue',
  ui: 'écran',
  config: 'configuration',
  http: 'réseau',
  system: 'système',
}

/**
 * The French name of a log source, or an honest sentence.
 *
 * The fallback is NOT the token here, unlike the fields and the blocks: those name
 * something the reader can act on — a field to correct, a block to confirm — whereas an
 * origin is a label. An unknown one is worth saying so rather than showing a word the
 * screen cannot translate.
 *
 * @param source - the origin, as the service writes it.
 */
export function logSourceLabelOf(source: string): string {
  return LOG_SOURCE_LABELS[source] ?? 'origine inconnue'
}
