/** Crockford base 32: no I, no L, no O, no U — an alphabet a human can read back. */
const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'

/**
 * Generates the idempotency key of one tap.
 *
 * One key per `pointerdown`, and the Hub keeps the last 32: a double tap, a
 * network replay or a browser retry then yield ONE label, because the second
 * command replays the acknowledgement and executes nothing (§4, §13.2).
 *
 * A ULID rather than a UUID because it sorts by time, which is what makes a
 * support log readable, and because 26 characters fit in a journal column.
 *
 * @returns 26 Crockford base 32 characters: 10 of timestamp, 16 of randomness.
 * @example
 * ulid() // '01J9F2ABCDE5T7M3XQ8W1KZP4R'
 */
export function ulid(): string {
  let time = Date.now()
  const chars = new Array<string>(26)
  for (let i = 9; i >= 0; i--) {
    chars[i] = ALPHABET[time % 32] as string
    time = Math.floor(time / 32)
  }
  const random = new Uint8Array(16)
  crypto.getRandomValues(random)
  for (let i = 0; i < 16; i++) {
    chars[10 + i] = ALPHABET[(random[i] as number) % 32] as string
  }
  return chars.join('')
}
