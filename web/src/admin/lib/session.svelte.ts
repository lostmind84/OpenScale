import * as api from './api'
import { AdminError } from './api'
import type { FaultDTO, HealthDTO } from './dto'

/** À quelle cadence le tableau de bord se relit, en millisecondes. */
const REFRESH_PERIOD_MS = 3000

/** Les pages de l'administration : deux bénévoles, six expertes (§14.4). */
export type PageID =
  | 'dashboard'
  | 'troubleshooting'
  | 'hardware'
  | 'label'
  | 'rules'
  | 'catalog'
  | 'journal'
  | 'station'

/** Les deux pages ouvertes par défaut, SANS mot de passe (ADR-018, important-10). */
export const VOLUNTEER_PAGES: PageID[] = ['dashboard', 'troubleshooting']

/**
 * Les pages derrière « Réglages avancés », protégées.
 *
 * La frontière est celle d'ADR-018 et elle ne se discute pas : ce qui ÉCRIT la
 * configuration est protégé, ce qui lit un port, interroge un statut ou sort une
 * étiquette de démonstration ne l'est pas. Quiconque est derrière le comptoir peut déjà
 * débrancher l'imprimante — un mot de passe n'ajouterait là aucune sécurité et
 * supprimerait tout le dépannage.
 */
export const EXPERT_PAGES: PageID[] = ['hardware', 'label', 'rules', 'catalog', 'journal', 'station']

/** Vrai quand la page nommée demande une session ouverte. */
export function needsPassword(page: PageID): boolean {
  return EXPERT_PAGES.includes(page)
}

/**
 * Tout ce que l'écran d'administration sait de son poste.
 *
 * Le tableau de bord se relit tout seul toutes les trois secondes, parce qu'un bénévole
 * le laisse ouvert pendant qu'il touche au matériel et que le feu doit VERDIR sous ses
 * yeux quand il rebranche le câble. La période est un paramètre du constructeur pour que
 * les tests appellent {@link refresh} et ne dorment jamais.
 */
export class Admin {
  /** Le dernier tableau de bord lu, ou null avant la première lecture. */
  health = $state<HealthDTO | null>(null)
  /** La page affichée. Le tableau de bord est celle qui s'ouvre. */
  page = $state<PageID>('dashboard')
  /** Une session d'administration est ouverte : les pages expertes sont accessibles. */
  expert = $state(false)
  /** La phrase française du dernier refus, vide quand il n'y a rien à signaler. */
  error = $state('')
  /** La phrase française de la dernière action réussie. */
  notice = $state('')
  /** Vrai pendant qu'une action est en vol : les boutons se désarment. */
  busy = $state(false)
  /**
   * Les 45 contrôles du dernier refus de configuration (§11.3).
   *
   * Ils vivent ici et non dans la phrase d'erreur parce qu'ils voyagent TOUS ENSEMBLE
   * dans le même corps : « cette configuration ne peut pas être appliquée » sans dire
   * quel champ est un message qui ne sert à rien.
   */
  lastFaults = $state<FaultDTO[]>([])

  #timer: ReturnType<typeof setInterval> | null = null

  constructor(private readonly periodMs = REFRESH_PERIOD_MS) {}

  /** Lit le tableau de bord et arme la relecture. */
  start(): void {
    void this.refresh()
    this.#timer = setInterval(() => void this.refresh(), this.periodMs)
  }

  /** Arrête la relecture. Une page qui s'en va ne laisse pas un intervalle derrière elle. */
  stop(): void {
    if (this.#timer !== null) clearInterval(this.#timer)
    this.#timer = null
  }

  /**
   * Relit `GET /admin/api/health`.
   *
   * Un échec de lecture n'efface pas le tableau de bord précédent : un poste dont le
   * service vient de redémarrer doit continuer d'afficher ce qu'il savait, avec la raison
   * à côté, plutôt que de se vider.
   */
  async refresh(): Promise<void> {
    try {
      this.health = await api.fetchHealth()
      this.error = ''
    } catch (failure) {
      this.error = sentenceOf(failure)
    }
  }

  /**
   * Exécute une action de dépannage et affiche ce qu'elle répond, EN FRANÇAIS.
   *
   * La phrase vient du service — c'est lui qui sait ce qu'il a fait — et le tableau de
   * bord est relu derrière, parce que la moitié de l'intérêt d'un bouton de dépannage est
   * de voir le feu changer de couleur.
   *
   * @param action - l'appel à passer.
   */
  async run(action: () => Promise<{ message: string }>): Promise<void> {
    this.busy = true
    this.notice = ''
    this.error = ''
    try {
      const done = await action()
      this.notice = done.message
    } catch (failure) {
      this.error = sentenceOf(failure)
      this.#forgetSessionIfRefused(failure)
    } finally {
      this.busy = false
    }
    await this.refresh()
  }

  /**
   * Ouvre une session d'administration.
   *
   * @param password - ce que le bénévole a tapé.
   * @returns vrai quand la session est ouverte.
   */
  async login(password: string): Promise<boolean> {
    this.busy = true
    this.error = ''
    try {
      await api.openSession(password)
      this.expert = true
      this.notice = 'Session d’administration ouverte.'
      return true
    } catch (failure) {
      this.expert = false
      this.error = sentenceOf(failure)
      return false
    } finally {
      this.busy = false
    }
  }

  /**
   * Réinitialise le mot de passe avec le code de secours de la fiche d'installation.
   *
   * @param code - les huit caractères de la fiche.
   * @param password - le nouveau mot de passe.
   */
  async recover(code: string, password: string): Promise<boolean> {
    this.busy = true
    this.error = ''
    try {
      await api.recoverSession(code, password)
      this.expert = true
      this.notice = 'Le mot de passe est remplacé et la session est ouverte.'
      return true
    } catch (failure) {
      this.error = sentenceOf(failure)
      return false
    } finally {
      this.busy = false
    }
  }

  /** Ferme la session et revient au tableau de bord. */
  async logout(): Promise<void> {
    await api.closeSession()
    this.expert = false
    this.page = 'dashboard'
    this.notice = 'Session d’administration fermée.'
  }

  /**
   * Va sur une page, en redemandant le mot de passe si elle en demande un.
   *
   * @param page - la page voulue.
   */
  open(page: PageID): void {
    this.error = ''
    this.notice = ''
    this.page = page
  }

  /**
   * Charge ce qu'une page experte a besoin de lire, et retombe sur le mot de passe si la
   * session a expiré pendant que la page était ouverte.
   *
   * Trente minutes suffisent à laisser un écran ouvert plus longtemps qu'une session, et
   * un tableau vide sans explication serait lu comme « il n'y a rien », qui est faux.
   *
   * @param load - la lecture à tenter.
   */
  async load<T>(load: () => Promise<T>): Promise<T | null> {
    try {
      const value = await load()
      this.error = ''
      this.lastFaults = []
      return value
    } catch (failure) {
      this.error = sentenceOf(failure)
      this.lastFaults = failure instanceof AdminError ? failure.faults : []
      this.#forgetSessionIfRefused(failure)
      return null
    }
  }

  /** Une session refusée est une session à rouvrir, jamais une panne à signaler. */
  #forgetSessionIfRefused(failure: unknown): void {
    if (failure instanceof AdminError && failure.needsPassword) this.expert = false
  }
}

/** La phrase d'un échec : celle du service quand il en a écrit une. */
function sentenceOf(failure: unknown): string {
  if (failure instanceof AdminError) return failure.message
  if (failure instanceof Error) return 'Le poste n’a pas répondu : ' + failure.message
  return 'Le poste n’a pas répondu.'
}
