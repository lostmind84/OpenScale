import { differences, valueAt } from './diff'
import type { Difference } from './diff'

/**
 * What one station's configuration file would change on another, and what it left behind.
 *
 * This is the clone of §11.5 seen from the browser: one operator exports the reference
 * station, carries the file to the three others, and checks that the fingerprint on screen
 * is the same. Everything here is a comparison between two documents — no field, no route,
 * no layout — which is why it is held to the word with no browser at all.
 */

/**
 * The two keys the comparison LEAVES OUT, and why it has to.
 *
 * `modified_at` is stamped by whoever writes the file — `writeConfig` fills it from the
 * station's own clock on every save — so the file carried from station 1 holds the instant
 * station 1 was configured and station 2 holds its own. Compared, the two ALWAYS differ:
 * the table announced « 1 champ qui change » and offered « Recopier ce champ » at the exact
 * moment §11.5 wanted somebody to read « rien ne change ». The fingerprint of §11.5 clears
 * the same field for the same reason (`Config.Fingerprint`), and copying it into the draft
 * would change nothing anyway — the next save overwrites it.
 *
 * `catalog.options.password` is a secret NEITHER document carries in clear: the station
 * blanks it before serving anything (`configPayload`) and `Config.Export` deletes it
 * outright, whatever `hardware` says. The row therefore compared « » to « — », which is a
 * difference between two ways of not saying a password — and « Recopier » treated it like
 * any other, wrote `undefined` into the draft, and `JSON.stringify` dropped the key on the
 * way out. The cooperative's WebDAV account disappeared from the file through Importer →
 * Recopier → Enregistrer, in silence. A write-only field has nothing to do in a
 * field-by-field diff; the service carries the secret over on its own side.
 */
const NOT_COMPARED = new Set(['modified_at', 'catalog.options.password'])

/**
 * What a hardware-free export does NOT carry, and the French name of each.
 *
 * `Config.Export(false)` clears seven things — the station number and name, the whole
 * network block, the station-specific keys of the three option maps, and the image path of
 * the catalog — and the import puts back the number and the network block. Everything else
 * comes back EMPTY, so those rows of the diff hold nothing, and « Recopier » copies
 * emptiness as faithfully as it copies a value: the WebDAV account of the catalog is in
 * that list. This is what the screen has to name before anybody presses the button.
 *
 * WHAT THE STATION PUTS BACK HAS NO ROW HERE, and the network block is the one that
 * changed sides. It used to be listed, and rightly: the file came back with an empty
 * address, and the emptiness was on its way into the draft. Two things happened since.
 * The decoder reads an empty address as the neutral loopback — it has to, or a station
 * installed from an export could not be administered at all — and `importConfig` therefore
 * decides the block explicitly and keeps the receiving station's own, exactly as it keeps
 * its number. Nothing empty reaches the draft any more, so a row here would warn about a
 * loss that cannot happen, at the very moment the Station page finally offers a field for
 * that address. `station.number` has never been listed, for this same reason.
 *
 * The rows watch the exact KEYS the export clears, and no longer the whole option maps.
 * Watching a map stopped working the day the export kept its shared keys: the map comes
 * back carrying the separator and the label offset, so it is not blank, so the warning
 * fell silent — including for the WebDAV account, the emptiness this screen exists to
 * name. The volunteer would have pressed « Recopier » on a file whose share address was
 * gone, and nothing would have said so.
 */
const CLONE_STRIPS: { path: string; name: string }[] = [
  { path: 'station.name', name: 'le nom du poste' },
  { path: 'scale.options.port', name: 'le port de la balance' },
  // Les TROIS clés d'appareil de l'imprimante, parce que l'export sans matériel efface
  // les trois (§8.4, `Config.Export`). La ligne n'en nommait qu'une : sur un poste réglé
  // sur `tcp`, « Recopier » emportait l'adresse de l'imprimante sans un mot, ce que cet
  // encadré existe précisément pour dire.
  { path: 'printer.options.queue', name: 'la file d’impression' },
  { path: 'printer.options.path', name: 'le nœud d’impression' },
  { path: 'printer.options.address', name: 'l’adresse de l’imprimante' },
  { path: 'catalog.options.url', name: 'l’adresse du partage' },
  { path: 'catalog.options.username', name: 'le compte du partage' },
  { path: 'catalog.images.path', name: 'le chemin des images' },
]

/**
 * What the file would change on the station, field by field.
 *
 * @param station - the configuration in service, or null when it could not be read.
 * @param file - what the station read in the imported file, or null before any import.
 * @returns the fields that differ, minus the ones {@link NOT_COMPARED} names. It is EMPTY
 * when there is nothing to compare against, which is why nothing may read « no
 * difference » out of its length alone.
 */
export function comparisonOf(
  station: Record<string, unknown> | null,
  file: Record<string, unknown> | null,
): Difference[] {
  if (station === null || file === null) return []
  return differences(station, file).filter((entry) => !NOT_COMPARED.has(entry.path))
}

/**
 * The blocks of §11.5 the file carries EMPTY while the station has a value there.
 *
 * @param station - the configuration in service.
 * @param file - what the station read in the imported file.
 */
export function strippedBlocks(
  station: Record<string, unknown> | null,
  file: Record<string, unknown> | null,
): { path: string; name: string }[] {
  if (station === null || file === null) return []
  const inService = station
  const inFile = file
  return CLONE_STRIPS.filter(
    (block) => isBlank(inFile, block.path) && !isBlank(inService, block.path),
  )
}

/**
 * True when a document carries nothing at that path: absent, null, empty text, empty map.
 *
 * The four forms all come out of one export: `Config.Export(false)` writes `""` on the
 * station name and on the image path, `null` on an option map it emptied whole, and drops
 * the station-specific key of one it kept — which reads as absent.
 *
 * @param document - the document to read.
 * @param path - the dotted path of the key.
 */
function isBlank(document: Record<string, unknown>, path: string): boolean {
  const value = valueAt(document, path)
  if (value === undefined || value === null || value === '') return true
  if (typeof value !== 'object' || Array.isArray(value)) return false
  return Object.keys(value).length === 0
}

/** « le nom du poste, le port de la balance et le chemin des images ». */
export function frenchList(names: string[]): string {
  if (names.length < 2) return names.join('')
  return `${names.slice(0, -1).join(', ')} et ${names[names.length - 1] ?? ''}`
}
