/**
 * Fitting a product name inside its tile, without ever truncating it.
 *
 * The name is THE element of the tile: 49 % of the real catalog has no photo, so
 * what a station certainly has is names — 8 to 69 characters, 27 on average
 * (§14.2). It is therefore sized first, and it is never cut: a customer who reads
 * « ♥AA-LA TOMME DES CROQUANTS AFFINE A LA LI… » cannot tell two cheeses apart.
 *
 * The mechanism is the one of the label (§7.3): shrink by small steps down to a
 * floor. The floor is the smallest body the typographic scale of §14.2 declares —
 * 18 px, its « mention légale » — because below that nothing on this screen is
 * meant to be read at 60 to 80 cm.
 */

/** Nominal body of a tile name, in px on the 16 px reference base: 34 px (§14.2). */
export const NAME_SIZE_MAX_PX = 34

/** Smallest body the scale of §14.2 declares, and therefore the floor of the shrink. */
export const NAME_SIZE_MIN_PX = 18

/** Shrink step. Half a pixel is to this screen what 0,1 mm is to the label (§7.3). */
export const NAME_SIZE_STEP_PX = 0.5

/** Lines a tile offers a name before the body has to give way (§14.2). */
export const NAME_MAX_LINES = 3

/**
 * Measures a run of text at a reference body.
 *
 * @param text - the run to measure.
 * @returns its advance width in px at {@link REFERENCE_SIZE_PX}.
 */
export type Measurer = (text: string) => number

/**
 * Body at which a {@link Measurer} works.
 *
 * One measurement per word is enough because the advance of vector text is
 * proportional to the body: measuring at 100 px and scaling avoids re-measuring
 * every word at every candidate size, which for 331 tiles is the difference
 * between imperceptible and visible.
 */
export const REFERENCE_SIZE_PX = 100

/**
 * Finds the largest body at which a name fits its tile in at most three lines.
 *
 * When even the floor needs a fourth line — and the longest real name, 69
 * characters, does — the floor is returned and THE TILE GROWS. Truncation is not
 * one of the outcomes: « sans troncature » is the requirement, « trois lignes »
 * is the preference (§14.2).
 *
 * @param name - the product name, as the catalog spells it.
 * @param contentWidthPx - usable width inside the tile, padding already removed.
 * @param measure - measures a run of text at {@link REFERENCE_SIZE_PX}.
 * @returns a body in px, between {@link NAME_SIZE_MIN_PX} and {@link NAME_SIZE_MAX_PX}.
 * @example
 * fitNameSize('AIL', 239, measureAtReferenceSize) // 34
 */
export function fitNameSize(name: string, contentWidthPx: number, measure: Measurer): number {
  const words = name.split(/\s+/u).filter((w) => w.length > 0)
  if (words.length === 0 || contentWidthPx <= 0) return NAME_SIZE_MAX_PX

  const widths = words.map(measure)
  const spaceWidth = measure(' ')

  for (let size = NAME_SIZE_MAX_PX; size > NAME_SIZE_MIN_PX; size -= NAME_SIZE_STEP_PX) {
    const scale = size / REFERENCE_SIZE_PX
    if (wrappedLines(widths, spaceWidth, scale, contentWidthPx) <= NAME_MAX_LINES) {
      return size
    }
  }
  return NAME_SIZE_MIN_PX
}

/**
 * Counts the lines a greedy word wrap produces, the way the browser wraps here.
 *
 * A word wider than the box is BROKEN, not left to overflow: the tiles declare
 * `overflow-wrap: anywhere`, so a 500 character word takes as many lines as it
 * needs. Modelling it any other way would let such a name keep its nominal body
 * and run out of its tile sideways — which is the one failure this whole module
 * exists to prevent.
 */
function wrappedLines(
  widths: number[],
  spaceWidth: number,
  scale: number,
  contentWidthPx: number,
): number {
  let lines = 1
  let used = 0
  for (const width of widths) {
    const scaled = width * scale
    if (used > 0 && used + spaceWidth * scale + scaled <= contentWidthPx) {
      used += spaceWidth * scale + scaled
      continue
    }
    if (used > 0) {
      lines++
      used = 0
    }
    if (scaled <= contentWidthPx) {
      used = scaled
      continue
    }
    const extra = Math.ceil(scaled / contentWidthPx) - 1
    lines += extra
    used = scaled - extra * contentWidthPx
  }
  return lines
}

/**
 * Builds a {@link Measurer} backed by a canvas, or null when there is none.
 *
 * Returning null rather than an estimate is deliberate: a guessed average advance
 * would be a number nobody measured, and the fallback — every name at its nominal
 * body — is both honest and readable.
 *
 * @param fontFamily - the family stack the tiles are rendered with.
 * @param weight - the weight tile names are rendered at.
 */
export function canvasMeasurer(fontFamily: string, weight: number): Measurer | null {
  const canvas = document.createElement('canvas')
  const context = canvas.getContext('2d')
  if (context === null || typeof context.measureText !== 'function') return null
  context.font = `${weight} ${REFERENCE_SIZE_PX}px ${fontFamily}`
  const cache = new Map<string, number>()
  return (text: string) => {
    const known = cache.get(text)
    if (known !== undefined) return known
    const width = context.measureText(text).width
    cache.set(text, width)
    return width
  }
}
