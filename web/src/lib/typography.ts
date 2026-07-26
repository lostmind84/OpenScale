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
 *
 * **What is held constant is the BOX, not the number of lines** (ADR-030). A name
 * is fitted into a block of fixed height, and a smaller body buys more lines
 * inside that same block: 8 characters and 69 characters both come out occupying
 * exactly {@link NAME_BOX_PX}, so 331 tiles draw one single rhythm instead of
 * 331 heights. Fitting to « at most three lines » could not do that — three lines
 * of 34 px and three lines of 18 px are two different tiles.
 */

/** Nominal body of a tile name, in px on the 16 px reference base: 34 px (§14.2). */
export const NAME_SIZE_MAX_PX = 34

/** Smallest body the scale of §14.2 declares, and therefore the floor of the shrink. */
export const NAME_SIZE_MIN_PX = 18

/** Shrink step. Half a pixel is to this screen what 0,1 mm is to the label (§7.3). */
export const NAME_SIZE_STEP_PX = 0.5

/**
 * The width the fit gives back, for what a measurement cannot know exactly.
 *
 * The column width the caller passes comes from `clientWidth`, which is a whole
 * number where a layout is not: 205 may really be 204,6. Half a percent covers
 * that rounding and nothing else — the metrics themselves agree to a tenth of a
 * pixel, once the name is drawn with the same numeral variant the canvas measures
 * with (see `font-variant-numeric` on `.name` in `Tile.svelte`).
 */
export const MEASUREMENT_TOLERANCE = 0.005

/** Leading of a tile name, as the tiles declare it. */
export const NAME_LINE_HEIGHT = 1.15

/**
 * Height of the block a name is fitted into, in px — `--tile-name` of `app.css`.
 *
 * 90 px is two lines at the nominal body and four at the floor, which is what the
 * longest real name — 69 characters — needs. It is the one number that decides
 * the height of every tile in the grid.
 */
export const NAME_BOX_PX = 90

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
 * How many lines of a given body fit in a block of a given height.
 *
 * @param sizePx - the body, in px.
 * @param boxHeightPx - the height of the block, in px.
 * @returns at least one line, whatever the numbers say.
 * @example
 * linesAvailable(34, 88) // 2
 * linesAvailable(18, 88) // 4
 */
export function linesAvailable(sizePx: number, boxHeightPx: number): number {
  return Math.max(1, Math.floor(boxHeightPx / (sizePx * NAME_LINE_HEIGHT)))
}

/**
 * Finds the largest body at which a name fills its block without overflowing it.
 *
 * When even the floor overflows — which no real name does, and a 500 character
 * one would — the floor is returned and THE ROW GROWS, all its tiles together.
 * Truncation is not one of the outcomes: « sans troncature » is the requirement
 * (§14.2), a single grid rhythm is the preference.
 *
 * @param name - the product name, as the catalog spells it.
 * @param contentWidthPx - usable width inside the tile, padding already removed.
 * @param measure - measures a run of text at {@link REFERENCE_SIZE_PX}.
 * @param boxHeightPx - height of the name block; defaults to {@link NAME_BOX_PX}.
 * @returns a body in px, between {@link NAME_SIZE_MIN_PX} and {@link NAME_SIZE_MAX_PX}.
 * @example
 * fitNameSize('AIL', 207, measureAtReferenceSize) // 34
 */
export function fitNameSize(
  name: string,
  contentWidthPx: number,
  measure: Measurer,
  boxHeightPx: number = NAME_BOX_PX,
): number {
  const pieces = tokenize(name, measure)
  if (pieces.length === 0 || contentWidthPx <= 0) return NAME_SIZE_MAX_PX

  const spaceWidth = spaceAdvance(measure)
  const usable = contentWidthPx * (1 - MEASUREMENT_TOLERANCE)

  for (let size = NAME_SIZE_MAX_PX; size > NAME_SIZE_MIN_PX; size -= NAME_SIZE_STEP_PX) {
    const scale = size / REFERENCE_SIZE_PX
    if (wrappedLines(pieces, spaceWidth, scale, usable) <= linesAvailable(size, boxHeightPx)) {
      return size
    }
  }
  return NAME_SIZE_MIN_PX
}

/**
 * A run of text the browser will not break inside, and how it joins the previous
 * one: after a space, or straight on.
 */
interface Piece {
  width: number
  /** Its length in characters, which is what a break inside it is counted in. */
  length: number
  /** True when a space separates it from the piece before. */
  spaced: boolean
}

/**
 * Where a browser may break a word: after a hyphen, and nowhere else inside one.
 *
 * MEASURED rather than read off UAX #14, because the two disagree. With
 * `overflow-wrap: normal`, « abc-def » in a box too narrow for it takes two lines
 * and « abc/def » takes one and overflows: the hyphen is a break opportunity in
 * Chrome, the solidus is not — so « CRANBERRY/CANNEBERGES » and « MANGUE/UNITE »
 * are single unbreakable pieces, and a model that split them would size those
 * names a line short of what it draws.
 *
 * The digit is rule LB25: inside a number, « 25-30 » holds together.
 *
 * Present in the real catalog in « ♥AA-LA TOMME », « Arc-en-Ciel », « ORLOFF- »
 * and « PERIGORD-MV ».
 */
const BREAK_AFTER = /(?<=[-‐–—])(?!\d)/u

/**
 * Cuts a name into the pieces the browser shapes and breaks it into.
 *
 * Measuring « Arc-en-Ciel » whole is wrong TWICE, and both errors point the same
 * way. The browser may break after each hyphen, which this model has to know; and
 * because it shapes each piece separately it loses the kerning a canvas keeps
 * across the hyphen, so the same word lays out **5,9 % wider** than a canvas says.
 * Measuring the pieces reproduces both.
 */
function tokenize(name: string, measure: Measurer): Piece[] {
  const pieces: Piece[] = []
  for (const word of name.split(/\s+/u).filter((w) => w.length > 0)) {
    const parts = word.split(BREAK_AFTER).filter((p) => p.length > 0)
    for (const [index, part] of parts.entries()) {
      pieces.push({ width: measure(part), length: [...part].length, spaced: index === 0 })
    }
  }
  return pieces
}

/**
 * The advance of a space, measured BETWEEN two letters and never on its own.
 *
 * `measureText(' ')` is not the width of a space in a line of text: a canvas
 * measures a string made only of whitespace differently, and on Inter it answers
 * 23,7 px where the layout uses 26,9 — 3,2 px short at the reference body, 1,1 px
 * at 34 px. One pixel per word gap is precisely what made « GRAINES DE CHIA BIO »
 * fit on two lines in the model and take three in the browser. Measuring the
 * difference between « a a » and « aa » gives the number the layout uses.
 *
 * @param measure - measures a run of text at {@link REFERENCE_SIZE_PX}.
 */
function spaceAdvance(measure: Measurer): number {
  return Math.max(0, measure('a a') - measure('aa'))
}

/**
 * Counts the lines a greedy wrap produces, the way the browser wraps here.
 *
 * A piece wider than the box is BROKEN, not left to overflow: the tiles declare
 * `overflow-wrap: break-word`, so a 500 character word takes as many lines as it
 * needs, while a piece that would fit on a line of its own is moved down whole
 * rather than split. Modelling it any other way would let such a name keep its
 * nominal body and run out of its tile sideways — which is the one failure this
 * whole module exists to prevent.
 */
function wrappedLines(
  pieces: Piece[],
  spaceWidth: number,
  scale: number,
  contentWidthPx: number,
): number {
  let lines = 1
  let used = 0
  for (const piece of pieces) {
    const scaled = piece.width * scale
    const glue = piece.spaced ? spaceWidth * scale : 0
    if (used > 0 && used + glue + scaled <= contentWidthPx) {
      used += glue + scaled
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
    lines += brokenLines(scaled, piece.length, contentWidthPx) - 1
    // The line a broken piece ends on is counted FULL, not with the remainder it
    // really leaves. A browser breaks a word between two glyphs, not at the
    // pixel: « COURGETTE » is 207,7 px wide in a 205 px tile, and the second line
    // starts with a whole « E » where this arithmetic would leave 2,7 px and let
    // the next word in.
    used = contentWidthPx
  }
  return lines
}

/**
 * How many lines a piece too wide for its column is broken into.
 *
 * Not `width / column`: a browser breaks BETWEEN GLYPHS, so every line but the
 * last stops short by up to one character. « CRANBERRY/CANNEBERGES » is 402 px in
 * a 205 px tile — 1,96 columns — and Chrome draws it on THREE lines of 187, 196
 * and 19 px. Counting characters per line reproduces that: 205 px holds ten of
 * its twenty-one characters, so it takes three lines and not two.
 *
 * @param scaledWidth - the width of the piece at the body being tried.
 * @param length - its length in characters.
 * @param contentWidthPx - the width of one line.
 */
function brokenLines(scaledWidth: number, length: number, contentWidthPx: number): number {
  const perLine = Math.max(1, Math.floor((contentWidthPx * length) / scaledWidth))
  return Math.ceil(length / perLine)
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
