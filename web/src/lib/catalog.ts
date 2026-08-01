import { normalize } from './normalize'

/**
 * Le catalogue tel que `GET /api/v1/catalog` le sert.
 *
 * La source de vérité est `internal/web/catalog.go`. La charge ne porte que les
 * produits PESABLES : un préemballé n'a pas de tuile, et ce n'est pas une erreur
 * masquée mais la réponse à « ce produit est-il pesable ? » (§10.3, ADR-021).
 */

/** Une tuile de la grille. */
export interface Product {
  /** `id` Odoo. Clé du PRODUCTEUR, stable d'un import à l'autre ; jamais un indice. */
  id: string
  /** Le nom reçu, affiché tel quel — accents, `♥` compris. */
  name: string
  /** Le nom désaccentué, calculé par `domain.Normalize` au moment de servir. */
  search: string
  /** Code de catégorie configuré : `fruits`, `vegetables`, `bulk`, `other`. */
  category_code: string
  mode: string
  unit_price_cents: number
  /** Le prix en euros, virgule française. À AFFICHER, jamais à recalculer. */
  unit_price_text: string
  /** ` €/kg`, ` € le litre`, ` € l'unité` — un affichage, jamais une règle. */
  price_suffix: string
  /** `/images/<sha>.<ext>`, vide sur 154 des 331 tuiles réelles. */
  image_url: string
  /** Un prix dérivé par palier configuré — le calcul de domain.Price, jamais
   *  refait ici : le texte arrive déjà arrondi du serveur. */
  prices: { code: string; text: string }[]
}

/** Un rayon de la grille, tel qu'il est configuré pour CE poste. */
export interface Category {
  code: string
  /** Libellé français. Il vient de la configuration, jamais du CSV. */
  label: string
  /** Rang d'affichage : configuration, et non classement figé dans le code. */
  rank: number
  color: string
  /** Un poste peut légitimement masquer un rayon : le poste fruits n'a pas le vrac. */
  visible: boolean
  /** L'effectif PESABLE, jamais le nombre de lignes reçues (§14.4). */
  product_count: number
}

/** Un niveau de tarif configuré. */
export interface Tier {
  code: string
  label: string
  abbrev: string
  rank: number
}

/** Les réglages d'écran dont la grille dépend (§11.2). */
export interface Presentation {
  show_grid_prices: boolean
  idle_timeout_s: number
  reprint_window_s: number
  sound: boolean
  /**
   * Faux — le défaut — retire de la grille les produits vendus à l'unité.
   *
   * `by_unit` et non `unit` : le mode de vente vient du PRÉFIXE du code-barres, seul
   * jeton que la caisse lit. La colonne `unite` du CSV n'est qu'un libellé de prix.
   */
  show_by_unit_products: boolean
  /**
   * Le nombre de colonnes de la grille. `0` — le défaut — laisse `auto-fill` décider.
   *
   * Le défaut n'est PAS un nombre, c'est un comportement : une configuration écrite
   * avant ce réglage garde la densité continue d'ADR-035 sur tous les écrans, sans
   * qu'un `5` figé vienne casser le 4K qui en montrait 10. Toute autre valeur est
   * ce nombre de colonnes partout — le fichier décrit une grille, pas une préférence.
   */
  grid_columns: number
}

/** Le corps de `GET /api/v1/catalog`. */
export interface Catalog {
  /** L'ETag, porté aussi dans le corps pour qu'un front puisse le journaliser. */
  revision: string
  /** Quand ce catalogue est entré en service, RFC 3339, ou vide si aucun ne l'est. */
  updated_at: string
  /** La version de l'application, affichée en permanence par la barre basse (§14.3). */
  app_version: string
  product_count: number
  categories: Category[]
  products: Product[]
  fallback_category: string
  pricing: { primary_code: string; primary_label: string; tiers: Tier[] }
  presentation: Presentation
}

/**
 * Combien de produits pesables une catégorie doit atteindre pour mériter sa puce.
 *
 * CONSTANTE DU CODE, pas un réglage : aucun exploitant n'a de choix légitime à
 * faire là-dessus (ADR-025). En deçà, la catégorie reste dans « Tout » et ses
 * produits restent atteignables par la recherche — jamais masqués. En 2022,
 * « Autres » comptait UN produit : un quart de barre de navigation pour une tuile.
 */
export const MIN_PRODUCTS_FOR_CHIP = 5

/** L'identifiant de la puce de la vue au repos, qui est toujours « Tout ». */
export const ALL_CATEGORIES = ''

/** Une puce de la barre basse. */
export interface Chip {
  /** Code de catégorie, ou {@link ALL_CATEGORIES} pour « Tout ». */
  code: string
  label: string
  color: string
  /** L'effectif PESABLE derrière la puce, jamais le nombre de lignes reçues. */
  count: number
}

/** Le mode de vente d'un produit qui s'achète à la pièce, tel que le serveur l'écrit. */
const BY_UNIT = 'by_unit'

/**
 * Les produits qu'un poste peut montrer : tout ce que le serveur a envoyé, moins
 * les rayons que ce poste masque, moins les produits vendus à l'unité s'il les masque.
 *
 * Les deux masquages sont ici, et non au serveur, pour la même raison : ce sont des
 * choix de POSTE et non des décisions d'import. Le serveur sert l'inventaire de ce qui
 * a une tuile, et `product_count` reste égal au `catalog_count` du flux d'état — que le
 * navigateur surveille pour redemander le catalogue. Filtrer là-bas ferait diverger ces
 * deux nombres en permanence, donc un `GET /api/v1/catalog` par événement.
 *
 * Masquer est un AFFICHAGE et jamais un refus : le chemin d'impression reste ouvert, et
 * le poste continue de ne juger que la qualification.
 *
 * @param catalog - le catalogue servi.
 * @returns les produits que ce poste montre, dans l'ordre où ils sont servis.
 */
export function visibleProducts(catalog: Catalog): Product[] {
  const hidden = new Set(catalog.categories.filter((c) => !c.visible).map((c) => c.code))
  const byUnitHidden = !catalog.presentation.show_by_unit_products
  if (hidden.size === 0 && !byUnitHidden) return catalog.products
  return catalog.products.filter(
    (p) => !hidden.has(p.category_code) && !(byUnitHidden && p.mode === BY_UNIT),
  )
}

/**
 * Construit la barre de filtres : « Tout » d'abord, puis une puce par catégorie
 * PEUPLÉE.
 *
 * L'effectif est recompté sur les produits RÉELLEMENT SERVIS plutôt que repris de
 * `Category.product_count`. Le serveur envoie bien les deux, et un test vérifie
 * qu'ils s'accordent ; mais le seul chiffre qu'un client puisse vérifier est celui
 * des tuiles qu'il voit, et une puce ne doit jamais pouvoir mentir sur sa grille.
 *
 * L'ordre et la couleur viennent de la configuration : la répartition réelle s'est
 * inversée entre 2022 et 2026, et aucune barre en dur n'y survit (§14.3-2).
 *
 * @param catalog - le catalogue servi.
 * @returns les puces à afficher, « Tout » en tête puis les catégories par rang.
 */
export function chips(catalog: Catalog): Chip[] {
  const shown = visibleProducts(catalog)
  const counts = new Map<string, number>()
  for (const p of shown) counts.set(p.category_code, (counts.get(p.category_code) ?? 0) + 1)

  const populated = catalog.categories
    .filter((c) => c.visible && (counts.get(c.code) ?? 0) >= MIN_PRODUCTS_FOR_CHIP)
    .slice()
    .sort((a, b) => a.rank - b.rank || a.code.localeCompare(b.code))
    .map((c) => ({
      code: c.code,
      label: c.label,
      color: c.color,
      count: counts.get(c.code) ?? 0,
    }))

  return [
    { code: ALL_CATEGORIES, label: 'Tout', color: 'var(--ink-muted)', count: shown.length },
    ...populated,
  ]
}

/**
 * Filtre la grille EN PLACE : une catégorie, puis une requête, et rien d'autre.
 *
 * Il n'y a AUCUN plafond de résultats. L'existant refusait au-delà de 50 et portait
 * un plafond de 120 tuiles par catégorie — plafond franchi en production, qui perd
 * six produits vendables en silence (§14.3-1). Aucun nombre de tuiles n'apparaît
 * dans ce fichier, et aucun appelant ne peut en passer un.
 *
 * @param products - les produits que le poste peut montrer.
 * @param category - un code de catégorie, ou {@link ALL_CATEGORIES}.
 * @param query - ce qui a été tapé jusqu'ici ; accents et casse sont sans effet.
 * @returns les produits correspondants, dans l'ordre du catalogue.
 */
export function filterProducts(products: Product[], category: string, query: string): Product[] {
  const needle = normalize(query)
  const words = needle.length === 0 ? [] : needle.split(' ')
  return products.filter((p) => {
    if (category !== ALL_CATEGORIES && p.category_code !== category) return false
    // Chaque mot tapé doit apparaître : « ail vio » trouve « Ail  violet », et
    // l'ordre dans lequel un client tape ses mots n'est pas une règle qu'il connaît.
    return words.every((w) => p.search.includes(w))
  })
}

/**
 * Le produit que désigne un toucher, ou `undefined` si le catalogue ne l'a plus.
 *
 * @param products - les produits que le poste peut montrer.
 * @param id - l'`id` Odoo porté par la tuile.
 */
export function productByID(products: Product[], id: string): Product | undefined {
  return products.find((p) => p.id === id)
}
