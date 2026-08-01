import type { Catalog } from './catalog'
import type { WeighRequest } from './dto'

/**
 * The four calls the client screen makes, and their exact routes (§14.5).
 *
 * They all return quickly by construction: printing is asynchronous of HTTP, the
 * `POST` answers in under 5 ms and the result comes back through the stream. A
 * silent printer never makes a browser request time out (§4).
 */

/** Fetches the complete catalog. There is no page, no cursor and no cap. */
export async function fetchCatalog(): Promise<Catalog> {
  const response = await fetch('/api/v1/catalog', { headers: { accept: 'application/json' } })
  if (!response.ok) throw new Error(`GET /api/v1/catalog: ${response.status}`)
  return (await response.json()) as Catalog
}

/** Submits one tap. The key makes a repeat harmless (§4). */
export async function weigh(request: WeighRequest): Promise<void> {
  await post('/api/v1/weigh', request)
}

/** Asks for the last label again, inside the reprint window (§8.5). */
export async function reprint(jobID: string, key: string): Promise<void> {
  await post('/api/v1/reprint', { job_id: jobID, key })
}

/** Abandons whatever is in progress and returns the station to rest. */
export async function cancel(): Promise<void> {
  await post('/api/v1/cancel', {})
}

/** Acknowledges a fault the customer has read. */
export async function dismiss(): Promise<void> {
  await post('/api/v1/dismiss', {})
}

/**
 * Reports an uncaught browser error to the technical log.
 *
 * It is deliberately fire-and-forget: the screen is already reloading, and a
 * failure to report an error must not become a second error.
 *
 * @param message - the message of the error.
 * @param stack - its stack, when the browser gave one.
 */
export function reportUIError(message: string, stack: string): void {
  void fetch('/api/v1/ui/error', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ message, stack }),
    keepalive: true,
  }).catch(() => {
    /* Nothing to do: the reload is what repairs the screen. */
  })
}

/**
 * Reports a browser LAYOUT notice, which is not a crash.
 *
 * Its own route rather than a flag on the one above: the station journals the two at
 * different levels and under different codes, and a screen that reported them through
 * the same door made a healthy station write « Erreur JavaScript » once per page load.
 *
 * @param message - what the browser said, verbatim. There is no stack: a notice has none.
 */
export function reportLayoutNotice(message: string): void {
  void fetch('/api/v1/ui/layout-notice', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ message }),
    keepalive: true,
  }).catch(() => {
    /* Nothing to do: the screen is fine, and this line is only a clue for later. */
  })
}

/** Posts a JSON body and refuses anything that is not accepted. */
async function post(route: string, body: unknown): Promise<void> {
  const response = await fetch(route, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!response.ok) throw new Error(`POST ${route}: ${response.status}`)
}
