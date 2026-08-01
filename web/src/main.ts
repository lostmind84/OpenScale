import { mount } from 'svelte'
import App from './App.svelte'
import { installErrorNet } from './lib/error-net'
import './app.css'

/**
 * Entry point of the client screen.
 *
 * Everything defensive a kiosk needs is here, and nowhere else: a JS bug never
 * leaves a dead screen, and no gesture of a browser survives on a self-service
 * station (§14.3, « Robustesse côté navigateur »).
 */

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

