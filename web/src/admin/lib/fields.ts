/**
 * Le nom français de chaque clé de configuration.
 *
 * Il existe parce que les refus du poste ne sont PAS auto-porteurs : le service répond
 * un couple clé + message, et « attendu : nombre entier » ne nomme rien tout seul. Tant
 * que l'écran affichait la clé, la phrase se suffisait ; masquée, il fallait autre chose
 * à mettre à sa place.
 *
 * C'est la SEULE source de ces noms : les pages le lisent pour dessiner leurs champs, et
 * la barre de refus le lit pour nommer ce que le poste a refusé. Un test vérifie que tout
 * chemin édité par une page y figure.
 *
 * Un libellé y est plus long que celui qu'une page pose sous son propre titre : « Port
 * série » se lit sous le panneau « Balance », mais la barre de refus, elle, n'a pas de
 * panneau au-dessus d'elle — le nom doit y suffire seul.
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
 * Le nom français d'une clé, ou la clé elle-même.
 *
 * Le repli n'est pas un pis-aller : un refus venu d'un contrôle qu'aucune page n'édite
 * doit rester lisible par quelqu'un au téléphone, plutôt que de disparaître.
 *
 * @param path - le chemin pointé de la clé, tel que le service le nomme.
 */
export function labelOf(path: string): string {
  return FIELD_LABELS[path] ?? path
}
