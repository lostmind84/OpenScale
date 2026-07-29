import * as api from './api'
import { AdminError } from './api'
import type { FaultDTO, HealthDTO } from './dto'

/** À quelle cadence le tableau de bord se relit, en millisecondes. */
const REFRESH_PERIOD_MS = 3000

/** Les pages de l'administration : deux bénévoles, sept expertes (§14.4). */
export type PageID =
  | 'dashboard'
  | 'troubleshooting'
  | 'hardware'
  | 'label'
  | 'rules'
  | 'catalog'
  | 'journal'
  | 'station'
  | 'update'

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
export const EXPERT_PAGES: PageID[] = [
  'hardware',
  'label',
  'rules',
  'catalog',
  'journal',
  'station',
  'update',
]

/** Vrai quand la page nommée demande une session ouverte. */
export function needsPassword(page: PageID): boolean {
  return EXPERT_PAGES.includes(page)
}

/** Ce que le panneau de mot de passe doit demander, et pourquoi. */
export interface PendingCredentials {
  /**
   * `password` quand la session manque ou a expiré ; `first-password` quand ce poste
   * n'a jamais eu de mot de passe — il n'y a alors rien à taper qu'un code de secours.
   */
  kind: 'password' | 'first-password'
  /** La phrase du service, celle qui dit pourquoi il a refusé. */
  message: string
}

/**
 * Vrai quand un refus se règle en s'authentifiant, et non en corrigeant sa saisie.
 *
 * 401 « session absente ou expirée » et 409 « aucun mot de passe n'est posé » sont les
 * deux seuls ; un 422 se règle en changeant le champ fautif, et rouvrir un panneau de mot
 * de passe par-dessus cacherait la faute qu'il faut lire.
 */
function needsCredentials(failure: unknown): failure is AdminError {
  return failure instanceof AdminError && (failure.status === 401 || failure.status === 409)
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
  /** Une session d'administration est ouverte : les actes protégés passent sans rien demander. */
  expert = $state(false)
  /**
   * Ce que le SONDAGE a à dire du lien avec le poste, et lui seul.
   *
   * Il tourne toutes les trois secondes. Tant qu'il partageait son champ avec les actes,
   * il effaçait « Mot de passe incorrect. » avant qu'on ait fini de le lire — pendant tout
   * le temps où l'administration a existé, aucun refus n'a jamais tenu à l'écran.
   */
  linkError = $state('')
  /** Ce que le dernier ACTE a à dire. Rien ne l'efface qu'un autre acte. */
  actionError = $state('')
  /** Vrai quand le poste répond « aucun mot de passe n'est posé » : le code de secours ouvre. */
  needsFirstPassword = $state(false)
  /** La phrase française de la dernière action réussie. */
  notice = $state('')
  /** Vrai pendant qu'une action est en vol : les boutons se désarment. */
  busy = $state(false)
  /**
   * Les 47 contrôles du dernier refus de configuration (§11.3).
   *
   * Ils vivent ici et non dans la phrase d'erreur parce qu'ils voyagent TOUS ENSEMBLE
   * dans le même corps : « cette configuration ne peut pas être appliquée » sans dire
   * quel champ est un message qui ne sert à rien.
   */
  lastFaults = $state<FaultDTO[]>([])
  /** L'acte qui attend une réponse du panneau de mot de passe, ou null. */
  pending = $state<PendingCredentials | null>(null)

  #timer: ReturnType<typeof setInterval> | null = null
  /** Ce qui rend la main à {@link protect} quand le panneau a répondu. */
  #answered: ((opened: boolean) => void) | null = null

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
      this.linkError = ''
    } catch (failure) {
      this.linkError = sentenceOf(failure)
    }
  }

  /**
   * Ouvre un acte : ce qui reste à l'écran de l'acte précédent s'en va, et rien d'autre.
   *
   * Le sondage n'est pas un acte, et c'est tout l'intérêt de la distinction.
   */
  #beginAction(): void {
    this.busy = true
    this.notice = ''
    this.actionError = ''
    this.needsFirstPassword = false
  }

  /** Enregistre le refus d'un acte, et ce qu'il implique de la session. */
  #failAction(failure: unknown): void {
    this.actionError = sentenceOf(failure)
    this.needsFirstPassword = failure instanceof AdminError && failure.status === 409
    this.#forgetSessionIfRefused(failure)
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
    this.#beginAction()
    try {
      const done = await action()
      this.notice = done.message
    } catch (failure) {
      this.#failAction(failure)
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
    this.#beginAction()
    try {
      await api.openSession(password)
      this.expert = true
      this.notice = 'Session d’administration ouverte.'
      return true
    } catch (failure) {
      this.expert = false
      this.#failAction(failure)
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
    this.#beginAction()
    try {
      const session = await api.recoverSession(code, password)
      this.expert = true
      // `warning` REMPLACE la phrase de succès plutôt que de s'y ajouter : quand elle
      // est posée, le mot de passe n'est PAS enregistré, et la bonne nouvelle
      // habituelle serait fausse à côté de celle qui compte.
      this.notice = session.warning ?? 'Le mot de passe est remplacé et la session est ouverte.'
      return true
    } catch (failure) {
      this.#failAction(failure)
      return false
    } finally {
      this.busy = false
    }
  }

  /**
   * Exécute un acte PROTÉGÉ, en ne demandant le mot de passe que s'il le faut.
   *
   * ADR-033 : on peut tout voir, on ne peut pas tout écrire. Le mot de passe n'est donc
   * plus une porte franchie avant de regarder, mais une question posée au moment d'agir.
   *
   * Ce qui rend la chose supportable est le REJEU. Sans lui, l'exploitant qui vient de
   * modifier sept champs et touche « Enregistrer » perdrait sa saisie et devrait tout
   * refaire après s'être authentifié — et il ne le ferait qu'une fois.
   *
   * @param action - l'appel protégé, qui sera peut-être passé DEUX fois.
   * @returns ce que l'acte a répondu, ou null s'il a échoué ou s'il a été abandonné.
   */
  async protect<T>(action: () => Promise<T>): Promise<T | null> {
    try {
      return await action()
    } catch (failure) {
      if (!needsCredentials(failure)) {
        this.#failAction(failure)
        return null
      }
      if (!(await this.#askForCredentials(failure))) return null
      try {
        return await action()
      } catch (again) {
        this.#failAction(again)
        return null
      }
    }
  }

  /** Ouvre le panneau et rend la main quand il a répondu. */
  #askForCredentials(failure: AdminError): Promise<boolean> {
    this.pending = {
      kind: failure.status === 409 ? 'first-password' : 'password',
      message: failure.message,
    }
    return new Promise<boolean>((resolve) => {
      this.#answered = resolve
    })
  }

  /**
   * Le panneau répond par un mot de passe.
   *
   * @param password - ce qui a été tapé.
   */
  async answerPassword(password: string): Promise<void> {
    if (await this.login(password)) this.#settle(true)
  }

  /**
   * Le panneau répond par le code de secours de la fiche, et un mot de passe neuf.
   *
   * @param code - les huit caractères de la fiche d'installation.
   * @param password - le mot de passe à poser.
   */
  async answerRecovery(code: string, password: string): Promise<void> {
    if (await this.recover(code, password)) this.#settle(true)
  }

  /** Le panneau est refermé sans réponse : l'acte est abandonné, jamais rejoué. */
  cancelPassword(): void {
    this.#settle(false)
  }

  /**
   * Referme le panneau et rend la main à l'acte qui attendait.
   *
   * La phrase « Session d'administration ouverte » est effacée au passage : s'authentifier
   * n'est pas l'acte, c'en est l'antichambre, et la laisser à l'écran la faisait cohabiter
   * avec le refus de l'enregistrement qu'elle venait d'autoriser — une bonne nouvelle
   * au-dessus d'une mauvaise, toutes deux vraies, et illisibles ensemble.
   */
  #settle(opened: boolean): void {
    this.pending = null
    this.notice = ''
    const answered = this.#answered
    this.#answered = null
    answered?.(opened)
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
    this.actionError = ''
    this.notice = ''
    this.needsFirstPassword = false
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
      this.actionError = ''
      this.lastFaults = []
      return value
    } catch (failure) {
      this.lastFaults = failure instanceof AdminError ? failure.faults : []
      this.#failAction(failure)
      return null
    }
  }

  /**
   * Enregistre un refus qui n'est pas passé par {@link run} ni par {@link load}.
   *
   * Un acte protégé doit LAISSER REMONTER les refus d'authentification, sans quoi
   * {@link protect} ne les voit jamais et ne peut ni demander le mot de passe ni rejouer.
   * Ce qui reste — un 422 et ses quarante-cinq contrôles — s'affiche par ici.
   *
   * @param failure - ce que l'appel a levé.
   */
  report(failure: unknown): void {
    this.lastFaults = failure instanceof AdminError ? failure.faults : []
    this.#failAction(failure)
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
