/**
 * Lazily-loaded administration bundle.
 *
 * Loaded on a three second press in the neutral bottom-right corner, INTO THE SAME
 * WINDOW: once loaded it shares the JS context of the client screen — same
 * `window`, same `window.onerror`, same heap. What the split really guarantees,
 * and what CI measures, is (a) the weight — the « < 110 ko gzip » budget of the
 * client screen contains no administration byte — and (b) the loading: on a
 * station that never opens it, which is the nominal case all day long, this file
 * is neither downloaded, nor parsed, nor executed (§14.1).
 *
 * Its contents belong to lot L8. What is here is the door, not the room.
 */

/**
 * Renders the administration screen inside a host element.
 *
 * @param host - where to append the screen, normally `document.body`.
 */
export function mountAdmin(host: HTMLElement): void {
  if (host.querySelector('[data-admin]') !== null) return
  const screen = document.createElement('div')
  screen.dataset.admin = ''
  screen.className = 'fatal'
  const title = document.createElement('h1')
  title.textContent = 'Administration'
  const detail = document.createElement('p')
  detail.textContent = 'Cet écran est livré par le lot L8.'
  const close = document.createElement('button')
  close.type = 'button'
  close.className = 'touch-target'
  close.textContent = 'Fermer'
  close.addEventListener('click', () => screen.remove())
  screen.append(title, detail, close)
  host.appendChild(screen)
}
