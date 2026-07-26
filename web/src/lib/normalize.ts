/**
 * Letters Unicode does NOT decompose, and the reason this module needs a table.
 *
 * NFD turns é into e + U+0301 but leaves Œ alone: in Unicode, Œ is a LETTER of
 * the French alphabet, not a typographic ligature like U+FB01 (fi). NFKD does not
 * help either. The values are lowercase because {@link normalize} folds down.
 */
const UNDECOMPOSABLE: Record<string, string> = {
  Œ: 'oe',
  œ: 'oe',
  Æ: 'ae',
  æ: 'ae',
  ß: 'ss',
  Ø: 'o',
  ø: 'o',
}

/** Matches every key of {@link UNDECOMPOSABLE}, and nothing else. */
const UNDECOMPOSABLE_PATTERN = new RegExp(`[${Object.keys(UNDECOMPOSABLE).join('')}]`, 'gu')

/** Matches a Unicode combining mark — the general category Go calls Mn. */
const COMBINING_MARK = /\p{Mn}/u

/** Matches a letter or a decimal digit — the categories Go calls L and Nd. */
const LETTER_OR_DIGIT = /[\p{L}\p{Nd}]/u

/**
 * Folds a product name or a search query to the single form the two are compared
 * against.
 *
 * It is the browser half of a contract whose other half is `domain.Normalize` in
 * Go: the server sends the accent-folded name computed when it serves the
 * catalog, the browser folds only the QUERY, and `web/testdata/normalization.json`
 * freezes both at once. If the two implementations ever disagree, CI breaks
 * (§14.3, §16.1).
 *
 * Five steps, in this order:
 *
 *  1. the letters of {@link UNDECOMPOSABLE} are unfolded by table — without it
 *     « Œuf chocolat lait cœur lacté » stays unreachable from the reduced
 *     keyboard, which offers the 26 letters and nothing else;
 *  2. NFD decomposition, then every combining mark is dropped: the accent folding;
 *  3. the result is lowercased ONE CODE POINT AT A TIME. Down and not up, because
 *     `'ß'.toUpperCase()` is "SS" in JavaScript and "ß" in Go; one code point at
 *     a time, because the full case mappings of JavaScript are context sensitive
 *     — a word-final Σ lowercases to ς, while Go always gives σ. Feeding the
 *     mapping a single code point removes the context, and with it the divergence;
 *  4. anything that is neither a letter nor a digit becomes a space — U+2665,
 *     present in 127 of the 355 real names, the apostrophe, the degree sign, the
 *     asterisk. A space rather than nothing, so that "s/v" reads as two words;
 *  5. runs of spaces collapse to one, and the ends are trimmed.
 *
 * @param s - a product name or a search query, in any Unicode normal form.
 * @returns the folded form: lowercase letters and digits separated by single spaces.
 * @example
 * normalize('♥AA-TOMME DE SAVOIE -MV') // 'aa tomme de savoie mv'
 */
export function normalize(s: string): string {
  const unfolded = s.replace(UNDECOMPOSABLE_PATTERN, (c) => UNDECOMPOSABLE[c] as string)
  const decomposed = unfolded.normalize('NFD')

  let out = ''
  let spacePending = false
  for (const ch of decomposed) {
    if (COMBINING_MARK.test(ch)) {
      // A combining mark carries the accent being dropped. It must not count as a
      // separator either: "e" + U+0301 is one letter, not two.
      continue
    }
    if (LETTER_OR_DIGIT.test(ch)) {
      if (spacePending && out.length > 0) out += ' '
      spacePending = false
      out += ch.toLowerCase()
      continue
    }
    // Deferred: an empty result means leading spaces, and a run of separators
    // writes a single space, only if something follows.
    spacePending = true
  }
  return out
}
