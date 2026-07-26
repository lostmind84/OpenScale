/**
 * Making a colour written by a human in a configuration file behave on screen.
 *
 * `categories[].color` comes from the station configuration, not from the code
 * (§14.3-2): someone picks four hexadecimal values in a JSON file, months apart,
 * with no preview. The ochre of the shipped file, `#B7950B`, reaches **2,7:1** on
 * white — a category letter drawn in it is unreadable at 80 cm, and no amount of
 * care in the CSS can rescue it, because the CSS never sees the value.
 *
 * So the screen never uses a configured colour as it is given. It uses a WASH of
 * it to identify a shelf, and a DARKENED form of it wherever the colour has to be
 * read. Both are computed here, from the value received, at render time.
 */

/** Contrast a category letter must reach against the surface it sits on. */
const MIN_CONTRAST = 4.5

/** How much of the hue survives in a wash. Enough to name a shelf, not to shout. */
const WASH_ALPHA = 0.1

/** Shown when a configured colour is missing or unreadable: the `--waiting` grey. */
const FALLBACK: RGB = { r: 0x8a, g: 0x86, b: 0x7c }

/** A colour, once parsed. */
interface RGB {
  r: number
  g: number
  b: number
}

/**
 * Parses `#rgb` or `#rrggbb`, and falls back rather than throwing.
 *
 * A wrong colour in a configuration file must cost a grey tile, never a screen
 * that does not render: this is the client screen of a self-service station.
 */
function parse(hex: string): RGB {
  const short = /^#([\da-f])([\da-f])([\da-f])$/iu.exec(hex)
  if (short !== null) {
    const [, r, g, b] = short as unknown as [string, string, string, string]
    return { r: Number.parseInt(r + r, 16), g: Number.parseInt(g + g, 16), b: Number.parseInt(b + b, 16) }
  }
  const long = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/iu.exec(hex)
  if (long === null) return FALLBACK
  const [, r, g, b] = long as unknown as [string, string, string, string]
  return { r: Number.parseInt(r, 16), g: Number.parseInt(g, 16), b: Number.parseInt(b, 16) }
}

/** Linearised sRGB channel, WCAG 2.1. */
function channel(value: number): number {
  const c = value / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

/** Relative luminance of a colour, between 0 and 1. */
function luminance({ r, g, b }: RGB): number {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

/** Contrast ratio between a colour and white, between 1 and 21. */
function contrastOnWhite(color: RGB): number {
  return 1.05 / (luminance(color) + 0.05)
}

/**
 * The wash a category paints behind its tiles and its chip.
 *
 * @param hex - the configured colour of the category.
 * @returns an `rgb()` string, opaque, as it composites over `--surface`.
 * @example
 * wash('#C0392B') // 'rgb(249, 233, 232)'
 */
export function wash(hex: string): string {
  const { r, g, b } = parse(hex)
  const over = (v: number): number => Math.round(255 - WASH_ALPHA * (255 - v))
  return `rgb(${over(r)}, ${over(g)}, ${over(b)})`
}

/**
 * The same colour, darkened until a letter drawn in it can be read.
 *
 * Darkening keeps the hue — an ochre stays an ochre, it only stops being pale —
 * which is what makes the four shelves still tell each other apart afterwards.
 *
 * @param hex - the configured colour of the category.
 * @returns an `rgb()` string reaching at least 4,5:1 on white.
 * @example
 * readable('#B7950B') // darker: the shipped ochre is 2,7:1 as received
 */
export function readable(hex: string): string {
  let color = parse(hex)
  // Sixteen steps of 6 % take any colour to black, and black is 21:1: the loop
  // therefore always ends, and it ends early for a colour that was already fine.
  for (let i = 0; i < 16 && contrastOnWhite(color) < MIN_CONTRAST; i++) {
    color = { r: Math.round(color.r * 0.94), g: Math.round(color.g * 0.94), b: Math.round(color.b * 0.94) }
  }
  return `rgb(${color.r}, ${color.g}, ${color.b})`
}
