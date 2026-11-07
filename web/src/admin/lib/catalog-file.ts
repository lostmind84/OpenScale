/** The file a station watches for when `catalog.options.file_name` says nothing. */
const DEFAULT_PATTERN = 'flv_{n}.csv'

/** What the pattern writes where the station number goes. */
const STATION_PLACEHOLDER = '{n}'

/**
 * The name of the catalog file a station waits for.
 *
 * It mirrors `catalog.FileName` on the Go side: an empty pattern is the default, and every
 * `{n}` becomes the station number. The screen saying another name than the one the
 * station looks for would send a producer to drop a file nobody reads.
 *
 * @param pattern - `catalog.options.file_name`, or undefined/empty when the file has none.
 * @param station - the number of this station.
 */
export function expectedCatalogFile(pattern: string | undefined, station: number): string {
  const chosen = pattern === undefined || pattern === '' ? DEFAULT_PATTERN : pattern
  return chosen.split(STATION_PLACEHOLDER).join(String(station))
}

/**
 * Whether every station using this pattern reads the SAME file.
 *
 * A pattern with no `{n}` names one file for the whole cooperative: in the default
 * deletion mode, the first station to read it takes it away from the others.
 *
 * @param pattern - `catalog.options.file_name`, or undefined/empty when the file has none.
 */
export function sharesOneFile(pattern: string | undefined): boolean {
  if (pattern === undefined || pattern === '') return false
  return !pattern.includes(STATION_PLACEHOLDER)
}
