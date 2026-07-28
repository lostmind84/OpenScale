/** La clé sous laquelle le navigateur garde les préférences de cet écran. */
const STORAGE_KEY = 'openscale.admin.preferences'

/** Ce que le navigateur retient quand les noms techniques sont demandés. */
const TECHNICAL = 'technical'

/**
 * Ce que la personne qui conduit l'écran a choisi de voir.
 *
 * Dans le NAVIGATEUR et non dans la configuration du poste : ce n'est pas un réglage de
 * magasin, aucun contrôle ne le valide, et il suit celui qui règle plutôt que la machine
 * qu'il règle. Un poste n'a donc rien de plus à écrire, à valider ni à recharger.
 */
class Preferences {
  /**
   * Vrai quand l'écran montre les clés de configuration, les noms de blocs et les codes
   * techniques.
   *
   * Décoché par défaut : 99 % des personnes devant cet écran ne sont pas développeuses,
   * et « limits.max_weight_g » sous un champ nommé « Poids maximum accepté » n'apprend
   * rien à qui n'ouvrira jamais le fichier.
   */
  showTechnicalNames = $state(read())

  /** Bascule l'affichage des noms techniques et s'en souvient. */
  toggleTechnicalNames(): void {
    this.showTechnicalNames = !this.showTechnicalNames
    write(this.showTechnicalNames)
  }
}

/**
 * Lit la préférence, et répond « non » à la moindre difficulté.
 *
 * Un navigateur de kiosque peut refuser le stockage local — mode privé, quota, stratégie
 * de groupe —, et une exception levée ici emporterait le montage de tout l'écran.
 */
function read(): boolean {
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) === TECHNICAL
  } catch {
    return false
  }
}

/** Écrit la préférence, et se tait quand le navigateur refuse. */
function write(technical: boolean): void {
  try {
    if (technical) globalThis.localStorage?.setItem(STORAGE_KEY, TECHNICAL)
    else globalThis.localStorage?.removeItem(STORAGE_KEY)
  } catch {
    // Un écran qui ne se souvient pas reste un écran qui marche.
  }
}

/** La préférence de cette session d'administration. */
export const preferences = new Preferences()
