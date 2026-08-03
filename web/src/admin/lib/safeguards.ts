import { labelOf } from './fields'

/**
 * The fourteen safeguards of §6.4, in EVALUATION ORDER, with their thresholds.
 *
 * The list is the one of `internal/domain/safeguard.go` and nothing else — a test reads
 * that file and refuses a code this table invents or forgets. The messages are QUOTED
 * from the same file, straight apostrophes included: they are what the customer reads,
 * not prose written for this screen. That they are only quoted is a GAP to §6.4, which
 * makes them editable from the Rules screen: what is missing is a configuration key per
 * message and the route that writes it — the validation of a submitted message already
 * exists in Go (`domain.CheckMessage`).
 *
 * The table lives in a module and not in the page because it is DATA, and because two
 * hundred lines of it in the middle of a page hid everything that page actually does.
 */

/** One threshold of §11.2 that a safeguard reads, edited on the Rules page and nowhere else. */
export interface Threshold {
  path: string
  label: string
  hint: string
}

/** One of the fourteen safeguards of §6.4. */
export interface Safeguard {
  /** Its rank in the EVALUATION ORDER, which is normative (§6.4). */
  rank: number
  /** The identifier the journal and the telephone use. English, and secondary. */
  code: string
  /** What a volunteer reads. */
  label: string
  /** What makes it fire, in one sentence. */
  when: string
  /** French, read-only: it says who has to act, and it is not a shop setting. */
  severity: string
  /** True when the rule stops the label: it draws the row, and it is never editable. */
  blocking: boolean
  /** The wording the customer reads, QUOTED from `internal/domain/safeguard.go`. */
  message: string
  /** The keys this rule owns. A key belongs to exactly one rule, and is edited once. */
  thresholds: Threshold[]
  /** The only rule that a configuration can switch off, empty for the other thirteen. */
  switchPath: string
  switchLabel: string
  /** Where the threshold lives when it is not a key this rule owns. */
  note: string
}

/**
 * The fourteen rules themselves.
 *
 * A rule whose threshold is EDITED ELSEWHERE says so through {@link labelOf} and never by
 * writing the key: the technical name is behind the « Montrer les noms techniques »
 * switch, and a note that spells it out puts back on screen what that switch hides.
 */
export const SAFEGUARDS: Safeguard[] = [
  {
    rank: 1,
    code: 'OVERLOAD',
    label: 'Surcharge',
    when: 'La balance annonce elle-même OL, ou le poids brut dépasse la capacité.',
    severity: 'Bloquant',
    blocking: true,
    message: "La balance est en surcharge. Retirez votre article.",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note: `Seuil : la capacité, réglée au garde-fou 9 sous « ${labelOf('limits.max_weight_g')} ».`,
  },
  {
    rank: 2,
    code: 'MEASUREMENT_EXPIRED',
    label: 'Poids périmé',
    when: 'La mesure est plus vieille que la péremption, dans les deux modes de stabilité.',
    severity: 'Bloquant',
    blocking: true,
    message: "Poids indisponible. Patientez ou appelez un bénévole.",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note: 'Aucun seuil à régler : le poste calcule lui-même à partir du rythme de la balance.',
  },
  {
    rank: 3,
    code: 'BASKET_MISSING',
    label: 'Panier absent',
    when: 'Le poids brut tombe dans la fenêtre négative du panier : il a été soulevé.',
    severity: 'Bloquant',
    blocking: true,
    message: "Le panier n'est pas sur la balance. Reposez-le.",
    thresholds: [
      {
        path: 'limits.basket_min_g',
        label: 'Bas de la fenêtre du panier',
        hint: 'En grammes, NÉGATIF : c’est le poids du panier que la balance a perdu.',
      },
      {
        path: 'limits.basket_max_g',
        label: 'Haut de la fenêtre du panier',
        hint: 'Négatif lui aussi, et plus proche de zéro que le bas.',
      },
    ],
    switchPath: 'limits.basket_check_enabled',
    switchLabel: 'Ce poste travaille avec un panier taré',
    note: 'La règle s’active ou non, en bloc : il n’y a pas de demi-mesure à régler.',
  },
  {
    rank: 4,
    code: 'SCALE_EMPTY',
    label: 'Plateau vide',
    when: 'Le poids brut ne sort pas de la bande « il n’y a rien sur le plateau ».',
    severity: 'Bloquant — un filet, hors parcours nominal',
    blocking: true,
    message: "Posez votre produit.",
    thresholds: [
      {
        path: 'limits.empty_max_g',
        label: 'Plateau considéré vide',
        hint: 'En dessous, le poste considère qu’il n’y a rien sur le plateau.',
      },
    ],
    switchPath: '',
    switchLabel: '',
    note:
      'Toucher une tuile sur un plateau vide ARME la sélection au lieu d’être refusé : ' +
      'la règle reste évaluée pour la saisie manuelle et les chemins dérivés.',
  },
  {
    rank: 5,
    code: 'TARE_REQUIRED',
    label: 'Remise à zéro nécessaire',
    when: 'Le brut est sous la bande du plateau vide, et hors de la fenêtre du panier.',
    severity: 'Bloquant',
    blocking: true,
    message: "La balance doit être remise à zéro.",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note:
      'Seuil : la valeur négative de celui du garde-fou 4, ' +
      `« ${labelOf('limits.empty_max_g')} ».`,
  },
  {
    rank: 6,
    code: 'WEIGHT_UNSTABLE',
    label: 'Pesée instable',
    when: 'La trame déclare la mesure instable.',
    severity: 'Information par défaut (A3)',
    blocking: false,
    message: "Pesée en cours…",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note:
      'La sévérité suit l’exigence de stabilité : elle passe à Bloquant quand celle-ci ' +
      'est réglée sur « blocking ». L’impression n’est jamais bloquée par défaut.',
  },
  {
    rank: 7,
    code: 'TARE_INVALID',
    label: 'Emballage incohérent',
    when: 'Une tare a été saisie, et elle atteint la pesée ou dépasse le maximum.',
    severity: 'Bloquant',
    blocking: true,
    message: "Le poids de l'emballage est supérieur ou égal à la pesée.",
    thresholds: [
      {
        path: 'limits.max_tare_g',
        label: 'Tare maximum',
        hint: 'Une tare plus lourde que le maximum est une faute de frappe.',
      },
    ],
    switchPath: '',
    switchLabel: '',
    note: '',
  },
  {
    rank: 8,
    code: 'WEIGHT_TOO_LOW',
    label: 'Poids trop faible',
    // « ne dépasse pas » and not « n'atteint pas »: the kernel fires on `net <= floor`,
    // so a net weight EQUAL to the floor is refused. The wording of this table is
    // uniform on that point -- « atteint » is >=, « dépasse » is > (see rule 7).
    when: 'Vente au poids : le NET est strictement positif et ne dépasse pas le plancher.',
    severity: 'Bloquant',
    blocking: true,
    message: "La balance doit être retarée, ou l'emballage est trop lourd.",
    thresholds: [
      {
        path: 'limits.min_weight_g',
        label: 'Poids minimum',
        hint: 'Une dérogation par produit existe, dans l’onglet Catalogue.',
      },
    ],
    switchPath: '',
    switchLabel: '',
    note: '',
  },
  {
    rank: 9,
    code: 'WEIGHT_TOO_HIGH',
    label: 'Poids trop élevé',
    when: 'Le NET dépasse la capacité — strictement, pour que la capacité reste atteignable.',
    severity: 'Bloquant',
    blocking: true,
    message: "{{.Weight}} kg, ça paraît un peu lourd !",
    thresholds: [
      {
        path: 'limits.max_weight_g',
        label: 'Poids maximum',
        hint: 'C’est la capacité du champ NNDDD du code-barres, pas un seuil de vraisemblance.',
      },
    ],
    switchPath: '',
    switchLabel: '',
    note: '',
  },
  {
    rank: 10,
    code: 'UNITS_OUT_OF_RANGE',
    label: 'Nombre d’unités hors plage',
    when: 'Vente à l’unité : la quantité sort de la plage.',
    severity: 'Bloquant',
    blocking: true,
    message: "{{.Quantity}} unités, ça paraît un peu beaucoup !",
    thresholds: [
      { path: 'limits.min_units', label: 'Unités minimum', hint: '' },
      { path: 'limits.max_units', label: 'Unités maximum', hint: '' },
    ],
    switchPath: '',
    switchLabel: '',
    note: '',
  },
  {
    rank: 11,
    code: 'AMOUNT_OUT_OF_CAPACITY',
    label: 'Montant hors capacité du code-barres',
    when: 'La charge utile encode un PRIX, et il dépasse ce que le champ peut porter.',
    severity: 'Bloquant',
    blocking: true,
    message: "Prix trop élevé pour le code-barres.",
    thresholds: [
      {
        path: 'limits.max_amount_cents',
        label: 'Montant maximum',
        hint: 'En centimes. Aucun préfixe du plan livré n’encode un prix : la règle est ' +
          'éprouvée sans qu’aucun produit puisse l’atteindre.',
      },
    ],
    switchPath: '',
    switchLabel: '',
    note: '',
  },
  {
    rank: 12,
    code: 'ZERO_PRICE',
    label: 'Prix nul',
    when: 'Le montant du tarif imprimé en grand vaut zéro.',
    severity: 'Bloquant',
    blocking: true,
    message: "Prix nul. Appelez un bénévole.",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note: 'Aucun seuil : un produit à 0 € est une anomalie sans nuance.',
  },
  {
    rank: 13,
    code: 'LIGHT_PRODUCT_ALLOWED',
    label: 'Produit léger autorisé',
    when: 'Le garde-fou 8 n’a pas déclenché grâce à la dérogation du produit.',
    severity: 'Information',
    blocking: false,
    message: '',
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note:
      'Aucun seuil général : c’est la dérogation par produit, listée plus bas et posée ' +
      'depuis l’onglet Catalogue. Rien ne s’affiche au client ; l’id du produit ' +
      'est journalisé.',
  },
  {
    rank: 14,
    code: 'PRODUCT_WITHDRAWN',
    label: 'Produit retiré',
    when: 'Quelqu’un a décidé de ne plus proposer ce produit.',
    severity: 'Bloquant',
    blocking: true,
    message: "Ce produit n'est pas disponible.",
    thresholds: [],
    switchPath: '',
    switchLabel: '',
    note:
      'Aucun seuil : c’est une décision humaine, prise depuis l’onglet Catalogue. ' +
      'Aucune règle d’import ne peut la déduire.',
  },
]

/**
 * The markers the shipped messages carry, out of the CLOSED list of `safeguard.go`.
 *
 * They are named on screen because they are visible in the quotations above: a reader who
 * does not know what `{{.Weight}}` is takes it for a defect of the message.
 */
export const PLACEHOLDERS = ['{{.Weight}}', '{{.Quantity}}']

/**
 * How a verdict names itself to a volunteer.
 *
 * The stream spells the code in English — `WEIGHT_TOO_LOW` — and a volunteer at the
 * counter must not have to translate it. An unknown code says so instead of being printed
 * as a label: this screen can be older than the binary it talks to.
 *
 * @param code - the English code, as the stream spells it.
 */
export function labelOfCode(code: string): string {
  return SAFEGUARDS.find((rule) => rule.code === code)?.label ?? 'Garde-fou inconnu de cet écran'
}

/**
 * How a severity reads. The service spells it `blocking` or `info`.
 *
 * @param severity - the token the service wrote.
 */
export function frenchSeverity(severity: string): string {
  if (severity === 'blocking') return 'Bloquant'
  if (severity === 'info') return 'Information'
  return 'Sévérité inconnue de cet écran'
}
