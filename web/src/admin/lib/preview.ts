/**
 * Why a label preview is missing, said with the ENGINE's own sentence whenever it can be.
 *
 * An `<img>` tag does not hand over the body of a refusal: it fires `error` and says
 * nothing more. The same address is therefore asked once again, as JSON, to read the
 * sentence of the 422 — « le décalage sort de la découpe » — which is the only one that
 * says which dot to give back.
 */

/** What the screen says when the station refused to render and said nothing readable. */
export const RENDER_REFUSED = 'L’aperçu n’a pas pu être rendu par le poste.'

/** What it says when the station DID render the address and the browser showed nothing. */
export const IMAGE_UNREADABLE = 'Le poste a rendu l’aperçu, mais le navigateur ne l’a pas affiché.'

/**
 * What the station answers about an address the browser could not display.
 *
 * @param url - the address to ask about, as JSON this time.
 */
export async function refusalOf(url: string): Promise<string> {
  try {
    const response = await fetch(url, { headers: { accept: 'application/json' } })
    if (response.ok) return IMAGE_UNREADABLE
    const problem = JSON.parse(await response.text()) as { message?: string }
    if (typeof problem.message === 'string' && problem.message !== '') return problem.message
    return RENDER_REFUSED
  } catch {
    // The station answered nothing readable at all: the fallback sentence says what is
    // known, and inventing a cause would say more than that.
    return RENDER_REFUSED
  }
}

/**
 * One number of a document, read by its dotted path; zero when the key is absent.
 *
 * @param document - a configuration exactly as the station serves it.
 * @param path - the dotted key.
 */
export function dotsAt(document: Record<string, unknown>, path: string): number {
  let node: unknown = document
  for (const key of path.split('.')) {
    if (node === null || typeof node !== 'object') return 0
    node = (node as Record<string, unknown>)[key]
  }
  return typeof node === 'number' ? node : 0
}
