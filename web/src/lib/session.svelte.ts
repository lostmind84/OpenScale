import { fetchCatalog } from './api'
import { MIN_PRODUCTS_FOR_CHIP, type Catalog, type Presentation } from './catalog'
import type { StateDTO } from './dto'
import { linkHealth, type LinkHealth } from './link'

/** À quelle cadence le chien de garde relit l'horloge, en millisecondes. */
const WATCHDOG_PERIOD_MS = 250

/** Réglages d'écran par défaut, tant que le catalogue n'est pas arrivé. */
const DEFAULT_PRESENTATION: Presentation = {
  show_grid_prices: true,
  idle_timeout_s: 45,
  reprint_window_s: 60,
  sound: true,
  // Faux comme le défaut du poste : un écran qui n'a pas encore reçu son catalogue ne
  // doit pas montrer une fraction de seconde des tuiles que ce poste masque.
  show_by_unit_products: false,
  // Automatique, comme le défaut du poste : la grille du squelette est celle
  // d'ADR-035 tant que personne n'a dit autre chose.
  grid_columns: 0,
  // Le défaut livré, comme le défaut du poste : sans catalogue encore reçu, la barre
  // du squelette n'a aucun seuil de POSTE à appliquer (ADR-059).
  min_products_for_chip: MIN_PRODUCTS_FOR_CHIP,
}

/**
 * Tout ce que l'écran sait de son poste, et comment il le garde vrai.
 *
 * Le catalogue vit ICI, dans le navigateur, et non dans une requête par frappe : le
 * filtrage reste instantané, et il reste FONCTIONNEL PENDANT UN REDÉMARRAGE DU
 * SERVICE (§14.3-3). L'état arrive par `EventSource`, qui se reconnecte tout seul —
 * toute la raison du choix de SSE contre WebSocket (ADR-014).
 */
export class Session {
  /** Dernier état reçu, ou null jusqu'au premier événement du flux. */
  state = $state<StateDTO | null>(null)
  /** Le catalogue complet, conservé à travers une reconnexion et un redémarrage. */
  catalog = $state<Catalog | null>(null)
  /** Ce qu'il faut dire du lien, recalculé par le chien de garde. */
  link = $state<LinkHealth>({ showWeight: true, banner: '', mustReconnect: false })
  /** Phrase française à afficher à la place de la grille si le catalogue manque. */
  catalogError = $state('')

  #source: EventSource | null = null
  #watchdog: ReturnType<typeof setInterval> | null = null
  #lastMessageAt = Date.now()
  #streamOpen = false
  #loading = false

  /** Les réglages d'écran servis avec le catalogue, ou leurs valeurs par défaut. */
  get presentation(): Presentation {
    return this.catalog?.presentation ?? DEFAULT_PRESENTATION
  }

  /** Ouvre le flux, charge le catalogue et lance le chien de garde. */
  start(): void {
    void this.#loadCatalog()
    this.#open()
    this.#watchdog = setInterval(() => this.#checkLink(), WATCHDOG_PERIOD_MS)
  }

  /** Ferme tout. Utilisé par les tests et par une page qui s'en va. */
  stop(): void {
    if (this.#watchdog !== null) clearInterval(this.#watchdog)
    this.#watchdog = null
    this.#source?.close()
    this.#source = null
    this.#streamOpen = false
  }

  /**
   * Ouvre `GET /api/v1/stream`.
   *
   * L'événement est NOMMÉ `state` côté serveur (`event: state`), donc il s'écoute
   * par `addEventListener` : `onmessage` ne reçoit que les événements anonymes et
   * resterait muet pour toujours.
   */
  #open(): void {
    const source = new EventSource('/api/v1/stream')
    this.#source = source
    source.onopen = () => {
      this.#streamOpen = true
      this.#lastMessageAt = Date.now()
    }
    source.addEventListener('state', (event) => this.#receive(event as MessageEvent<string>))
    source.onerror = () => {
      this.#streamOpen = false
    }
  }

  /**
   * Enregistre un état, et redemande le catalogue quand ce qu'il porte a bougé.
   *
   * DEUX raisons de le redemander, et pas une seule. Le nombre de tuiles se compare
   * au catalogue chargé ; les réglages d'écran, eux, se comparent à l'état PRÉCÉDENT,
   * parce que l'empreinte voyage dans le flux et non dans le catalogue. Sans cette
   * seconde comparaison, un exploitant change le nombre de colonnes, enregistre, et
   * rien ne se passe sur l'écran d'à côté — la conclusion « ce réglage ne fait rien »
   * contre laquelle le contrôle 46 d'ADR-031 avait été écrit. C'est déjà vrai de
   * `show_grid_prices`, invisible aujourd'hui, et réparé au passage.
   */
  #receive(event: MessageEvent<string>): void {
    this.#lastMessageAt = Date.now()
    this.#streamOpen = true
    const next = JSON.parse(event.data) as StateDTO
    const previous = this.state
    this.state = next
    if (this.catalog === null) return
    // La bascule de catalogue est DIFFÉRÉE par le Hub jusqu'au repos (§10.8) : quand
    // le compte bouge, plus personne n'a le doigt sur une tuile. La requête est
    // validée par ETag, donc un catalogue inchangé coûte un 304 — et une présentation
    // inchangée, jamais de requête du tout.
    const countMoved = next.catalog_count !== this.catalog.product_count
    // Le premier état reçu n'a pas de précédent, et il n'en a pas besoin : `start()`
    // vient de charger le catalogue, donc sa présentation est déjà celle du poste.
    const presentationMoved =
      previous !== null && next.presentation_digest !== previous.presentation_digest
    if (countMoved || presentationMoved) {
      void this.#loadCatalog()
    }
  }

  /** Relit l'horloge et décide ce que l'écran dit du lien. */
  #checkLink(): void {
    const health = linkHealth(Date.now() - this.#lastMessageAt, this.#streamOpen)
    this.link = health
    if (health.mustReconnect) {
      this.#source?.close()
      this.#lastMessageAt = Date.now()
      this.#open()
    }
  }

  /**
   * Charge le catalogue, et conserve le précédent quand le chargement échoue.
   *
   * Conserver le précédent est tout l'intérêt : un poste dont le service redémarre
   * doit continuer à montrer ses tuiles.
   */
  async #loadCatalog(): Promise<void> {
    if (this.#loading) return
    this.#loading = true
    try {
      this.catalog = await fetchCatalog()
      this.catalogError = ''
    } catch {
      if (this.catalog === null) {
        this.catalogError = 'Catalogue indisponible. Prévenez un responsable.'
      }
    } finally {
      this.#loading = false
    }
  }
}
