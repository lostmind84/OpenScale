import { GRID_COLUMNS_AUTO } from '../../lib/grid'
import { NAME_SIZE_MIN_PX } from '../../lib/typography'
import { frenchInteger } from './format'
import type { ReadState } from './read-state'

/**
 * What the drafted client grid comes to, put into French.
 *
 * Not one sentence here decides anything: the numbers come from the browser, which is the
 * only side that knows how many columns `auto-fill` makes of a `clamp()` on this screen.
 * What lives here is the READING of those numbers — the plurals, the order in which they
 * are said, and the price each density is paid at — and it lives apart from the page so
 * that it can be held to the word with no layout at all.
 */

/**
 * What the layout answered about the draft grid.
 *
 * The page keeps `null` for « it has answered nothing »: jsdom lays nothing out, an old
 * browser may lay out something else, and a screen that states a count it has not read is
 * the one failure the whole panel is written against.
 */
export interface ScreenCount {
  columns: number
  /** Whole rows of the height the client grid gives its tiles. */
  rows: number
  /** Usable width inside one tile, its padding and border already removed. */
  contentWidthPx: number
  /** Height of the block a name is fitted into, at the draft. */
  nameBoxPx: number
  /** What the tile is scaled by, and therefore where a name starts its shrink. */
  tileScale: number
}

/** How many names come out at the floor, and how many rows carry one. */
export interface FloorReached {
  names: number
  rows: number
}

/** The window the count is true of. */
export interface Viewport {
  width: number
  height: number
}

/** Everything the sentences below are drawn from. */
export interface GridDraft {
  /** The column setting AS DRAFTED, {@link GRID_COLUMNS_AUTO} for automatic. */
  columns: number
  /** What the browser answered, or null while it has answered nothing. */
  layout: ScreenCount | null
  /** How many tiles the grid would draw at the draft, the by-unit switch included. */
  tileCount: number
  /** What the name measurement found, or null when nothing could be measured. */
  floor: FloorReached | null
  viewport: Viewport
}

/** « 7 colonnes × 3 rangées », the two numbers in the words of the request. */
export function gridSize(layout: ScreenCount): string {
  const columns = `${frenchInteger(layout.columns)} ${layout.columns > 1 ? 'colonnes' : 'colonne'}`
  const rows = `${frenchInteger(layout.rows)} ${layout.rows > 1 ? 'rangées' : 'rangée'}`
  return `${columns} × ${rows}`
}

/**
 * What the draft grid comes to, in French, and what it costs.
 *
 * It follows the DRAFT and not the saved file, so the trade-off is read before the save
 * rather than after — and it states nothing the screen has not read. Column count first,
 * because that is the word the request arrived in.
 *
 * @param draft - the setting, what the browser answered, and what it was asked of.
 */
export function gridSentences(draft: GridDraft): string[] {
  const layout = draft.layout
  const lines: string[] = []

  if (draft.columns === GRID_COLUMNS_AUTO) {
    lines.push(
      layout === null
        ? 'Automatique : la grille suit la largeur de l’écran. Un écran plus large en ' +
            'montre davantage sans qu’on y revienne.'
        : `Automatique : ${gridSize(layout)} sur cet écran. Un écran plus large en montrera ` +
            'davantage sans qu’on y revienne.',
    )
    return lines
  }

  if (layout === null) {
    lines.push(
      `${frenchInteger(draft.columns)} ${draft.columns > 1 ? 'colonnes' : 'colonne'} sur tous ` +
        'les écrans. Cet écran ne sait pas dire combien de rangées cela fait ici.',
    )
    return lines
  }

  const seen = layout.columns * layout.rows
  lines.push(
    `${gridSize(layout)} — ${frenchInteger(seen)} ${seen > 1 ? 'tuiles' : 'tuile'} d’un coup, ` +
      `sur cet écran (${String(draft.viewport.width)} × ${String(draft.viewport.height)}).`,
  )
  if (draft.tileCount > 0) {
    const screens = Math.ceil(draft.tileCount / seen)
    lines.push(
      draft.tileCount > 1
        ? `Les ${frenchInteger(draft.tileCount)} tuiles de la grille tiennent en ` +
            `${frenchInteger(screens)} ${screens > 1 ? 'écrans' : 'écran'}.`
        : 'La seule tuile de la grille tient en un écran.',
    )
  }
  // What ADR-025 demands of a setting: not what it buys, what it costs. High densities
  // are paid for in uneven rows, and that has to be legible BEFORE the save, not
  // discovered on the station afterwards.
  const floor = draft.floor
  if (floor !== null && floor.names > 0) {
    const many = floor.names > 1
    lines.push(
      `${frenchInteger(floor.names)} ${many ? 'noms' : 'nom'} sur ` +
        `${frenchInteger(draft.tileCount)} ${many ? 'atteignent' : 'atteint'} le plancher ` +
        `de ${frenchInteger(NAME_SIZE_MIN_PX)} px : ` +
        (floor.rows > 1
          ? `leurs ${frenchInteger(floor.rows)} rangées peuvent être plus hautes que les autres.`
          : 'leur rangée peut être plus haute que les autres.'),
    )
  }
  return lines
}

/**
 * What this station does with its by-unit products, in one French sentence.
 *
 * It follows the SWITCH and not the saved file, so that the trade-off is legible before
 * the save rather than after. And it states nothing the page has not read: a station
 * whose client catalog could not be opened does not know this number.
 *
 * @param state - where the read of the client catalog stands.
 * @param count - how many products of the catalog in service are sold by unit.
 * @param shown - whether the draft leaves their tiles in the grid.
 */
export function byUnitSentence(state: ReadState, count: number, shown: boolean): string {
  if (state === 'loading') return 'Lecture du catalogue en service…'
  if (state === 'unread') {
    return (
      'Le catalogue en service n’a pas pu être lu : cet écran ne sait pas combien de ' +
      'produits se vendent à l’unité.'
    )
  }
  if (count === 0) {
    return 'Aucun produit vendu à l’unité dans le catalogue en service.'
  }
  const many = count > 1
  const subject = many ? 'produits vendus à l’unité sont' : 'produit vendu à l’unité est'
  const said = shown
    ? `${many ? 'montrés' : 'montré'} dans la grille de ce poste`
    : `${many ? 'masqués' : 'masqué'} sur ce poste`
  return `${frenchInteger(count)} ${subject} ${said}.`
}

/**
 * The sentence that keeps « sur cet écran » honest, empty when it is.
 *
 * The administration is reachable from a laptop over the network, and the count is then
 * true of the laptop and not of the station. Zero extra data, zero extra route: the case
 * is NAMED instead of being silently wrong.
 *
 * @param hostname - the host the administration is being read from.
 */
export function otherScreenWarning(hostname: string): string {
  if (['localhost', '127.0.0.1'].includes(hostname)) return ''
  return 'Cet écran n’est pas celui du poste : ce compte vaut pour l’écran que vous lisez.'
}
