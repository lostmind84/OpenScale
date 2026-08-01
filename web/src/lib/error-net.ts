import { reportLayoutNotice, reportUIError } from './api'

/**
 * The last-resort net of the client screen: ERR-UI-01 (§14.3).
 *
 * It lives in `lib/` rather than in `main.ts` for one reason, and it is the reason the
 * defect below shipped: `main.ts` mounts the application on import, so nothing could
 * exercise the net without starting a whole screen. What no test can reach, no test
 * guards.
 */

/** Seconds an unrecoverable browser error stays on screen before the reload. */
const RELOAD_AFTER_S = 5

/**
 * The messages a browser sends through `window.onerror` that are NOT exceptions.
 *
 * A `ResizeObserver` that has not settled within the frame is reported this way, and
 * the specification says so: it is a NOTICE that the next delivery was skipped, not a
 * failure of the page. No exception is thrown, no stack exists, and there is nothing
 * for a volunteer to do about it.
 *
 * MEASURED 01/08/2026 on the pilot station. `ui.grid_columns` pinned to ten put the
 * grid's measure-then-style cycle out of step, the browser said so, and the net read
 * the notice as a crash: veil, reload, re-layout, same notice — 43 journal entries at
 * 5,12 s apart, which is `RELOAD_AFTER_S` exactly. Ten columns was only the trigger:
 * ANY layout hiccup — a screen replugged, a rotation — blanked a self-service station
 * and reloaded it forever.
 *
 * Matched on the PREFIX because the tail of the sentence differs between browsers and
 * between versions, and a station that stopped recognising the notice would fall
 * straight back into the loop.
 */
const LAYOUT_NOTICE_PREFIX = 'ResizeObserver loop'

/**
 * Catches what escaped, says so in French, reports it and reloads.
 *
 * The reload is what repairs the screen: a volunteer must never find a station
 * frozen on a blank page. The hard guarantee — an expired weight is never
 * printed — is held on the Go side and owes nothing to this (§13.2).
 *
 * @returns the teardown that removes both listeners, which only the test bench uses:
 * a station installs the net once and keeps it for the life of the page.
 */
export function installErrorNet(): () => void {
  let firing = false
  let noticed = false

  const fire = (message: string, stack: string): void => {
    if (firing) return
    firing = true
    reportUIError(message, stack)
    showFatalScreen()
    setTimeout(() => location.reload(), RELOAD_AFTER_S * 1000)
  }

  /**
   * Records a layout notice ONCE, and leaves the screen alone.
   *
   * Once, because a grid that does not settle says so on every frame, and swapping a
   * loop of reloads for a loop of journal writes would fix nothing. Recorded at all,
   * because the notice is a real symptom — the grid did not converge — and it is the
   * only line that named this defect. Silence here would have hidden it.
   *
   * Through its OWN route, so the station journals ERR-UI-02 at warn level rather than
   * « Erreur JavaScript dans l'écran client » at error level, which was true of neither.
   */
  const notice = (message: string): void => {
    if (noticed) return
    noticed = true
    reportLayoutNotice(message)
  }

  const onError = (e: ErrorEvent): void => {
    // The stack is what separates the two: an exception carries one, a notice never
    // does. The prefix alone would silence « ResizeObserver loop » read off a real
    // TypeError, which is an exception like any other.
    if (e.error === undefined || e.error === null) {
      if (e.message.startsWith(LAYOUT_NOTICE_PREFIX)) {
        notice(e.message)
        return
      }
    }
    fire(e.message, e.error?.stack ?? '')
  }
  const onRejection = (e: PromiseRejectionEvent): void => {
    fire(String(e.reason), (e.reason as Error | undefined)?.stack ?? '')
  }

  window.addEventListener('error', onError)
  window.addEventListener('unhandledrejection', onRejection)
  return () => {
    window.removeEventListener('error', onError)
    window.removeEventListener('unhandledrejection', onRejection)
  }
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
