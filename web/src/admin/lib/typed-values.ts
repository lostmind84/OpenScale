/**
 * What an emptied box must NOT write, and what it gets back instead.
 *
 * `Number('')` is 0. Clearing a threshold used to write `0` — saved by a keystroke that
 * looked like an erasure rather than an edit — and erasing « Dénominateur » put a
 * division by zero into the price of every product. So an empty box writes nothing, the
 * draft keeps what the file holds, and the box is given that value back on the way out.
 *
 * The pair matters: without the restore, the box STAYED wrong on screen while the
 * configuration held something else — every box of the Rules page is driven by `value=`,
 * and an edit that changes no state renders nothing.
 */

/**
 * The number an operator typed, or null when the box holds nothing usable.
 *
 * @param typed - what the box holds.
 */
export function numberTyped(typed: string): number | null {
  const value = Number(typed)
  if (typed.trim() === '' || Number.isNaN(value)) return null
  return value
}

/**
 * The discount an operator typed with a comma or a dot, or null.
 *
 * The second decimal is refused AT THE KEYSTROKE and not at the save: the kernel rejects
 * `10,25` when it decodes, and this screen must not build a file the station will throw
 * back. Same silence as {@link numberTyped} on an empty box, and for the same reason —
 * erasing « Remise » would drop the member discount on every product.
 *
 * @param typed - what the box holds.
 */
export function discountTyped(typed: string): number | null {
  const text = typed.trim().replace(',', '.')
  if (!/^\d{1,3}(\.\d)?$/u.test(text)) return null
  const value = Number(text)
  return value > 100 ? null : value
}

/**
 * Puts back in a box the value the draft actually holds.
 *
 * Restoring on the way OUT rather than on each keystroke is what lets « effacer puis
 * retaper » still work.
 *
 * @param target - what the `focusout` event named.
 * @param stored - what the draft holds for that key.
 */
export function restoreBox(target: EventTarget | null, stored: string): void {
  if (!(target instanceof HTMLInputElement) || target.value === stored) return
  target.value = stored
}
