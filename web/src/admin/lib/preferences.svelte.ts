/** The key under which the browser keeps the preferences of this screen. */
const STORAGE_KEY = 'openscale.admin.preferences'

/** What the browser remembers when the technical names are asked for. */
const TECHNICAL = 'technical'

/**
 * What the person driving the screen has chosen to see.
 *
 * In the BROWSER and not in the station's configuration: this is no shop setting, no
 * check validates it, and it follows the person doing the settings rather than the
 * machine being set. A station therefore has nothing more to write, to validate, or to
 * reload.
 */
class Preferences {
  /**
   * True when the screen shows the configuration keys, the block names and the technical
   * codes.
   *
   * Unticked by default: 99 % of the people standing in front of this screen are not
   * developers, and « limits.max_weight_g » under a field named « Poids maximum accepté »
   * teaches nothing to someone who will never open the file.
   */
  showTechnicalNames = $state(read())

  /** Toggles the display of the technical names, and remembers it. */
  toggleTechnicalNames(): void {
    this.showTechnicalNames = !this.showTechnicalNames
    write(this.showTechnicalNames)
  }
}

/**
 * Reads the preference, and answers « no » at the slightest difficulty.
 *
 * A kiosk browser may refuse local storage — private mode, quota, group policy — and an
 * exception thrown here would carry away the mounting of the whole screen.
 */
function read(): boolean {
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) === TECHNICAL
  } catch {
    return false
  }
}

/** Writes the preference, and keeps quiet when the browser refuses. */
function write(technical: boolean): void {
  try {
    if (technical) globalThis.localStorage?.setItem(STORAGE_KEY, TECHNICAL)
    else globalThis.localStorage?.removeItem(STORAGE_KEY)
  } catch {
    // A screen that does not remember is still a screen that works.
  }
}

/** The preference of this administration session. */
export const preferences = new Preferences()
