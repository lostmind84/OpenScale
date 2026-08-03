/**
 * A raw scale frame, read twice: as bytes, and as a human reads it.
 *
 * This is the « dump hexa + ASCII » of `openscale capture` (§15.1) carried to the screen.
 * It lives in a module of its own because it is a pure translation of bytes — no state,
 * no layout — and because a mute station and a station that speaks without being
 * understood call for two opposite gestures, which makes this reading the one thing a
 * support call starts from.
 */

/** Les caractères de commande qu'une trame de balance porte (§9.1), par leur nom. */
const CONTROL_NAMES: Record<number, string> = {
  0: 'NUL',
  2: 'STX',
  3: 'ETX',
  4: 'EOT',
  5: 'ENQ',
  6: 'ACK',
  9: 'TAB',
  10: 'LF',
  13: 'CR',
  21: 'NAK',
  27: 'ESC',
  127: 'DEL',
}

/**
 * Les octets d'une trame, en hexadécimal : ce qu'un support demande d'abord.
 *
 * @param frame - une trame brute, telle que le poste l'a lue sur le câble.
 */
export function hexOf(frame: string): string {
  return [...new TextEncoder().encode(frame)]
    .map((byte) => byte.toString(16).toUpperCase().padStart(2, '0'))
    .join(' ')
}

/**
 * La trame DÉCODÉE : les mêmes octets tels qu'un humain les lit.
 *
 * Les caractères de commande sont NOMMÉS plutôt que rendus : c'est le STX manquant ou le
 * CR en trop qui explique un poste muet, et un caractère invisible ne se lit pas au
 * téléphone.
 *
 * @param frame - une trame brute, telle que le poste l'a lue sur le câble.
 */
export function decodedOf(frame: string): string {
  let read = ''
  for (const character of frame) {
    const code = character.codePointAt(0) ?? 0
    if (code >= 32 && code !== 127) {
      read += character
      continue
    }
    read += `⟨${CONTROL_NAMES[code] ?? code.toString(16).toUpperCase().padStart(2, '0')}⟩`
  }
  return read
}
