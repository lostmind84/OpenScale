import type { StateDTO } from './dto'

/**
 * Si le poste peut peser ET imprimer, pour autant qu'il en sache quelque chose.
 *
 * C'est ce que dit la pastille de la barre du bas : verte, elle annonce « Balance et
 * imprimante disponibles » à un bénévole qui n'ouvrira jamais l'administration.
 *
 * # POURQUOI CETTE FONCTION EXISTE
 *
 * Elle s'allumait sur `snapshot === null` et restait allumée pendant tout
 * `initializing` : un poste dont ni la balance ni l'imprimante n'étaient branchées
 * affichait « disponibles » dès l'apparition de l'écran, et le disait d'autant plus
 * longtemps que le matériel manquait. Relevé sur le banc L0 du 29/07/2026.
 *
 * **L'absence d'information n'est pas la santé.** Une pastille dont le vert signifie
 * « rien ne m'a encore contredit » dit l'inverse de la vérité au seul moment où on la
 * regarde — celui où l'on se demande si le poste marche.
 *
 * # CE QUI RESTE VERT, ET POURQUOI CE N'EST PAS LA MÊME CHOSE
 *
 * `printer.health === 'unknown'` reste vert **à dessein**. Sur une file en RAW,
 * l'imprimante prend les octets et ne répond rien (niveau N1, §8.5) : c'est l'état
 * NORMAL et permanent d'un poste sain. Le peindre en orange laisserait les quatre
 * postes du parc orange pour toujours, ce qui apprend à ignorer la pastille.
 *
 * La distinction est celle-ci : `unknown` est une réponse — « ce chemin ne sait pas
 * dire » —, alors que `null` et `initializing` sont l'absence de question posée.
 */
export function hardwareIsHealthy(snapshot: StateDTO | null): boolean {
  if (snapshot === null || snapshot.state === 'initializing') {
    return false
  }
  return snapshot.scale.connected && snapshot.printer.health !== 'faulted'
}
