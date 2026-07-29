import { mount, unmount } from 'svelte'
import App from './App.svelte'

/**
 * The administration bundle, loaded LAZILY.
 *
 * The way in is the settings key at the right of the permanent bottom bar, which ADR-032
 * makes the ONE entry from the station itself: no URL to type, no way out of the kiosk.
 * `App.svelte` imports this module on that press and mounts it IN THE SAME WINDOW, so
 * from that point on it shares the JS context of the client screen — same `window`, same
 * error net installed by `main.ts`, same heap. What the split into two entries really
 * guarantees, and what CI measures, is (a) weight — the « < 110 ko gzip » budget of the
 * client screen carries no administration byte — and (b) loading: on a station that never
 * opens it, which is the nominal case all day long, this file is neither downloaded, nor
 * parsed, nor executed (§14.1).
 *
 * That key is the only thing painted ABOVE the out-of-service overlay (`StatusBar.svelte`),
 * and that is what keeps this module reachable at all: a station comes out of the installer
 * in `OutOfService`, where the overlay would otherwise bury the one entry on the very
 * station that has to be configured.
 *
 * `web/src/admin.ts` mounts the same screen as a document of its own, served on `/admin`.
 * That path is out of reach AT THE STATION — the browser is started with `--kiosk <url>`,
 * so there is no address bar in front of the volunteer — and exists for repair from
 * another machine on the network.
 */

/** What is mounted right now: a second press of the settings key does not mount a duplicate. */
let opened: { component: unknown; host: HTMLElement } | null = null

/**
 * Mounts the administration screen under a host element.
 *
 * @param host - what to hang it under: `document.body` from the client screen,
 *   the `#app` of `admin.html` when the administration is opened as its own page.
 */
export function mountAdmin(host: HTMLElement): void {
  if (opened !== null || host.querySelector('[data-admin]') !== null) return
  const target = document.createElement('div')
  host.appendChild(target)
  const component = mount(App, { target, props: { onclose: closeAdmin } })
  opened = { component, host: target }
}

/**
 * Removes the administration screen and gives the grid back.
 *
 * It is the only exit the screen offers, and the station offers no other in front of the
 * volunteer: the browser is started with `--kiosk`, so there is neither browser chrome nor
 * an address bar to leave by (§15.2).
 */
export function closeAdmin(): void {
  if (opened === null) return
  const { component, host } = opened
  opened = null
  void unmount(component as Parameters<typeof unmount>[0])
  host.remove()
}
