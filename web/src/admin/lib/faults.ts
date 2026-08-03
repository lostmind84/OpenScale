import type { Draft } from './draft.svelte'

/**
 * Ce qu'un des contrôles de §11.3 a dit d'une clé, lu depuis n'importe quel écran.
 *
 * Les 47 contrôles refusent TOUS D'UN COUP et nomment chacun une CLÉ : une page qui
 * n'afficherait que le bandeau global laisse chercher laquelle. Les quatre pages qui
 * éditent la configuration posaient donc la même question, dans quatre copies de trois
 * lignes — et une copie qui oublie `allowed` jette la moitié du contrôle.
 */

/**
 * Le message du contrôle qui a refusé cette clé, vide quand il n'y en a pas.
 *
 * @param draft - la configuration en cours d'édition.
 * @param path - le chemin pointé de la clé.
 */
export function faultOf(draft: Draft, path: string): string {
  return draft.faults.find((fault) => fault.field === path)?.message ?? ''
}

/**
 * Les valeurs qu'un contrôle a nommées comme acceptables (§11.4, étape 1).
 *
 * « Ce port n'existe pas sur ce poste » sans la liste des ports qui existent est un refus
 * sur lequel personne ne peut agir.
 *
 * @param draft - la configuration en cours d'édition.
 * @param path - le chemin pointé de la clé.
 */
export function allowedFor(draft: Draft, path: string): string[] {
  return draft.faults.find((fault) => fault.field === path)?.allowed ?? []
}
