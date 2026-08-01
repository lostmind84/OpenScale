/**
 * The one place the grid of the client screen is spelled.
 *
 * Two screens draw this declaration: the grid itself, and the invisible probe the
 * administration measures a draft against. They must agree WORD FOR WORD — a probe
 * declaring anything else counts the columns of a grid nobody sees — and they used to
 * be two literals in two files, in two components, with nothing linking them.
 */

/** `ui.grid_columns` when the grid fills itself, which is the default. */
export const GRID_COLUMNS_AUTO = 0

/**
 * What the automatic grid declares, and the string the stylesheet also carries.
 *
 * Exported so a test can hold the two together: the stylesheet of `Grid.svelte` is the
 * single place the DEFAULT is applied — an automatic grid gets no inline style at all —
 * so this constant cannot apply it, only state it.
 */
export const AUTOMATIC_COLUMNS = 'repeat(auto-fill, minmax(var(--tile-min), 1fr))'

/**
 * The column declaration for a setting.
 *
 * `minmax(0, 1fr)` and never `1fr`, which is `minmax(auto, 1fr)`: an `auto` track does
 * not go below the min-content width of what it holds, and what it holds is
 * « CRANBERRY/CANNEBERGES ». At ten columns that alone would lay out a grid wider than
 * the screen, which is how the client screen would gain the horizontal scrollbar a
 * kiosk must never have.
 *
 * @param columns - the setting: {@link GRID_COLUMNS_AUTO}, or the pinned count.
 * @returns the value of `grid-template-columns`.
 */
export function gridTemplateColumns(columns: number): string {
  if (columns === GRID_COLUMNS_AUTO) return AUTOMATIC_COLUMNS
  return `repeat(${String(columns)}, minmax(0, 1fr))`
}
