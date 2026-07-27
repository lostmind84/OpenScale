import * as api from './api'
import type { ConfirmationDTO, FaultDTO } from './dto'
import type { Admin } from './session.svelte'

/**
 * La configuration en cours d'édition, et les cinq étapes de §11.4 vues de l'écran.
 *
 * Un seul objet pour les six pages expertes, parce que le fichier est UN document : la
 * page Matériel et la page Règles éditent deux blocs du même JSON, et deux brouillons
 * séparés finiraient par en écrire un par-dessus l'autre.
 *
 * Ce que cette classe rend visible, et qui n'existe nulle part ailleurs :
 *
 *  - les 45 contrôles refusent TOUS D'UN COUP (§11.3) — un écran qui corrige une faute,
 *    enregistre, et découvre la deuxième est un écran qu'on abandonne ;
 *  - le compte à rebours de 60 s : le poste revient tout seul à la version précédente si
 *    personne ne confirme, et l'écran doit le dire pendant qu'il court ;
 *  - les clés retirées, que §11.3 refuse et que l'écran propose de laisser tomber.
 */
export class Draft {
  /** Le document tel qu'il est édité, ou null avant la première lecture. */
  config = $state<Record<string, unknown> | null>(null)
  /** L'empreinte de la configuration en service, en huit caractères. */
  fingerprint = $state('')
  /** Les clés que ce binaire refuse et que le fichier porte encore (§11.3, contrôle 20). */
  retired = $state<string[]>([])
  /** La confirmation attendue, quand il y en a une. */
  pending = $state<ConfirmationDTO | null>(null)
  /** Ce que les 45 contrôles ont dit du dernier enregistrement refusé. */
  faults = $state<FaultDTO[]>([])
  /** Vrai dès qu'un champ a bougé : le bouton « Enregistrer » s'arme. */
  dirty = $state(false)

  constructor(private readonly admin: Admin) {}

  /** Lit la configuration en service. */
  async load(): Promise<void> {
    const body = await this.admin.load(() => api.fetchConfig())
    if (body === null) return
    this.config = body.config
    this.fingerprint = body.config_fingerprint
    // `?? []` bien que le service ne serve plus `null` : ce poste peut tourner un binaire
    // plus ancien, et c'est exactement ce `null` qui rendait l'administration inatteignable.
    this.retired = body.retired_keys ?? []
    this.pending = body.pending_confirmation
    this.faults = []
    this.dirty = false
  }

  /**
   * Lit une valeur par son chemin pointé, « scale.options.port ».
   *
   * @param path - le chemin de la clé dans le document.
   */
  value(path: string): unknown {
    let node: unknown = this.config
    for (const key of path.split('.')) {
      if (node === null || typeof node !== 'object') return undefined
      node = (node as Record<string, unknown>)[key]
    }
    return node
  }

  /** Une valeur en texte, vide quand la clé n'est pas là. */
  text(path: string): string {
    const value = this.value(path)
    return value === undefined || value === null ? '' : String(value)
  }

  /** Une valeur en nombre, zéro quand la clé n'est pas là. */
  number(path: string): number {
    const value = this.value(path)
    return typeof value === 'number' ? value : Number(value ?? 0)
  }

  /** Une valeur en booléen. */
  flag(path: string): boolean {
    return this.value(path) === true
  }

  /**
   * Écrit une valeur par son chemin, en créant les objets manquants.
   *
   * @param path - le chemin de la clé.
   * @param value - ce qu'elle vaut désormais.
   */
  set(path: string, value: unknown): void {
    if (this.config === null) return
    const keys = path.split('.')
    const last = keys.pop()
    if (last === undefined) return
    let node: Record<string, unknown> = this.config
    for (const key of keys) {
      const next = node[key]
      if (next === null || typeof next !== 'object') node[key] = {}
      node = node[key] as Record<string, unknown>
    }
    node[last] = value
    this.dirty = true
  }

  /**
   * Enregistre : validation, écriture atomique, rechargement à chaud (§11.4).
   *
   * @returns vrai quand la configuration est appliquée.
   */
  async save(): Promise<boolean> {
    if (this.config === null) return false
    this.faults = []
    const document = this.config
    const body = await this.admin.load(() => api.saveConfig(document))
    if (body === null) {
      this.faults = faultsOfLastRefusal(this.admin)
      return false
    }
    this.config = body.config
    this.fingerprint = body.config_fingerprint
    // `?? []` bien que le service ne serve plus `null` : ce poste peut tourner un binaire
    // plus ancien, et c'est exactement ce `null` qui rendait l'administration inatteignable.
    this.retired = body.retired_keys ?? []
    this.pending = body.pending_confirmation
    this.dirty = false
    return true
  }

  /** Confirme la configuration en service et arrête le compte à rebours. */
  async confirm(): Promise<void> {
    await this.admin.run(() => api.confirmConfig())
    this.pending = null
  }

  /** Laisse tomber une clé que ce binaire refuse (§11.3, contrôle 20). */
  dropRetired(key: string): void {
    const keys = key.split('.')
    const last = keys.pop()
    if (last === undefined || this.config === null) return
    let node: unknown = this.config
    for (const step of keys) {
      if (node === null || typeof node !== 'object') return
      node = (node as Record<string, unknown>)[step]
    }
    if (node === null || typeof node !== 'object') return
    delete (node as Record<string, unknown>)[last]
    this.retired = this.retired.filter((candidate) => candidate !== key)
    this.dirty = true
  }
}

/**
 * Les fautes du dernier refus.
 *
 * `Admin.load` a déjà mis la phrase du refus dans `error` ; les 45 contrôles voyagent
 * dans le même corps, et {@link api.AdminError} ne les porte pas. Elles sont donc lues
 * ici sur l'erreur elle-même, faute de quoi l'écran dirait « cette configuration ne peut
 * pas être appliquée » sans dire QUEL champ.
 */
function faultsOfLastRefusal(admin: Admin): FaultDTO[] {
  return admin.lastFaults
}
