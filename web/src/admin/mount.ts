import { mount, unmount } from 'svelte'
import App from './App.svelte'

/**
 * Le bundle d'administration, chargé PARESSEUSEMENT.
 *
 * Chargé sur un appui de trois secondes dans le coin bas-droit neutre, DANS LA MÊME
 * FENÊTRE : une fois chargé il partage le contexte JS de l'écran client — même `window`,
 * même `window.onerror`, même tas. Ce que la séparation garantit réellement, et que la CI
 * mesure, c'est (a) le poids — le budget « < 110 ko gzip » de l'écran client ne contient
 * aucun octet d'administration — et (b) le chargement : sur un poste qui ne l'ouvre jamais,
 * ce qui est le cas nominal toute la journée, ce fichier n'est ni téléchargé, ni analysé,
 * ni exécuté (§14.1).
 */

/** Ce qui est monté en ce moment : un deuxième appui long ne monte pas un doublon. */
let opened: { component: unknown; host: HTMLElement } | null = null

/**
 * Monte l'écran d'administration dans un élément hôte.
 *
 * @param host - où l'accrocher, normalement `document.body`.
 */
export function mountAdmin(host: HTMLElement): void {
  if (opened !== null || host.querySelector('[data-admin]') !== null) return
  const target = document.createElement('div')
  host.appendChild(target)
  const component = mount(App, { target, props: { onclose: closeAdmin } })
  opened = { component, host: target }
}

/**
 * Retire l'écran d'administration et rend la grille.
 *
 * C'est la seule sortie sur un poste en kiosque : il n'y a derrière cet écran ni bureau, ni
 * barre des tâches, ni `Alt+F4` (§15.2).
 */
export function closeAdmin(): void {
  if (opened === null) return
  const { component, host } = opened
  opened = null
  void unmount(component as Parameters<typeof unmount>[0])
  host.remove()
}
