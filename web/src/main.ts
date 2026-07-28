import { mount } from 'svelte'
import App from './App.svelte'
import { reportUIError } from './lib/api'
import './app.css'

/**
 * Entry point of the client screen.
 *
 * Everything defensive a kiosk needs is here, and nowhere else: a JS bug never
 * leaves a dead screen, and no gesture of a browser survives on a self-service
 * station (§14.3, « Robustesse côté navigateur »).
 */

/** Seconds an unrecoverable browser error stays on screen before the reload. */
const RELOAD_AFTER_S = 5

installErrorNet()
// A self-service station is not a document: no context menu, no drag, no zoom. The
// administration is one, though, and it opens in this same window (§14.1): « Copier » on
// the detail of an error is what a volunteer on the telephone is being asked for, and a
// station driven with a mouse has no other way to reach it.
document.addEventListener('contextmenu', (e) => {
  if (insideAdmin(e.target)) return
  e.preventDefault()
})
document.addEventListener('dragstart', (e) => e.preventDefault())

mount(App, { target: document.getElementById('app') as HTMLElement })

/**
 * Whether an event happened inside the administration screen.
 *
 * @param target - what the event names, which is not always an element.
 */
function insideAdmin(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest('[data-admin]') !== null
}

/**
 * Catches what escaped, says so in French, reports it and reloads.
 *
 * The reload is what repairs the screen: a volunteer must never find a station
 * frozen on a blank page. The hard guarantee — an expired weight is never
 * printed — is held on the Go side and owes nothing to this (§13.2).
 */
function installErrorNet(): void {
  let firing = false
  const fire = (message: string, stack: string): void => {
    if (firing) return
    firing = true
    reportUIError(message, stack)
    showFatalScreen()
    setTimeout(() => location.reload(), RELOAD_AFTER_S * 1000)
  }
  window.addEventListener('error', (e) => fire(e.message, e.error?.stack ?? ''))
  window.addEventListener('unhandledrejection', (e) =>
    fire(String(e.reason), (e.reason as Error | undefined)?.stack ?? ''),
  )
}

/**
 * Replaces the screen with the only sentence that is useful at that point.
 *
 * Built node by node rather than through `innerHTML`: nothing here comes from
 * outside, and a screen that is repairing itself is the last place to leave a
 * parsing path open.
 */
function showFatalScreen(): void {
  const screen = document.createElement('div')
  screen.className = 'fatal'
  screen.append(
    element('h1', 'Une erreur est survenue'),
    element('p', 'L’écran va se recharger tout seul.'),
    element('p', 'ERR-UI-01', 'code'),
  )
  document.body.appendChild(screen)
}

/** Creates one element carrying text and nothing else. */
function element(tag: string, text: string, className = ''): HTMLElement {
  const node = document.createElement(tag)
  node.textContent = text
  if (className !== '') node.className = className
  return node
}
