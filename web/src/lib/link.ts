/**
 * Health of the link between the screen and the service, as a pure function.
 *
 * A browser that has stopped hearing from the station must SAY SO, and it must
 * stop showing a weight nobody is measuring any more. The rule is worth isolating
 * from the transport because it is the one thing here that can be got wrong
 * silently — and because a pure function is testable without a single timer
 * (§14.3, « Robustesse côté navigateur »).
 *
 * The hard guarantee is elsewhere: even a frozen browser cannot make an expired
 * weight print, because that is held on the Go side (§13.2). What this file
 * protects is the customer's belief in what they are reading.
 */

/** Milliseconds of silence after which the weight is no longer shown. */
export const WEIGHT_HIDDEN_AFTER_MS = 1_500

/** Milliseconds of silence after which « Poids indisponible » becomes visible. */
export const UNAVAILABLE_AFTER_MS = 2_000

/** Milliseconds of silence after which the stream is torn down and reopened. */
export const RECONNECT_AFTER_MS = 5_000

/** What the screen must do about the link, right now. */
export interface LinkHealth {
  /** Show the weight, or hide it because it may no longer be true. */
  showWeight: boolean
  /** French banner line, empty when there is nothing to say about the link. */
  banner: string
  /** Tear the stream down and open a new one: waiting longer has stopped helping. */
  mustReconnect: boolean
}

/**
 * Decides what the screen shows about a link that has been silent for a while.
 *
 * @param silentForMs - milliseconds since the last message, heartbeat included.
 * @param streamOpen - whether the `EventSource` believes it is connected.
 * @returns what to show and whether to force a reconnection.
 * @example
 * linkHealth(2_100, true).banner // 'Poids indisponible'
 */
export function linkHealth(silentForMs: number, streamOpen: boolean): LinkHealth {
  if (!streamOpen) {
    return {
      showWeight: silentForMs < WEIGHT_HIDDEN_AFTER_MS,
      banner: 'Reconnexion…',
      mustReconnect: silentForMs >= RECONNECT_AFTER_MS,
    }
  }
  if (silentForMs >= RECONNECT_AFTER_MS) {
    return { showWeight: false, banner: 'Poids indisponible', mustReconnect: true }
  }
  if (silentForMs >= UNAVAILABLE_AFTER_MS) {
    return { showWeight: false, banner: 'Poids indisponible', mustReconnect: false }
  }
  if (silentForMs >= WEIGHT_HIDDEN_AFTER_MS) {
    return { showWeight: false, banner: '', mustReconnect: false }
  }
  return { showWeight: true, banner: '', mustReconnect: false }
}
